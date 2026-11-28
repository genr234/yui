package clips

import (
	"fmt"
	"strconv"
	"strings"
)

// Layouts a player can pick for a clip.
const (
	LayoutWide = "wide" // 16:9, 1280x720: the game, the facecam in a corner
	LayoutTall = "tall" // 9:16, 720x1280: the game on top, a big facecam below
)

// Rect is a part of a frame in fractions of its size (0..1), x/y top-left.
type Rect struct {
	X, Y, W, H float64
}

// valid reports whether r is a usable, non-empty part of the frame.
func (r Rect) valid() bool {
	return r.W > 0.01 && r.H > 0.01 && r.X >= 0 && r.Y >= 0 && r.X+r.W <= 1.0001 && r.Y+r.H <= 1.0001
}

// full reports whether r covers (nearly) the whole frame.
func (r Rect) full() bool {
	return r.X < 0.002 && r.Y < 0.002 && r.W > 0.996 && r.H > 0.996
}

// sprite is a reaction drawn into the clip: emote input index and when it
// pops up, in seconds from the clip's start.
type sprite struct {
	Input int
	At    float64
	Lane  int
}

// composeSpec is everything the filter graph depends on. Pure, tested.
type composeSpec struct {
	Layout   string
	Duration float64
	// Game is the crop of the screen recording that holds the game.
	Game Rect
	// Camera: the facecam input index (-1 = no facecam), its crop (zero =
	// centred square) and how late it starts after the clip, in seconds.
	CamInput int
	CamCrop  Rect
	CamDelay float64
	// Mask and Ring are the facecam's circle mask and ring inputs.
	MaskInput int
	RingInput int
	LogoInput int // -1 = no logo
	Sprites   []sprite
}

// geometry of a layout for the given facecam choice.
type geometry struct {
	W, H int
	// GameBox is where the game is fitted (it keeps its aspect inside).
	GX, GY, GW, GH int
	// Facecam diameter and top-left; Cam = false when there is none.
	Cam    bool
	D      int
	CX, CY int
	// Logo height and top-left.
	LogoH, LX, LY int
	// Reactions float up from (EmoteX - lane*EmoteStep, EmoteY).
	EmoteW, EmoteX, EmoteStep, EmoteY, EmoteRise int
}

func layoutGeometry(layout string, cam bool) geometry {
	if layout == LayoutTall {
		g := geometry{W: 720, H: 1280, Cam: cam, LogoH: 64, LX: 720 - 64 - 28, LY: 28,
			EmoteW: 84, EmoteX: 720 - 84 - 24, EmoteStep: 64, EmoteY: 1280 - 132 - 40, EmoteRise: 520}
		if cam {
			// Game in the top half, a big round facecam under it.
			g.GX, g.GY, g.GW, g.GH = 0, 120, 720, 640
			g.D = 440
			g.CX, g.CY = (720-g.D)/2, 120+640+(1280-120-640-g.D)/2
		} else {
			g.GX, g.GY, g.GW, g.GH = 0, 120, 720, 1040
		}
		return g
	}
	g := geometry{W: 1280, H: 720, Cam: cam, LogoH: 56, LX: 1280 - 56 - 22, LY: 22,
		EmoteW: 76, EmoteX: 1280 - 76 - 28, EmoteStep: 60, EmoteY: 720 - 120 - 24, EmoteRise: 360,
		GX: 0, GY: 0, GW: 1280, GH: 720}
	if cam {
		g.D = 232
		g.CX, g.CY = 34, 720-g.D-34
	}
	return g
}

func f3(v float64) string { return strconv.FormatFloat(v, 'f', 4, 64) }

// cropExpr crops a fraction of the input, keeping even sizes.
func cropExpr(r Rect) string {
	return fmt.Sprintf("crop=w='trunc(iw*%s/2)*2':h='trunc(ih*%s/2)*2':x='trunc(iw*%s)':y='trunc(ih*%s)'",
		f3(r.W), f3(r.H), f3(r.X), f3(r.Y))
}

