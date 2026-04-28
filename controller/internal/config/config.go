package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const fileName = "controller.json"
const envConfigPath = "YUI_KIOSK_CONFIG"

type Config struct {
	ChromePath          string   `json:"chrome_path"`
	URL                 string   `json:"url"`
	Flags               []string `json:"flags"`
	UserDataDir         string   `json:"user_data_dir"`
	RestartDelaySeconds int      `json:"restart_delay_seconds"`
	RestartMaxSeconds   int      `json:"restart_max_seconds"`
	LogPath             string   `json:"log_path"`
	StatusPath          string   `json:"status_path"`
	PlatformEnabled     bool     `json:"platform_enabled"`
	PlatformHTTPAddr    string   `json:"platform_http_addr"`
	PlatformBridgeAddr  string   `json:"platform_bridge_addr"`
	PlatformDebugPort   int      `json:"platform_remote_debugging_port"`
	PlatformDevServer   string   `json:"platform_dev_server"`

	ConfigPath    string `json:"-"`
	ConfigDir     string `json:"-"`
	ImportedFrom  string `json:"-"`
	ImportWarning string `json:"-"`
	UsingDefaults bool   `json:"-"`
}

func Default() Config {
	return Config{
		ChromePath: `C:\Program Files\Google\Chrome\Application\chrome.exe`,
		URL:        "https://sicilykioskuser-acireale.flazio.com/?DIGI=20&id_totem=20",
		Flags: []string{
			"--kiosk",
			`--user-agent=chrome`,
			"--disable-pinch",
			"--disable-session-restore",
			`--js-flags=--expose-gc`,
			"--disable-session-crashed-bubble",
			"--disable-infobars",
			"--no-first-run",
			"--fast",
			"--fast-start",
			"--disable-tab-switcher",
			"--ignore-certificate-errors",
			"--enable-low-res-tiling",
		},
		RestartDelaySeconds: 3,
		RestartMaxSeconds:   30,
		UserDataDir:         "chrome-profile",
		LogPath:             "controller.log",
		StatusPath:          "status.json",
		PlatformEnabled:     true,
		PlatformHTTPAddr:    "127.0.0.1:7072",
		PlatformBridgeAddr:  "127.0.0.1:7071",
		PlatformDebugPort:   9222,
	}
}

func Load() (Config, error) {
	exePath, err := os.Executable()
	if err != nil {
		return Config{}, fmt.Errorf("find executable path: %w", err)
	}

	exeDir := filepath.Dir(exePath)
	candidates := configCandidates(exeDir)

	cfg := Default()

	for _, configPath := range candidates {
		data, err := os.ReadFile(configPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return Config{}, fmt.Errorf("read config %s: %w", configPath, err)
		}

		if err := json.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse config %s: %w", configPath, err)
		}

		cfg.ConfigPath = configPath
		cfg.ConfigDir = filepath.Dir(configPath)
		applyDefaults(&cfg)
		resolvePaths(&cfg)

		return cfg, nil
	}

	if importedCfg, ok, err := importExistingKioskConfig(exeDir, candidates); err != nil {
		return Config{}, err
	} else if ok {
		return importedCfg, nil
	}

	if selectedCfg, ok, err := importSelectedKioskConfig(exeDir, candidates); err != nil {
		return Config{}, err
	} else if ok {
		return selectedCfg, nil
	}

	cfg.ConfigPath = candidates[0]
	cfg.ConfigDir = filepath.Dir(candidates[0])
	cfg.UsingDefaults = true
	resolvePaths(&cfg)

	return cfg, nil
}

func ImportFromSelection() (Config, error) {
	exePath, err := os.Executable()
	if err != nil {
		return Config{}, fmt.Errorf("find executable path: %w", err)
	}

	exeDir := filepath.Dir(exePath)
	candidates := configCandidates(exeDir)
	cfg, ok, err := importSelectedKioskConfig(exeDir, candidates)
	if err != nil {
		return Config{}, err
	}
	if !ok {
		return Config{}, fmt.Errorf("no chrome.bat selected")
	}
	return cfg, nil
}

