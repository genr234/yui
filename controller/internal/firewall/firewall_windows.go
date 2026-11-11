//go:build windows

// Package firewall lets phones reach the controller's pad links through
// Windows Firewall. Kiosks usually sit on a network Windows calls Public,
// where unsolicited inbound UDP is dropped, so a phone's first ICE check
// never arrives and the link falls back to the server, where the pad's
// input can't travel. One inbound UDP rule for controller.exe fixes that.
package firewall

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows/registry"
)

// RuleName is the inbound rule this package keeps.
const RuleName = "Yui kiosk pad links (UDP)"

const createNoWindow = 0x08000000

// Ensure adds the rule for the running executable if it is missing. Adding
// it needs admin rights: the controller asks Windows to elevate netsh only
// when that happens silently (an admin account with UAC set to elevate
// without prompting), so a kiosk never shows a consent dialog. Otherwise it
// returns an error saying how to add the rule by hand.
func Ensure(ctx context.Context) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if present(ctx, exe) {
		return "present", nil
	}
	if !silentElevation() {
		return "", fmt.Errorf("rule %q missing and adding it would show a UAC prompt; run as admin: %s", RuleName, "netsh "+strings.Join(addArgs(exe), " "))
	}
	// Start-Process -Verb RunAs is ShellExecute's elevation, which UAC grants
	// without a dialog here. Arguments go through the environment so the
	// path needs no PowerShell quoting.
	script := `$p = Start-Process -FilePath netsh.exe -ArgumentList $env:YUI_FW_ARGS -Verb RunAs -WindowStyle Hidden -Wait -PassThru; exit $p.ExitCode`
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	cmd.Env = append(os.Environ(), "YUI_FW_ARGS="+strings.Join(quoteArgs(addArgs(exe)), " "))
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("add rule: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if !present(ctx, exe) {
		return "", errors.New("add rule: netsh finished but the rule is not there")
	}
	return "added", nil
}

func addArgs(exe string) []string {
	return []string{"advfirewall", "firewall", "add", "rule", "name=" + RuleName, "dir=in", "action=allow",
		"program=" + exe, "protocol=UDP", "profile=any", "enable=yes"}
}

// quoteArgs wraps values with spaces for netsh's command line.
func quoteArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if k, v, ok := strings.Cut(a, "="); ok && strings.ContainsAny(v, " \t") {
			a = k + `="` + v + `"`
		}
		out[i] = a
	}
	return out
}

// present reports whether an enabled rule with RuleName allows this exe.
// Reading rules needs no admin rights.
func present(ctx context.Context, exe string) bool {
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "netsh", "advfirewall", "firewall", "show", "rule", "name="+RuleName, "verbose")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), strings.ToLower(exe))
}

// silentElevation: UAC is on but elevates admins without asking
// (ConsentPromptBehaviorAdmin 0), or UAC is off. Either way no dialog.
func silentElevation() bool {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	if lua, _, err := k.GetIntegerValue("EnableLUA"); err == nil && lua == 0 {
		return true
	}
	consent, _, err := k.GetIntegerValue("ConsentPromptBehaviorAdmin")
	return err == nil && consent == 0
}
