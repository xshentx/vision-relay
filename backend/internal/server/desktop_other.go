//go:build !windows && !darwin

package server

func focusClientWindow() bool {
	return false
}
