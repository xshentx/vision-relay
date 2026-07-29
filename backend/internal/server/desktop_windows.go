//go:build windows

package server

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32DLL               = windows.NewLazySystemDLL("user32.dll")
	procFindWindowW         = user32DLL.NewProc("FindWindowW")
	procShowWindow          = user32DLL.NewProc("ShowWindow")
	procBringWindowToTop    = user32DLL.NewProc("BringWindowToTop")
	procSetWindowPos        = user32DLL.NewProc("SetWindowPos")
	procSetForegroundWindow = user32DLL.NewProc("SetForegroundWindow")
)

const (
	swRestore     = 9
	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpShowWindow = 0x0040
)

func focusClientWindow() bool {
	title, err := windows.UTF16PtrFromString(appDisplayName)
	if err != nil {
		return false
	}
	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(title)))
	if hwnd == 0 {
		return false
	}
	procShowWindow.Call(hwnd, swRestore)
	brought, _, _ := procBringWindowToTop.Call(hwnd)
	positioned, _, _ := procSetWindowPos.Call(hwnd, 0, 0, 0, 0, swpNoSize|swpNoMove|swpShowWindow)
	focused, _, _ := procSetForegroundWindow.Call(hwnd)
	return focused != 0 || brought != 0 || positioned != 0
}
