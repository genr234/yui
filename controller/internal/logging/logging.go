package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

func Setup(path string) (*os.File, error) {
	file, err := open(path)
	if err == nil {
		configure(file)
		return file, nil
	}

	for _, fallback := range fallbackPaths() {
		file, fallbackErr := open(fallback)
		if fallbackErr != nil {
			continue
		}
		configure(file)
		log.Printf("primary log path unavailable: %s: %v", path, err)
		log.Printf("using fallback log path: %s", fallback)
		return file, nil
	}

	return nil, fmt.Errorf("open log %s: %w", path, err)
}

func open(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
}

// The log file is written synchronously; the console only best-effort. On
// Windows a console that is in QuickEdit "Select" mode (one touch on a kiosk
// screen is enough) blocks every write to it until the selection is cleared,
// which used to freeze the whole controller, Chrome restarts included.
func configure(file *os.File) {
	disableQuickEdit()
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetOutput(io.MultiWriter(file, console()))
}

var (
	consoleOnce sync.Once
	consoleOut  *asyncWriter
)

func console() io.Writer {
	consoleOnce.Do(func() { consoleOut = newAsyncWriter(os.Stderr, 512) })
	return consoleOut
}

// ConsoleDropped is how many log lines the console missed because it was not
// accepting writes (for diagnostics and telemetry).
func ConsoleDropped() int64 {
	if consoleOut == nil {
		return 0
	}
	return consoleOut.dropped.Load()
}

// asyncWriter hands writes to a goroutine and never blocks: when the queue is
// full (the destination is stuck) lines are dropped and counted.
type asyncWriter struct {
	ch      chan []byte
	dropped atomic.Int64
}

func newAsyncWriter(w io.Writer, size int) *asyncWriter {
	a := &asyncWriter{ch: make(chan []byte, size)}
	go func() {
		for line := range a.ch {
			_, _ = w.Write(line)
		}
	}()
	return a
}

func (a *asyncWriter) Write(p []byte) (int, error) {
	line := append([]byte(nil), p...)
	select {
	case a.ch <- line:
	default:
		a.dropped.Add(1)
	}
	return len(p), nil
}

func fallbackPaths() []string {
	var paths []string

	if exePath, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(exePath), "controller.log"))
	}
	if programData := os.Getenv("ProgramData"); programData != "" {
		paths = append(paths, filepath.Join(programData, "YuiKiosk", "controller.log"))
	}
	paths = append(paths, filepath.Join(os.TempDir(), "yui-kiosk-controller.log"))

	return paths
}
