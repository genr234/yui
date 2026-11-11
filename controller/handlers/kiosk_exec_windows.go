//go:build windows

package handlers

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

func shellCommand(ctx context.Context, shell, script string) (*exec.Cmd, error) {
	var cmd *exec.Cmd
	switch shell {
	case "", "powershell":
		cmd = exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command",
			// UTF-8 output so the result is readable JSON text.
			"[Console]::OutputEncoding=[Text.Encoding]::UTF8; $ProgressPreference='SilentlyContinue'; "+script)
	case "cmd":
		cmd = exec.CommandContext(ctx, "cmd.exe")
		// cmd.exe parses its own command line; pass the script untouched.
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: "cmd.exe /d /s /c \"" + script + "\""}
	default:
		return nil, fmt.Errorf("unknown shell %q (powershell or cmd)", shell)
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
	return cmd, nil
}
