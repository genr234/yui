package handlers

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"kiosk/controller/internal/logging"
	"kiosk/controller/internal/pad/pads"
	"kiosk/controller/internal/remotecontrol"
	"kiosk/controller/internal/telemetry"
	"kiosk/controller/internal/version"
)

// KioskDebugCommands let someone away from the kiosk find out what is wrong:
// a diagnostics snapshot, log tails, files under the native and runtimes
// folders, a screenshot of the whole desktop and stopping the native program.
// They run from the control room's command channel (remoteCommandAllowed).
func KioskDebugCommands() []Command {
	return []Command{DebugDiagnosticsCommand{}, DebugLogsCommand{}, DebugListCommand{}, DebugReadCommand{}, DebugScreenCommand{}, DebugNativeStopCommand{}}
}

type DebugDiagnosticsCommand struct{}

func (DebugDiagnosticsCommand) Name() string { return "debug.diagnostics" }
func (DebugDiagnosticsCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	out := map[string]any{
		"health": telemetry.Snapshot(version.Version, version.Commit),
		"now":    time.Now().UTC().Format(time.RFC3339Nano),
	}
	if mgr := r.nativeIfStarted(); mgr != nil {
		out["native"] = mgr.Status()
	} else {
		out["native"] = "not started since the controller started"
	}
	snap := r.padSnapshot()
	out["pad_session"] = snap.Session
	r.padMu.Lock()
	hub := r.padRouter
	r.padMu.Unlock()
	if hub == nil {
		out["input_hub"] = "not started"
	} else {
		layer, _ := hub.Stack().Top()
		players := []map[string]any{}
		for _, p := range snap.Players {
			slot, ok := hub.SlotOf(p.ID)
			entry := map[string]any{"player": p}
			if ok {
				entry["slot"] = slot
			}
			players = append(players, entry)
		}
		out["players"] = players
		out["input_hub"] = map[string]any{"top_layer": layer.String(), "sensitive": hub.Sensitive()}
		if a, ok := hub.Pads().(*pads.Adapter); ok && a != nil {
			out["virtual_pads"] = a.Stats()
		}
	}
	r.padMu.Lock()
	padMgr := r.padMgr
	r.padMu.Unlock()
	if padMgr != nil {
		inputs := map[string]any{}
		for _, in := range padMgr.Inputs() {
			inputs[in.PlayerID] = in.State
		}
		out["latest_inputs"] = inputs
	}
	return out, nil
}

// ---- logs and files ------------------------------------------------------

// debugRoots are the folders debug.ls and debug.read may look in: the
// native programs' data and logs, and the runtimes. Not the rest of the
// config folder, which holds the kiosk's credentials and store.
func (r *Registry) debugRoots() map[string]string {
	dir := r.snapshotConfig().ConfigDir
	return map[string]string{
		"native":   filepath.Join(dir, "native"),
		"runtimes": filepath.Join(dir, "runtimes"),
	}
}

// debugPath resolves "native/..." or "runtimes/..." inside its root.
func (r *Registry) debugPath(rel string) (string, error) {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	root, rest, _ := strings.Cut(rel, "/")
	base, ok := r.debugRoots()[root]
	if !ok {
		return "", fmt.Errorf("path must start with native/ or runtimes/")
	}
	full := filepath.Join(base, filepath.FromSlash(rest))
	if full != base && !underRoot(full, base) {
		return "", errors.New("path leaves its folder")
	}
	return full, nil
}

