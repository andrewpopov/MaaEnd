package innkeeping

import (
	"image"
	"image/draw"
	"os"
	"testing"
)

func BenchmarkIdentifyCapturedTray(b *testing.B) {
	f, err := os.Open("testdata/inn-fin-held.png")
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		b.Fatal(err)
	}
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lib.Identify(img, 483, 623, 28)
	}
}

func BenchmarkReadCapturedWorldIcons(b *testing.B) {
	f, err := os.Open("testdata/inn-fin-held.png")
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		b.Fatal(err)
	}
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		b.Fatal(err)
	}
	catalog, err := LoadCatalog("../../../assets/data/Innkeeping/recipes.json")
	if err != nil {
		b.Fatal(err)
	}
	opaque := image.NewRGBA(img.Bounds())
	draw.Draw(opaque, opaque.Bounds(), img, img.Bounds().Min, draw.Src)
	for name, frame := range map[string]image.Image{"decoded": img, "rgba": opaque} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				readLivePhase(frame, nil, lib, catalog, "active")
			}
		})
	}
}
