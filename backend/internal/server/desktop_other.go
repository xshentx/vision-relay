//go:build !windows

package server

func focusClientWindow() bool {
	return false
}
