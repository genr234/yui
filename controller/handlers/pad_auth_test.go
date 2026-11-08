package handlers

import "testing"

func TestPadSensitiveIsPublic(t *testing.T) {
	if !publicAuthMethod("pad.sensitive") {
		t.Fatal("pad.sensitive must work on the locked PIN screen")
	}
	if publicAuthMethod("pad.kick") {
		t.Fatal("pad.kick must stay behind the admin PIN")
	}
}
