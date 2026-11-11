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

func TestParseSnapMessages(t *testing.T) {
	if m, err := ParseControl([]byte(`{"t":"snap"}`)); err != nil || m != (Snap{T: "snap"}) {
		t.Fatalf("snap: %#v %v", m, err)
	}
	if m, err := ParseControl([]byte(`{"t":"snap_result","id":"a","ok":true,"mime":"image/jpeg","size":10,"chunks":1}`)); err != nil || m.(SnapResult).Chunks != 1 {
		t.Fatalf("result: %#v %v", m, err)
	}
	if _, err := ParseControl([]byte(`{"t":"snap_result","id":"a","ok":false,"error":"Wait a moment"}`)); err != nil {
		t.Fatal(err)
	}
	if m, err := ParseControl([]byte(`{"t":"snap_chunk","id":"a","seq":0,"data":"AAAA"}`)); err != nil || m.(SnapChunk).Data != "AAAA" {
		t.Fatalf("chunk: %#v %v", m, err)
	}
	for _, bad := range []string{
		`{"t":"snap_result","ok":true,"size":1,"chunks":1}`,
		`{"t":"snap_result","id":"a","ok":true}`,
		`{"t":"snap_chunk","id":"a","seq":-1,"data":"x"}`,
		`{"t":"snap_chunk","id":"a","seq":0}`,
	} {
		if _, err := ParseControl([]byte(bad)); err == nil {
			t.Fatalf("%s accepted", bad)
		}
	}
}
