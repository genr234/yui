package router

import (
	"testing"

	"kiosk/controller/internal/pad/protocol"
)

func TestProfileReplyReachesTheHook(t *testing.T) {
	type got struct {
		player string
		reply  protocol.ProfileReply
	}
	var seen []got
	r, err := New(Config{ProfileReply: func(id string, reply protocol.ProfileReply) {
		seen = append(seen, got{id, reply})
	}})
	if err != nil {
		t.Fatal(err)
	}
	sess := &fakeSession{}
	r.Attach(sess)
	sess.fireControl("p1", protocol.ProfileReply{T: "profile_reply", AppID: "dev.example.game", Granted: true})
	if len(seen) != 1 || seen[0].player != "p1" || seen[0].reply.AppID != "dev.example.game" || !seen[0].reply.Granted {
		t.Fatalf("seen %+v", seen)
	}
}

func TestProfileReplyWithoutAHookIsIgnored(t *testing.T) {
	r, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	sess := &fakeSession{}
	r.Attach(sess)
	sess.fireControl("p1", protocol.ProfileReply{T: "profile_reply", AppID: "x", Granted: true})
}
