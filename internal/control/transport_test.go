package control

import (
	"strings"
	"testing"
)

func TestFriendlyBLEErrorsExplainWindowsSessionFailures(t *testing.T) {
	for _, raw := range []string{"Incorrect function.", "discover services: async operation failed with status 3: HRESULT 0x80000013"} {
		got := friendlyBLEError(errString(raw)).Error()
		if !strings.Contains(got, "Windows") || !strings.Contains(got, "other lighting apps") {
			t.Errorf("friendly message for %q is not actionable: %q", raw, got)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }
