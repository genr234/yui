package clips

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Output limits.
const (
	// MinSecs and MaxSecs bound a clip's length.
	MinSecs = 3
	MaxSecs = 30
	// maxSprites bounds the reactions drawn into one clip (the newest win).
	maxSprites = 18
	exportKbps = 4500
	exportFPS  = 30
)

// Reaction is an emote a player sent, at wall time At.
type Reaction struct {
	At    time.Time
	Emote string
}

// CameraClip is the facecam footage for a clip: self-contained WebM
// segments as the overlay recorded them.
type CameraClip struct {
	Segments []CameraSegment
	Crop     Rect // zero = centred square
}

// CameraSegment is one WebM segment, Start/End in wall time.
type CameraSegment struct {
	Start, End time.Time
	Data       []byte
}

// Job is one clip to export.
type Job struct {
	From, To  time.Time
	Layout    string
	Game      Rect
	Camera    *CameraClip // nil = no facecam
	Reactions []Reaction
}

// Video is a finished clip.
type Video struct {
	MP4  []byte
	W, H int
	Secs float64
}

// exportInputs is what export writes into its work dir before it runs
// ffmpeg; args reference these names relative to the work dir.
type exportInputs struct {
	screenFiles []string  // copied screen segments, in order
	screenStart time.Time // wall time of the first one's first frame
	camFiles    []string
	camStarts   []time.Time
	camEnds     []time.Time
	sprites     []string // emote file per sprite
	spriteAt    []float64
}

// exportArgs builds the ffmpeg command line for a clip. Inputs are files in
// the work dir (ffmpeg runs there). Pure; tested.
func exportArgs(job Job, in exportInputs, enc []string) ([]string, composeSpec) {
	dur := job.To.Sub(job.From).Seconds()
	off := job.From.Sub(in.screenStart).Seconds()
	if off < 0 {
		off = 0
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-nostats", "-y",
		"-ss", f3(off), "-t", f3(dur), "-i", "concat:" + strings.Join(in.screenFiles, "|")}
	spec := composeSpec{Layout: job.Layout, Duration: dur, Game: job.Game, CamInput: -1, MaskInput: -1, RingInput: -1, LogoInput: -1}
	idx := 1
	if job.Camera != nil && len(in.camFiles) > 0 {
		camOff := job.From.Sub(in.camStarts[0]).Seconds()
		if camOff > 0 {
			args = append(args, "-ss", f3(camOff))
		} else {
			spec.CamDelay = -camOff
		}
		args = append(args, "-f", "concat", "-safe", "0", "-i", "camera.ffconcat")
		spec.CamInput = idx
		spec.CamCrop = job.Camera.Crop
		idx++
		args = append(args, "-loop", "1", "-framerate", strconv.Itoa(exportFPS), "-i", "mask.png")
		spec.MaskInput = idx
		idx++
		args = append(args, "-loop", "1", "-framerate", strconv.Itoa(exportFPS), "-i", "ring.png")
		spec.RingInput = idx
		idx++
	}
	for i, name := range in.sprites {
		args = append(args, "-loop", "1", "-framerate", strconv.Itoa(exportFPS), "-i", name)
		spec.Sprites = append(spec.Sprites, sprite{Input: idx, At: in.spriteAt[i]})
		idx++
	}
	ats := make([]float64, len(spec.Sprites))
	for i, sp := range spec.Sprites {
		ats[i] = sp.At
	}
	for i, l := range lanes(ats) {
		spec.Sprites[i].Lane = l
	}
	args = append(args, "-loop", "1", "-framerate", strconv.Itoa(exportFPS), "-i", "logo.png")
	spec.LogoInput = idx

	args = append(args, "-filter_complex", filterGraph(spec), "-map", "[vout]", "-map", "0:a?")
	args = append(args, enc...)
	args = append(args, "-r", strconv.Itoa(exportFPS), "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "128k", "-ar", "48000",
		"-t", f3(dur), "-movflags", "+faststart", "clip.mp4")
	return args, spec
}

// cameraConcat is the ffconcat list for the facecam segments. Segments
// overlap (the next recorder starts before the last one stops), so each one
// is cut where the next begins; a gap is held on the last frame.
func cameraConcat(files []string, starts, ends []time.Time) string {
	var b strings.Builder
	b.WriteString("ffconcat version 1.0\n")
	for i, name := range files {
		fmt.Fprintf(&b, "file '%s'\n", name)
		var d float64
		if i+1 < len(files) {
			d = starts[i+1].Sub(starts[i]).Seconds()
		} else {
			d = ends[i].Sub(starts[i]).Seconds()
		}
		if d > 0.05 {
			if i+1 < len(files) {
				fmt.Fprintf(&b, "outpoint %s\n", f3(d))
			}
			fmt.Fprintf(&b, "duration %s\n", f3(d))
		}
	}
	return b.String()
}

