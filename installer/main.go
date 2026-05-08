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
	"strconv"
	"strings"
	"time"
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

var (
	autoUpdateMode bool
	parentPID      int
)

func main() {
	args := userArgs()
	if len(args) > 0 && args[0] == "--version" {
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
	log.Printf("selected action: %s", modeLabel(mode))
	log.Printf("selected target: %s", target)

	if mode == modeInstall && !hasElevationMarker() && !autoUpdateMode {
		log.Printf("confirming install with user")
		if err := confirmInstall(target); err != nil {
			fatal(err)
		}
		log.Printf("install confirmed")
	}

	log.Printf("checking elevation requirement")
	relaunched, err := relaunchElevatedIfNeeded(mode, target)
	if err != nil {
		fatal(err)
	}
	if relaunched {
		log.Printf("elevated installer launched; exiting current process")
		return
	}

	if mode == modeRestore {
		if err := restore(target); err != nil {
			fatal(err)
		}
		return
	}

	if autoUpdateMode {
		log.Printf("waiting for parent process before auto update")
		waitForParentExit(parentPID, 45*time.Second)
	}

	if err := install(target); err != nil {
		fatal(err)
	}
	log.Printf("install finished successfully")
}

func selectActionAndTarget() (installMode, string, error) {
	args := userArgs()
	if len(args) > 0 && args[0] != "" {
		if args[0] == "--auto-update" {
			target, err := parseAutoUpdateArgs(args[1:])
			return modeInstall, target, err
		}
		if args[0] == "--restore" {
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

func parseAutoUpdateArgs(args []string) (string, error) {
	autoUpdateMode = true
	for len(args) > 0 {
		switch args[0] {
		case "--parent-pid":
			if len(args) < 2 {
				return "", errors.New("--parent-pid requires a value")
			}
			pid, err := strconv.Atoi(args[1])
			if err != nil {
				return "", fmt.Errorf("parse parent pid: %w", err)
			}
			parentPID = pid
			args = args[2:]
		default:
			return filepath.Abs(args[0])
		}
	}
	return "", errors.New("--auto-update requires a chrome.bat target")
}

func selectTargetArg(index int) (string, error) {
	args := userArgs()
	argIndex := index - 1
	if len(args) > argIndex && args[argIndex] != "" {
		return filepath.Abs(args[argIndex])
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
	if autoUpdateMode {
		return modeInstall, nil
	}

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
	menuPath := filepath.Join(targetDir, "yui-menu.bat")
	bootstrapLogPath := filepath.Join(targetDir, "controller-bootstrap.log")

	log.Printf("installer version: %s", installerVersion)
	log.Printf("install target: %s", target)
	log.Printf("target dir: %s", targetDir)

	originalData, err := os.ReadFile(target)
	if err != nil {
		return fmt.Errorf("read selected chrome.bat %s: %w", target, err)
	}
	installComplete := false
	defer func() {
		if installComplete {
			return
		}
		if err := os.WriteFile(target, originalData, 0644); err != nil {
			log.Printf("rollback failed for %s: %v", target, err)
		} else {
			log.Printf("rolled back chrome.bat after failed install")
		}
	}()

	if err := ensureOriginalBackup(target, backupPath); err != nil {
		return err
	}
	if err := writeAsset("assets/controller.exe", controllerPath, 0755); err != nil {
		return err
	}
	if err := writeAsset("assets/chrome.bat", target, 0644); err != nil {
		return err
	}
	if err := writeAsset("assets/yui-menu.bat", menuPath, 0644); err != nil {
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

	verifySummary, verifyErr := verifyPostInstall(targetDir, target, backupPath, controllerPath, menuPath)
	if verifyErr != nil {
		return verifyErr
	}
	installComplete = true
	if autoUpdateMode {
		return nil
	}
	_, _, _ = messageBox(
		"Yui Kiosk Installer",
		"Installed successfully.\n\nThe controller has been started.\n\n"+verifySummary,
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

func confirmInstall(target string) error {
	target = filepath.Clean(target)
	targetDir := filepath.Dir(target)
	backupPath := filepath.Join(targetDir, "chrome.original.bat")
	previewPath := target
	if isHijacked, err := containsMarker(target); err == nil && isHijacked && fileExists(backupPath) {
		previewPath = backupPath
	}

	preview, ok, err := readBatchPreview(previewPath)
	if err != nil {
		return err
	}

	var body strings.Builder
	body.WriteString("Ready to install Yui into this kiosk batch.\n\n")
	body.WriteString("Target:\n")
	body.WriteString(target)
	body.WriteString("\n\nBackup:\n")
	body.WriteString(backupPath)
	body.WriteString("\n\n")
	if ok {
		body.WriteString("Detected Chrome:\n")
		body.WriteString(preview.ChromePath)
		body.WriteString("\n\nDetected URL:\n")
		body.WriteString(valueOr(preview.URL, "(none found)"))
		body.WriteString("\n\nFlags: ")
		body.WriteString(fmt.Sprintf("%d", len(preview.Flags)))
		body.WriteString("\n")
	} else {
		body.WriteString("Warning: no Chrome command was detected. Yui can still install, but the controller may use defaults or ask for recovery input.\n")
	}
	body.WriteString("\nTap Yes to install. Tap No to cancel.")

	result, _, _ := messageBox(
		"Yui Kiosk Installer",
		body.String(),
		messageQuestion|messageYesNo,
	)
	if result != dialogYes {
		return errors.New("install canceled")
	}

	return nil
}

type batchPreview struct {
	ChromePath string
	URL        string
	Flags      []string
}

func readBatchPreview(path string) (batchPreview, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return batchPreview{}, false, fmt.Errorf("read batch preview %s: %w", path, err)
	}

	for _, rawLine := range strings.Split(string(data), "\n") {
		line := cleanBatchLine(rawLine)
		if line == "" {
			continue
		}
		chromePath, args, ok := parseChromeCommand(line)
		if !ok {
			continue
		}
		flags, url := splitChromeArgs(args)
		return batchPreview{ChromePath: chromePath, URL: url, Flags: flags}, true, nil
	}

	return batchPreview{}, false, nil
}

func cleanBatchLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	lower := strings.ToLower(line)
	if strings.HasPrefix(lower, "rem ") || strings.HasPrefix(line, "::") {
		return ""
	}
	return line
}

func parseChromeCommand(line string) (string, []string, bool) {
	tokens := tokenizeCommandLine(line)
	for i, token := range tokens {
		if strings.EqualFold(filepath.Base(token), "chrome.exe") {
			return token, tokens[i+1:], true
		}
	}

	lower := strings.ToLower(line)
	idx := strings.Index(lower, "chrome.exe")
	if idx == -1 {
		return "", nil, false
	}
	start := idx
	for start > 0 {
		ch := line[start-1]
		if ch == '"' || ch == ' ' || ch == '\t' {
			break
		}
		start--
	}
	path := strings.Trim(line[start:idx+len("chrome.exe")], `" `)
	tail := strings.TrimSpace(line[idx+len("chrome.exe"):])
	tail = strings.TrimPrefix(tail, `"`)
	return path, tokenizeCommandLine(tail), true
}

func tokenizeCommandLine(line string) []string {
	var tokens []string
	var current strings.Builder
	inQuotes := false

	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch ch {
		case '"':
			inQuotes = !inQuotes
		case ' ', '\t':
			if inQuotes {
				current.WriteByte(ch)
				continue
			}
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

func splitChromeArgs(args []string) ([]string, string) {
	flags := make([]string, 0, len(args))
	url := ""
	for _, arg := range args {
		lower := strings.ToLower(arg)
		if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
			url = arg
			continue
		}
		flags = append(flags, arg)
	}
	return flags, url
}

func verifyPostInstall(targetDir, target, backupPath, controllerPath, menuPath string) (string, error) {
	checks := []struct {
		label string
		path  string
	}{
		{"controller.exe", controllerPath},
		{"original backup", backupPath},
		{"recovery menu", menuPath},
	}

	for _, check := range checks {
		if !fileExists(check.path) {
			return "", fmt.Errorf("post-install verification failed: %s missing at %s", check.label, check.path)
		}
	}
	if ok, err := containsMarker(target); err != nil {
		return "", err
	} else if !ok {
		return "", fmt.Errorf("post-install verification failed: chrome.bat does not contain Yui marker")
	}

	runtimeSummary := waitForRuntimeFiles(targetDir, 8*time.Second)
	return "Verified files:\ncontroller.exe\nchrome.bat\nchrome.original.bat\nyui-menu.bat\n\n" + runtimeSummary, nil
}

func waitForRuntimeFiles(targetDir string, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	var found []string
	for {
		found = existingRuntimeFiles(targetDir)
		if len(found) >= 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if len(found) == 0 {
		return "Runtime files were not observed yet. Check controller-bootstrap.log or yui-menu.bat if the kiosk does not recover."
	}

	return "Observed runtime files:\n" + strings.Join(found, "\n")
}

func existingRuntimeFiles(targetDir string) []string {
	candidates := runtimeFileCandidates(targetDir)
	found := make([]string, 0, len(candidates))
	seen := map[string]bool{}
	for _, path := range candidates {
		if seen[path] || !fileExists(path) {
			continue
		}
		seen[path] = true
		found = append(found, path)
	}
	return found
}

func runtimeFileCandidates(targetDir string) []string {
	names := []string{"controller.json", "controller.log", "status.json", "yui-store.db"}
	dirs := []string{targetDir}
	if programData := os.Getenv("ProgramData"); programData != "" {
		dirs = append(dirs,
			filepath.Join(programData, "YuiKiosk"),
			filepath.Join(programData, "Yui", "Kiosk"),
		)
	}
	if allUsers := os.Getenv("ALLUSERSPROFILE"); allUsers != "" {
		dirs = append(dirs,
			filepath.Join(allUsers, "YuiKiosk"),
			filepath.Join(allUsers, "Yui", "Kiosk"),
		)
	}

	var paths []string
	for _, dir := range dirs {
		for _, name := range names {
			paths = append(paths, filepath.Join(dir, name))
		}
	}
	return paths
}

func valueOr(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func modeLabel(mode installMode) string {
	switch mode {
	case modeInstall:
		return "install"
	case modeRestore:
		return "restore"
	default:
		return fmt.Sprintf("unknown(%d)", mode)
	}
}

func userArgs() []string {
	args := make([]string, 0, len(os.Args)-1)
	for _, arg := range os.Args[1:] {
		if arg == "--elevated" {
			continue
		}
		args = append(args, arg)
	}
	return args
}

func hasElevationMarker() bool {
	for _, arg := range os.Args[1:] {
		if arg == "--elevated" {
			return true
		}
	}
	return false
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
	if autoUpdateMode {
		os.Exit(1)
	}
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
