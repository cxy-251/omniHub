// Package main 是 omniHub HTTP 后端服务的入口。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"omnihub/internal/application"
	"omnihub/internal/auth"
	"omnihub/internal/buildinfo"
	"omnihub/internal/config"
	"omnihub/internal/network"
	"omnihub/internal/server"
)

func main() {
	if handled, err := runMangaCommand(os.Args[1:]); handled {
		if err != nil {
			log.Print(err)
			os.Exit(1)
		}
		return
	}
	options, err := parseLaunchOptions(os.Args[1:])
	if err != nil {
		log.Print(err)
		os.Exit(2)
	}
	if options.version {
		fmt.Println(buildinfo.Description())
		return
	}
	if err := run(options); err != nil {
		log.Printf("omniHub server stopped: %v", err)
		os.Exit(1)
	}
}

func run(options launchOptions) (result error) {
	log.Println("Starting omniHub server...")

	// 1. 先确定配置目录并获取单实例锁，再允许加载流程创建默认配置。
	configDir, err := config.GetConfigDir()
	if err != nil {
		return fmt.Errorf("determine config directory: %w", err)
	}

	// 2. 单实例文件锁排他控制（第 9.4 节）
	lockPath := filepath.Join(configDir, "omnihub.lock")
	lock, err := server.AcquireLock(lockPath)
	if err != nil {
		return fmt.Errorf("single instance check: %w", err)
	}
	defer func() {
		if err := lock.Release(); err != nil {
			result = errors.Join(result, fmt.Errorf("release instance lock: %w", err))
		}
	}()
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	// 3. 组装可恢复的应用生命周期；主库离线不关闭端口或创建替代账号库。
	localToken, err := auth.GenerateSecureToken()
	if err != nil {
		return fmt.Errorf("generate local credential: %w", err)
	}
	app, err := application.New(cfg, configDir, localToken)
	if err != nil {
		return fmt.Errorf("initialize application: %w", err)
	}
	defer func() {
		if err := app.Close(); err != nil {
			result = errors.Join(result, fmt.Errorf("close application: %w", err))
		}
	}()
	watchContext, stopWatcher := context.WithCancel(context.Background())
	defer stopWatcher()
	go app.WatchLibraries(watchContext)

	// 6. 端口占用探测与自适应
	listener, port, err := network.ListenAvailable(cfg.Server.Host, cfg.Server.Port, 10)
	if err != nil {
		return fmt.Errorf("bind HTTP port: %w", err)
	}
	if port != cfg.Server.Port {
		log.Printf("Port %d occupied, auto-fallback to %d", cfg.Server.Port, port)
	}
	defer func() {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			result = errors.Join(result, fmt.Errorf("close HTTP listener: %w", err))
		}
	}()

	// 本机客户端通过受限配置文件发现服务与免密凭证，凭证不通过 HTTP 下发。
	localInfo, err := json.Marshal(map[string]interface{}{"url": fmt.Sprintf("http://127.0.0.1:%d", port), "token": localToken})
	if err != nil {
		return fmt.Errorf("encode local connection: %w", err)
	}
	if err := config.WritePrivateFile(filepath.Join(configDir, "local-client.json"), localInfo); err != nil {
		return fmt.Errorf("write local connection: %w", err)
	}

	// 7. mDNS 广播：只在允许局域网访问时广播；开关变化时随之启停。
	var mdnsMu sync.Mutex
	var mdns *network.MDNSService
	setMDNS := func(enabled bool) {
		mdnsMu.Lock()
		defer mdnsMu.Unlock()
		if !enabled && mdns != nil {
			mdns.Shutdown()
			mdns = nil
			log.Printf("mDNS stopped")
		} else if enabled && mdns == nil {
			if service, err := network.StartMDNS(port); err == nil {
				mdns = service
				log.Printf("mDNS service broadcasted on omnihub.local:%d", port)
			}
		}
	}
	setMDNS(app.LANEnabled())
	app.OnLANChanged = setMDNS
	defer setMDNS(false)

	// 广域网：按保存的开关托管 cloudflared 隧道（用户自己的命名隧道配置），退出时结束它。
	tunnelConfig := cfg.Server.CloudflaredConfig
	if tunnelConfig == "" {
		tunnelConfig = network.DefaultTunnelConfig()
	}
	app.Tunnel = network.NewTunnel(network.FindCloudflared(cfg.Server.Cloudflared), tunnelConfig)
	app.Tunnel.SetEnabled(app.WANEnabled())
	defer app.Tunnel.SetEnabled(false)

	httpServer := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Server.Host, port),
		Handler:           app,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 * 1024,
	}

	// 8. 优雅退出控制
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stopChan)

	serveError := make(chan error, 1)
	go func() {
		log.Printf("omniHub server listening on http://%s:%d", cfg.Server.Host, port)
		serveError <- httpServer.Serve(listener)
	}()
	// Connection discovery is written and HTTP is serving before opening Qt.
	// Closing/yielding the GUI must not terminate LAN access or an owned game.
	if options.desktop {
		if err := startDesktop(options.clientPath); err != nil {
			return fmt.Errorf("start native client: %w", err)
		}
	}

	select {
	case <-stopChan:
	case err := <-serveError:
		if !errors.Is(err, http.ErrServerClosed) {
			result = fmt.Errorf("serve HTTP: %w", err)
		}
	}
	stopWatcher()
	log.Println("Shutting down omniHub server...")
	if err := app.Close(); err != nil {
		result = errors.Join(result, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("Server forced shutdown: %v", err)
		if err := httpServer.Close(); err != nil {
			result = errors.Join(result, fmt.Errorf("close HTTP server: %w", err))
		}
	}
	log.Println("omniHub server exited gracefully.")
	return result
}
