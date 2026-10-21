//go:build windows

package platform

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
const appRunValue = "LumiDesk"

type WindowsStartup struct{}

func (WindowsStartup) Enabled() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
			return false, nil
		}
		return false, err
	}
	defer k.Close()
	_, _, err = k.GetStringValue(appRunValue)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return false, nil
	}
	return err == nil, err
}

func (WindowsStartup) SetEnabled(enabled bool) error {
	if !enabled {
		k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
		if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("open Windows startup registry key: %w", err)
		}
		defer k.Close()
		if err := k.DeleteValue(appRunValue); err != nil && !errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
			return fmt.Errorf("remove LumiDesk from Windows startup: %w", err)
		}
		return nil
	}
	command, err := CurrentExecutableStartupCommand()
	if err != nil {
		return err
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open Windows startup registry key: %w", err)
	}
	defer k.Close()
	if err := k.SetStringValue(appRunValue, command); err != nil {
		return fmt.Errorf("register LumiDesk to start with Windows: %w", err)
	}
	return nil
}

func AcquireSingleInstance(name string) (release func(), alreadyRunning bool, err error) {
	mutexName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, false, err
	}
	handle, err := windows.CreateMutex(nil, false, mutexName)
	if err != nil {
		if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			_ = windows.CloseHandle(handle)
			return nil, true, nil
		}
		return nil, false, err
	}
	return func() { _ = windows.CloseHandle(handle) }, false, nil
}

type sessionCallbacks struct {
	query func() bool
	end   func(bool)
}

var (
	monitorMu sync.Mutex
	monitor   sessionCallbacks
	wndProc   = syscall.NewCallback(dispatchSessionMessage)
)

const (
	wmQueryEndSession = 0x0011
	wmEndSession      = 0x0016
	wmQuit            = 0x0012
	endSessionLogoff  = 0x80000000
)

type wndClass struct {
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
}

var (
	user32            = syscall.NewLazyDLL("user32.dll")
	registerClassW    = user32.NewProc("RegisterClassW")
	createWindowExW   = user32.NewProc("CreateWindowExW")
	defWindowProcW    = user32.NewProc("DefWindowProcW")
	getMessageW       = user32.NewProc("GetMessageW")
	translateMessage  = user32.NewProc("TranslateMessage")
	dispatchMessage   = user32.NewProc("DispatchMessageW")
	postThreadMessage = user32.NewProc("PostThreadMessageW")
	getModuleHandleW  = syscall.NewLazyDLL("kernel32.dll").NewProc("GetModuleHandleW")
)

// StartSessionMonitor installs an invisible top-level window so Windows can
// notify the process with WM_QUERYENDSESSION before terminating user apps.
func StartSessionMonitor(onQuery func() bool, onEnd func(ending bool)) (func(), error) {
	monitorMu.Lock()
	monitor = sessionCallbacks{query: onQuery, end: onEnd}
	monitorMu.Unlock()
	ready := make(chan struct {
		threadID uint32
		err      error
	}, 1)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		threadID := windows.GetCurrentThreadId()
		className, _ := syscall.UTF16PtrFromString("LumiDeskShutdownMonitor")
		instance, _, _ := getModuleHandleW.Call(0)
		wc := wndClass{WndProc: wndProc, Instance: instance, ClassName: className}
		atom, _, registerErr := registerClassW.Call(uintptr(unsafe.Pointer(&wc)))
		if atom == 0 && registerErr != syscall.Errno(1410) { // already registered is okay
			ready <- struct {
				threadID uint32
				err      error
			}{threadID, registerErr}
			return
		}
		hwnd, _, createErr := createWindowExW.Call(
			0x00000080, // WS_EX_TOOLWINDOW
			uintptr(unsafe.Pointer(className)), 0, 0,
			0, 0, 0, 0, 0, 0, instance, 0,
		)
		if hwnd == 0 {
			ready <- struct {
				threadID uint32
				err      error
			}{threadID, createErr}
			return
		}
		ready <- struct {
			threadID uint32
			err      error
		}{threadID, nil}
		var msg [48]byte // MSG is 48 bytes on 64-bit Windows.
		for {
			r, _, _ := getMessageW.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0)
			if int32(r) <= 0 {
				return
			}
			_, _, _ = translateMessage.Call(uintptr(unsafe.Pointer(&msg[0])))
			_, _, _ = dispatchMessage.Call(uintptr(unsafe.Pointer(&msg[0])))
		}
	}()
	info := <-ready
	if info.err != nil {
		return nil, fmt.Errorf("start Windows shutdown monitor: %w", info.err)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			_, _, _ = postThreadMessage.Call(uintptr(info.threadID), wmQuit, 0, 0)
			<-stopped
			monitorMu.Lock()
			monitor = sessionCallbacks{}
			monitorMu.Unlock()
		})
	}, nil
}

func dispatchSessionMessage(hwnd, message, wParam, lParam uintptr) uintptr {
	monitorMu.Lock()
	callbacks := monitor
	monitorMu.Unlock()
	switch message {
	case wmQueryEndSession:
		if lParam&endSessionLogoff != 0 {
			return 1
		}
		if callbacks.query != nil && !callbacks.query() {
			return 0
		}
		return 1
	case wmEndSession:
		if callbacks.end != nil {
			callbacks.end(wParam != 0)
		}
		return 0
	}
	r, _, _ := defWindowProcW.Call(hwnd, message, wParam, lParam)
	return r
}
