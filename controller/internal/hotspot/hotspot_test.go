package hotspot

import (
	"path/filepath"
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

func TestOriginalIsKeptOnce(t *testing.T) {
	band, orig := parseStart("orig {\"ssid\":\"DIGIKIOSK1 7274\",\"passphrase\":\"secret12\",\"band\":\"Auto\"}\r\non 5ghz\r\n")
	if band != "5ghz" || orig == nil || orig.SSID != "DIGIKIOSK1 7274" || orig.Band != "Auto" {
		t.Fatalf("band %q orig %+v", band, orig)
	}
	if _, again := parseStart("on auto"); again != nil {
		t.Fatal("no orig line means the hotspot was already ours")
	}

	path := filepath.Join(t.TempDir(), "hotspot-original.json")
	if got, err := LoadOriginal(path); got != nil || err != nil {
		t.Fatalf("missing file: %v %v", got, err)
	}
	if err := SaveOriginal(path, orig); err != nil {
		t.Fatal(err)
	}
	// A later save must not replace the kiosk's real settings.
	if err := SaveOriginal(path, &Original{SSID: "Yui KKTC"}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadOriginal(path)
	if err != nil || got == nil || *got != *orig {
		t.Fatalf("loaded %+v %v", got, err)
	}
}
