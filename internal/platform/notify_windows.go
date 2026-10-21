//go:build windows

package platform

import (
	"syscall"
	"unsafe"
)

func NotifyAlreadyRunning() {
	title, _ := syscall.UTF16PtrFromString("LumiDesk is already running")
	message, _ := syscall.UTF16PtrFromString("LumiDesk is already open in the system tray.")
	messageBoxW := syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")
	_, _, _ = messageBoxW.Call(0, uintptr(unsafe.Pointer(message)), uintptr(unsafe.Pointer(title)), 0x40)
}
