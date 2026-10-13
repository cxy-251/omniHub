// Package main 是 omniHub HTTP 后端服务的入口。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"omnihub/internal/auth"
	"omnihub/internal/config"
	"omnihub/internal/library"
	"omnihub/internal/network"
	"omnihub/internal/server"
)

func main() {
	log.Println("Starting omniHub server...")

	// 1. 获取并加载全局配置
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	configDir, err := config.GetConfigDir()
	if err != nil {
		log.Fatalf("Failed to determine config dir: %v", err)
	}

	// 2. 单实例文件锁排他控制（第 9.4 节）
	lockPath := filepath.Join(configDir, "omnihub.lock")
	lock, err := server.AcquireLock(lockPath)
	if err != nil {
		log.Fatalf("Single instance check failed: %v", err)
	}
	defer func() {
		_ = lock.Release()
	}()

	// 3. 初始化媒体库管理器（支持 0 库安全启动）
	libMgr := library.NewManager()
	identityChanged := false
	if cfg.PrimaryLibrary != "" {
		lib, err := libMgr.MountKnownLibrary(cfg.PrimaryLibrary, true, cfg.IsLibraryReadOnly(cfg.PrimaryLibrary), cfg.KnownLibrary(cfg.PrimaryLibrary))
		if err != nil {
			log.Printf("Warning: failed to mount primary library %s: %v", cfg.PrimaryLibrary, err)
		} else {
			identityChanged = cfg.RememberLibrary(lib) || identityChanged
		}
	}
	for _, libPath := range cfg.Libraries {
		if libPath != cfg.PrimaryLibrary {
			lib, err := libMgr.MountKnownLibrary(libPath, false, cfg.IsLibraryReadOnly(libPath), cfg.KnownLibrary(libPath))
			if err != nil {
				log.Printf("Warning: failed to mount library %s: %v", libPath, err)
			} else {
				identityChanged = cfg.RememberLibrary(lib) || identityChanged
			}
		}
	}
	if identityChanged {
		if err := config.Save(cfg); err != nil {
			log.Fatalf("Persist library identities: %v", err)
		}
	}

	// 4. 固定全局状态位置；离线或新增主库不能隐式换成另一套账号。
	stateDir, stateChanged, err := cfg.ResolveStateDirectory(configDir, libMgr.GetPrimary())
	if err != nil {
		log.Fatalf("Resolve global state: %v", err)
	}
	if stateChanged {
		if err := config.Save(cfg); err != nil {
			log.Fatalf("Persist global state location: %v", err)
		}
	}
	authDBPath := filepath.Join(stateDir, "users.db")

	authDB, err := auth.OpenDB(authDBPath)
	if err != nil {
		log.Fatalf("Failed to open auth db at %s: %v", authDBPath, err)
	}
	defer authDB.Close()
	if !cfg.StateInitialized {
		cfg.StateInitialized = true
		if err := config.Save(cfg); err != nil {
			log.Fatalf("Persist global state initialization: %v", err)
		}
	}

	// 5. 组装 HTTP 路由
	localToken, err := auth.GenerateSecureToken()
	if err != nil {
		log.Fatalf("Generate local credential: %v", err)
	}
	router, err := server.NewRouter(libMgr, authDB, filepath.Dir(authDBPath), localToken)
	if err != nil {
		log.Fatalf("Failed to initialize router: %v", err)
	}
	defer func() {
		if err := router.Close(); err != nil {
			log.Printf("Close router: %v", err)
		}
	}()

	// 6. 端口占用探测与自适应
	listener, port, err := network.ListenAvailable(cfg.Server.Host, cfg.Server.Port, 10)
	if err != nil {
		log.Fatalf("Port binding check failed: %v", err)
	}
	if port != cfg.Server.Port {
		log.Printf("Port %d occupied, auto-fallback to %d", cfg.Server.Port, port)
	}
	defer listener.Close()

	// 本机客户端通过受限配置文件发现服务与免密凭证，凭证不通过 HTTP 下发。
	localInfo, err := json.Marshal(map[string]interface{}{"url": fmt.Sprintf("http://127.0.0.1:%d", port), "token": localToken})
	if err != nil {
		log.Fatalf("Encode local connection: %v", err)
	}
	if err := config.WritePrivateFile(filepath.Join(configDir, "local-client.json"), localInfo); err != nil {
		log.Fatalf("Write local connection: %v", err)
	}

	// 7. 启动 mDNS 广播
	mdns, err := network.StartMDNS(port)
	if err == nil {
		defer mdns.Shutdown()
		log.Printf("mDNS service broadcasted on omnihub.local:%d", port)
	}

	httpServer := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Server.Host, port),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 * 1024,
	}

	// 8. 优雅退出控制
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("omniHub server listening on http://%s:%d", cfg.Server.Host, port)
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	<-stopChan
	log.Println("Shutting down omniHub server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("Server forced shutdown: %v", err)
		if err := httpServer.Close(); err != nil {
			log.Printf("Close HTTP server: %v", err)
		}
	}
	log.Println("omniHub server exited gracefully.")
}
