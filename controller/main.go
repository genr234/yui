package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"kiosk/controller/internal/config"
	controllerlog "kiosk/controller/internal/logging"
	"kiosk/controller/internal/nativeui"
	"kiosk/controller/internal/singleinstance"
	"kiosk/controller/internal/status"
	"kiosk/controller/internal/supervisor"
	"kiosk/controller/internal/version"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version":
			fmt.Println(version.Version)
			return nil
		case "--check":
			report, err := runCheck()
			if report != "" {
				fmt.Println(report)
			}
			return err
		case "--menu":
			return runMenu()
		}
	}

	lock, alreadyRunning, err := singleinstance.Acquire("Local\\YuiKioskController")
	if err != nil {
		return fmt.Errorf("single-instance guard failed: %w", err)
	}
	if alreadyRunning {
		return nil
	}
	defer lock.Release()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logFile, err := controllerlog.Setup(cfg.LogPath)
	if err != nil {
		return err
	}
	defer logFile.Close()

	defer func() {
		if value := recover(); value != nil {
			log.Printf("controller panic: %v", value)
		}
	}()

	logControllerStartup(cfg)

	statusWriter := status.New(cfg.StatusPath, version.Version, cfg.ConfigPath, cfg.ChromePath)
	statusWriter.Write(status.State{
		Event:      "controller_started",
		ChromePath: cfg.ChromePath,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runner := supervisor.New(cfg, statusWriter)
	if err := runner.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("supervisor stopped: %w", err)
	}

	log.Printf("controller shutting down")
	statusWriter.Write(status.State{
		Event:      "controller_stopped",
		ChromePath: cfg.ChromePath,
	})

	return nil
}

func runCheck() (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}

	report := buildCheckReport(cfg)

	logFile, logErr := controllerlog.Setup(cfg.LogPath)
	if logErr == nil {
		defer logFile.Close()
		log.Printf("controller check")
		log.Printf("%s", report)
	}

	statusWriter := status.New(cfg.StatusPath, version.Version, cfg.ConfigPath, cfg.ChromePath)
	statusWriter.Write(status.State{
		Event:      "controller_check",
		ChromePath: cfg.ChromePath,
	})

	return report, nil
}

func buildCheckReport(cfg config.Config) string {
	chromeState := "missing"
	if _, err := os.Stat(cfg.ChromePath); err == nil || filepath.Base(cfg.ChromePath) == cfg.ChromePath {
		chromeState = "ok"
	}

	configState := "generated/default"
	if !cfg.UsingDefaults {
		configState = "loaded"
	}

	return fmt.Sprintf(
		"Yui Kiosk Controller\nVersion: %s\nConfig: %s (%s)\nChrome path: %s (%s)\nUser data dir: %s\nLog path: %s\nStatus path: %s",
		version.Version,
		cfg.ConfigPath,
		configState,
		cfg.ChromePath,
		chromeState,
		cfg.UserDataDir,
		cfg.LogPath,
		cfg.StatusPath,
	)
}

func runMenu() error {
	choice, err := nativeui.AskTryContinue(
		"Yui Controller Menu",
		"Tap Try Again for diagnostics.\nTap Continue for recovery actions.\nTap Cancel to exit.",
	)
	if err != nil {
		return err
	}

	switch choice {
	case "try":
		report, err := runCheck()
		if err != nil {
			_ = nativeui.ShowError("Yui Controller Check", err.Error())
			return err
		}
		_ = nativeui.ShowInfo("Yui Controller Check", report)
		return nil
	case "continue":
		return runRecoveryMenu()
	default:
		return nil
	}
}

func runRecoveryMenu() error {
	choice, err := nativeui.AskYesNoCancel(
		"Yui Recovery",
		"Tap Yes to select chrome.exe.\nTap No to re-import the original kiosk chrome.bat.\nTap Cancel to exit.",
	)
	if err != nil {
		return err
	}

	switch choice {
	case "yes":
		return runSelectChromePath()
	case "no":
		return runReimportConfig()
	default:
		return nil
	}
}

func runSelectChromePath() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	selectedPath, ok, err := nativeui.SelectFile(
		"Select chrome.exe",
		`C:\Program Files\Google\Chrome\Application`,
		"Chrome executable (chrome.exe)\x00chrome.exe\x00Applications (*.exe)\x00*.exe\x00All files (*.*)\x00*.*\x00",
	)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	cfg.ChromePath = selectedPath
	if err := config.Save(cfg); err != nil {
		return err
	}

	_ = nativeui.ShowInfo("Yui Recovery", "Saved the selected chrome.exe path to controller.json.")
	return nil
}

func runReimportConfig() error {
	cfg, err := config.ImportFromSelection()
	if err != nil {
		_ = nativeui.ShowError("Yui Recovery", err.Error())
		return err
	}

	_ = nativeui.ShowInfo(
		"Yui Recovery",
		fmt.Sprintf("Re-imported kiosk config.\n\nConfig: %s\nImported from: %s", cfg.ConfigPath, cfg.ImportedFrom),
	)
	return nil
}

func logControllerStartup(cfg config.Config) {
	log.Printf("controller starting")
	log.Printf("version: %s", version.Version)
	log.Printf("config path: %s", cfg.ConfigPath)
	log.Printf("config dir: %s", cfg.ConfigDir)
	if cfg.ImportedFrom != "" {
		log.Printf("imported kiosk config from: %s", cfg.ImportedFrom)
	}
	if cfg.ImportWarning != "" {
		log.Printf("config import warning: %s", cfg.ImportWarning)
	}
	if cfg.UsingDefaults {
		log.Printf("config file not found; using defaults")
	}
}
