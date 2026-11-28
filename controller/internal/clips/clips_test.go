package clips

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kiosk/controller/internal/audio"
)

func TestParseListLine(t *testing.T) {
	s, ok := parseListLine("seg007.ts,14.000000,16.000000")
	if !ok || s.file != "seg007.ts" || s.start != 14 || s.end != 16 {
		t.Fatalf("got %+v %v", s, ok)
	}
	for _, bad := range []string{"", "seg.ts,1", "seg.ts,x,2", "seg.ts,3,2", "../x.ts,1,2", `a\b.ts,1,2`} {
		if _, ok := parseListLine(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestLanesSpreadCloseReactions(t *testing.T) {
	got := lanes([]float64{0, 0.2, 0.4, 3, 3.1})
	want := []int{0, 1, 2, 0, 1}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("lanes = %v, want %v", got, want)
		}
	}
}

func TestCameraConcatCutsOverlaps(t *testing.T) {
	t0 := time.Unix(100, 0)
	got := cameraConcat([]string{"c0.webm", "c1.webm"},
		[]time.Time{t0, t0.Add(2 * time.Second)},
		[]time.Time{t0.Add(2150 * time.Millisecond), t0.Add(4100 * time.Millisecond)})
	want := "ffconcat version 1.0\nfile 'c0.webm'\noutpoint 2.0000\nduration 2.0000\nfile 'c1.webm'\nduration 2.1000\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestFilterGraphShapes(t *testing.T) {
	g := filterGraph(composeSpec{Layout: LayoutWide, Duration: 10, Game: Rect{0.1, 0, 0.8, 1}, CamInput: 1, MaskInput: 2, RingInput: 3, LogoInput: 5,
		Sprites: []sprite{{Input: 4, At: 1.5}}})
	for _, want := range []string{"crop=w='trunc(iw*0.8000/2)*2'", "[cam][mask]alphamerge[camr]", "hflip", "[4:v]scale=76:-1", "[5:v]scale=-1:56", "[vout]"} {
		if !strings.Contains(g, want) {
			t.Errorf("graph lacks %q:\n%s", want, g)
		}
	}
	g = filterGraph(composeSpec{Layout: LayoutTall, Duration: 10, CamInput: -1, MaskInput: -1, RingInput: -1, LogoInput: -1})
	if strings.Contains(g, "alphamerge") || strings.Contains(g, "crop=w='trunc(iw") {
		t.Errorf("tall graph without camera or crop:\n%s", g)
	}
}

func TestRingAndMaskImages(t *testing.T) {
	m := circleMask(100)
	if m.GrayAt(50, 50).Y != 255 || m.GrayAt(0, 0).Y != 0 {
		t.Fatal("mask is not a filled circle")
	}
	r := ringImage(100)
	if r.Bounds().Dx() != ringSize(100) {
		t.Fatal("ring size")
	}
	c := ringSize(100) / 2
	if px := r.NRGBAAt(c, c); px.A != 255 || px.R != 255 {
		t.Fatalf("ring centre %+v", px)
	}
}

// --- end to end with the local ffmpeg (skipped when it is missing) ---

func needFFmpeg(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	if testing.Short() {
		t.Skip("short")
	}
	return path
}

func x264(kbps, fps, gop int) []string {
	return []string{"-c:v", "libx264", "-preset", "ultrafast", "-g", itoa(gop), "-bf", "0", "-b:v", itoa(kbps) + "k"}
}

func toneFrames(ctx context.Context) (<-chan *audio.Frame, error) {
	ch := make(chan *audio.Frame, 8)
	go func() {
		defer close(ch)
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		var n int
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			f := new(audio.Frame)
			for i := 0; i < len(f); i += 2 {
				v := float32(0.2)
				if (n/24)%2 == 0 {
					v = -0.2
				}
				f[i], f[i+1] = v, v
				n++
			}
			select {
			case ch <- f:
			default:
			}
		}
	}()
	return ch, nil
}

