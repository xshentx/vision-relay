//go:build windows

package server

import (
	"syscall"
	"unsafe"
)

const (
	wmSetIcon     = 0x0080
	iconSmall     = 0
	iconBig       = 1
	iconSmall2    = 2
	imageIcon     = 1
	gclpHIcon     = -14
	gclpHIconSm   = -34
	smCXIcon      = 11
	smCYIcon      = 12
	smCXSmallIcon = 49
	smCYSmallIcon = 50
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procSendMessageW     = user32.NewProc("SendMessageW")
	procLoadImageW       = user32.NewProc("LoadImageW")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procSetClassLongPtrW = user32.NewProc("SetClassLongPtrW")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
)

func setNativeWindowIcon(window unsafe.Pointer) {
	hwnd := uintptr(window)
	if hwnd == 0 {
		return
	}
	hinst, _, _ := procGetModuleHandleW.Call(0)
	if hinst == 0 {
		return
	}
	large := loadIconResource(hinst, systemMetric(smCXIcon), systemMetric(smCYIcon))
	small := loadIconResource(hinst, systemMetric(smCXSmallIcon), systemMetric(smCYSmallIcon))
	if large != 0 {
		procSendMessageW.Call(hwnd, wmSetIcon, iconBig, large)
		procSetClassLongPtrW.Call(hwnd, classLongIndex(gclpHIcon), large)
	}
	if small != 0 {
		procSendMessageW.Call(hwnd, wmSetIcon, iconSmall, small)
		procSendMessageW.Call(hwnd, wmSetIcon, iconSmall2, small)
		procSetClassLongPtrW.Call(hwnd, classLongIndex(gclpHIconSm), small)
	}
}

func loadIconResource(hinst uintptr, width, height int) uintptr {
	icon, _, _ := procLoadImageW.Call(
		hinst,
		uintptr(unsafe.Pointer(uintptr(1))),
		imageIcon,
		uintptr(width),
		uintptr(height),
		0,
	)
	return icon
}

func systemMetric(index int) int {
	value, _, _ := procGetSystemMetrics.Call(uintptr(index))
	if value == 0 {
		return 32
	}
	return int(value)
}

func classLongIndex(index int) uintptr {
	return uintptr(int32(index))
}
