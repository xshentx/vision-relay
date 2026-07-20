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
	procSetForegroundWindow = user32DLL.NewProc("SetForegroundWindow")
)

const swRestore = 9

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
	focused, _, _ := procSetForegroundWindow.Call(hwnd)
	return focused != 0
}
