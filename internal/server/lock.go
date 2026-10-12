// Package server 组装 HTTP API 路由、嵌入式静态资源分发与单实例进程锁。
// 遵循 ARCHITECTURE.md 第 9 节与第 0.6 节规范。
package server

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// SingleInstanceLock 封装基于系统 flock 的单实例排他文件锁（第 9.4 节）。
type SingleInstanceLock struct {
	file *os.File
}

// AcquireLock 尝试获取排他文件锁。若已被其他进程持有则返回错误。
func AcquireLock(lockPath string) (*SingleInstanceLock, error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create lock directory: %w", err)
	}

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open lock file %s: %w", lockPath, err)
	}

	// 非阻塞排他锁 LOCK_EX | LOCK_NB
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, errors.New("another instance of omniHub is already running")
		}
		return nil, fmt.Errorf("flock failed: %w", err)
	}

	return &SingleInstanceLock{file: f}, nil
}

// Release 释放排他文件锁。
func (l *SingleInstanceLock) Release() error {
	if l.file != nil {
		_ = syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
		err := l.file.Close()
		l.file = nil
		return err
	}
	return nil
}
