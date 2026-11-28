package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"kiosk/controller/internal/audio"
	"kiosk/controller/internal/clips"
	"kiosk/controller/internal/pad/router"
	"kiosk/controller/internal/remotecontrol"
	"kiosk/controller/internal/video"
)

// remoteGameArea reads the page's game rect over CDP; replaced by tests.
var remoteGameArea = remotecontrol.GameArea

// Clips (plan: instant replay). While a phone is connected the kiosk keeps
// the last minute or so of its screen and sound (internal/clips). A player
// pins a moment from their phone and gets back an MP4 of the seconds before
// it, with the kiosk camera's facecam in a round bubble when they want it.
//
// The facecam is the overlay's: it keeps its own ring of camera footage
// (platform/src/sdk/clips/recorder.ts) and hands it over on request:
//
//	clips.camera.status  { active, width?, height? }        → { ok }
//	clips.camera.deliver { req_id, segments, crop, error? }  → { ok }
//	event clips.camera.request { req_id, from_ms, to_ms }
//
// Both commands are public: they only hand footage to the controller, which
// sends it to nobody but the phone that asked for the clip.

const (
	clipCameraStale = 45 * time.Second
	clipCameraWait  = 8 * time.Second
	clipWindowWait  = 4 * time.Second
)

func ClipsCommands() []Command {
	return []Command{clipsCameraStatusCommand{}, clipsCameraDeliverCommand{}}
}

type clipsState struct {
	once   sync.Once
	buf    *clips.Buffer
	export sync.Mutex // one export at a time

	mu        sync.Mutex
	marks     map[string]clipMarkInfo
	camActive bool
	camSeen   time.Time
	waits     map[string]chan cameraDelivery
	seq       int
}

type clipMarkInfo struct {
	at   time.Time
	game clips.Rect
}

type cameraDelivery struct {
	Segments []struct {
		StartMs int64  `json:"start_ms"`
		EndMs   int64  `json:"end_ms"`
		Mime    string `json:"mime"`
		Data    string `json:"data"`
	} `json:"segments"`
	Crop *struct {
		X, Y, W, H float64
	} `json:"crop"`
	Error string `json:"error"`
}

// clipBuffer is the replay buffer, built on first use.
func (r *Registry) clipBuffer() *clips.Buffer {
	c := &r.clipsSt
	c.once.Do(func() {
		c.marks = map[string]clipMarkInfo{}
		c.waits = map[string]chan cameraDelivery{}
		c.buf = clips.NewBuffer(clips.BufferConfig{
			Helper:  r.videoHelper,
			Encoder: clipEncoder,
			Audio:   r.clipAudio,
		})
	})
	return c.buf
}

func clipEncoder(helper string) (func(kbps, fps, gop int) []string, bool) {
	enc, ok := video.FindEncoder(nil, helper)
	if !ok {
		return nil, false
	}
	return enc.Args, true
}

// clipsRecording reports whether the replay buffer may be using the encoder.
func (r *Registry) clipsRecording() bool {
	c := &r.clipsSt
	return c.buf != nil && c.buf.Available(time.Now()) > 0
}

// setClipsWanted records while at least one phone is connected.
func (r *Registry) setClipsWanted(on bool) {
	buf := r.clipBuffer()
	if !buf.Supported() {
		return
	}
	buf.SetWanted(on)
}

// clipAudio captures what the speaker phone would hear: the native game's
// process tree, or Chrome's. The capture ends (and the buffer asks again)
// when that target changes.
func (r *Registry) clipAudio(ctx context.Context) (<-chan *audio.Frame, error) {
	a := r.Audio()
	process, pid := a.Target()
	if process == "browser" {
		pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		resolved, err := r.chromePID(pctx)
		cancel()
		if err != nil {
			return nil, err
		}
		pid = resolved
	}
	if pid <= 0 {
		return nil, errors.New("clips: no process to capture")
	}
	cctx, cancel := context.WithCancel(ctx)
	capt, err := audio.StartCapture(cctx, &audio.ProcessLoopback{PID: uint32(pid)}, 20)
	if err != nil {
		cancel()
		return nil, err
	}
	out := make(chan *audio.Frame, 20)
	go func() {
		defer close(out)
		defer cancel()
		check := time.NewTicker(2 * time.Second)
		defer check.Stop()
		for {
			select {
			case <-cctx.Done():
				return
			case <-check.C:
				if p, np := a.Target(); p != process || (p == "native" && np != pid) {
					return
				}
			case f, ok := <-capt.Frames():
				if !ok {
					return
				}
				select {
				case out <- f:
				default:
				}
			}
		}
	}()
	return out, nil
}

