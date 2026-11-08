// Package session runs a kiosk play session: Rails session lifecycle, ActionCable
// signaling, and one pion peer per player (contracts §2 and §3).
//
// The kiosk creates data channels "control" (ordered, reliable) and "input"
// (ordered false, maxRetransmits 0), sends the offer, and answers a control
// hello with welcome. Input frames are decoded with internal/pad/protocol;
// stale seq values are dropped. Callers read the latest state per player.
//
// ICE-failure fallback carries control JSON on the ActionCable signal action.
// That path is not live on Rails yet. PlaySessionChannel relays only kiosk
// types offer|ice|bye and player types answer|ice|hello, and drops anything
// else. This package still speaks the extra type "control" so the fallback
// can be tested against a fake cable server:
//
//	kiosk  -> {to, type:"control", data:{...control object...}}
//	player -> {from, type:"control", data:{...control object...}}
//
// data is a JSON object, the same shape as a control frame, and stays under
// the channel's 64 KiB limit. It does not reuse offer, answer, or ice payloads.
// Wiring it up means extending contracts §2 and the channel allow-lists together.
package session
