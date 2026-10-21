//go:build !windows

package platform

import "context"

type WindowsStartup struct{}

func (WindowsStartup) Enabled() (bool, error) { return false, nil }
func (WindowsStartup) SetEnabled(bool) error  { return nil }

func AcquirePreLoginAgent() (func(), bool, error) { return func() {}, false, nil }
func WaitForPreLoginAgent(context.Context) error  { return nil }
func MarkDesktopRunning() (func(), error)         { return func() {}, nil }
func DesktopIsRunning() bool                      { return false }

func AcquireSingleInstance(string) (func(), bool, error) { return func() {}, false, nil }
func StartSessionMonitor(func() bool, func(bool)) (func(), error) {
	return func() {}, nil
}
