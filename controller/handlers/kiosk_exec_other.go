//go:build !windows

package handlers

import (
	"context"
	"fmt"
	"os/exec"
)

func shellCommand(ctx context.Context, shell, script string) (*exec.Cmd, error) {
	switch shell {
	case "", "sh":
		return exec.CommandContext(ctx, "/bin/sh", "-c", script), nil
	default:
		return nil, fmt.Errorf("unknown shell %q (sh)", shell)
	}
}
