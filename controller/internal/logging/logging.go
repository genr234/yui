package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
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

func configure(file *os.File) {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetOutput(io.MultiWriter(os.Stderr, file))
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
