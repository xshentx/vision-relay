//go:build !windows

package server

import (
	"errors"
	"syscall"
)

func isAddressInUseError(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE)
}
