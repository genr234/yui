package supervisor

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"kiosk/controller/internal/config"
	"kiosk/controller/internal/nativeui"
	"kiosk/controller/internal/status"
)

type Supervisor struct {
	cfg    config.Config
	status *status.Writer
}

func New(cfg config.Config, statusWriter *status.Writer) *Supervisor {
	return &Supervisor{cfg: cfg, status: statusWriter}
}

func (s *Supervisor) Run(ctx context.Context) error {
	baseDelay := time.Duration(s.cfg.RestartDelaySeconds) * time.Second
	maxDelay := time.Duration(s.cfg.RestartMaxSeconds) * time.Second
	delay := baseDelay
	restartCount := 0

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		args := chromeArgs(s.cfg)
		log.Printf("starting chrome: path=%q args=%q", s.cfg.ChromePath, args)
		s.status.Write(status.State{
			Event:        "starting_chrome",
			ChromePath:   s.cfg.ChromePath,
			RestartCount: restartCount,
		})

		cmd, usedPath, selectedPath, err := startChrome(ctx, s.cfg.ChromePath, args)
		if err != nil {
			log.Printf("chrome start failed: %v", err)
			nextRestart := time.Now().Add(delay)
			s.status.Write(status.State{
				Event:         "chrome_start_failed",
				ChromePath:    s.cfg.ChromePath,
				RestartCount:  restartCount,
				NextRestartAt: nextRestart.Format(time.RFC3339),
				LastError:     err.Error(),
			})
			if err := sleep(ctx, delay); err != nil {
				return err
			}
			delay = nextDelay(delay, maxDelay)
			continue
		}

		if usedPath != "" && usedPath != s.cfg.ChromePath {
			log.Printf("chrome path resolved to: %s", usedPath)
		}
		if selectedPath {
			s.cfg.ChromePath = usedPath
			if err := config.Save(s.cfg); err != nil {
				log.Printf("selected chrome path could not be saved: %v", err)
			} else {
				log.Printf("selected chrome path saved to config")
			}
		}

		startedAt := time.Now()
		s.status.Write(status.State{
			Event:        "chrome_started",
			ChromePath:   usedPath,
			ChromePID:    cmd.Process.Pid,
			RestartCount: restartCount,
		})

		err = cmd.Wait()
		if err == nil {
			log.Printf("chrome exited")
			if s.monitorDetachedChrome(ctx, usedPath, restartCount) {
				delay = baseDelay
				continue
			}
		} else if errors.Is(ctx.Err(), context.Canceled) {
			return ctx.Err()
		} else {
			log.Printf("chrome exited with error: %v", err)
		}

		if time.Since(startedAt) >= 30*time.Second {
			delay = baseDelay
		}
		restartCount++
		nextRestart := time.Now().Add(delay)
		log.Printf("restarting chrome in %s", delay)
		lastError := ""
		if err != nil {
			lastError = err.Error()
		}
		s.status.Write(status.State{
			Event:         "chrome_exited",
			ChromePath:    usedPath,
			RestartCount:  restartCount,
			NextRestartAt: nextRestart.Format(time.RFC3339),
			LastError:     lastError,
		})
		if err := sleep(ctx, delay); err != nil {
			return err
		}
		delay = nextDelay(delay, maxDelay)
	}
}

func (s *Supervisor) monitorDetachedChrome(ctx context.Context, usedPath string, restartCount int) bool {
	if !s.cfg.PlatformEnabled || s.cfg.PlatformDebugPort <= 0 {
		return false
	}
	if !chromeDebugReachable(ctx, s.cfg.PlatformDebugPort, 5*time.Second) {
		return false
	}

	log.Printf("chrome launcher exited but remote debugging is still reachable; monitoring existing chrome")
	s.status.Write(status.State{
		Event:        "chrome_detached",
		ChromePath:   usedPath,
		RestartCount: restartCount,
	})

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return true
		case <-ticker.C:
			if !chromeDebugReachable(ctx, s.cfg.PlatformDebugPort, 0) {
				log.Printf("detached chrome remote debugging is no longer reachable")
				return false
			}
		}
	}
}

