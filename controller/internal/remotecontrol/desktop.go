package remotecontrol

import (
	"bytes"
	"encoding/base64"
	"errors"
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

// ErrDesktopUnsupported: native-window capture needs Windows.
var ErrDesktopUnsupported = errors.New("desktop capture is only supported on Windows")

// Shot is an encoded JPEG and its pixel size.
type Shot struct {
	JPEG []byte
	W, H int
}

// CropDesktop crops a full-desktop image (whose top-left is at origin in
// desktop coordinates) to rect, shrinks it to maxSide and encodes a JPEG.
// A rect that misses the image is an error; one that overhangs is clamped.
func CropDesktop(img *image.RGBA, origin image.Point, rect image.Rectangle, maxSide, quality int) (Shot, error) {
	if rect.Empty() {
		return Shot{}, errors.New("the game area is empty")
	}
	want := rect.Sub(origin).Intersect(img.Bounds())
	if want.Empty() {
		return Shot{}, errors.New("the game area is off screen")
	}
	crop := image.NewRGBA(image.Rect(0, 0, want.Dx(), want.Dy()))
	for y := 0; y < want.Dy(); y++ {
		copy(crop.Pix[y*crop.Stride:y*crop.Stride+want.Dx()*4], img.Pix[(want.Min.Y+y)*img.Stride+want.Min.X*4:])
	}
	scaled := scaleDown(crop, maxSide)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, scaled, &jpeg.Options{Quality: quality}); err != nil {
		return Shot{}, err
	}
	return Shot{JPEG: buf.Bytes(), W: scaled.Bounds().Dx(), H: scaled.Bounds().Dy()}, nil
}

// CaptureDesktopArea grabs rect (primary-monitor pixels, native windows
// included) as a JPEG. Windows only.
func CaptureDesktopArea(rect image.Rectangle, maxSide, quality int) (Shot, error) {
	img, origin, err := grabDesktop()
	if err != nil {
		return Shot{}, err
	}
	return CropDesktop(img, origin, rect, maxSide, quality)
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
