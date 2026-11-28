package router

import (
	"bytes"
	"context"
	"encoding/base64"
	"sync"
	"testing"
	"time"

	"kiosk/controller/internal/pad/protocol"
)

type fakeClipper struct {
	mu    sync.Mutex
	mark  ClipMark
	err   error
	video ClipVideo
	reqs  []ClipRequest
}

func (f *fakeClipper) Mark(context.Context, string, time.Time) (ClipMark, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mark, f.err
}

func (f *fakeClipper) Make(_ context.Context, req ClipRequest) (ClipVideo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, req)
	return f.video, nil
}

func (f *fakeSession) clipSent(to string) []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []any
	for _, s := range f.sent {
		if s.to != to {
			continue
		}
		switch s.msg.(type) {
		case protocol.ClipReady, protocol.ClipResult, protocol.ClipChunk:
			out = append(out, s.msg)
		}
	}
	return out
}

func waitClip(t *testing.T, f *fakeSession, to string, n int) []any {
	t.Helper()
	for i := 0; i < 400; i++ {
		if got := f.clipSent(to); len(got) >= n {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("want %d clip messages, got %d: %+v", n, len(f.clipSent(to)), f.clipSent(to))
	return nil
}

func clipRouter(t *testing.T, c Clipper, sess Session) (*Router, func(time.Duration)) {
	t.Helper()
	r, err := New(Config{Clips: c})
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Unix(1000, 0)
	var mu sync.Mutex
	r.now = func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }
	r.Attach(sess)
	return r, func(d time.Duration) { mu.Lock(); clock = clock.Add(d); mu.Unlock() }
}

func TestClipFlowDeliversVideo(t *testing.T) {
	mp4 := bytes.Repeat([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p'}, ClipChunkBytes/2+99)
	c := &fakeClipper{mark: ClipMark{Secs: 45, Camera: true}, video: ClipVideo{MP4: mp4, W: 1280, H: 720, Secs: 20}}
	sess := &fakeSession{}
	r, _ := clipRouter(t, c, sess)
	r.clipNoteReaction("heart")

	sess.fireControl("p1", protocol.Clip{T: "clip"})
	got := waitClip(t, sess, "p1", 1)
	ready, ok := got[0].(protocol.ClipReady)
	if !ok || ready.Secs != ClipMaxSecs || !ready.Camera {
		t.Fatalf("ready %+v", got[0])
	}
	sess.fireControl("p1", protocol.ClipMake{T: "clip_make", ID: ready.ID, Secs: 20, Camera: true, Layout: "tall"})
	n := (len(mp4) + ClipChunkBytes - 1) / ClipChunkBytes
	got = waitClip(t, sess, "p1", 2+n)
	head := got[1].(protocol.ClipResult)
	if !head.OK || head.Chunks != n || head.Size != len(mp4) || head.Mime != "video/mp4" || head.W != 1280 {
		t.Fatalf("head %+v", head)
	}
	var out []byte
	for i, m := range got[2:] {
		ch := m.(protocol.ClipChunk)
		if ch.Seq != i || ch.ID != ready.ID {
			t.Fatalf("chunk %d %+v", i, ch)
		}
		raw, _ := base64.StdEncoding.DecodeString(ch.Data)
		out = append(out, raw...)
	}
	if !bytes.Equal(out, mp4) {
		t.Fatal("reassembled clip differs")
	}
	c.mu.Lock()
	req := c.reqs[0]
	c.mu.Unlock()
	if req.To.Sub(req.From) != 20*time.Second || !req.Camera || req.Layout != "tall" {
		t.Fatalf("request %+v", req)
	}
	// The reaction came at the clip's last instant, which is outside [from, to).
	if len(req.Reactions) != 0 {
		t.Fatalf("reactions %+v", req.Reactions)
	}

	// The moment is used up.
	sess.fireControl("p1", protocol.ClipMake{T: "clip_make", ID: ready.ID, Secs: 20, Layout: "wide"})
	got = waitClip(t, sess, "p1", 3+n)
	if e := got[len(got)-1].(protocol.ClipResult); e.OK || e.Error != ClipErrGone {
		t.Fatalf("reuse: %+v", e)
	}
}

func TestClipSkipsPasswordTyping(t *testing.T) {
	c := &fakeClipper{mark: ClipMark{Secs: 30}, video: ClipVideo{MP4: []byte{1}}}
	sess := &fakeSession{}
	r, advance := clipRouter(t, c, sess)
	r.SetSensitive(true)
	advance(2 * time.Second)
	r.SetSensitive(false)
	advance(8 * time.Second)
	r.clipNoteReaction("star")
	advance(2 * time.Second) // the password ended 10 s before the clip

	sess.fireControl("p1", protocol.Clip{T: "clip"})
	ready := waitClip(t, sess, "p1", 1)[0].(protocol.ClipReady)
	sess.fireControl("p1", protocol.ClipMake{T: "clip_make", ID: ready.ID, Secs: 30, Layout: "wide"})
	waitClip(t, sess, "p1", 3)
	c.mu.Lock()
	req := c.reqs[0]
	c.mu.Unlock()
	if d := req.To.Sub(req.From); d != 10*time.Second {
		t.Fatalf("clip is %v, want 10s after the password", d)
	}
	if len(req.Reactions) != 1 || req.Reactions[0].Emote != "star" {
		t.Fatalf("reactions %+v", req.Reactions)
	}
}

func TestClipRefusedWhileTypingAndWhenTooShort(t *testing.T) {
	c := &fakeClipper{mark: ClipMark{Secs: 1}}
	sess := &fakeSession{}
	r, _ := clipRouter(t, c, sess)
	sess.fireControl("p1", protocol.Clip{T: "clip"})
	if e := waitClip(t, sess, "p1", 1)[0].(protocol.ClipResult); e.OK || e.Error != ClipErrEmpty {
		t.Fatalf("short: %+v", e)
	}
	r.SetSensitive(true)
	sess.fireControl("p2", protocol.Clip{T: "clip"})
	if e := waitClip(t, sess, "p2", 1)[0].(protocol.ClipResult); e.OK || e.Error != ClipErrSensitive {
		t.Fatalf("sensitive: %+v", e)
	}
}

func TestClipMarkUserError(t *testing.T) {
	c := &fakeClipper{err: UserError("Clips are warming up")}
	sess := &fakeSession{}
	clipRouter(t, c, sess)
	sess.fireControl("p1", protocol.Clip{T: "clip"})
	if e := waitClip(t, sess, "p1", 1)[0].(protocol.ClipResult); e.OK || e.Error != "Clips are warming up" {
		t.Fatalf("%+v", e)
	}
}
