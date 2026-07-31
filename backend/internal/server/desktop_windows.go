//go:build windows

package server

import (
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32DLL                    = windows.NewLazySystemDLL("user32.dll")
	procFindWindowW              = user32DLL.NewProc("FindWindowW")
	procGetForegroundWindow      = user32DLL.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessID = user32DLL.NewProc("GetWindowThreadProcessId")
	procAllowSetForegroundWindow = user32DLL.NewProc("AllowSetForegroundWindow")
	procShowWindowAsync          = user32DLL.NewProc("ShowWindowAsync")
	procBringWindowToTop         = user32DLL.NewProc("BringWindowToTop")
	procSetWindowPos             = user32DLL.NewProc("SetWindowPos")
	procSetForegroundWindow      = user32DLL.NewProc("SetForegroundWindow")
	procSwitchToThisWindow       = user32DLL.NewProc("SwitchToThisWindow")
)

const (
	swRestore     = 9
	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpShowWindow = 0x0040
)

var (
	// HWND_TOPMOST and HWND_NOTOPMOST are the pointer-sized representations
	// of -1 and -2. Keeping them as variables avoids uintptr constant overflow
	// on 32-bit Windows builds.
	hwndTopmost    = ^uintptr(0)
	hwndNotTopmost = ^uintptr(1)

	clientWindowHandle atomic.Uintptr
)

type clientWindowActivationAPI struct {
	showWindowAsync       func(hwnd uintptr, command int)
	allowForegroundWindow func(hwnd uintptr)
	bringWindowToTop      func(hwnd uintptr)
	setWindowPos          func(hwnd, insertAfter uintptr, flags uint32)
	setForegroundWindow   func(hwnd uintptr)
	switchToWindow        func(hwnd uintptr)
	getForegroundWindow   func() uintptr
}

var nativeClientWindowActivationAPI = clientWindowActivationAPI{
	showWindowAsync: func(hwnd uintptr, command int) {
		procShowWindowAsync.Call(hwnd, uintptr(command))
	},
	allowForegroundWindow: func(hwnd uintptr) {
		var processID uint32
		procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&processID)))
		if processID != 0 {
			procAllowSetForegroundWindow.Call(uintptr(processID))
		}
	},
	bringWindowToTop: func(hwnd uintptr) {
		procBringWindowToTop.Call(hwnd)
	},
	setWindowPos: func(hwnd, insertAfter uintptr, flags uint32) {
		procSetWindowPos.Call(hwnd, insertAfter, 0, 0, 0, 0, uintptr(flags))
	},
	setForegroundWindow: func(hwnd uintptr) {
		procSetForegroundWindow.Call(hwnd)
	},
	switchToWindow: func(hwnd uintptr) {
		// SwitchToThisWindow is the same non-keyboard transition used by the
		// shell's task switching path. It is only used after the supported
		// foreground APIs leave another window active.
		procSwitchToThisWindow.Call(hwnd, 1)
	},
	getForegroundWindow: func() uintptr {
		hwnd, _, _ := procGetForegroundWindow.Call()
		return hwnd
	},
}

// rememberClientWindow lets activation requests handled by the primary process
// use the exact WebView HWND rather than relying on its current title.
func rememberClientWindow(hwnd uintptr) func() {
	clientWindowHandle.Store(hwnd)
	return func() {
		clientWindowHandle.CompareAndSwap(hwnd, 0)
	}
}

func findClientWindow() uintptr {
	if hwnd := clientWindowHandle.Load(); hwnd != 0 {
		return hwnd
	}
	title, err := windows.UTF16PtrFromString(appDisplayName)
	if err != nil {
		return 0
	}
	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(title)))
	return hwnd
}

func focusClientWindow() bool {
	hwnd := findClientWindow()
	if hwnd == 0 {
		return false
	}
	return activateClientWindow(hwnd, nativeClientWindowActivationAPI)
}

func activateClientWindow(hwnd uintptr, api clientWindowActivationAPI) bool {
	if hwnd == 0 {
		return false
	}

	api.showWindowAsync(hwnd, swRestore)
	api.allowForegroundWindow(hwnd)

	// SetForegroundWindow alone is advisory and is commonly rejected when the
	// primary process has been in the tray while another application owns the
	// foreground lock. Briefly moving the window through the topmost band makes
	// the requested Z-order change effective without leaving the app always-on-top.
	flags := uint32(swpNoSize | swpNoMove | swpShowWindow)
	api.setWindowPos(hwnd, hwndTopmost, flags)
	api.setWindowPos(hwnd, hwndNotTopmost, flags)
	api.bringWindowToTop(hwnd)
	api.setForegroundWindow(hwnd)

	if api.getForegroundWindow() != hwnd {
		api.switchToWindow(hwnd)
		api.setForegroundWindow(hwnd)
	}

	// Make a final best-effort cleanup in case the first HWND_NOTOPMOST
	// transition raced with window restoration.
	api.setWindowPos(hwnd, hwndNotTopmost, flags)
	return api.getForegroundWindow() == hwnd
}
