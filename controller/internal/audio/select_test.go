package audio

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"kiosk/controller/internal/probe"
)

func TestChoose(t *testing.T) {
	if d := Choose(probe.Report{OS: "windows", WindowsBuild: 22631, ProcessLoopbackSupported: true}); d.Backend != BackendProcessLoopback {
		t.Fatalf("%+v", d)
	}
	if d := Choose(probe.Report{OS: "windows", WindowsBuild: 19045}); d.Backend != BackendPCMSocket {
		t.Fatalf("%+v", d)
	}
	if d := Choose(probe.Report{OS: "darwin"}); d.Backend != BackendPCMSocket {
		t.Fatalf("%+v", d)
	}
}

type failingSource struct{ srcErr }

func (*failingSource) Name() string { return "failing" }
func (*failingSource) Start(context.Context) (<-chan Chunk, error) {
	return nil, errors.New("AUDCLNT_E_DEVICE_INVALIDATED")
}

func TestAutoSourceFallsBackToSocket(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &AutoSource{
		Probe:       probe.Report{OS: "windows", WindowsBuild: 26100, ProcessLoopbackSupported: true},
		PID:         1234,
		SocketAddr:  "127.0.0.1:0",
		newLoopback: func(uint32) Source { return &failingSource{} },
	}
	if _, err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if d := a.Decision(); d.Backend != BackendPCMSocket || a.Name() != "pcm-socket" {
		t.Fatalf("%+v %s", d, a.Name())
	}

	forced := &AutoSource{Force: BackendProcessLoopback, newLoopback: func(uint32) Source { return &failingSource{} }}
	if _, err := forced.Start(ctx); err == nil {
		t.Fatal("forced loopback should not fall back")
	}
}

func TestLoopbackUnsupportedOffWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("real backend")
	}
	if _, err := (&ProcessLoopback{PID: 1}).Start(context.Background()); !errors.Is(err, ErrLoopbackUnsupported) {
		t.Fatalf("got %v", err)
	}
}
