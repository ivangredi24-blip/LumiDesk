package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoggerRotatesAtSizeLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lumidesk.log")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", maxLogBytes)), 0600); err != nil {
		t.Fatal(err)
	}
	logger, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	logger.Printf("rotation test")
	if info, err := os.Stat(path + ".1"); err != nil || info.Size() != maxLogBytes {
		t.Fatalf("rotated log missing or wrong size: info=%v err=%v", info, err)
	}
	if data, err := os.ReadFile(path); err != nil || !strings.Contains(string(data), "rotation test") {
		t.Fatalf("new log file missing entry: data=%q err=%v", data, err)
	}
}
