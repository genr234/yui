package router

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"time"

	"kiosk/controller/internal/pad/protocol"
)

// Clip the last moments (contracts §3 "clip"): a phone pins "now" with
// `clip`; the kiosk answers `clip_ready` with how much footage it has and
// whether a facecam can be added. The phone then picks a length, layout and
// facecam and sends `clip_make`; the kiosk renders the video (Config.Clips)
// and sends it to that one phone as `clip_result` plus `clip_chunk`s, paced
// by the data channel's buffer.
//
// Footage from while someone typed a password is never put in a clip: the
// clip starts after the last such moment, or is refused.

const (
	// ClipEvery is the least time between two clip taps from one phone.
	ClipEvery = 2 * time.Second
	// ClipChunkBytes is the MP4 bytes per clip_chunk (about 44 KB base64).
	ClipChunkBytes = 32 * 1024
	// ClipMaxBytes is the largest clip delivered.
	ClipMaxBytes = 48 * 1024 * 1024
	// ClipMinSecs and ClipMaxSecs bound a clip's length.
	ClipMinSecs = 3
	ClipMaxSecs = 30
	// clipMarkTTL is how long a pinned moment can still be made into a clip.
	clipMarkTTL    = 2 * time.Minute
	clipMarkTime   = 10 * time.Second
	clipMakeTime   = 3 * time.Minute
	clipHighWater  = 1 << 20
	clipStall      = 30 * time.Second
	clipMemory     = 3 * time.Minute // reactions and password spans kept
	clipMaxRecords = 600
)

// Messages shown to the player.
const (
	ClipErrWait      = "Wait a moment"
	ClipErrSensitive = "Clips are off while someone is typing a password"
	ClipErrDirect    = "Clips need a direct connection to the screen"
	ClipErrNone      = "Clips are not available here"
	ClipErrFailed    = "Couldn't make the clip"
	ClipErrGone      = "That moment is gone, clip again"
	ClipErrTooBig    = "The clip was too large to send"
	ClipErrEmpty     = "Nothing to clip yet, try again in a few seconds"
	ClipErrPassword  = "That moment had someone typing a password"
)

// ClipMark is what the kiosk has for a pinned moment.
type ClipMark struct {
	Secs   int  // footage before the moment
	Camera bool // a facecam can be added
}

// ClipReaction is an emote sent during the clip, for drawing into it.
type ClipReaction struct {
	At    time.Time
	Emote string
}

// ClipRequest is one clip to render.
type ClipRequest struct {
	ID        string
	From, To  time.Time
	Camera    bool
	Layout    string
	Reactions []ClipReaction
}

// ClipVideo is a rendered clip.
type ClipVideo struct {
	MP4  []byte
	W, H int
	Secs float64
}

// Clipper renders clips; the controller supplies it (Config.Clips).
type Clipper interface {
	// Mark pins the moment at for clip id (e.g. remembers where the game
	// is on screen right now).
	Mark(ctx context.Context, id string, at time.Time) (ClipMark, error)
	Make(ctx context.Context, req ClipRequest) (ClipVideo, error)
}

// bufferedControl is implemented by sessions that report how much is queued
// on a player's control channel.
type bufferedControl interface {
	ControlBuffered(playerID string) (uint64, bool)
}

type clipMark struct {
	player string
	at     time.Time
	ClipMark
}

type timeSpan struct{ start, end time.Time } // end zero = still going

// clipState is the router's clip bookkeeping (Router.mu).
type clipState struct {
	marks     map[string]clipMark
	busy      map[string]bool // marking or making, per player
	last      map[string]time.Time
	seq       int
	reactions []ClipReaction
	sensitive []timeSpan
}

func newClipState() clipState {
	return clipState{marks: map[string]clipMark{}, busy: map[string]bool{}, last: map[string]time.Time{}}
}

// clipNoteSensitive records a password prompt opening or closing. r.mu held.
func (r *Router) clipNoteSensitive(active bool, now time.Time) {
	c := &r.clip
	if active {
		c.sensitive = append(c.sensitive, timeSpan{start: now})
	} else if n := len(c.sensitive); n > 0 && c.sensitive[n-1].end.IsZero() {
		c.sensitive[n-1].end = now
	}
	r.clipPruneLocked(now)
}

