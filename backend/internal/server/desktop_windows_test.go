//go:build windows

package server

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestActivateClientWindowForcesZOrderBeforeForeground(t *testing.T) {
	const hwnd = uintptr(1234)
	var calls []string
	api := clientWindowActivationAPI{
		showWindowAsync: func(got uintptr, command int) {
			calls = append(calls, fmt.Sprintf("show(%d,%d)", got, command))
		},
		allowForegroundWindow: func(got uintptr) {
			calls = append(calls, fmt.Sprintf("allow(%d)", got))
		},
		setWindowPos: func(got, insertAfter uintptr, flags uint32) {
			calls = append(calls, fmt.Sprintf("position(%d,%d,%d)", got, insertAfter, flags))
		},
		bringWindowToTop: func(got uintptr) {
			calls = append(calls, fmt.Sprintf("bring(%d)", got))
		},
		setForegroundWindow: func(got uintptr) {
			calls = append(calls, fmt.Sprintf("foreground(%d)", got))
		},
		switchToWindow: func(uintptr) {
			t.Fatal("shell fallback should not run when activation succeeds")
		},
		getForegroundWindow: func() uintptr {
			calls = append(calls, "get-foreground")
			return hwnd
		},
	}

	if !activateClientWindow(hwnd, api) {
		t.Fatal("activation unexpectedly failed")
	}
	flags := uint32(swpNoSize | swpNoMove | swpShowWindow)
	want := []string{
		fmt.Sprintf("show(%d,%d)", hwnd, swRestore),
		fmt.Sprintf("allow(%d)", hwnd),
		fmt.Sprintf("position(%d,%d,%d)", hwnd, hwndTopmost, flags),
		fmt.Sprintf("position(%d,%d,%d)", hwnd, hwndNotTopmost, flags),
		fmt.Sprintf("bring(%d)", hwnd),
		fmt.Sprintf("foreground(%d)", hwnd),
		"get-foreground",
		fmt.Sprintf("position(%d,%d,%d)", hwnd, hwndNotTopmost, flags),
		"get-foreground",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("activation calls = %#v; want %#v", calls, want)
	}
}

func TestActivateClientWindowUsesShellFallbackAndVerifiesForeground(t *testing.T) {
	const hwnd = uintptr(1234)
	var calls []string
	foregroundChecks := 0
	api := clientWindowActivationAPI{
		showWindowAsync:       func(uintptr, int) {},
		allowForegroundWindow: func(uintptr) {},
		setWindowPos:          func(uintptr, uintptr, uint32) {},
		bringWindowToTop:      func(uintptr) {},
		setForegroundWindow: func(uintptr) {
			calls = append(calls, "foreground")
		},
		switchToWindow: func(uintptr) {
			calls = append(calls, "switch")
		},
		getForegroundWindow: func() uintptr {
			foregroundChecks++
			calls = append(calls, "get-foreground")
			if foregroundChecks == 1 {
				return 999
			}
			return hwnd
		},
	}

	if !activateClientWindow(hwnd, api) {
		t.Fatal("activation unexpectedly failed after shell fallback")
	}
	want := []string{"foreground", "get-foreground", "switch", "foreground", "get-foreground"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("foreground fallback calls = %#v; want %#v", calls, want)
	}
}

func TestActivateClientWindowReportsFailureWhenForegroundDoesNotChange(t *testing.T) {
	api := clientWindowActivationAPI{
		showWindowAsync:       func(uintptr, int) {},
		allowForegroundWindow: func(uintptr) {},
		setWindowPos:          func(uintptr, uintptr, uint32) {},
		bringWindowToTop:      func(uintptr) {},
		setForegroundWindow:   func(uintptr) {},
		switchToWindow:        func(uintptr) {},
		getForegroundWindow:   func() uintptr { return 999 },
	}
	if activateClientWindow(1234, api) {
		t.Fatal("activation reported success while another HWND remained foreground")
	}
}

func TestFinishDesktopInstanceActivationFallsBackAfterFocusTimeout(t *testing.T) {
	fallbackErr := errors.New("fallback failed")
	fallbackCalls := 0
	err := finishDesktopInstanceActivation(false, func() bool {
		t.Fatal("zero timeout should not perform another focus attempt")
		return false
	}, 0, func() error {
		fallbackCalls++
		return fallbackErr
	})
	if !errors.Is(err, fallbackErr) || fallbackCalls != 1 {
		t.Fatalf("fallback result = calls %d, err %v", fallbackCalls, err)
	}
}

func TestFinishDesktopInstanceActivationStopsAfterVerifiedFocus(t *testing.T) {
	focusCalls := 0
	err := finishDesktopInstanceActivation(false, func() bool {
		focusCalls++
		return true
	}, time.Second, func() error {
		t.Fatal("fallback ran after focus succeeded")
		return nil
	})
	if err != nil || focusCalls != 1 {
		t.Fatalf("focus result = calls %d, err %v", focusCalls, err)
	}
}

func TestOpenHealthyDesktopManagementOpensInCurrentSession(t *testing.T) {
	const managementURL = "http://127.0.0.1:18473/"
	opened := ""
	err := openHealthyDesktopManagement(managementURL, func(got string) bool {
		return got == managementURL
	}, func(got string) error {
		opened = got
		return nil
	})
	if err != nil || opened != managementURL {
		t.Fatalf("management fallback = opened %q, err %v", opened, err)
	}
}

func TestOpenHealthyDesktopManagementSkipsUnhealthyServer(t *testing.T) {
	err := openHealthyDesktopManagement("http://127.0.0.1:18473/", func(string) bool {
		return false
	}, func(string) error {
		t.Fatal("unhealthy management server was opened")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRememberClientWindowClearsOnlyItsOwnHandle(t *testing.T) {
	clientWindowHandle.Store(0)
	t.Cleanup(func() { clientWindowHandle.Store(0) })
	first := rememberClientWindow(101)
	second := rememberClientWindow(202)

	first()
	if got := clientWindowHandle.Load(); got != 202 {
		t.Fatalf("older cleanup cleared newer HWND: got %d, want 202", got)
	}
	second()
	if got := clientWindowHandle.Load(); got != 0 {
		t.Fatalf("current HWND was not cleared: got %d", got)
	}
}
