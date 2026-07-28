//go:build windows

package server

import (
	"errors"

	"golang.org/x/sys/windows"
)

func isAddressInUseError(err error) bool {
	return errors.Is(err, windows.WSAEADDRINUSE)
}
