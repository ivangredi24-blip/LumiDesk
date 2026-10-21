package logging

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

const (
	maxLogBytes = 5 << 20
	keepLogs    = 3
)

type Logger struct {
	mu     sync.Mutex
	logger *log.Logger
	file   *os.File
	path   string
}

func New(dir string) (*Logger, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	path := filepath.Join(dir, "lumidesk.log")
	if info, err := os.Stat(path); err == nil && info.Size() >= maxLogBytes {
		rotate(path)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	return &Logger{logger: log.New(f, "", log.LstdFlags|log.LUTC), file: f, path: path}, nil
}

func (l *Logger) Printf(format string, args ...any) {
	if l == nil || l.logger == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if info, err := l.file.Stat(); err == nil && info.Size() >= maxLogBytes {
		l.rotateOpen()
	}
	l.logger.Printf(format, args...)
}

func (l *Logger) Close() error {
	if l == nil || l.file == nil || l.file == os.Stderr {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close()
}

func (l *Logger) rotateOpen() {
	_ = l.file.Close()
	rotate(l.path)
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		l.file = os.Stderr
		l.logger.SetOutput(os.Stderr)
		return
	}
	l.file = f
	l.logger.SetOutput(f)
}

func rotate(path string) {
	_ = os.Remove(fmt.Sprintf("%s.%d", path, keepLogs))
	for i := keepLogs - 1; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", path, i), fmt.Sprintf("%s.%d", path, i+1))
	}
	_ = os.Rename(path, fmt.Sprintf("%s.1", path))
}
