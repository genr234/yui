package audio

import (
	"context"
	"errors"
)

// ErrLoopbackUnsupported is returned by ProcessLoopback.Start on platforms
// without WASAPI process loopback.
var ErrLoopbackUnsupported = errors.New("audio: process loopback capture is not supported on this platform")

// ProcessLoopback captures the audio one process (and, by default, its child
// processes) renders, using WASAPI process loopback
// (AUDIOCLIENT_ACTIVATION_TYPE_PROCESS_LOOPBACK, Windows build >= 20348).
// It delivers 48 kHz stereo s16 converted to float. Use it with a native
// session's PID, or Chrome's browser PID for web apps (Chrome renders audio
// in a child "audio service" process, hence the process tree).
type ProcessLoopback struct {
	srcErr
	PID uint32
	// ExcludeTree captures everything except the process tree instead
	// (PROCESS_LOOPBACK_MODE_EXCLUDE_TARGET_PROCESS_TREE).
	ExcludeTree bool
}

func (p *ProcessLoopback) Name() string { return "process-loopback" }

// Start activates the loopback client and begins capture. Activation
// failures (process gone, feature missing, no audio endpoint) are returned
// here, so the caller can fall back to the PCM socket.
func (p *ProcessLoopback) Start(ctx context.Context) (<-chan Chunk, error) {
	return p.start(ctx)
}
