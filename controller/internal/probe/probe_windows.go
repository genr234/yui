//go:build windows

package probe

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const powershellTimeout = 10 * time.Second

const (
	audioEndpointsScript = "Get-PnpDevice -Class AudioEndpoint | Select-Object FriendlyName,Status | ConvertTo-Json"
	gpusScript           = "Get-CimInstance Win32_VideoController | Select-Object Name | ConvertTo-Json"
)

func collect(ctx context.Context) Report {
	report := Report{OS: runtime.GOOS}

	if info := windows.RtlGetVersion(); info != nil {
		report.WindowsBuild = int(info.BuildNumber)
	} else {
		report.Errors = append(report.Errors, "windows build: RtlGetVersion returned nil")
	}

	if out, err := runPowerShell(ctx, audioEndpointsScript); err != nil {
		report.Errors = append(report.Errors, "audio endpoints: "+err.Error())
	} else if endpoints, err := parseAudioEndpoints(out); err != nil {
		report.Errors = append(report.Errors, err.Error())
	} else {
		report.AudioEndpoints = endpoints
	}

	if out, err := runPowerShell(ctx, gpusScript); err != nil {
		report.Errors = append(report.Errors, "gpus: "+err.Error())
	} else if gpus, err := parseGPUs(out); err != nil {
		report.Errors = append(report.Errors, err.Error())
	} else {
		report.GPUs = gpus
	}

	return report
}

func runPowerShell(ctx context.Context, script string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, powershellTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("powershell timed out after %s", powershellTimeout)
	}
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%w: %s", err, firstLine(msg))
		}
		return nil, err
	}
	return out, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
