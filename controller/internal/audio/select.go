package audio

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"kiosk/controller/internal/probe"
)

// Backend names a capture backend.
type Backend string

const (
	BackendAuto            Backend = ""
	BackendProcessLoopback Backend = "process-loopback"
	BackendPCMSocket       Backend = "pcm-socket"
)

// Decision is the backend chosen from a probe report and why.
type Decision struct {
	Backend Backend
	Reason  string
}

// Choose applies D7: process loopback when the probe says the Windows build
// supports it, otherwise the PCM socket.
func Choose(r probe.Report) Decision {
	if r.ProcessLoopbackSupported {
		return Decision{BackendProcessLoopback, fmt.Sprintf("windows build %d >= %d", r.WindowsBuild, probe.ProcessLoopbackMinBuild)}
	}
	if r.WindowsBuild > 0 {
		return Decision{BackendPCMSocket, fmt.Sprintf("windows build %d < %d", r.WindowsBuild, probe.ProcessLoopbackMinBuild)}
	}
	return Decision{BackendPCMSocket, "process loopback needs Windows (os " + r.OS + ")"}
}

// AutoSource picks a backend at Start: the forced one, else Choose(Probe).
// If process loopback fails to activate (no PID, process gone, no audio
// endpoint, API missing) it falls back to the PCM socket and records why.
type AutoSource struct {
	Probe       probe.Report
	Force       Backend
	PID         uint32 // process (tree) to capture with loopback
	SocketAddr  string // PCM socket address; empty = DefaultPCMSocketAddr
	newLoopback func(pid uint32) Source

	mu       sync.Mutex
	chosen   Source
	decision Decision
}

func (a *AutoSource) Name() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.chosen == nil {
		return "auto"
	}
	return a.chosen.Name()
}

// Decision reports the backend in use and why (after Start).
func (a *AutoSource) Decision() Decision {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.decision
}

func (a *AutoSource) Err() error {
	a.mu.Lock()
	src := a.chosen
	a.mu.Unlock()
	if src == nil {
		return nil
	}
	return src.Err()
}

func (a *AutoSource) Start(ctx context.Context) (<-chan Chunk, error) {
	d := Decision{Backend: a.Force, Reason: "forced"}
	if d.Backend == BackendAuto {
		d = Choose(a.Probe)
	}
	if d.Backend == BackendProcessLoopback {
		var src Source
		if a.newLoopback != nil {
			src = a.newLoopback(a.PID)
		} else {
			src = &ProcessLoopback{PID: a.PID}
		}
		chunks, err := src.Start(ctx)
		if err == nil {
			a.set(src, d)
			return chunks, nil
		}
		if a.Force == BackendProcessLoopback {
			return nil, err
		}
		d = Decision{BackendPCMSocket, "process loopback failed: " + err.Error()}
	}
	if d.Backend != BackendPCMSocket {
		return nil, errors.New("audio: unknown backend " + string(d.Backend))
	}
	src := &PCMSocket{Addr: a.SocketAddr}
	chunks, err := src.Start(ctx)
	if err != nil {
		return nil, err
	}
	a.set(src, d)
	return chunks, nil
}

func (a *AutoSource) set(src Source, d Decision) {
	a.mu.Lock()
	a.chosen, a.decision = src, d
	a.mu.Unlock()
}
