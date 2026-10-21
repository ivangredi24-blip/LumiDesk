//go:build !windows

package platform

type WindowsStartup struct{}

func (WindowsStartup) Enabled() (bool, error) { return false, nil }
func (WindowsStartup) SetEnabled(bool) error  { return nil }

func AcquireSingleInstance(string) (func(), bool, error) { return func() {}, false, nil }
func StartSessionMonitor(func() bool, func(bool)) (func(), error) {
	return func() {}, nil
}