func underRoot(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

const debugMaxBytes = 512 << 10

// tailLines returns the last n lines of path (reading at most
// debugMaxBytes), optionally only those matching grep.
func tailLines(path string, n int, grep string) (map[string]any, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	start := int64(0)
	if info.Size() > debugMaxBytes {
		start = info.Size() - debugMaxBytes
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	var re *regexp.Regexp
	if grep != "" {
		if re, err = regexp.Compile("(?i)" + grep); err != nil {
			return nil, fmt.Errorf("grep: %w", err)
		}
	}
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if re != nil && !re.MatchString(line) {
			continue
		}
		lines = append(lines, line)
		if len(lines) > n {
			lines = lines[1:]
		}
	}
	return map[string]any{"path": path, "size": info.Size(), "modified": info.ModTime().UTC(), "lines": lines}, nil
}

type DebugLogsCommand struct{}

func (DebugLogsCommand) Name() string { return "debug.logs" }

// debug.logs {source: "controller" (default) | "native", app?, lines?, grep?}.
// "native" reads the newest stdout/stderr log of a native program.
func (DebugLogsCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		Source string `json:"source"`
		App    string `json:"app"`
		Lines  int    `json:"lines"`
		Grep   string `json:"grep"`
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &p)
	}
	if p.Lines <= 0 {
		p.Lines = 200
	}
	p.Lines = min(p.Lines, 2000)
	switch p.Source {
	case "", "controller":
		path := logging.Path()
		if path == "" {
			return nil, errors.New("no log file in use")
		}
		return tailLines(path, p.Lines, p.Grep)
	case "native":
		dir := filepath.Join(r.snapshotConfig().ConfigDir, "native", "logs")
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		type cand struct {
			path string
			mod  time.Time
		}
		var files []cand
		for _, e := range entries {
			if e.IsDir() || (p.App != "" && !strings.Contains(e.Name(), p.App)) {
				continue
			}
			if info, err := e.Info(); err == nil {
				files = append(files, cand{filepath.Join(dir, e.Name()), info.ModTime()})
			}
		}
		if len(files) == 0 {
			return nil, errors.New("no native logs")
		}
		sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
		return tailLines(files[0].path, p.Lines, p.Grep)
	}
	return nil, fmt.Errorf("unknown source %q", p.Source)
}

type DebugListCommand struct{}

func (DebugListCommand) Name() string { return "debug.ls" }

// debug.ls {path: "native/..." | "runtimes/...", depth?} lists files.
func (DebugListCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		Path  string `json:"path"`
		Depth int    `json:"depth"`
	}
	_ = json.Unmarshal(params, &p)
	if p.Path == "" {
		p.Path = "native"
	}
	if p.Depth <= 0 {
		p.Depth = 2
	}
	p.Depth = min(p.Depth, 6)
	root, err := r.debugPath(p.Path)
	if err != nil {
		return nil, err
	}
	var entries []map[string]any
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if rel == "." {
			return nil
		}
		depth := strings.Count(filepath.ToSlash(rel), "/") + 1
		if d.IsDir() && depth >= p.Depth {
			entries = append(entries, map[string]any{"path": filepath.ToSlash(rel) + "/"})
			return filepath.SkipDir
		}
		e := map[string]any{"path": filepath.ToSlash(rel)}
		if d.IsDir() {
			e["path"] = filepath.ToSlash(rel) + "/"
		} else if info, err := d.Info(); err == nil {
			e["size"] = info.Size()
			e["modified"] = info.ModTime().UTC()
		}
		entries = append(entries, e)
		if len(entries) >= 500 {
			return errors.New("stop")
		}
		return nil
	})
	return map[string]any{"root": root, "entries": entries, "truncated": err != nil}, nil
}

type DebugReadCommand struct{}

func (DebugReadCommand) Name() string { return "debug.read" }

// debug.read {path, lines?, grep?} tails a text file under native/ or runtimes/.
func (DebugReadCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		Path  string `json:"path"`
		Lines int    `json:"lines"`
		Grep  string `json:"grep"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	full, err := r.debugPath(p.Path)
	if err != nil {
		return nil, err
	}
	if p.Lines <= 0 {
		p.Lines = 400
	}
	return tailLines(full, min(p.Lines, 4000), p.Grep)
}

// ---- screen and programs ----------------------------------------------------

type DebugScreenCommand struct{}

func (DebugScreenCommand) Name() string { return "debug.screen" }
func (DebugScreenCommand) Handle(_ *Registry, params json.RawMessage) (any, error) {
	var p struct {
		MaxSide int `json:"max_side"`
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &p)
	}
	if p.MaxSide <= 0 || p.MaxSide > 1920 {
		p.MaxSide = 1280
	}
	return remotecontrol.CaptureDesktop(p.MaxSide)
}

type DebugNativeStopCommand struct{}

func (DebugNativeStopCommand) Name() string { return "debug.nativeStop" }

// debug.nativeStop {force?} stops whichever native program is running.
func (DebugNativeStopCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var p struct {
		Force bool `json:"force"`
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &p)
	}
	mgr := r.nativeIfStarted()
	if mgr == nil {
		return map[string]string{"state": "none running"}, nil
	}
	return mgr.Stop("", p.Force)
}