func Save(cfg Config) error {
	if cfg.ConfigPath == "" {
		return fmt.Errorf("config path is empty")
	}

	if err := os.MkdirAll(filepath.Dir(cfg.ConfigPath), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(cfg.ConfigPath, data, 0644); err != nil {
		return fmt.Errorf("write config %s: %w", cfg.ConfigPath, err)
	}

	return nil
}

func configCandidates(exeDir string) []string {
	var paths []string

	if explicitPath := os.Getenv(envConfigPath); explicitPath != "" {
		paths = append(paths, explicitPath)
	}

	if programData := os.Getenv("ProgramData"); programData != "" {
		paths = append(paths,
			filepath.Join(programData, "YuiKiosk", fileName),
			filepath.Join(programData, "Yui", "Kiosk", fileName),
		)
	}

	if commonProgramData := os.Getenv("ALLUSERSPROFILE"); commonProgramData != "" {
		paths = append(paths,
			filepath.Join(commonProgramData, "YuiKiosk", fileName),
			filepath.Join(commonProgramData, "Yui", "Kiosk", fileName),
		)
	}

	if runtime.GOOS == "windows" {
		paths = append(paths,
			filepath.Join(`C:\ProgramData`, "YuiKiosk", fileName),
			filepath.Join(`C:\ProgramData`, "Yui", "Kiosk", fileName),
		)
	}

	if workingDir, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(workingDir, fileName))
	}

	paths = append(paths, filepath.Join(exeDir, fileName))

	return unique(paths)
}

func applyDefaults(cfg *Config) {
	defaults := Default()

	if cfg.ChromePath == "" {
		cfg.ChromePath = defaults.ChromePath
	}
	if cfg.URL == "" {
		cfg.URL = defaults.URL
	}
	if len(cfg.Flags) == 0 {
		cfg.Flags = defaults.Flags
	}
	if cfg.UserDataDir == "" {
		cfg.UserDataDir = defaults.UserDataDir
	}
	if cfg.RestartDelaySeconds <= 0 {
		cfg.RestartDelaySeconds = defaults.RestartDelaySeconds
	}
	if cfg.RestartMaxSeconds <= 0 {
		cfg.RestartMaxSeconds = defaults.RestartMaxSeconds
	}
	if cfg.LogPath == "" {
		cfg.LogPath = defaults.LogPath
	}
	if cfg.StatusPath == "" {
		cfg.StatusPath = defaults.StatusPath
	}
	if cfg.PlatformHTTPAddr == "" {
		cfg.PlatformHTTPAddr = defaults.PlatformHTTPAddr
	}
	if cfg.PlatformBridgeAddr == "" {
		cfg.PlatformBridgeAddr = defaults.PlatformBridgeAddr
	}
	if cfg.PlatformDebugPort <= 0 {
		cfg.PlatformDebugPort = defaults.PlatformDebugPort
	}
	applyEnvOverrides(cfg)
}

func applyEnvOverrides(cfg *Config) {
	if value := os.Getenv("YUI_PLATFORM_HTTP_ADDR"); value != "" {
		cfg.PlatformHTTPAddr = value
	}
	if value := os.Getenv("YUI_PLATFORM_BRIDGE_ADDR"); value != "" {
		cfg.PlatformBridgeAddr = value
	}
	if value := os.Getenv("YUI_PLATFORM_DEV_SERVER"); value != "" {
		cfg.PlatformDevServer = value
	}
}

func resolvePaths(cfg *Config) {
	if hasPathSeparator(cfg.ChromePath) && !filepath.IsAbs(cfg.ChromePath) {
		cfg.ChromePath = filepath.Join(cfg.ConfigDir, cfg.ChromePath)
	}
	if !filepath.IsAbs(cfg.UserDataDir) {
		cfg.UserDataDir = filepath.Join(cfg.ConfigDir, cfg.UserDataDir)
	}
	if !filepath.IsAbs(cfg.LogPath) {
		cfg.LogPath = filepath.Join(cfg.ConfigDir, cfg.LogPath)
	}
	if !filepath.IsAbs(cfg.StatusPath) {
		cfg.StatusPath = filepath.Join(cfg.ConfigDir, cfg.StatusPath)
	}
}

func hasPathSeparator(path string) bool {
	return filepath.Base(path) != path
}

func unique(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	result := make([]string, 0, len(paths))

	for _, path := range paths {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		result = append(result, path)
	}

	return result
}
