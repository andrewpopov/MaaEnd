package charactercontroller

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"testing"
)

func TestTouchCameraUsesNativeCameraScanOriginAndYawDirection(t *testing.T) {
	for _, delta := range []struct{ dx, dy int }{{90, 0}, {-90, 0}, {0, 60}, {0, -60}} {
		begin, end := touchCameraSwipe(delta.dx, delta.dy)
		if begin != (maa.Rect{520, 360, 1, 1}) || end != (maa.Rect{520 + delta.dx, 360 - delta.dy, 1, 1}) {
			t.Fatalf("incorrect Android camera gesture: %v -> %v", begin, end)
		}
		again, _ := touchCameraSwipe(delta.dx, delta.dy)
		if again != begin {
			t.Fatal("new touch gestures must not accumulate desktop hover offsets")
		}
	}
	_, end := touchCameraSwipe(1000, -1000)
	if end[0] < 0 || end[0] >= screenW || end[1] < 0 || end[1] >= screenH {
		t.Fatal("touch endpoint outside screen")
	}
}
