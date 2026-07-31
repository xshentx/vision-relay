//go:build !windows && !darwin

package server

func rememberClientWindow(_ uintptr) func() {
	return func() {}
}

func focusClientWindow() bool {
	return false
}
