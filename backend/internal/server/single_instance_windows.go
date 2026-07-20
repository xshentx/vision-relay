//go:build windows

package server

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

func acquireDesktopInstance(activation chan<- struct{}) (bool, func(), error) {
	mutexName, activationName, err := desktopInstanceObjectNames()
	if err != nil {
		return false, func() {}, err
	}
	return acquireDesktopInstanceWithNames(mutexName, activationName, activation)
}

// desktopInstanceObjectNames uses the global kernel-object namespace so the
// same Windows user cannot start separate primaries through Fast User
// Switching or RDP sessions. Including the user SID keeps independent Windows
// accounts isolated from one another.
func desktopInstanceObjectNames() (string, string, error) {
	tokenUser, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", "", fmt.Errorf("get current Windows user: %w", err)
	}
	if tokenUser == nil || tokenUser.User.Sid == nil {
		return "", "", errors.New("current Windows user has no SID")
	}
	sid := tokenUser.User.Sid.String()
	if sid == "" {
		return "", "", errors.New("format current Windows user SID")
	}
	prefix := `Global\VisionRelay.` + sid
	return prefix + `.SingleInstance.v2`, prefix + `.Activate.v2`, nil
}

func acquireDesktopInstanceWithNames(mutexName, activationName string, activation chan<- struct{}) (bool, func(), error) {
	mutexNamePtr, err := windows.UTF16PtrFromString(mutexName)
	if err != nil {
		return false, func() {}, err
	}
	mutex, mutexErr := windows.CreateMutex(nil, false, mutexNamePtr)
	if errors.Is(mutexErr, windows.ERROR_ALREADY_EXISTS) {
		if mutex != 0 {
			_ = windows.CloseHandle(mutex)
		}
		return false, func() {}, signalDesktopInstance(activationName)
	}
	if mutexErr != nil {
		if mutex != 0 {
			_ = windows.CloseHandle(mutex)
		}
		return false, func() {}, mutexErr
	}

	activationNamePtr, err := windows.UTF16PtrFromString(activationName)
	if err != nil {
		_ = windows.CloseHandle(mutex)
		return false, func() {}, err
	}
	event, err := windows.CreateEvent(nil, 0, 0, activationNamePtr)
	if err != nil {
		_ = windows.CloseHandle(mutex)
		return false, func() {}, err
	}

	done := make(chan struct{})
	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		for {
			result, waitErr := windows.WaitForSingleObject(event, windows.INFINITE)
			if waitErr != nil || result != windows.WAIT_OBJECT_0 {
				return
			}
			select {
			case <-done:
				return
			default:
			}
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
			_ = windows.SetEvent(event)
			<-waitDone
			_ = windows.CloseHandle(event)
			_ = windows.CloseHandle(mutex)
		})
	}
	return true, release, nil
}

func signalDesktopInstance(activationName string) error {
	activationNamePtr, err := windows.UTF16PtrFromString(activationName)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for {
		event, openErr := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, activationNamePtr)
		if openErr == nil {
			defer windows.CloseHandle(event)
			if err := windows.SetEvent(event); err != nil {
				return fmt.Errorf("signal existing instance: %w", err)
			}
			return nil
		}
		lastErr = openErr
		if time.Now().After(deadline) {
			return fmt.Errorf("open existing activation event: %w", lastErr)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
