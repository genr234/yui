package remotecontrol

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/jpeg"
	"time"
)

// Desktop is a screenshot of the whole screen, native windows included.
type Desktop struct {
	Image  string `json:"image"` // data:image/jpeg;base64,...
	Width  int    `json:"width"` // screen size in pixels
	Height int    `json:"height"`
	At     string `json:"at"`
}

func encodeDesktop(img *image.RGBA, maxSide int) (Desktop, error) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	scaled := scaleDown(img, maxSide)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, scaled, &jpeg.Options{Quality: 70}); err != nil {
		return Desktop{}, err
	}
	return Desktop{
		Image:  "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()),
		Width:  w,
		Height: h,
		At:     time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// scaleDown shrinks img so its longer side is at most maxSide (box filter).
func scaleDown(img *image.RGBA, maxSide int) *image.RGBA {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	long := max(w, h)
	if maxSide <= 0 || long <= maxSide {
		return img
	}
	nw, nh := max(1, w*maxSide/long), max(1, h*maxSide/long)
	out := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		y0, y1 := y*h/nh, max((y+1)*h/nh, y*h/nh+1)
		for x := 0; x < nw; x++ {
			x0, x1 := x*w/nw, max((x+1)*w/nw, x*w/nw+1)
			var r, g, b, n uint32
			for sy := y0; sy < y1; sy++ {
				row := img.Pix[sy*img.Stride:]
				for sx := x0; sx < x1; sx++ {
					p := row[sx*4:]
					r += uint32(p[0])
					g += uint32(p[1])
					b += uint32(p[2])
					n++
				}
			}
			o := out.Pix[y*out.Stride+x*4:]
			o[0], o[1], o[2], o[3] = uint8(r/n), uint8(g/n), uint8(b/n), 255
		}
	}
	return out
}
