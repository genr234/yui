package hotspot

import (
	"strings"
	"testing"
)

func TestSSIDAndPassword(t *testing.T) {
	if got := SSID(" kktc "); got != "Yui KKTC" || !strings.HasPrefix(got, SSIDPrefix) {
		t.Fatalf("SSID = %q", got)
	}
	a, b := Password(), Password()
	if len(a) != 10 || a == b || strings.ContainsAny(a, "il1o0") {
		t.Fatalf("passwords %q %q", a, b)
	}
}