// filterGraph builds the -filter_complex for a clip. The screen recording is
// input 0 (already trimmed to the clip). Output pads: [vout].
func filterGraph(s composeSpec) string {
	g := layoutGeometry(s.Layout, s.CamInput >= 0)
	var f []string
	game := "[0:v]setpts=PTS-STARTPTS,fps=30"
	if s.Game.valid() && !s.Game.full() {
		game += "," + cropExpr(s.Game)
	}
	f = append(f, game+",split[gbg][gfg]")
	// Background: the game itself, zoomed to cover, blurred and dimmed, so
	// any aspect fills the frame without black bars.
	f = append(f, fmt.Sprintf("[gbg]scale=%d:%d:force_original_aspect_ratio=increase:flags=fast_bilinear,crop=%d:%d,boxblur=24:2,eq=brightness=-0.16:saturation=1.2[bg]",
		g.W/4, g.H/4, g.W/4, g.H/4))
	f = append(f, fmt.Sprintf("[bg]scale=%d:%d:flags=bilinear,setsar=1[bgs]", g.W, g.H))
	f = append(f, fmt.Sprintf("[gfg]scale=%d:%d:force_original_aspect_ratio=decrease:flags=bicubic,setsar=1[fg]", g.GW, g.GH))
	f = append(f, fmt.Sprintf("[bgs][fg]overlay=x='%d+(%d-w)/2':y='%d+(%d-h)/2'[base0]", g.GX, g.GW, g.GY, g.GH))
	cur := "base0"
	n := 0
	next := func() string { n++; return "v" + strconv.Itoa(n) }

	if g.Cam {
		cam := fmt.Sprintf("[%d:v]setpts=PTS-STARTPTS", s.CamInput)
		if s.CamDelay > 0.02 {
			cam += fmt.Sprintf(",tpad=start_duration=%s:start_mode=clone", f3(s.CamDelay))
		}
		cam += ",fps=30"
		if s.CamCrop.valid() {
			cam += "," + cropExpr(s.CamCrop)
		} else {
			cam += ",crop=w='trunc(min(iw,ih)/2)*2':h='trunc(min(iw,ih)/2)*2'"
		}
		cam += fmt.Sprintf(",scale=%d:%d:flags=bicubic,setsar=1,hflip,format=yuva420p[cam]", g.D, g.D)
		f = append(f, cam)
		f = append(f, fmt.Sprintf("[%d:v]format=gray,setsar=1[mask]", s.MaskInput))
		f = append(f, "[cam][mask]alphamerge[camr]")
		off := ringBorder(g.D) + ringShadow(g.D)
		o := next()
		f = append(f, fmt.Sprintf("[%s][%d:v]overlay=x=%d:y=%d:shortest=1[%s]", cur, s.RingInput, g.CX-off, g.CY-off, o))
		cur = o
		o = next()
		f = append(f, fmt.Sprintf("[%s][camr]overlay=x=%d:y=%d:eof_action=repeat[%s]", cur, g.CX, g.CY, o))
		cur = o
	}

	// Reactions: each one pops in, wobbles up and fades out over 2.2 s.
	const life = 2.2
	for i, sp := range s.Sprites {
		in := fmt.Sprintf("e%d", i)
		f = append(f, fmt.Sprintf("[%d:v]scale=%d:-1,format=rgba,fade=t=in:st=%s:d=0.18:alpha=1,fade=t=out:st=%s:d=0.5:alpha=1[%s]",
			sp.Input, g.EmoteW, f3(sp.At), f3(sp.At+life-0.5), in))
		x := g.EmoteX - sp.Lane*g.EmoteStep
		o := next()
		f = append(f, fmt.Sprintf("[%s][%s]overlay=x='%d+14*sin((t-%s)*7)':y='%d-(t-%s)*%s':enable='between(t,%s,%s)':shortest=1[%s]",
			cur, in, x, f3(sp.At), g.EmoteY, f3(sp.At), f3(float64(g.EmoteRise)/life), f3(sp.At), f3(sp.At+life), o))
		cur = o
	}

	if s.LogoInput >= 0 {
		f = append(f, fmt.Sprintf("[%d:v]scale=-1:%d,format=rgba,colorchannelmixer=aa=0.9[logo]", s.LogoInput, g.LogoH))
		o := next()
		f = append(f, fmt.Sprintf("[%s][logo]overlay=x=%d:y=%d:shortest=1[%s]", cur, g.LX, g.LY, o))
		cur = o
	}
	f = append(f, fmt.Sprintf("[%s]format=yuv420p[vout]", cur))
	return strings.Join(f, ";")
}

// laneFor spreads reactions that are close in time over three lanes.
func lanes(times []float64) []int {
	out := make([]int, len(times))
	var lastAt [3]float64
	for i := range lastAt {
		lastAt[i] = -100
	}
	for i, t := range times {
		best := 0
		for l := 1; l < 3; l++ {
			if lastAt[l] < lastAt[best] {
				best = l
			}
		}
		// Prefer lane 0 when it is free again.
		if t-lastAt[0] > 0.9 {
			best = 0
		}
		out[i] = best
		lastAt[best] = t
	}
	return out
}
