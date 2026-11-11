package useractivity

import "testing"

func TestSetIdleTools(t *testing.T) {
	defer SetIdleTools(nil)
	SetIdleTools(nil)
	if !idleToolNames()["detectmouseactivity.exe"] {
		t.Fatal("default missing")
	}
	SetIdleTools([]string{" Other.EXE ", ""})
	got := idleToolNames()
	if len(got) != 1 || !got["other.exe"] {
		t.Fatalf("got %v", got)
	}
	SetIdleTools([]string{})
	if len(idleToolNames()) != 0 {
		t.Fatal("empty list should pause nothing")
	}
}