// clipGameRect is where the game is on screen, in fractions of the screen.
func (r *Registry) clipGameRect(ctx context.Context) clips.Rect {
	full := clips.Rect{X: 0, Y: 0, W: 1, H: 1}
	if mgr := r.nativeIfStarted(); mgr != nil && mgr.Status().Active() {
		ev := r.nativeWin.event()
		if ev == nil || ev.Rect == nil || ev.Screen.W <= 0 || ev.Screen.H <= 0 {
			return full
		}
		sw, sh := float64(ev.Screen.W), float64(ev.Screen.H)
		return fitRect(float64(ev.Rect.X)/sw, float64(ev.Rect.Y)/sh, float64(ev.Rect.W)/sw, float64(ev.Rect.H)/sh)
	}
	clip, w, h, err := remoteGameArea(ctx, r.snapshotConfig().PlatformDebugPort)
	if err != nil || w <= 0 || h <= 0 {
		return full
	}
	return fitRect(clip.X/w, clip.Y/h, clip.W/w, clip.H/h)
}

// fitRect clamps a rect in fractions to the frame.
func fitRect(x, y, w, h float64) clips.Rect {
	x0, y0 := math.Max(0, x), math.Max(0, y)
	x1, y1 := math.Min(1, x+w), math.Min(1, y+h)
	if x1-x0 < 0.05 || y1-y0 < 0.05 {
		return clips.Rect{X: 0, Y: 0, W: 1, H: 1}
	}
	return clips.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// cameraLive reports whether the overlay's camera ring is recording.
func (r *Registry) cameraLive() bool {
	c := &r.clipsSt
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.camActive && time.Since(c.camSeen) < clipCameraStale
}

// --- router.Clipper ---

type registryClipper struct{ r *Registry }

func (rc registryClipper) Mark(ctx context.Context, id string, at time.Time) (router.ClipMark, error) {
	r := rc.r
	buf := r.clipBuffer()
	if !buf.Supported() {
		return router.ClipMark{}, router.UserError(router.ClipErrNone)
	}
	avail := buf.Available(at)
	if avail <= 0 {
		if errors.Is(buf.Err(), clips.ErrUnavailable) {
			return router.ClipMark{}, router.UserError(router.ClipErrNone)
		}
		return router.ClipMark{}, router.UserError(router.ClipErrEmpty)
	}
	game := r.clipGameRect(ctx)
	c := &r.clipsSt
	c.mu.Lock()
	for k, m := range c.marks {
		if time.Since(m.at) > 3*time.Minute {
			delete(c.marks, k)
		}
	}
	c.marks[id] = clipMarkInfo{at: at, game: game}
	c.mu.Unlock()
	return router.ClipMark{Secs: int(avail / time.Second), Camera: r.cameraLive()}, nil
}

func (rc registryClipper) Make(ctx context.Context, req router.ClipRequest) (router.ClipVideo, error) {
	r := rc.r
	c := &r.clipsSt
	c.mu.Lock()
	mark, ok := c.marks[req.ID]
	delete(c.marks, req.ID)
	c.mu.Unlock()
	game := clips.Rect{X: 0, Y: 0, W: 1, H: 1}
	if ok {
		game = mark.game
	}

	c.export.Lock()
	defer c.export.Unlock()
	buf := r.clipBuffer()
	segs, err := buf.Window(ctx, req.From, req.To, clipWindowWait)
	if err != nil {
		return router.ClipVideo{}, router.UserError(router.ClipErrGone)
	}
	from := req.From
	if segs[0].Start.After(from) {
		from = segs[0].Start
	}
	if req.To.Sub(from) < router.ClipMinSecs*time.Second {
		return router.ClipVideo{}, router.UserError(router.ClipErrGone)
	}
	job := clips.Job{From: from, To: req.To, Layout: req.Layout, Game: game}
	for _, rc := range req.Reactions {
		job.Reactions = append(job.Reactions, clips.Reaction{At: rc.At, Emote: rc.Emote})
	}
	if req.Camera {
		cam, err := r.requestCamera(ctx, from, req.To)
		if err != nil {
			log.Printf("clips: facecam left out: %v", err)
		}
		job.Camera = cam
	}
	helper, err := r.videoHelper()
	if err != nil {
		return router.ClipVideo{}, router.UserError(router.ClipErrNone)
	}
	encArgs, ok := clipEncoder(helper)
	if !ok {
		return router.ClipVideo{}, router.UserError(router.ClipErrNone)
	}
	paths := make([]string, len(segs))
	for i, s := range segs {
		paths[i] = s.Path
	}
	ex := &clips.Exporter{Helper: helper, Encoder: encArgs}
	started := time.Now()
	v, err := ex.Export(ctx, job, paths, segs[0].Start)
	if err != nil {
		return router.ClipVideo{}, err
	}
	log.Printf("clips: %s %.1fs clip (%s, facecam %v, %d reactions) in %s, %d KB",
		req.ID, v.Secs, job.Layout, job.Camera != nil, len(job.Reactions), time.Since(started).Round(100*time.Millisecond), len(v.MP4)/1024)
	return router.ClipVideo{MP4: v.MP4, W: v.W, H: v.H, Secs: v.Secs}, nil
}

// requestCamera asks the overlay for its facecam footage of [from, to].
func (r *Registry) requestCamera(ctx context.Context, from, to time.Time) (*clips.CameraClip, error) {
	if !r.cameraLive() {
		return nil, errors.New("camera is off")
	}
	c := &r.clipsSt
	ch := make(chan cameraDelivery, 1)
	c.mu.Lock()
	c.seq++
	id := fmt.Sprintf("cam%d", c.seq)
	c.waits[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.waits, id)
		c.mu.Unlock()
	}()
	r.Emit("clips.camera.request", map[string]any{"req_id": id, "from_ms": from.UnixMilli(), "to_ms": to.UnixMilli()})
	var d cameraDelivery
	select {
	case d = <-ch:
	case <-time.After(clipCameraWait):
		return nil, errors.New("the overlay did not answer")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if d.Error != "" {
		return nil, errors.New(d.Error)
	}
	cam := &clips.CameraClip{}
	for _, s := range d.Segments {
		data, err := base64.StdEncoding.DecodeString(s.Data)
		if err != nil || len(data) == 0 || s.EndMs <= s.StartMs {
			continue
		}
		cam.Segments = append(cam.Segments, clips.CameraSegment{Start: time.UnixMilli(s.StartMs), End: time.UnixMilli(s.EndMs), Data: data})
	}
	if len(cam.Segments) == 0 {
		return nil, errors.New("no footage")
	}
	if d.Crop != nil {
		cam.Crop = clips.Rect{X: d.Crop.X, Y: d.Crop.Y, W: d.Crop.W, H: d.Crop.H}
	}
	return cam, nil
}

type clipsCameraStatusCommand struct{}

func (clipsCameraStatusCommand) Name() string { return "clips.camera.status" }
func (clipsCameraStatusCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var body struct {
		Active bool `json:"active"`
	}
	if err := json.Unmarshal(params, &body); err != nil {
		return nil, err
	}
	r.clipBuffer()
	c := &r.clipsSt
	c.mu.Lock()
	c.camActive = body.Active
	c.camSeen = time.Now()
	c.mu.Unlock()
	return map[string]bool{"ok": true}, nil
}

type clipsCameraDeliverCommand struct{}

func (clipsCameraDeliverCommand) Name() string { return "clips.camera.deliver" }
func (clipsCameraDeliverCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var body struct {
		ReqID string `json:"req_id"`
		cameraDelivery
	}
	if err := json.Unmarshal(params, &body); err != nil {
		return nil, err
	}
	r.clipBuffer()
	c := &r.clipsSt
	c.mu.Lock()
	ch := c.waits[body.ReqID]
	delete(c.waits, body.ReqID)
	c.mu.Unlock()
	if ch != nil {
		ch <- body.cameraDelivery
	}
	return map[string]bool{"ok": true}, nil
}
