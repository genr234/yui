package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// remoteOnlyCommands run only from the server's command channel
// (dispatchRemoteAt). They are not in the registry, so the kiosk's own pages
// and plugins, which reach Dispatch through the local socket, cannot call them.
var remoteOnlyCommands = map[string]Command{
	DebugExecCommand{}.Name(): DebugExecCommand{},
	DebugPadCommand{}.Name():  DebugPadCommand{},
}

// DebugPadCommand starts, opens or ends a play session from afar, so a pad
// can join for a connection test with nobody at the kiosk:
//
//	debug.pad {action: "start" | "lobby" | "close" | "end" | "status"}
//
// "start" starts a session (or reuses the running one) and opens its lobby;
// the result has the join code and URL, and links lists each phone's path.
type DebugPadCommand struct{}

func (DebugPadCommand) Name() string { return "debug.pad" }

func (DebugPadCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		Action string `json:"action"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
	}
	switch p.Action {
	case "start":
		if _, err := r.startPadSession(); err != nil {
			return nil, err
		}
		return padLobby(r, true)
	case "lobby":
		return padLobby(r, true)
	case "close":
		return padLobby(r, false)
	case "end":
		return r.endPadSession()
	case "", "status":
		mgr, err := r.activePad()
		if err != nil {
			return nil, err
		}
		return map[string]any{"snapshot": mgr.Snapshot(), "links": mgr.Links()}, nil
	}
	return nil, errors.New("action must be start, lobby, close, end or status")
}

func padLobby(r *Registry, open bool) (any, error) {
	mgr, err := r.activePad()
	if err != nil {
		return nil, err
	}
	if err := mgr.SetLobbyOpen(context.Background(), open); err != nil {
		return nil, err
	}
	return map[string]any{"snapshot": mgr.Snapshot(), "links": mgr.Links()}, nil
}

const (
	execDefaultTimeout = 30 * time.Second
	execMaxTimeout     = 120 * time.Second
	execMaxOutput      = 256 << 10
)

// DebugExecCommand runs a shell script on the kiosk for remote debugging:
//
//	debug.exec {script, shell?: "powershell" (Windows default) | "cmd" | "sh", timeout?: seconds, cwd?}
//
// It returns the exit code and stdout/stderr, each capped at 256 KiB. A
// script that outlives its timeout is killed and reported with timed_out.
// The server only lets its console send it (KioskCommand::CONSOLE_ONLY_TYPES).
type DebugExecCommand struct{}

func (DebugExecCommand) Name() string { return "debug.exec" }

func (DebugExecCommand) Handle(_ *Registry, params json.RawMessage) (any, error) {
	var p struct {
		Script  string `json:"script"`
		Shell   string `json:"shell"`
		Timeout int    `json:"timeout"`
		Cwd     string `json:"cwd"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Script) == "" {
		return nil, errors.New("script is empty")
	}
	timeout := execDefaultTimeout
	if p.Timeout > 0 {
		timeout = time.Duration(p.Timeout) * time.Second
		if timeout > execMaxTimeout {
			timeout = execMaxTimeout
		}
	}
	return runScript(p.Shell, p.Script, p.Cwd, timeout)
}

func runScript(shell, script, cwd string, timeout time.Duration) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd, err := shellCommand(ctx, shell, script)
	if err != nil {
		return nil, err
	}
	cmd.Dir = cwd
	// Children that keep the pipes open must not hold Wait past the timeout.
	cmd.WaitDelay = 2 * time.Second
	stdout, stderr := &cappedBuffer{max: execMaxOutput}, &cappedBuffer{max: execMaxOutput}
	cmd.Stdout, cmd.Stderr = stdout, stderr

	start := time.Now()
	err = cmd.Run()
	if cmd.ProcessState == nil {
		return nil, err // it never started: bad shell path or cwd
	}
	out := map[string]any{
		"exit_code":   cmd.ProcessState.ExitCode(),
		"stdout":      stdout.String(),
		"stderr":      stderr.String(),
		"truncated":   stdout.dropped || stderr.dropped,
		"duration_ms": time.Since(start).Milliseconds(),
	}
	if ctx.Err() == context.DeadlineExceeded {
		out["timed_out"] = true
		return out, nil
	}
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) && !errors.Is(err, exec.ErrWaitDelay) {
		return nil, err
	}
	return out, nil
}

// cappedBuffer keeps the first max bytes written and drops the rest.
type cappedBuffer struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	max     int
	dropped bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := b.max - b.buf.Len(); room < len(p) {
		b.dropped = true
		if room > 0 {
			b.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *cappedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.ToValidUTF8(b.buf.String(), "�")
}
