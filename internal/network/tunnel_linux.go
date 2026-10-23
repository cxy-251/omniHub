package network

import "syscall"

// childProcAttr：omniHub 异常退出时 cloudflared 随之结束，不留孤儿隧道。
func childProcAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM} }
