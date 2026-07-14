//go:build linux

package main

import "syscall"

func redirectStderr(fd uintptr) {
	// Dup2 is not available on Linux ARM64; Dup3 with flags=0 is equivalent.
	_ = syscall.Dup3(int(fd), 2, 0)
}
