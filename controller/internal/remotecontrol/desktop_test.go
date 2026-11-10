package remotecontrol

import (
	"image"
	"strings"
	"testing"
)

func TestEncodeDesktopScalesDown(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	d, err := encodeDesktop(img, 960)
	if err != nil {
		t.Fatal(err)
	}
	if d.Width != 1920 || d.Height != 1080 || !strings.HasPrefix(d.Image, "data:image/jpeg;base64,") {
		t.Fatalf("got %+v", d.Width)
	}
	if got := scaleDown(img, 960).Bounds().Dx(); got != 960 {
		t.Fatalf("scaled width %d", got)
	}
}
