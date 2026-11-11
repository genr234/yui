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

// Snap the screen (contracts §3 "snap"): a phone asks for a picture of the
// game area. The router enforces who may ask and how often, runs the capture
// the owner supplied (Config.Snap) and delivers the JPEG to that one phone
// as one snap_result plus ordered snap_chunk messages on the control channel.

const (
	// SnapEvery is the least time between two snaps from one phone.
	SnapEvery = 5 * time.Second
	// SnapChunkBytes is the JPEG bytes per snap_chunk (about 22 KB of base64,
	// well under the signaling and SCTP message limits).
	SnapChunkBytes = 16 * 1024
	// SnapMaxBytes is the largest JPEG delivered (128 chunks).
	SnapMaxBytes = 2 * 1024 * 1024
	// snapTimeout bounds one capture.
	snapTimeout = 15 * time.Second
)

// Messages shown to the player.
const (
	SnapErrWait      = "Wait a moment"
	SnapErrSensitive = "Snapshots are off while someone is typing a password"
	SnapErrDirect    = "Snapshots need a direct connection to the screen"
	SnapErrNone      = "Snapshots are not available here"
	SnapErrFailed    = "Couldn't grab the screen"
	SnapErrTooBig    = "The snapshot was too large to send"
)

// UserError is a Snap capture error whose text is safe to show the player.
type UserError string

func (e UserError) Error() string { return string(e) }

// SnapImage is a captured JPEG.
type SnapImage struct {
	JPEG []byte
	W, H int
}

// directControl is implemented by sessions that can tell whether a phone's
// control messages travel over its WebRTC data channel (not the cable
// fallback, which must not carry images).
type directControl interface {
	DirectControl(playerID string) bool
}

func (r *Router) onSnap(playerID string) {
	r.mu.Lock()
	sensitive := r.sensitive
	sess := r.sess
	now := r.clock()
	last, seen := r.snapLast[playerID]
	var errText string
	switch {
	case sess == nil:
		r.mu.Unlock()
		return
	case r.cfg.Snap == nil:
		errText = SnapErrNone
	case sensitive:
		errText = SnapErrSensitive
	case r.snapBusy[playerID] || (seen && now.Sub(last) < SnapEvery):
		errText = SnapErrWait
	default:
		if dc, ok := sess.(directControl); ok && !dc.DirectControl(playerID) {
			errText = SnapErrDirect
		}
	}
	if errText == "" {
		r.snapBusy[playerID] = true
		r.snapLast[playerID] = now
		r.snapSeq++
	}
	id := fmt.Sprintf("s%d", r.snapSeq)
	r.mu.Unlock()

	if errText != "" {
		r.sendSnapError(playerID, "", errText)
		return
	}
	go r.runSnap(playerID, id)
}

func (r *Router) runSnap(playerID, id string) {
	defer func() {
		r.mu.Lock()
		delete(r.snapBusy, playerID)
		r.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), snapTimeout)
	defer cancel()
	img, err := r.cfg.Snap(ctx)
	if err != nil {
		log.Printf("pad hub: snap for %s: %v", playerID, err)
		text := SnapErrFailed
		var ue UserError
		if errors.As(err, &ue) {
			text = string(ue)
		}
		r.sendSnapError(playerID, id, text)
		return
	}
	if len(img.JPEG) == 0 {
		r.sendSnapError(playerID, id, SnapErrFailed)
		return
	}
	if len(img.JPEG) > SnapMaxBytes {
		r.sendSnapError(playerID, id, SnapErrTooBig)
		return
	}
	if r.Sensitive() { // a password prompt may have opened while capturing
		r.sendSnapError(playerID, id, SnapErrSensitive)
		return
	}
	if err := r.deliverSnap(playerID, id, img); err != nil {
		log.Printf("pad hub: snap to %s: %v", playerID, err)
	}
}

// deliverSnap sends the header, then every chunk in order. It stops at the
// first send error; the phone times the transfer out.
func (r *Router) deliverSnap(playerID, id string, img SnapImage) error {
	n := (len(img.JPEG) + SnapChunkBytes - 1) / SnapChunkBytes
	head := protocol.SnapResult{T: "snap_result", ID: id, OK: true, Mime: "image/jpeg", Size: len(img.JPEG), Chunks: n, W: img.W, H: img.H}
	if err := r.send(playerID, head); err != nil {
		return err
	}
	for i := 0; i < n; i++ {
		end := min((i+1)*SnapChunkBytes, len(img.JPEG))
		chunk := protocol.SnapChunk{T: "snap_chunk", ID: id, Seq: i, Data: base64.StdEncoding.EncodeToString(img.JPEG[i*SnapChunkBytes : end])}
		if err := r.send(playerID, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (r *Router) sendSnapError(playerID, id, text string) {
	if id == "" {
		id = "e"
	}
	if err := r.send(playerID, protocol.SnapResult{T: "snap_result", ID: id, OK: false, Error: text}); err != nil {
		log.Printf("pad hub: snap error to %s: %v", playerID, err)
	}
}

func (r *Router) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}
