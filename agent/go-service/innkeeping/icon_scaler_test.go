package innkeeping

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"testing"

	xdraw "golang.org/x/image/draw"
)

func TestCachedIconScalerPreservesNormalizedPixels(t *testing.T) {
	img := navImage(t, "inn-fin-held")
	for _, radius := range []int{8, 14, 18, 20, 22, 28, 32, 64, 66} {
		for _, center := range []image.Point{{483, 623}, {291, 70}, {640, 360}} {
			t.Run(fmt.Sprintf("r%d-%d-%d", radius, center.X, center.Y), func(t *testing.T) {
				t.Parallel()
				box := image.Rect(center.X-radius, center.Y-radius, center.X+radius, center.Y+radius)
				original, cached := image.NewRGBA(image.Rect(0, 0, 24, 24)), image.NewRGBA(image.Rect(0, 0, 24, 24))
				xdraw.CatmullRom.Scale(original, original.Bounds(), img, box, draw.Src, nil)
				cachedIconScaler(radius).Scale(cached, cached.Bounds(), img, box, draw.Src, nil)
				if !bytes.Equal(original.Pix, cached.Pix) {
					t.Fatal("cached scaler changed calibrated icon samples")
				}
			})
		}
	}
}