// Exporter turns buffered footage into a finished MP4.
type Exporter struct {
	// Helper is the ffmpeg path.
	Helper string
	// Encoder is the video codec options (encoder ladder entry, rate control).
	Encoder func(kbps, fps, gop int) []string
	// Command runs ffmpeg; default exec.CommandContext.
	Command func(ctx context.Context, name string, args ...string) *exec.Cmd
	// TempDir holds the work dirs; "" = os.TempDir().
	TempDir string
}

// Export renders job from the screen segments (copied by the caller, in
// order, the first starting at screenStart). The work dir is removed after.
func (e *Exporter) Export(ctx context.Context, job Job, screen []string, screenStart time.Time) (Video, error) {
	if len(screen) == 0 {
		return Video{}, errors.New("clips: no footage")
	}
	dir, err := os.MkdirTemp(e.TempDir, "yui-clip-")
	if err != nil {
		return Video{}, err
	}
	defer os.RemoveAll(dir)

	in := exportInputs{screenStart: screenStart}
	for i, src := range screen {
		name := fmt.Sprintf("s%03d.ts", i)
		if err := copyFile(src, filepath.Join(dir, name)); err != nil {
			return Video{}, fmt.Errorf("clips: copy footage: %w", err)
		}
		in.screenFiles = append(in.screenFiles, name)
	}
	if job.Layout != LayoutTall {
		job.Layout = LayoutWide
	}
	g := layoutGeometry(job.Layout, job.Camera != nil)
	if job.Camera != nil {
		segs := append([]CameraSegment(nil), job.Camera.Segments...)
		sort.Slice(segs, func(i, j int) bool { return segs[i].Start.Before(segs[j].Start) })
		for i, s := range segs {
			if len(s.Data) == 0 {
				continue
			}
			name := fmt.Sprintf("c%03d.webm", i)
			if err := writeFile(dir, name, s.Data); err != nil {
				return Video{}, err
			}
			in.camFiles = append(in.camFiles, name)
			in.camStarts = append(in.camStarts, s.Start)
			in.camEnds = append(in.camEnds, s.End)
		}
		if len(in.camFiles) == 0 {
			job.Camera = nil
			g = layoutGeometry(job.Layout, false)
		} else {
			if err := writeFile(dir, "camera.ffconcat", []byte(cameraConcat(in.camFiles, in.camStarts, in.camEnds))); err != nil {
				return Video{}, err
			}
			if err := writePNG(filepath.Join(dir, "mask.png"), circleMask(g.D)); err != nil {
				return Video{}, err
			}
			if err := writePNG(filepath.Join(dir, "ring.png"), ringImage(g.D)); err != nil {
				return Video{}, err
			}
		}
	}
	dur := job.To.Sub(job.From).Seconds()
	reactions := job.Reactions
	if len(reactions) > maxSprites {
		reactions = reactions[len(reactions)-maxSprites:]
	}
	written := map[string]bool{}
	for _, r := range reactions {
		at := r.At.Sub(job.From).Seconds()
		png, ok := emoteSprite(r.Emote)
		if !ok || at < 0 || at > dur-0.3 {
			continue
		}
		name := "emote-" + r.Emote + ".png"
		if !written[name] {
			if err := writeFile(dir, name, png); err != nil {
				return Video{}, err
			}
			written[name] = true
		}
		in.sprites = append(in.sprites, name)
		in.spriteAt = append(in.spriteAt, at)
	}
	if err := writeFile(dir, "logo.png", logoSprite()); err != nil {
		return Video{}, err
	}

	enc := e.Encoder(exportKbps, exportFPS, exportFPS*2)
	args, _ := exportArgs(job, in, enc)
	command := e.Command
	if command == nil {
		command = exec.CommandContext
	}
	cmd := command(ctx, e.Helper, args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	hideWindow(cmd)
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 600 {
			msg = msg[len(msg)-600:]
		}
		return Video{}, fmt.Errorf("clips: ffmpeg: %w: %s", err, msg)
	}
	mp4, err := os.ReadFile(filepath.Join(dir, "clip.mp4"))
	if err != nil {
		return Video{}, err
	}
	return Video{MP4: mp4, W: g.W, H: g.H, Secs: dur}, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
