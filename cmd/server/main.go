// Package main 是 omniHub HTTP 后端服务的入口。
package main

import (
	"context"
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
	"omnihub/internal/scanner"
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
		_ = os.Remove(lockPath)
	}()

	// 3. 初始化媒体库管理器（支持 0 库安全启动）
	libMgr := library.NewManager()
	if cfg.PrimaryLibrary != "" {
		_, err := libMgr.AddLibrary(cfg.PrimaryLibrary, true)
		if err != nil {
			log.Printf("Warning: failed to mount primary library %s: %v", cfg.PrimaryLibrary, err)
		} else {
			go func(root string) {
				time.Sleep(3 * time.Second) // 延迟启动，优先保证前端初始化响应
				gCount, _ := scanner.ScanGames(filepath.Join(root, "standalone_games"))
				mCount, _ := scanner.ScanManga(filepath.Join(root, "media_library", "manga"))
				log.Printf("Read-only scan complete: %d games, %d mangas indexed", gCount, mCount)
			}(cfg.PrimaryLibrary)
		}
	}
	for _, libPath := range cfg.Libraries {
		if libPath != cfg.PrimaryLibrary {
			_, _ = libMgr.AddLibrary(libPath, false)
		}
	}

	// 4. 初始化用户认证数据库（主库存在时放主库，否则放用户配置目录）
	authDBPath := filepath.Join(configDir, "users.db")
	primaryLib := libMgr.GetPrimary()
	if primaryLib != nil && primaryLib.IsOnline {
		authDBPath = filepath.Join(primaryLib.RootPath, library.DataDirName, "users.db")
	}

	authDB, err := auth.OpenDB(authDBPath)
	if err != nil {
		log.Fatalf("Failed to open auth db at %s: %v", authDBPath, err)
	}
	defer authDB.Close()

	// 5. 组装 HTTP 路由
	router := server.NewRouter(libMgr, authDB)

	// 6. 端口占用探测与自适应
	port, err := network.FindAvailablePort(cfg.Server.Port, 10)
	if err != nil {
		log.Fatalf("Port binding check failed: %v", err)
	}
	if port != cfg.Server.Port {
		log.Printf("Port %d occupied, auto-fallback to %d", cfg.Server.Port, port)
	}

	// 7. 启动 mDNS 广播
	mdns, err := network.StartMDNS(port)
	if err == nil {
		defer mdns.Shutdown()
		log.Printf("mDNS service broadcasted on omnihub.local:%d", port)
	}

	httpServer := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.Server.Host, port),
		Handler: router,
	}

	// 8. 优雅退出控制
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("omniHub server listening on http://%s:%d", cfg.Server.Host, port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	<-stopChan
	log.Println("Shutting down omniHub server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("Server forced shutdown: %v", err)
	}
	log.Println("omniHub server exited gracefully.")
}
