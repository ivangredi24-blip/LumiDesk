package platform

import (
	"fmt"
	"os"
	"strings"
)

type StartupRegistrar interface {
	Enabled() (bool, error)
	SetEnabled(bool) error
}

type BeforeLoginStartupRegistrar interface {
	BeforeLoginEnabled() (bool, error)
	SetBeforeLoginEnabled(bool) error
}

func QuoteStartupCommand(executable string) string {
	return `"` + strings.ReplaceAll(executable, `"`, `\"`) + `" --startup`
}

func QuoteBeforeLoginCommand(executable, configDir string) string {
	quotedExecutable := `"` + strings.ReplaceAll(executable, `"`, `\"`) + `"`
	quotedConfigDir := `"` + strings.ReplaceAll(configDir, `"`, `\"`) + `"`
	return quotedExecutable + ` --prelogin-lights --config-dir ` + quotedConfigDir
}

func CurrentExecutableStartupCommand() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate LumiDesk executable: %w", err)
	}
	return QuoteStartupCommand(exe), nil
}
