package remotecontrol

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func TestClampClip(t *testing.T) {
	whole := Clip{0, 0, 1080, 1920}
	cases := []struct {
		name, raw string
		want      Clip
	}{
		{"none", "null", whole},
		{"garbage", "nope", whole},
		{"missing field", `{"x":0,"y":0,"w":10}`, whole},
		{"inside", `{"x":0,"y":656,"w":1080,"h":608}`, Clip{0, 656, 1080, 608}},
		{"overhang", `{"x":-20,"y":1800,"w":1200,"h":400}`, Clip{0, 1800, 1080, 120}},
		{"outside", `{"x":0,"y":3000,"w":100,"h":100}`, whole},
		{"sliver", `{"x":0,"y":0,"w":3,"h":500}`, whole},
	}
	for _, c := range cases {
		if got := ClampClip(c.raw, 1080, 1920); got != c.want {
			t.Errorf("%s: got %+v want %+v", c.name, got, c.want)
		}
	}
}

func TestCropDesktop(t *testing.T) {
	// A 200x100 desktop whose origin is (-50, 0); the right half is red.
	img := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for y := 0; y < 100; y++ {
		for x := 100; x < 200; x++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	// Desktop x 50..150 is image x 100..200: all red.
	shot, err := CropDesktop(img, image.Pt(-50, 0), image.Rect(50, 0, 150, 100), 0, 80)
	if err != nil {
		t.Fatal(err)
	}
	if shot.W != 100 || shot.H != 100 {
		t.Fatalf("size %dx%d", shot.W, shot.H)
	}
	dec, err := jpeg.Decode(bytes.NewReader(shot.JPEG))
	if err != nil {
		t.Fatal(err)
	}
	r, g, _, _ := dec.At(50, 50).RGBA()
	if r>>8 < 200 || g>>8 > 60 {
		t.Fatalf("not red: r=%d g=%d", r>>8, g>>8)
	}
	// Shrunk to maxSide, overhang clamped.
	shot, err = CropDesktop(img, image.Pt(-50, 0), image.Rect(100, 0, 400, 100), 50, 80)
	if err != nil || shot.W != 25 || shot.H != 50 {
		t.Fatalf("%+v %v", shot, err)
	}
	if _, err := CropDesktop(img, image.Pt(0, 0), image.Rect(500, 500, 600, 600), 0, 80); err == nil {
		t.Fatal("off-screen rect accepted")
	}
	if _, err := CropDesktop(img, image.Point{}, image.Rectangle{}, 0, 80); err == nil {
		t.Fatal("empty rect accepted")
	}
}
