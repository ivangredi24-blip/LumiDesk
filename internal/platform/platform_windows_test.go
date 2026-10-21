//go:build windows

package platform

import (
	"fmt"
	"os"
	"testing"
)

func TestSingleInstanceMutex(t *testing.T) {
	name := fmt.Sprintf(`Local\LumiDesk.Test.%d`, os.Getpid())
	release, exists, err := AcquireSingleInstance(name)
	if err != nil || exists {
		t.Fatalf("first mutex acquisition failed: exists=%t err=%v", exists, err)
	}
	defer release()
	secondRelease, exists, err := AcquireSingleInstance(name)
	if err != nil || !exists {
		t.Fatalf("second mutex acquisition should detect the first instance: exists=%t err=%v", exists, err)
	}
	if secondRelease != nil {
		secondRelease()
	}
}

func TestSessionMonitorStartsAndStops(t *testing.T) {
	stop, err := StartSessionMonitor(func() bool { return true }, func(bool) {})
	if err != nil {
		t.Fatalf("create native session monitor: %v", err)
	}
	stop()
}