// clipNoteReaction records an emote for clips. Takes r.mu.
func (r *Router) clipNoteReaction(emote string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.clock()
	r.clip.reactions = append(r.clip.reactions, ClipReaction{At: now, Emote: emote})
	r.clipPruneLocked(now)
}

func (r *Router) clipPruneLocked(now time.Time) {
	c := &r.clip
	cut := now.Add(-clipMemory)
	i := 0
	for i < len(c.reactions) && (c.reactions[i].At.Before(cut) || len(c.reactions)-i > clipMaxRecords) {
		i++
	}
	c.reactions = c.reactions[i:]
	j := 0
	for j < len(c.sensitive) && !c.sensitive[j].end.IsZero() && c.sensitive[j].end.Before(cut) {
		j++
	}
	c.sensitive = c.sensitive[j:]
	for id, m := range c.marks {
		if now.Sub(m.at) > clipMarkTTL {
			delete(c.marks, id)
		}
	}
}

func (r *Router) clipForget(playerID string) {
	delete(r.clip.last, playerID)
	for id, m := range r.clip.marks {
		if m.player == playerID {
			delete(r.clip.marks, id)
		}
	}
}

func (r *Router) onClip(playerID string) {
	r.mu.Lock()
	sess := r.sess
	now := r.clock()
	last, seen := r.clip.last[playerID]
	var errText string
	switch {
	case sess == nil:
		r.mu.Unlock()
		return
	case r.cfg.Clips == nil:
		errText = ClipErrNone
	case r.sensitive:
		errText = ClipErrSensitive
	case r.clip.busy[playerID] || (seen && now.Sub(last) < ClipEvery):
		errText = ClipErrWait
	default:
		if dc, ok := sess.(directControl); ok && !dc.DirectControl(playerID) {
			errText = ClipErrDirect
		}
	}
	var id string
	if errText == "" {
		r.clip.busy[playerID] = true
		r.clip.last[playerID] = now
		r.clip.seq++
		id = fmt.Sprintf("c%d", r.clip.seq)
		// A new tap replaces the player's earlier, unused moment.
		r.clipForget(playerID)
		r.clip.last[playerID] = now
	}
	r.mu.Unlock()
	if errText != "" {
		r.sendClipError(playerID, "", errText)
		return
	}
	go r.runClipMark(playerID, id, now)
}

func (r *Router) runClipMark(playerID, id string, at time.Time) {
	defer func() {
		r.mu.Lock()
		delete(r.clip.busy, playerID)
		r.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), clipMarkTime)
	defer cancel()
	mark, err := r.cfg.Clips.Mark(ctx, id, at)
	if err != nil {
		log.Printf("pad hub: clip mark for %s: %v", playerID, err)
		r.sendClipError(playerID, id, userText(err, ClipErrNone))
		return
	}
	mark.Secs = min(mark.Secs, ClipMaxSecs)
	if mark.Secs < ClipMinSecs {
		r.sendClipError(playerID, id, ClipErrEmpty)
		return
	}
	r.mu.Lock()
	r.clip.marks[id] = clipMark{player: playerID, at: at, ClipMark: mark}
	r.mu.Unlock()
	if err := r.send(playerID, protocol.ClipReady{T: "clip_ready", ID: id, Secs: mark.Secs, Camera: mark.Camera}); err != nil {
		log.Printf("pad hub: clip_ready to %s: %v", playerID, err)
	}
}

