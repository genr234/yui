package session

import "testing"

func TestNewWiFiQR(t *testing.T) {
	w := NewWiFi("Yui KKTC", `a;b:c\d`)
	if w.QR != `WIFI:T:WPA;S:Yui KKTC;P:a\;b\:c\\d;;` {
		t.Fatalf("qr = %q", w.QR)
	}
}
