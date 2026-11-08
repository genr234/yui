// Package session runs a kiosk play session: Rails session lifecycle, ActionCable
// signaling, and one pion peer per player (contracts §2 and §3).
//
// The kiosk creates data channels "control" (ordered, reliable) and "input"
// (ordered false, maxRetransmits 0), sends the offer, and answers a control
// hello with welcome. Input frames are decoded with internal/pad/protocol;
// stale seq values are dropped. Callers read the latest state per player.
//
// With Config.Audio, every peer also gets an Opus send track before the
// offer, and the kiosk's sound is written into the speaker's track only
// (audio.go): the speaker role comes from Rails, system be_speaker and
// volume are handled here, and moving the speaker needs no renegotiation.
//
// ICE-failure fallback carries control JSON on the ActionCable signal action
// as type "control" (contracts §2). data is one control object, the same
// shape as a §3 frame, and stays under the channel's 64 KiB limit. It does
// not reuse offer, answer, or ice payloads.
//
//	kiosk  -> {to, type:"control", data:{...control object...}}
//	player -> {from, type:"control", data:{...control object...}}
package session