func (r *Router) onClipMake(playerID string, m protocol.ClipMake) {
	r.mu.Lock()
	now := r.clock()
	r.clipPruneLocked(now)
	mark, ok := r.clip.marks[m.ID]
	var errText string
	var req ClipRequest
	switch {
	case r.sess == nil:
		r.mu.Unlock()
		return
	case !ok || mark.player != playerID:
		errText = ClipErrGone
	case r.clip.busy[playerID]:
		errText = ClipErrWait
	default:
		delete(r.clip.marks, m.ID)
		secs := max(ClipMinSecs, min(m.Secs, mark.Secs, ClipMaxSecs))
		from := mark.at.Add(-time.Duration(secs) * time.Second)
		// Skip past any password typing inside the clip.
		for _, s := range r.clip.sensitive {
			if s.start.After(mark.at) || (!s.end.IsZero() && !s.end.After(from)) {
				continue
			}
			if s.end.IsZero() || !s.end.Before(mark.at) {
				errText = ClipErrPassword
				break
			}
			from = s.end
		}
		if errText == "" && mark.at.Sub(from) < ClipMinSecs*time.Second {
			errText = ClipErrPassword
		}
		if errText == "" {
			req = ClipRequest{ID: m.ID, From: from, To: mark.at, Camera: m.Camera && mark.Camera, Layout: m.Layout}
			for _, rc := range r.clip.reactions {
				if !rc.At.Before(from) && rc.At.Before(mark.at) {
					req.Reactions = append(req.Reactions, rc)
				}
			}
			r.clip.busy[playerID] = true
		}
	}
	r.mu.Unlock()
	if errText != "" {
		r.sendClipError(playerID, m.ID, errText)
		return
	}
	go r.runClipMake(playerID, req)
}

func (r *Router) runClipMake(playerID string, req ClipRequest) {
	defer func() {
		r.mu.Lock()
		delete(r.clip.busy, playerID)
		r.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), clipMakeTime)
	defer cancel()
	video, err := r.cfg.Clips.Make(ctx, req)
	if err != nil {
		log.Printf("pad hub: clip for %s: %v", playerID, err)
		r.sendClipError(playerID, req.ID, userText(err, ClipErrFailed))
		return
	}
	if len(video.MP4) == 0 {
		r.sendClipError(playerID, req.ID, ClipErrFailed)
		return
	}
	if len(video.MP4) > ClipMaxBytes {
		r.sendClipError(playerID, req.ID, ClipErrTooBig)
		return
	}
	if err := r.deliverClip(ctx, playerID, req.ID, video); err != nil {
		log.Printf("pad hub: clip to %s: %v", playerID, err)
	}
}

// deliverClip sends the header, then every chunk in order, waiting while the
// player's control channel has more than clipHighWater queued.
func (r *Router) deliverClip(ctx context.Context, playerID, id string, v ClipVideo) error {
	n := (len(v.MP4) + ClipChunkBytes - 1) / ClipChunkBytes
	head := protocol.ClipResult{T: "clip_result", ID: id, OK: true, Mime: "video/mp4", Size: len(v.MP4), Chunks: n, W: v.W, H: v.H, Secs: v.Secs}
	if err := r.send(playerID, head); err != nil {
		return err
	}
	r.mu.Lock()
	bc, paced := r.sess.(bufferedControl)
	r.mu.Unlock()
	for i := 0; i < n; i++ {
		if paced {
			if err := r.waitDrain(ctx, bc, playerID); err != nil {
				return err
			}
		}
		end := min((i+1)*ClipChunkBytes, len(v.MP4))
		chunk := protocol.ClipChunk{T: "clip_chunk", ID: id, Seq: i, Data: base64.StdEncoding.EncodeToString(v.MP4[i*ClipChunkBytes : end])}
		if err := r.send(playerID, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (r *Router) waitDrain(ctx context.Context, bc bufferedControl, playerID string) error {
	stallUntil := time.Now().Add(clipStall)
	for {
		queued, ok := bc.ControlBuffered(playerID)
		if !ok {
			return errors.New("control channel closed")
		}
		if queued < clipHighWater {
			return nil
		}
		if time.Now().After(stallUntil) {
			return errors.New("control channel stalled")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(15 * time.Millisecond):
		}
	}
}

func (r *Router) sendClipError(playerID, id, text string) {
	if id == "" {
		id = "e"
	}
	if err := r.send(playerID, protocol.ClipResult{T: "clip_result", ID: id, OK: false, Error: text}); err != nil {
		log.Printf("pad hub: clip error to %s: %v", playerID, err)
	}
}

// userText is err's text when it is a UserError, else fallback.
func userText(err error, fallback string) string {
	var ue UserError
	if errors.As(err, &ue) {
		return string(ue)
	}
	return fallback
}
