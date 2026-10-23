//go:build !linux

package network

import "syscall"

func childProcAttr() *syscall.SysProcAttr { return nil }
