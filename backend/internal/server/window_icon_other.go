//go:build !windows

package server

import "unsafe"

func setNativeWindowIcon(_ unsafe.Pointer) {}
