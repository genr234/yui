package router

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"sync"
	"testing"
	"time"

	"kiosk/controller/internal/pad/protocol"
)

type directSession struct {
	fakeSession
	direct bool
}

func (d *directSession) DirectControl(string) bool { return d.direct }

func (f *fakeSession) snapSent(to string) []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []any
	for _, s := range f.sent {
		if s.to != to {
			continue
		}
		switch s.msg.(type) {
		case protocol.SnapResult, protocol.SnapChunk:
			out = append(out, s.msg)
		}
	}
	return out
}

func waitSnap(t *testing.T, f *fakeSession, to string, n int) []any {
	t.Helper()
	for i := 0; i < 200; i++ {
		if got := f.snapSent(to); len(got) >= n {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("want %d snap messages, got %d", n, len(f.snapSent(to)))
	return nil
}

func snapRouter(t *testing.T, snap func(context.Context) (SnapImage, error), sess Session) (*Router, *time.Time) {
	t.Helper()
	r, err := New(Config{Snap: snap})
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Unix(1000, 0)
	var mu sync.Mutex
	r.now = func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }
	r.Attach(sess)
	return r, &clock
}

func TestSnapDeliversChunksThatReassemble(t *testing.T) {
	jpeg := bytes.Repeat([]byte{0xff, 0xd8, 0x12, 0x34, 0x56}, SnapChunkBytes*5/5+777) // several chunks
	sess := &fakeSession{}
	snapRouter(t, func(context.Context) (SnapImage, error) { return SnapImage{JPEG: jpeg, W: 720, H: 1280}, nil }, sess)
	sess.fireControl("p1", protocol.Snap{T: "snap"})

	wantChunks := (len(jpeg) + SnapChunkBytes - 1) / SnapChunkBytes
	got := waitSnap(t, sess, "p1", 1+wantChunks)
	head, ok := got[0].(protocol.SnapResult)
	if !ok || !head.OK || head.Chunks != wantChunks || head.Size != len(jpeg) || head.W != 720 {
		t.Fatalf("header %+v", got[0])
	}
	var out []byte
	for i, m := range got[1:] {
		c := m.(protocol.SnapChunk)
		if c.ID != head.ID || c.Seq != i {
			t.Fatalf("chunk %d: %+v", i, c)
		}
		raw, err := base64.StdEncoding.DecodeString(c.Data)
		if err != nil || len(raw) > SnapChunkBytes {
			t.Fatalf("chunk %d: %v len %d", i, err, len(raw))
		}
		out = append(out, raw...)
	}
	if !bytes.Equal(out, jpeg) {
		t.Fatal("reassembled bytes differ")
	}
}

func TestSnapRateLimitsPerPhone(t *testing.T) {
	sess := &fakeSession{}
	var calls int
	var mu sync.Mutex
	r, clock := snapRouter(t, func(context.Context) (SnapImage, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return SnapImage{JPEG: []byte{1, 2, 3}}, nil
	}, sess)
	sess.fireControl("p1", protocol.Snap{T: "snap"})
	waitSnap(t, sess, "p1", 2)
	// Too soon: dropped with "Wait a moment".
	sess.fireControl("p1", protocol.Snap{T: "snap"})
	got := waitSnap(t, sess, "p1", 3)
	if e := got[2].(protocol.SnapResult); e.OK || e.Error != SnapErrWait {
		t.Fatalf("second snap: %+v", e)
	}
	// Another phone is not limited.
	sess.fireControl("p2", protocol.Snap{T: "snap"})
	waitSnap(t, sess, "p2", 2)
	// After the interval p1 may snap again.
	*clock = clock.Add(SnapEvery)
	sess.fireControl("p1", protocol.Snap{T: "snap"})
	waitSnap(t, sess, "p1", 5)
	mu.Lock()
	defer mu.Unlock()
	if calls != 3 {
		t.Fatalf("captures %d, want 3", calls)
	}
	_ = r
}

func TestSnapRefusedWhileSensitive(t *testing.T) {
	sess := &fakeSession{}
	called := false
	r, _ := snapRouter(t, func(context.Context) (SnapImage, error) { called = true; return SnapImage{JPEG: []byte{1}}, nil }, sess)
	r.SetSensitive(true)
	sess.fireControl("p1", protocol.Snap{T: "snap"})
	got := waitSnap(t, sess, "p1", 1)
	if e := got[0].(protocol.SnapResult); e.OK || e.Error != SnapErrSensitive || called {
		t.Fatalf("%+v called=%v", e, called)
	}
}

func TestSnapNeedsADirectChannel(t *testing.T) {
	sess := &directSession{}
	snapRouter(t, func(context.Context) (SnapImage, error) { return SnapImage{JPEG: []byte{1}}, nil }, sess)
	sess.fireControl("p1", protocol.Snap{T: "snap"})
	got := waitSnap(t, &sess.fakeSession, "p1", 1)
	if e := got[0].(protocol.SnapResult); e.OK || e.Error != SnapErrDirect {
		t.Fatalf("%+v", e)
	}
}

func TestSnapCaptureFailureAndUnavailable(t *testing.T) {
	sess := &fakeSession{}
	snapRouter(t, func(context.Context) (SnapImage, error) { return SnapImage{}, errors.New("boom") }, sess)
	sess.fireControl("p1", protocol.Snap{T: "snap"})
	if e := waitSnap(t, sess, "p1", 1)[0].(protocol.SnapResult); e.OK || e.Error != SnapErrFailed {
		t.Fatalf("%+v", e)
	}

	off := &fakeSession{}
	snapRouter(t, nil, off)
	off.fireControl("p1", protocol.Snap{T: "snap"})
	if e := waitSnap(t, off, "p1", 1)[0].(protocol.SnapResult); e.OK || e.Error != SnapErrNone {
		t.Fatalf("%+v", e)
	}
}

func TestSnapOnlyOneInFlight(t *testing.T) {
	sess := &fakeSession{}
	release := make(chan struct{})
	r, clock := snapRouter(t, func(context.Context) (SnapImage, error) {
		<-release
		return SnapImage{JPEG: []byte{1}}, nil
	}, sess)
	sess.fireControl("p1", protocol.Snap{T: "snap"})
	*clock = clock.Add(time.Minute)
	sess.fireControl("p1", protocol.Snap{T: "snap"}) // still busy
	if e := waitSnap(t, sess, "p1", 1)[0].(protocol.SnapResult); e.OK || e.Error != SnapErrWait {
		t.Fatalf("%+v", e)
	}
	close(release)
	waitSnap(t, sess, "p1", 3)
	_ = r
}
