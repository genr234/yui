package main

import (
	"embed"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed assets/*
var assets embed.FS

const bootstrapMarker = "YUI_KIOSK_BOOTSTRAP"
const installerVersion = "0.1.0"

type installMode int

const (
	modeInstall installMode = iota
	modeRestore
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println(installerVersion)
		return
	}

	logFile := setupLog()
	if logFile != nil {
		defer logFile.Close()
	}

	mode, target, err := selectActionAndTarget()
	if err != nil {
		fatal(err)
	}

	if mode == modeRestore {
		if err := restore(target); err != nil {
			fatal(err)
		}
		return
	}

	if err := install(target); err != nil {
		fatal(err)
	}
}

func selectActionAndTarget() (installMode, string, error) {
	if len(os.Args) > 1 && os.Args[1] != "" {
		if os.Args[1] == "--restore" {
			target, err := selectTargetArg(2)
			return modeRestore, target, err
		}
		target, err := selectTargetArg(1)
		return modeInstall, target, err
	}

	path, ok, err := selectFile(
		"Select the kiosk chrome.bat",
		defaultInitialDir(),
		"Batch files (*.bat)\x00*.bat\x00All files (*.*)\x00*.*\x00",
	)
	if err != nil {
		return modeInstall, "", fmt.Errorf("select chrome.bat: %w", err)
	}
	if !ok {
		return modeInstall, "", errors.New("install canceled; no chrome.bat selected")
	}

	mode, err := chooseModeForSelectedTarget(path)
	if err != nil {
		return modeInstall, "", err
	}

	return mode, path, nil
}

func selectTargetArg(index int) (string, error) {
	if len(os.Args) > index && os.Args[index] != "" {
		return filepath.Abs(os.Args[index])
	}
	path, ok, err := selectFile(
		"Select the kiosk chrome.bat",
		defaultInitialDir(),
		"Batch files (*.bat)\x00*.bat\x00All files (*.*)\x00*.*\x00",
	)
	if err != nil {
		return "", fmt.Errorf("select chrome.bat: %w", err)
	}
	if !ok {
		return "", errors.New("operation canceled; no chrome.bat selected")
	}
	return path, nil
}

func chooseModeForSelectedTarget(target string) (installMode, error) {
	target = filepath.Clean(target)
	backupPath := filepath.Join(filepath.Dir(target), "chrome.original.bat")
	hasBackup := fileExists(backupPath)
	isHijacked, err := containsMarker(target)
	if err != nil {
		return modeInstall, err
	}

	if isHijacked && !hasBackup {
		result, _, _ := messageBox(
			"Yui Kiosk Installer",
			"Yui is already installed here, but chrome.original.bat is missing.\n\nTap Yes to choose the original kiosk batch now.\nTap No to refresh Yui without a backup.\nTap Cancel to stop.",
			messageQuestion|messageYesNoCancel,
		)
		switch result {
		case dialogYes:
			if err := captureOriginalBackup(target, backupPath); err != nil {
				return modeInstall, err
			}
			return modeInstall, nil
		case dialogNo:
			return modeInstall, nil
		default:
			return modeInstall, errors.New("operation canceled")
		}
	}

	if !isHijacked || !hasBackup {
		return modeInstall, nil
	}

	result, _, _ := messageBox(
		"Yui Kiosk Installer",
		"Yui is already installed here.\n\nTap Yes to update Yui.\nTap No to restore the original kiosk chrome.bat.\nTap Cancel to do nothing.",
		messageQuestion|messageYesNoCancel,
	)
	switch result {
	case dialogYes:
		return modeInstall, nil
	case dialogNo:
		return modeRestore, nil
	default:
		return modeInstall, errors.New("operation canceled")
	}
}

func install(target string) error {
	target = filepath.Clean(target)
	targetDir := filepath.Dir(target)
	backupPath := filepath.Join(targetDir, "chrome.original.bat")
	controllerPath := filepath.Join(targetDir, "controller.exe")
	bootstrapLogPath := filepath.Join(targetDir, "controller-bootstrap.log")

	log.Printf("installer version: %s", installerVersion)
	log.Printf("install target: %s", target)
	log.Printf("target dir: %s", targetDir)

	if err := ensureOriginalBackup(target, backupPath); err != nil {
		return err
	}
	if err := writeAsset("assets/controller.exe", controllerPath, 0755); err != nil {
		return err
	}
	if err := writeAsset("assets/chrome.bat", target, 0644); err != nil {
		return err
	}
	if err := appendLine(bootstrapLogPath, "installer wrote bootstrap and controller.exe"); err != nil {
		log.Printf("bootstrap log write failed: %v", err)
	}

	log.Printf("installed controller: %s", controllerPath)
	log.Printf("installed bootstrap: %s", target)

	cmd := exec.Command(controllerPath)
	cmd.Dir = targetDir
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start controller after install: %w", err)
	}
	log.Printf("started controller pid=%d", cmd.Process.Pid)
	_, _, _ = messageBox(
		"Yui Kiosk Installer",
		"Installed successfully.\n\nThe controller has been started.",
		messageInfo|messageOK,
	)

	return nil
}

func restore(target string) error {
	target = filepath.Clean(target)
	targetDir := filepath.Dir(target)
	backupPath := filepath.Join(targetDir, "chrome.original.bat")
	bootstrapLogPath := filepath.Join(targetDir, "controller-bootstrap.log")

	log.Printf("installer version: %s", installerVersion)
	log.Printf("restore target: %s", target)

	data, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("read original backup %s: %w", backupPath, err)
	}
	if err := os.WriteFile(target, data, 0644); err != nil {
		return fmt.Errorf("restore original chrome.bat: %w", err)
	}
	if err := appendLine(bootstrapLogPath, "installer restored original chrome.bat"); err != nil {
		log.Printf("bootstrap log write failed: %v", err)
	}

	log.Printf("restored original batch from: %s", backupPath)
	_, _, _ = messageBox(
		"Yui Kiosk Installer",
		"Restored the original kiosk chrome.bat.",
		messageInfo|messageOK,
	)

	return nil
}

func ensureOriginalBackup(target string, backupPath string) error {
	if _, err := os.Stat(backupPath); err == nil {
		log.Printf("backup already exists; preserving: %s", backupPath)
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check backup %s: %w", backupPath, err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		return fmt.Errorf("read selected chrome.bat %s: %w", target, err)
	}
	if strings.Contains(string(data), bootstrapMarker) {
		log.Printf("selected batch already appears hijacked and no original backup exists")
		return nil
	}

	return writeBackup(backupPath, data)
}

func containsMarker(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	return strings.Contains(string(data), bootstrapMarker), nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func writeAsset(assetPath string, targetPath string, mode os.FileMode) error {
	data, err := assets.ReadFile(assetPath)
	if err != nil {
		return fmt.Errorf("read embedded %s: %w", assetPath, err)
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return fmt.Errorf("create target dir: %w", err)
	}
	if err := os.WriteFile(targetPath, data, mode); err != nil {
		return fmt.Errorf("write %s: %w", targetPath, err)
	}

	return nil
}

func setupLog() *os.File {
	path := installerLogPath()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("installer log unavailable: %v", err)
		return nil
	}

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetOutput(io.MultiWriter(os.Stderr, file))
	log.Printf("installer starting")
	log.Printf("version: %s", installerVersion)

	return file
}

func installerLogPath() string {
	if exePath, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exePath), "yui-kiosk-installer.log")
	}
	return filepath.Join(os.TempDir(), "yui-kiosk-installer.log")
}

func appendLine(path string, message string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = fmt.Fprintf(file, "%s\n", message)
	return err
}

func fatal(err error) {
	log.Printf("install failed: %v", err)
	_, _, _ = messageBox("Yui Kiosk Installer", err.Error(), messageError)
	os.Exit(1)
}

func captureOriginalBackup(target string, backupPath string) error {
	path, ok, err := selectFile(
		"Select the original kiosk chrome.bat",
		filepath.Dir(target),
		"Batch files (*.bat)\x00*.bat\x00All files (*.*)\x00*.*\x00",
	)
	if err != nil {
		return fmt.Errorf("select original kiosk batch: %w", err)
	}
	if !ok {
		return errors.New("no original kiosk batch selected")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read original kiosk batch %s: %w", path, err)
	}
	if strings.Contains(string(data), bootstrapMarker) {
		return fmt.Errorf("selected file is already the Yui bootstrap, not the original kiosk batch")
	}

	return writeBackup(backupPath, data)
}

func writeBackup(backupPath string, data []byte) error {
	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		return fmt.Errorf("write original backup %s: %w", backupPath, err)
	}
	log.Printf("wrote original backup: %s", backupPath)
	return nil
}