func startChrome(ctx context.Context, configuredPath string, args []string) (*exec.Cmd, string, bool, error) {
	var lastErr error

	for _, chromePath := range chromePathCandidates(configuredPath) {
		log.Printf("trying chrome path: %q", chromePath)
		cmd := exec.CommandContext(ctx, chromePath, args...)
		if err := cmd.Start(); err != nil {
			lastErr = err
			continue
		}
		go terminateOnCancel(ctx, cmd)
		return cmd, chromePath, false, nil
	}

	if selectedPath, ok, err := nativeui.SelectFile(
		"Select chrome.exe",
		`C:\Program Files\Google\Chrome\Application`,
		"Chrome executable (chrome.exe)\x00chrome.exe\x00Applications (*.exe)\x00*.exe\x00All files (*.*)\x00*.*\x00",
	); err != nil {
		log.Printf("chrome file picker failed: %v", err)
	} else if ok {
		log.Printf("trying selected chrome path: %q", selectedPath)
		cmd := exec.CommandContext(ctx, selectedPath, args...)
		if err := cmd.Start(); err != nil {
			lastErr = err
		} else {
			go terminateOnCancel(ctx, cmd)
			return cmd, selectedPath, true, nil
		}
	}

	return nil, "", false, lastErr
}

func chromePathCandidates(configuredPath string) []string {
	return unique([]string{
		configuredPath,
		`/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`,
		`/Applications/Chromium.app/Contents/MacOS/Chromium`,
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		filepath.Join(`C:\Program Files`, "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(`C:\Program Files (x86)`, "Google", "Chrome", "Application", "chrome.exe"),
		"google-chrome",
		"chromium",
		"chrome.exe",
	})
}

func chromeArgs(cfg config.Config) []string {
	args := make([]string, 0, len(cfg.Flags)+1)
	skipNext := false
	hasRemoteDebugging := false
	for _, flag := range cfg.Flags {
		if skipNext {
			skipNext = false
			continue
		}
		lower := strings.ToLower(flag)
		if strings.HasPrefix(lower, "--remote-debugging-port=") {
			hasRemoteDebugging = true
			continue
		}
		if lower == "--remote-debugging-port" {
			hasRemoteDebugging = true
			skipNext = true
			continue
		}
		if strings.HasPrefix(lower, "--user-data-dir=") {
			continue
		}
		if lower == "--user-data-dir" {
			skipNext = true
			continue
		}
		args = append(args, flag)
	}
	if cfg.UserDataDir != "" {
		args = append(args, "--user-data-dir="+cfg.UserDataDir)
	}
	if cfg.PlatformEnabled && cfg.PlatformDebugPort > 0 && !hasRemoteDebugging {
		args = append(args, fmt.Sprintf("--remote-debugging-port=%d", cfg.PlatformDebugPort))
	}
	launchURL := cfg.URL
	if launchURL != "" {
		args = append(args, launchURL)
	}
	return args
}

func chromeDebugReachable(ctx context.Context, port int, wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	for {
		if isChromeDebugReachable(ctx, port) {
			return true
		}
		if wait <= 0 || time.Now().After(deadline) {
			return false
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}

func isChromeDebugReachable(ctx context.Context, port int) bool {
	reqCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	url := fmt.Sprintf("http://127.0.0.1:%d/json/version", port)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func nextDelay(current time.Duration, max time.Duration) time.Duration {
	if max <= 0 || current >= max {
		return current
	}
	next := current * 2
	if next > max {
		return max
	}
	return next
}

func sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func unique(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))

	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}

	return result
}

func terminateOnCancel(ctx context.Context, cmd *exec.Cmd) {
	<-ctx.Done()
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
