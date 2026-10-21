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

func QuoteStartupCommand(executable string) string {
	return `"` + strings.ReplaceAll(executable, `"`, `\"`) + `" --startup`
}

func CurrentExecutableStartupCommand() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate LumiDesk executable: %w", err)
	}
	return QuoteStartupCommand(exe), nil
}
