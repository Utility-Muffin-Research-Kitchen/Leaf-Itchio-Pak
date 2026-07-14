//go:build darwin

package main

import "syscall"

func redirectStderr(fd uintptr) {
	_ = syscall.Dup2(int(fd), 2)
}
