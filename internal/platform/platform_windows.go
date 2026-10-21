//go:build windows

package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
const appRunValue = "LumiDesk"
const beforeLoginTaskName = "LumiDesk_PreLoginLights"
const beforeLoginAgentMutex = `Global\LumiDesk.PreLoginAgent`
const desktopRunningMutex = `Global\LumiDesk.DesktopRunning`
const beforeLoginMarkerKey = `Software\LumiDesk`
const beforeLoginMarkerValue = "PreLoginTaskRegistered"
const beforeLoginEnabledMarkerValue = "PreLoginLightsEnabled"
const beforeLoginTaskVersion = "system-v2"
const seeMaskNoCloseProcess = 0x00000040

type shellExecuteInfo struct {
	cbSize         uint32
	fMask          uint32
	hwnd           windows.Handle
	lpVerb         *uint16
	lpFile         *uint16
	lpParameters   *uint16
	lpDirectory    *uint16
	nShow          int32
	hInstApp       windows.Handle
	lpIDList       uintptr
	lpClass        *uint16
	hkeyClass      windows.Handle
	dwHotKey       uint32
	hIconOrMonitor windows.Handle
	hProcess       windows.Handle
}

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

func (WindowsStartup) BeforeLoginEnabled() (bool, error) {
	enabled, markerErr := beforeLoginLightsEnabled()
	registered, registrationErr := beforeLoginTaskRegistered()
	exists, queryErr := queryBeforeLoginTask()
	if queryErr != nil {
		if markerErr != nil {
			return false, markerErr
		}
		if enabled && registrationErr == nil && registered {
			return true, nil
		}
		if !enabled && registrationErr == nil && registered {
			return false, nil
		}
		return false, queryErr
	}
	return exists && enabled && registered, nil
}

func (WindowsStartup) SetBeforeLoginEnabled(enabled bool) error {
	if !enabled {
		// Keep the SYSTEM task installed but inert. This avoids asking for UAC
		// again just to turn the feature off; uninstall removes the task.
		return setBeforeLoginLightsEnabled(false)
	}
	registered, err := beforeLoginTaskRegistered()
	if err != nil {
		return fmt.Errorf("read pre-login task registration state: %w", err)
	}
	exists, queryErr := queryBeforeLoginTask()
	if registered && (exists || queryErr != nil) {
		return setBeforeLoginLightsEnabled(true)
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate LumiDesk executable: %w", err)
	}
	configRoot, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("locate the current user's settings folder: %w", err)
	}
	configDir := filepath.Join(configRoot, "LumiDesk")
	command := QuoteBeforeLoginCommand(executable, configDir)
	if err := runElevatedTaskScheduler("/Create", "/SC", "ONSTART", "/TN", beforeLoginTaskName, "/TR", command, "/RU", "SYSTEM", "/F"); err != nil {
		return fmt.Errorf("register the SYSTEM pre-login task (Windows administrator approval is required): %w", err)
	}
	if err := setBeforeLoginTaskMarker(true); err != nil {
		_ = runElevatedTaskScheduler("/Delete", "/TN", beforeLoginTaskName, "/F")
		return fmt.Errorf("save pre-login task registration state: %w", err)
	}
	return setBeforeLoginLightsEnabled(true)
}

func queryBeforeLoginTask() (bool, error) {
	output, err := exec.Command("schtasks.exe", "/Query", "/TN", beforeLoginTaskName).CombinedOutput()
	if err == nil {
		return true, nil
	}
	if taskNotFoundError(err, output) {
		return false, nil
	}
	return false, fmt.Errorf("query pre-login startup task: %w: %s", err, strings.TrimSpace(string(output)))
}

func beforeLoginTaskRegistered() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, beforeLoginMarkerKey, registry.QUERY_VALUE)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer k.Close()
	value, _, err := k.GetStringValue(beforeLoginMarkerValue)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return false, nil
	}
	return value == beforeLoginTaskVersion, err
}

func beforeLoginLightsEnabled() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, beforeLoginMarkerKey, registry.QUERY_VALUE)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer k.Close()
	value, _, err := k.GetStringValue(beforeLoginEnabledMarkerValue)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return false, nil
	}
	return value == "1", err
}

func setBeforeLoginLightsEnabled(enabled bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, beforeLoginMarkerKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	value := "0"
	if enabled {
		value = "1"
	}
	return k.SetStringValue(beforeLoginEnabledMarkerValue, value)
}

func setBeforeLoginTaskMarker(enabled bool) error {
	if !enabled {
		k, err := registry.OpenKey(registry.CURRENT_USER, beforeLoginMarkerKey, registry.SET_VALUE)
		if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
			return nil
		}
		if err != nil {
			return err
		}
		defer k.Close()
		if err := k.DeleteValue(beforeLoginMarkerValue); err != nil && !errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
			return err
		}
		return nil
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, beforeLoginMarkerKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(beforeLoginMarkerValue, beforeLoginTaskVersion)
}

