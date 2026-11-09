package protocol

import "testing"

func TestParseProfileReply(t *testing.T) {
	msg, err := ParseControl([]byte(`{"t":"profile_reply","app_id":"dev.genr.party-quiz","granted":true}`))
	if err != nil {
		t.Fatal(err)
	}
	reply, ok := msg.(ProfileReply)
	if !ok || reply.AppID != "dev.genr.party-quiz" || !reply.Granted {
		t.Fatalf("got %#v", msg)
	}
	for _, bad := range []string{
		`{"t":"profile_reply","granted":true}`,
		`{"t":"profile_reply","app_id":"x"}`,
		`{"t":"profile_reply","app_id":"","granted":true}`,
		`{"t":"profile_reply","app_id":"x","granted":"yes"}`,
	} {
		if _, err := ParseControl([]byte(bad)); err == nil {
			t.Fatalf("%s accepted", bad)
		}
	}
}
