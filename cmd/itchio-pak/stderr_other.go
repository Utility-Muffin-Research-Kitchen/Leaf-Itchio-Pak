//go:build !linux && !darwin

package main

// redirectStderr is best-effort on unsupported development hosts. Device
// builds use the Linux implementation above.
func redirectStderr(_ uintptr) {}