func runElevatedTaskScheduler(args ...string) error {
	arguments := make([]string, len(args))
	for index, arg := range args {
		arguments[index] = windows.EscapeArg(arg)
	}
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		systemRoot = `C:\Windows`
	}
	file, err := syscall.UTF16PtrFromString(filepath.Join(systemRoot, "System32", "schtasks.exe"))
	if err != nil {
		return err
	}
	verb, _ := syscall.UTF16PtrFromString("runas")
	parameters, err := syscall.UTF16PtrFromString(strings.Join(arguments, " "))
	if err != nil {
		return err
	}
	info := shellExecuteInfo{
		cbSize:       uint32(unsafe.Sizeof(shellExecuteInfo{})),
		fMask:        seeMaskNoCloseProcess,
		lpVerb:       verb,
		lpFile:       file,
		lpParameters: parameters,
		nShow:        0,
	}
	shell32 := syscall.NewLazyDLL("shell32.dll")
	shellExecuteExW := shell32.NewProc("ShellExecuteExW")
	result, _, callErr := shellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if result == 0 {
		if callErr != syscall.Errno(0) {
			return callErr
		}
		return errors.New("could not launch the elevated Task Scheduler command")
	}
	if info.hProcess == 0 {
		return errors.New("elevated Task Scheduler process handle was not returned")
	}
	defer windows.CloseHandle(info.hProcess)
	waitResult, err := windows.WaitForSingleObject(info.hProcess, 120000)
	if err != nil {
		return fmt.Errorf("wait for elevated Task Scheduler command: %w", err)
	}
	if waitResult == 0x00000102 {
		return errors.New("elevated Task Scheduler command timed out")
	}
	if waitResult != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("unexpected Task Scheduler wait result 0x%08X", waitResult)
	}
	var exitCode uint32
	if err := windows.GetExitCodeProcess(info.hProcess, &exitCode); err != nil {
		return fmt.Errorf("read elevated Task Scheduler exit code: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("Task Scheduler exited with code %d", exitCode)
	}
	return nil
}

// AcquirePreLoginAgent serializes the boot helper with the desktop process so
// both processes never own the controller's single BLE connection at once.
func AcquirePreLoginAgent() (release func(), alreadyRunning bool, err error) {
	runtime.LockOSThread()
	security, err := preLoginMutexSecurityAttributes()
	if err != nil {
		runtime.UnlockOSThread()
		return nil, false, err
	}
	name, err := syscall.UTF16PtrFromString(beforeLoginAgentMutex)
	if err != nil {
		runtime.UnlockOSThread()
		return nil, false, err
	}
	handle, err := windows.CreateMutex(security, true, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		_ = windows.CloseHandle(handle)
		runtime.UnlockOSThread()
		return nil, true, nil
	}
	if err != nil {
		runtime.UnlockOSThread()
		return nil, false, err
	}
	return func() {
		_ = windows.ReleaseMutex(handle)
		_ = windows.CloseHandle(handle)
		runtime.UnlockOSThread()
	}, false, nil
}

// MarkDesktopRunning keeps a cross-session marker so a late boot task won't
// compete with LumiDesk after the user has signed in.
func MarkDesktopRunning() (func(), error) {
	security, err := preLoginMutexSecurityAttributes()
	if err != nil {
		return nil, err
	}
	name, err := syscall.UTF16PtrFromString(desktopRunningMutex)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateMutex(security, false, name)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return nil, err
	}
	return func() { _ = windows.CloseHandle(handle) }, nil
}

func preLoginMutexSecurityAttributes() (*windows.SecurityAttributes, error) {
	securityDescriptor, err := windows.SecurityDescriptorFromString("D:(A;;0x00100001;;;BU)(A;;0x00100001;;;SY)")
	if err != nil {
		return nil, err
	}
	return &windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: securityDescriptor,
	}, nil
}

func DesktopIsRunning() bool {
	name, err := syscall.UTF16PtrFromString(desktopRunningMutex)
	if err != nil {
		return false
	}
	handle, err := windows.OpenMutex(windows.SYNCHRONIZE|windows.MUTEX_MODIFY_STATE, false, name)
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(handle)
	return true
}

// WaitForPreLoginAgent waits for an already-running headless task to finish,
// leaving the BLE connection available for the interactive desktop app.
func WaitForPreLoginAgent(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	name, err := syscall.UTF16PtrFromString(beforeLoginAgentMutex)
	if err != nil {
		return err
	}
	handle, err := windows.OpenMutex(windows.SYNCHRONIZE, false, name)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open the pre-login BLE coordination mutex: %w", err)
	}
	defer windows.CloseHandle(handle)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		wait := 500 * time.Millisecond
		if deadline, ok := ctx.Deadline(); ok {
			remaining := time.Until(deadline)
			if remaining < wait {
				wait = remaining
			}
		}
		if wait <= 0 {
			return context.DeadlineExceeded
		}
		result, err := windows.WaitForSingleObject(handle, uint32(wait.Milliseconds()))
		if err != nil {
			return fmt.Errorf("wait for the pre-login BLE task: %w", err)
		}
		switch result {
		case windows.WAIT_OBJECT_0, windows.WAIT_ABANDONED:
			_ = windows.ReleaseMutex(handle)
			return nil
		case 0x00000102: // WAIT_TIMEOUT
			continue
		default:
			return fmt.Errorf("unexpected pre-login mutex wait result 0x%08X", result)
		}
	}
}

func taskNotFoundError(err error, output []byte) bool {
	text := strings.ToLower(string(output))
	return strings.Contains(text, "cannot find") || strings.Contains(text, "not found") || strings.Contains(text, "does not exist")
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
