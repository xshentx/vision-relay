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
// relay backend and its database remain single-instance across Fast User
// Switching and RDP sessions. If the owning session's HWND is inaccessible,
// duplicate launches fall back to the management page in the current session.
// Including the user SID keeps independent Windows accounts isolated.
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

			// Try from the newly launched process first because it is closest to
			// the user's launch gesture. The event remains necessary when the
			// primary is reopening a closed window.
			focused := focusClientWindow()
			if err := windows.SetEvent(event); err != nil {
				return fmt.Errorf("signal existing instance: %w", err)
			}
			return finishDesktopInstanceActivation(focused, focusClientWindow, time.Second, openDesktopManagementInCurrentSession)
		}
		lastErr = openErr
		if time.Now().After(deadline) {
			return fmt.Errorf("open existing activation event: %w", lastErr)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func finishDesktopInstanceActivation(alreadyFocused bool, focus func() bool, timeout time.Duration, fallback func() error) error {
	if alreadyFocused {
		return nil
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if focus() {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fallback()
}

func openDesktopManagementInCurrentSession() error {
	managementURL := localServerURL(defaultManagementAddr)
	token, err := loadOrCreateManagementToken("")
	if err != nil {
		return fmt.Errorf("load management token: %w", err)
	}
	launchURL := managementBootstrapURL(managementURL, token)
	return openHealthyDesktopManagement(
		managementURL,
		existingVisionRelayHealthy,
		func(string) error { return openBrowser(launchURL) },
	)
}

func openHealthyDesktopManagement(managementURL string, healthy func(string) bool, open func(string) error) error {
	if !healthy(managementURL) {
		return nil
	}
	if err := open(managementURL); err != nil {
		return fmt.Errorf("open management UI in current Windows session: %w", err)
	}
	return nil
}
