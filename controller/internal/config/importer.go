package config

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"kiosk/controller/internal/nativeui"
)

func importExistingKioskConfig(exeDir string, configCandidates []string) (Config, bool, error) {
	for _, batchPath := range batchCandidates(exeDir) {
		if cfg, ok, err := importBatchConfig(batchPath, configCandidates); ok || err != nil {
			return cfg, ok, err
		}
	}

	return Config{}, false, nil
}

func importSelectedKioskConfig(exeDir string, configCandidates []string) (Config, bool, error) {
	batchPath, ok, err := nativeui.SelectFile(
		"Select the kiosk chrome.bat",
		exeDir,
		"Batch files (*.bat)\x00*.bat\x00All files (*.*)\x00*.*\x00",
	)
	if err != nil || !ok {
		return Config{}, false, err
	}
	return importBatchConfig(batchPath, configCandidates)
}

func importBatchConfig(batchPath string, configCandidates []string) (Config, bool, error) {
	cfg, ok, err := parseBatchConfig(batchPath)
	if err != nil || !ok {
		return Config{}, ok, err
	}

	configPath, err := writeImportedConfig(cfg, configCandidates)
	if err != nil {
		configPath = fallbackConfigPath(configCandidates)
		cfg.ImportWarning = err.Error()
	}

	cfg.ConfigPath = configPath
	cfg.ConfigDir = filepath.Dir(configPath)
	cfg.ImportedFrom = batchPath
	applyDefaults(&cfg)
	resolvePaths(&cfg)

	return cfg, true, nil
}

func fallbackConfigPath(candidates []string) string {
	if len(candidates) == 0 {
		return fileName
	}
	return candidates[len(candidates)-1]
}

func batchCandidates(exeDir string) []string {
	var paths []string

	if workingDir, err := os.Getwd(); err == nil {
		paths = append(paths, batchNames(workingDir)...)
	}
	paths = append(paths, batchNames(exeDir)...)

	return unique(paths)
}

func batchNames(dir string) []string {
	names := []string{
		"chrome.bat",
		"chrome.original.bat",
		"chrome.bat.original",
		"chrome.bat.bak",
		"chrome.original",
	}

	paths := make([]string, 0, len(names))
	for _, name := range names {
		paths = append(paths, filepath.Join(dir, name))
	}
	return paths
}

func parseBatchConfig(path string) (Config, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, false, nil
		}
		return Config{}, false, fmt.Errorf("read batch file %s: %w", path, err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := cleanBatchLine(scanner.Text())
		if line == "" {
			continue
		}

		chromePath, chromeArgs, ok := parseChromeCommand(line)
		if !ok {
			continue
		}

		cfg := Default()
		cfg.ChromePath = expandWindowsEnv(chromePath)
		cfg.Flags, cfg.URL = splitChromeArgs(chromeArgs)
		cfg.Flags, cfg.UserDataDir = extractUserDataDir(cfg.Flags, cfg.UserDataDir)
		applyDefaults(&cfg)

		return cfg, true, nil
	}
	if err := scanner.Err(); err != nil {
		return Config{}, false, fmt.Errorf("scan batch file %s: %w", path, err)
	}

	return Config{}, false, nil
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
	if chromeIndex := findChromeToken(tokens); chromeIndex != -1 {
		return tokens[chromeIndex], tokens[chromeIndex+1:], true
	}

	chromePath, tail, ok := extractChromePath(line)
	if !ok {
		return "", nil, false
	}

	return chromePath, tokenizeCommandLine(tail), true
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

var driveChromePathPattern = regexp.MustCompile(`(?i)[a-z]:\\.*?chrome\.exe`)

func extractChromePath(line string) (string, string, bool) {
	match := driveChromePathPattern.FindStringIndex(line)
	if match == nil {
		return "", "", false
	}

	path := strings.Trim(line[match[0]:match[1]], `" `)
	tail := strings.TrimSpace(line[match[1]:])
	tail = strings.TrimPrefix(tail, `"`)
	tail = strings.TrimSpace(tail)

	return path, tail, true
}

func findChromeToken(tokens []string) int {
	for i, token := range tokens {
		if strings.EqualFold(filepath.Base(token), "chrome.exe") {
			return i
		}
	}
	return -1
}

func splitChromeArgs(args []string) ([]string, string) {
	flags := make([]string, 0, len(args))
	url := ""

	for _, arg := range args {
		if strings.HasPrefix(strings.ToLower(arg), "http://") ||
			strings.HasPrefix(strings.ToLower(arg), "https://") {
			url = arg
			continue
		}
		flags = append(flags, arg)
	}

	return flags, url
}

func expandWindowsEnv(value string) string {
	replacements := map[string]string{
		"%ProgramFiles%":      os.Getenv("ProgramFiles"),
		"%ProgramFiles(x86)%": os.Getenv("ProgramFiles(x86)"),
		"%SystemDrive%":       os.Getenv("SystemDrive"),
		"%SystemRoot%":        os.Getenv("SystemRoot"),
		"%WINDIR%":            os.Getenv("WINDIR"),
	}

	for key, replacement := range replacements {
		if replacement == "" {
			continue
		}
		value = strings.ReplaceAll(value, key, replacement)
		value = strings.ReplaceAll(value, strings.ToLower(key), replacement)
	}

	return value
}

func extractUserDataDir(flags []string, fallback string) ([]string, string) {
	result := make([]string, 0, len(flags))
	userDataDir := fallback
	const prefix = "--user-data-dir="

	for i := 0; i < len(flags); i++ {
		flag := flags[i]
		lower := strings.ToLower(flag)

		if strings.HasPrefix(lower, prefix) {
			userDataDir = strings.TrimPrefix(flag, prefix)
			continue
		}
		if lower == "--user-data-dir" && i+1 < len(flags) {
			userDataDir = flags[i+1]
			i++
			continue
		}

		result = append(result, flag)
	}

	return result, userDataDir
}

func writeImportedConfig(cfg Config, candidates []string) (string, error) {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode imported config: %w", err)
	}
	data = append(data, '\n')

	var lastErr error
	for _, path := range candidates {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			lastErr = err
			continue
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			lastErr = err
			continue
		}
		return path, nil
	}

	return "", fmt.Errorf("write imported config: %w", lastErr)
}
