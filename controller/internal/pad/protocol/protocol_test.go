package protocol

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const fixtures = "../../../../protocol/fixtures"

func files(t *testing.T, dir string) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(fixtures, dir, "*.json"))
	if err != nil || len(m) == 0 {
		t.Fatalf("no fixtures in %s: %v", dir, err)
	}
	return m
}

func TestInputFixtures(t *testing.T) {
	paths := files(t, "input")
	if len(paths) < 4 {
		t.Fatalf("want >=4 input fixtures, got %d", len(paths))
	}
	for _, p := range paths {
		raw, _ := os.ReadFile(p)
		var fx struct {
			Name   string     `json:"name"`
			Fields InputState `json:"fields"`
			Hex    string     `json:"hex"`
		}
		if err := json.Unmarshal(raw, &fx); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		t.Run(fx.Name, func(t *testing.T) {
			want, _ := hex.DecodeString(fx.Hex)
			got := EncodeInput(fx.Fields)
			if hex.EncodeToString(got) != fx.Hex {
				t.Fatalf("encode:\n got %x\nwant %x", got, want)
			}
			dec, err := DecodeInput(want)
			if err != nil {
				t.Fatal(err)
			}
			if dec != fx.Fields {
				t.Fatalf("decode: got %+v want %+v", dec, fx.Fields)
			}
		})
	}
}

func TestDecodeInputErrors(t *testing.T) {
	if _, err := DecodeInput(make([]byte, 47)); err == nil {
		t.Error("expected length error")
	}
	b := EncodeInput(InputState{})
	b[0] = 2
	if _, err := DecodeInput(b); err == nil {
		t.Error("expected version error")
	}
}

func TestSeqNewer(t *testing.T) {
	cases := []struct {
		a, b uint16
		want bool
	}{
		{2, 1, true}, {1, 2, false}, {5, 5, false},
		{0, 65535, true}, {65535, 0, false}, {10, 65530, true},
		{0x8000, 0, false}, {0x7fff, 0, true},
	}
	for _, c := range cases {
		if got := SeqNewer(c.a, c.b); got != c.want {
			t.Errorf("SeqNewer(%d,%d)=%v want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestButtonBits(t *testing.T) {
	if ButtonRecenter != 1<<14 || ButtonA != 1 {
		t.Fatal("button constants")
	}
}

func TestControlValid(t *testing.T) {
	for _, p := range files(t, "control") {
		raw, _ := os.ReadFile(p)
		msg, err := ParseControl(raw)
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(p), err)
			continue
		}
		// Round trip: re-marshalled message must keep the same "t".
		out, err := json.Marshal(msg)
		if err != nil {
			t.Fatal(err)
		}
		var a, b struct{ T string }
		_ = json.Unmarshal(raw, &a)
		_ = json.Unmarshal(out, &b)
		if a.T != b.T {
			t.Errorf("%s: t %q != %q", filepath.Base(p), a.T, b.T)
		}
	}
}

func TestControlInvalid(t *testing.T) {
	for _, p := range files(t, "control/invalid") {
		raw, _ := os.ReadFile(p)
		if _, err := ParseControl(raw); err == nil {
			t.Errorf("%s: expected error", filepath.Base(p))
		}
	}
}
