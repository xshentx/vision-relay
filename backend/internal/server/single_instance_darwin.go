//go:build darwin

package server

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

func acquireDesktopInstance(activation chan<- struct{}) (bool, func(), error) {
	lockPath, socketPath, err := darwinDesktopInstancePaths()
	if err != nil {
		return false, func() {}, err
	}
	return acquireDarwinDesktopInstance(lockPath, socketPath, activation)
}

func darwinDesktopInstancePaths() (string, string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", "", fmt.Errorf("locate macOS cache directory: %w", err)
	}
	dir := filepath.Join(cacheDir, appSlug)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", fmt.Errorf("create single-instance directory: %w", err)
	}
	return filepath.Join(dir, "instance.lock"), filepath.Join(dir, "activate.sock"), nil
}

func acquireDarwinDesktopInstance(lockPath, socketPath string, activation chan<- struct{}) (bool, func(), error) {
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false, func() {}, err
	}
	if err := unix.Flock(int(lockFile.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = lockFile.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return false, func() {}, signalDarwinDesktopInstance(socketPath)
		}
		return false, func() {}, err
	}

	// Holding the lock proves no live primary owns the old socket, so removing a
	// stale socket cannot disconnect another instance.
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		_ = unix.Flock(int(lockFile.Fd()), unix.LOCK_UN)
		_ = lockFile.Close()
		return false, func() {}, err
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(socketPath)
		_ = unix.Flock(int(lockFile.Fd()), unix.LOCK_UN)
		_ = lockFile.Close()
		return false, func() {}, err
	}

	done := make(chan struct{})
	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				select {
				case <-done:
					return
				default:
					// Avoid spinning if the listener reports a transient error.
					time.Sleep(25 * time.Millisecond)
					continue
				}
			}
			_ = connection.SetReadDeadline(time.Now().Add(time.Second))
			var signal [1]byte
			_, _ = connection.Read(signal[:])
			_ = connection.Close()
			select {
			case activation <- struct{}{}:
			default:
			}
		}
	}()

	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			close(done)
			_ = listener.Close()
			<-waitDone
			_ = os.Remove(socketPath)
			_ = unix.Flock(int(lockFile.Fd()), unix.LOCK_UN)
			_ = lockFile.Close()
		})
	}
	return true, release, nil
}

func signalDarwinDesktopInstance(socketPath string) error {
	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for {
		connection, err := net.DialTimeout("unix", socketPath, 250*time.Millisecond)
		if err == nil {
			_, writeErr := connection.Write([]byte{1})
			_ = connection.Close()
			if writeErr != nil {
				return fmt.Errorf("signal existing instance: %w", writeErr)
			}
			return nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return fmt.Errorf("connect to existing activation socket: %w", lastErr)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
