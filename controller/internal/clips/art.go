package clips

import (
	"bytes"
	"embed"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

// Sprites drawn into clips: Kenney's emotes (CC0, see art/LICENSE-emotes.txt;
// rendered from platform/src/assets/kenney/emotes at 112x132) and the Yui
// logo. ffmpeg cannot read the SVG originals.
//
//go:embed art/*.png
var artFS embed.FS

// emoteSprite is the PNG for a reaction emote, false for an unknown one.
func emoteSprite(emote string) ([]byte, bool) {
	if emote == "" || emote == "logo" {
		return nil, false
	}
	b, err := artFS.ReadFile("art/" + emote + ".png")
	return b, err == nil
}

func logoSprite() []byte {
	b, _ := artFS.ReadFile("art/logo.png")
	return b
}

// circleMask is a d x d grey image, white inside an anti-aliased circle.
// alphamerge uses it to cut the facecam round.
func circleMask(d int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, d, d))
	r := float64(d) / 2
	for y := 0; y < d; y++ {
		for x := 0; x < d; x++ {
			dist := math.Hypot(float64(x)+0.5-r, float64(y)+0.5-r)
			img.SetGray(x, y, color.Gray{Y: uint8(255 * clamp01(r-dist+0.5))})
		}
	}
	return img
}

// ringSize is the side of ringImage for a facecam of diameter d.
func ringSize(d int) int { return d + 2*ringBorder(d) + 2*ringShadow(d) }

func ringBorder(d int) int { return max(4, d/36) }
func ringShadow(d int) int { return max(8, d/14) }

// ringImage is the white ring and soft drop shadow drawn under the round
// facecam. The facecam goes at offset (ringBorder+ringShadow) inside it.
func ringImage(d int) *image.NRGBA {
	b, s := ringBorder(d), ringShadow(d)
	side := ringSize(d)
	img := image.NewNRGBA(image.Rect(0, 0, side, side))
	c := float64(side) / 2
	outer := float64(d)/2 + float64(b)
	drop := float64(s) / 3 // the shadow sits a little lower
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			// Shadow: a soft falloff outside the ring.
			sd := math.Hypot(px-c, py-c-drop) - outer
			shadow := 0.0
			if sd < float64(s) {
				t := clamp01(1 - sd/float64(s))
				shadow = 0.42 * t * t
			}
			// Ring: opaque white disc of radius outer.
			ring := clamp01(outer - math.Hypot(px-c, py-c) + 0.5)
			a := ring + shadow*(1-ring)
			if a <= 0 {
				continue
			}
			// White over black shadow, un-premultiplied.
			v := uint8(255 * ring / a)
			img.SetNRGBA(x, y, color.NRGBA{R: v, G: v, B: v, A: uint8(255 * clamp01(a))})
		}
	}
	return img
}

func clamp01(v float64) float64 {
	return math.Max(0, math.Min(1, v))
}

func writePNG(path string, img image.Image) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func writeFile(dir, name string, data []byte) error {
	return os.WriteFile(filepath.Join(dir, name), data, 0o644)
}