type probe struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func ffprobe(t *testing.T, data []byte) probe {
	t.Helper()
	f := filepath.Join(t.TempDir(), "out.mp4")
	if err := os.WriteFile(f, data, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", "-show_format", f).Output()
	if err != nil {
		t.Fatalf("ffprobe: %v", err)
	}
	var p probe
	if err := json.Unmarshal(out, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

// cameraSegment records secs of a synthetic webcam as a WebM, like the
// overlay's MediaRecorder does.
func cameraSegment(t *testing.T, ffmpeg string, secs float64) []byte {
	t.Helper()
	f := filepath.Join(t.TempDir(), "cam.webm")
	cmd := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=640x480:rate=30",
		"-t", f3(secs), "-c:v", "libvpx", "-b:v", "900k", "-deadline", "realtime", "-cpu-used", "8", f)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("no libvpx: %v %s", err, out)
	}
	data, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRecordAndExport(t *testing.T) {
	ffmpeg := needFFmpeg(t)
	b := NewBuffer(BufferConfig{
		Helper:  func() (string, error) { return ffmpeg, nil },
		Encoder: func(string) (func(int, int, int) []string, bool) { return x264, true },
		Audio:   toneFrames,
		Source:  "testsrc2=size=1920x1080:rate=30",
		Dir:     t.TempDir(),
	})
	defer b.Close()
	b.SetWanted(true)

	deadline := time.Now().Add(30 * time.Second)
	for b.Available(time.Now()) < 7*time.Second {
		if time.Now().After(deadline) {
			t.Fatalf("buffer did not fill: %v", b.Err())
		}
		time.Sleep(200 * time.Millisecond)
	}
	to := time.Now()
	from := to.Add(-5 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	segs, err := b.Window(ctx, from, to, 4*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if segs[0].Start.After(from) || segs[len(segs)-1].End.Before(to) {
		t.Fatalf("window %v..%v not covered by %v..%v", from, to, segs[0].Start, segs[len(segs)-1].End)
	}
	paths := make([]string, len(segs))
	for i, s := range segs {
		paths[i] = s.Path
	}

	camData := cameraSegment(t, ffmpeg, 3.2)
	cam := &CameraClip{
		Segments: []CameraSegment{
			{Start: from.Add(-1 * time.Second), End: from.Add(2200 * time.Millisecond), Data: camData},
			{Start: from.Add(2 * time.Second), End: from.Add(5200 * time.Millisecond), Data: camData},
		},
		Crop: Rect{0.2, 0.1, 0.5, 0.66},
	}
	e := &Exporter{Helper: ffmpeg, Encoder: x264}
	for _, layout := range []string{LayoutWide, LayoutTall} {
		job := Job{From: from, To: to, Layout: layout, Game: Rect{0.125, 0, 0.75, 1}, Camera: cam,
			Reactions: []Reaction{{At: from.Add(time.Second), Emote: "heart"}, {At: from.Add(1200 * time.Millisecond), Emote: "laugh"}, {At: from, Emote: "bogus"}}}
		v, err := e.Export(ctx, job, paths, segs[0].Start)
		if err != nil {
			t.Fatalf("%s: %v", layout, err)
		}
		p := ffprobe(t, v.MP4)
		var vw, vh int
		var hasAudio bool
		for _, s := range p.Streams {
			if s.CodecType == "video" {
				vw, vh = s.Width, s.Height
			}
			hasAudio = hasAudio || s.CodecType == "audio"
		}
		if vw != v.W || vh != v.H || !hasAudio {
			t.Fatalf("%s: got %dx%d audio=%v, want %dx%d with audio", layout, vw, vh, hasAudio, v.W, v.H)
		}
		if d := p.Format.Duration; !strings.HasPrefix(d, "5.") && !strings.HasPrefix(d, "4.9") {
			t.Fatalf("%s: duration %s, want ~5s", layout, d)
		}
		if os.Getenv("CLIPS_KEEP") != "" {
			_ = os.WriteFile(filepath.Join(os.Getenv("CLIPS_KEEP"), layout+".mp4"), v.MP4, 0o644)
		}
	}

	// Without a camera.
	v, err := e.Export(ctx, Job{From: from, To: to, Layout: LayoutWide}, paths, segs[0].Start)
	if err != nil {
		t.Fatal(err)
	}
	if v.W != 1280 || len(v.MP4) == 0 {
		t.Fatalf("plain export %+v", v.W)
	}
}
