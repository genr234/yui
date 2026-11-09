package session

import (
	"testing"
	"time"

	"kiosk/controller/internal/pad/protocol"
)

func rosterChannel(id string, slot int, host bool, channel string) Player {
	p := rosterJSON(id, slot, host, true, true)
	p.SpeakerChannel = channel
	return p
}

// TestStereoSpeakerPair: a second phone becomes the right speaker, both
// phones then get mono streams (the host is left), and when one leaves the
// other plays the whole mix again.
func TestStereoSpeakerPair(t *testing.T) {
	f := newFake(t)
	a, _ := toneAudio(t)
	mgr := f.manager(t)
	mgr.cfg.Audio = a
	cancel, done := start(t, f, mgr)
	defer stop(t, cancel, done)

	host := newAudioPhone(t, f, "p1")
	guest := newAudioPhone(t, f, "p2")
	stopRoute := make(chan struct{})
	defer close(stopRoute)
	go routeSignals(t, f, map[string]*audioPhone{"p1": host, "p2": guest}, stopRoute)

	f.push(t, map[string]any{"type": "player_joined", "player": rosterJSON("p1", 1, true, true, true)})
	f.push(t, map[string]any{"type": "player_joined", "player": rosterJSON("p2", 2, false, false, true)})
	waitPackets(t, host, 0, 20)
	waitFor(t, "guest ready", func() bool { return guest.lastPlayer() != nil })
	if host.monoPkts.Load() != 0 || host.stereoPkts.Load() == 0 {
		t.Fatalf("one speaker should get the stereo stream: mono %d stereo %d", host.monoPkts.Load(), host.stereoPkts.Load())
	}

	// The guest taps "Be the right speaker".
	guest.send(t, protocol.System{T: "system", Action: "be_speaker", Channel: "right"})
	waitFor(t, "pair", func() bool { return mgr.AudioTarget() == "p1" && mgr.AudioRightTarget() == "p2" })
	if !f.hasCall("POST /api/kiosk/play_sessions/sess-1/players/p2/speaker?channel=right") {
		t.Fatal("rails was not told about the pair")
	}
	waitPackets(t, guest, 0, 20)
	if guest.stereoPkts.Load() != 0 {
		t.Fatalf("right phone got %d stereo packets", guest.stereoPkts.Load())
	}
	hostStereo := host.stereoPkts.Load()
	from := host.monoPkts.Load()
	waitFor(t, "host mono", func() bool { return host.monoPkts.Load()-from > 20 })
	if host.stereoPkts.Load()-hostStereo > 3 {
		t.Fatal("left phone still gets the stereo stream")
	}
	waitFor(t, "channels announced", func() bool {
		g, h := guest.lastPlayer(), host.lastPlayer()
		return g["speaker"] == true && g["speaker_channel"] == "right" && h["speaker_channel"] == "left"
	})
	snap := mgr.Snapshot()
	if snap.Players[0].SpeakerChannel != "left" || snap.Players[1].SpeakerChannel != "right" || !snap.Players[1].Speaker {
		t.Fatalf("snapshot %+v", snap.Players)
	}

	// Rails confirms (in either order); nothing changes.
	f.push(t, map[string]any{"type": "player_updated", "player": rosterChannel("p2", 2, false, "right")})
	f.push(t, map[string]any{"type": "player_updated", "player": rosterChannel("p1", 1, true, "left")})
	time.Sleep(100 * time.Millisecond)
	if mgr.AudioTarget() != "p1" || mgr.AudioRightTarget() != "p2" {
		t.Fatalf("pair %q %q", mgr.AudioTarget(), mgr.AudioRightTarget())
	}

	// The right phone leaves: the host plays the whole mix again.
	guestFrom := guest.packets.Load()
	stereoFrom := host.stereoPkts.Load()
	f.push(t, map[string]any{"type": "player_left", "player": rosterChannel("p2", 2, false, "right")})
	f.push(t, map[string]any{"type": "player_updated", "player": rosterJSON("p2", 2, false, false, false)})
	f.push(t, map[string]any{"type": "player_updated", "player": rosterJSON("p1", 1, true, true, true)})
	waitFor(t, "single stream", func() bool { return mgr.AudioTarget() == "p1" && mgr.AudioRightTarget() == "" })
	waitFor(t, "host stereo again", func() bool { return host.stereoPkts.Load()-stereoFrom > 20 })
	if guest.packets.Load()-guestFrom > 3 {
		t.Fatal("the phone that left still gets sound")
	}
	waitFor(t, "host back to mono", func() bool { return mgr.Snapshot().Players[0].SpeakerChannel == "" })
	if mgr.Snapshot().Players[0].SpeakerChannel != "" {
		t.Fatalf("host still has a channel: %+v", mgr.Snapshot().Players[0])
	}
}
