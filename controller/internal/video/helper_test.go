package video

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

func golden(t *testing.T, name string, args []string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	got := strings.Join(args, "\n") + "\n"
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if string(want) != got {
		t.Errorf("%s differs:\n--- want\n%s--- got\n%s", name, want, got)
	}
}

func TestHelperArgsGolden(t *testing.T) {
	for _, enc := range encoderLadder {
		for _, relayed := range []bool{false, true} {
			kbps, fps := tierParams(enc, relayed)
			tier := "direct"
			if relayed {
				tier = "relayed"
			}
			args := helperArgs(helperOpts{Hwnd: 0x1234, Width: 854, Height: 480, FPS: fps, Kbps: kbps, Encoder: enc, Port: 5004})
			golden(t, "argv_"+enc.Name+"_"+tier+".txt", args)
		}
	}
}

func TestProbeArgsGolden(t *testing.T) {
	golden(t, "probe_libx264.txt", probeArgs(encoderLadder[len(encoderLadder)-1]))
}

func TestTierParams(t *testing.T) {
	hw, sw := encoderLadder[0], encoderLadder[len(encoderLadder)-1]
	check := func(e encoder, relayed bool, kbps, fps int) {
		t.Helper()
		if k, f := tierParams(e, relayed); k != kbps || f != fps {
			t.Errorf("%s relayed=%v: %d/%d want %d/%d", e.Name, relayed, k, f, kbps, fps)
		}
	}
	check(hw, false, 3000, 60)
	check(sw, false, 3000, 30)
	check(hw, true, 1200, 30)
	check(sw, true, 1200, 30)
}

func TestLadderOrder(t *testing.T) {
	var names []string
	for _, e := range encoderLadder {
		names = append(names, e.Name)
	}
	if got := strings.Join(names, ","); got != "h264_nvenc,h264_qsv,h264_amf,h264_mf,libx264" {
		t.Fatal(got)
	}
}
