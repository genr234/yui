package handlers

import (
	"encoding/json"

	"kiosk/controller/internal/config"
	"kiosk/controller/internal/nativeui"
)

func ConfigCommands() []Command {
	return []Command{
		ConfigGetCommand{},
		ConfigUpdateCommand{},
		PlatformReimportCommand{},
		PlatformSelectChromeCommand{},
	}
}

type ConfigGetCommand struct{}

func (ConfigGetCommand) Name() string { return "config.get" }

func (ConfigGetCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	return publicConfig(r.cfg), nil
}

type ConfigUpdateCommand struct{}

func (ConfigUpdateCommand) Name() string { return "config.update" }

func (ConfigUpdateCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var patch map[string]any
	if err := json.Unmarshal(params, &patch); err != nil {
		return r.cfg, err
	}

	cfg := r.cfg
	if value, ok := patch["url"].(string); ok {
		cfg.URL = value
	}
	if value, ok := patch["chrome_path"].(string); ok {
		cfg.ChromePath = value
	}
	if value, ok := patch["platform_enabled"].(bool); ok {
		cfg.PlatformEnabled = value
	}
	if value, ok := patch["platform_http_addr"].(string); ok {
		cfg.PlatformHTTPAddr = value
	}
	if value, ok := patch["platform_bridge_addr"].(string); ok {
		cfg.PlatformBridgeAddr = value
	}
	if value, ok := patch["platform_remote_debugging_port"].(float64); ok {
		cfg.PlatformDebugPort = int(value)
	}
	if value, ok := patch["auto_update_enabled"].(bool); ok {
		cfg.AutoUpdateEnabled = value
	}
	if value, ok := patch["auto_update_repo"].(string); ok {
		cfg.AutoUpdateRepo = value
	}
	if value, ok := patch["auto_update_interval_minutes"].(float64); ok {
		cfg.AutoUpdateInterval = int(value)
	}

	if err := config.Save(cfg); err != nil {
		return publicConfig(r.cfg), err
	}
	r.cfg = cfg
	return publicConfig(cfg), nil
}

func publicConfig(cfg config.Config) any {
	type public config.Config
	out := public(cfg)
	out.AdminPIN = config.PINHash{}
	for i := range out.Accounts {
		out.Accounts[i].DeviceToken = ""
	}
	return out
}

type PlatformReimportCommand struct{}

func (PlatformReimportCommand) Name() string { return "platform.reimport" }

func (PlatformReimportCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	cfg, err := config.ImportFromSelection()
	if err == nil {
		r.cfg = cfg
	}
	return publicConfig(cfg), err
}

type PlatformSelectChromeCommand struct{}

func (PlatformSelectChromeCommand) Name() string { return "platform.selectChrome" }

func (PlatformSelectChromeCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	path, ok, err := nativeui.SelectFile(
		"Select chrome.exe",
		`C:\Program Files\Google\Chrome\Application`,
		"Chrome executable (chrome.exe)\x00chrome.exe\x00Applications (*.exe)\x00*.exe\x00All files (*.*)\x00*.*\x00",
	)
	if err != nil || !ok {
		return r.cfg, err
	}
	r.cfg.ChromePath = path
	return publicConfig(r.cfg), config.Save(r.cfg)
}
