// Command innframe inspects recorded inn HUD and inventory images without a controller.
package main

import (
	"encoding/json"
	"flag"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/innkeeping"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	"github.com/rs/zerolog/log"
)

func main() {
	file := flag.String("image", "", "1280x720 recorded screenshot")
	data := flag.String("data", "install/data/Innkeeping", "inn catalog and icon directory")
	hudOnly := flag.Bool("hud-only", false, "skip floating-food scans when only HUD and text are needed")
	tokenFile := flag.String("tokens", "", "native OCR tokens captured from this same image")
	phase := flag.String("phase", "active", "independently verified screenshot layout: active or kitchen")
	flag.Parse()
	if *file == "" || (*tokenFile == "" && *phase != "active" && *phase != "kitchen") {
		log.Fatal().Msg("Specify an image and a supported, independently verified layout")
	}
	f, err := os.Open(*file)
	if err != nil {
		log.Fatal().Err(err).Msg("Open recorded frame")
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		log.Fatal().Err(err).Msg("Decode recorded frame")
	}
	if img.Bounds() != image.Rect(0, 0, 1280, 720) {
		log.Fatal().Msg("Recorded frame must be1280x720")
	}
	catalog, err := innkeeping.LoadCatalog(filepath.Join(*data, "recipes.json"))
	if err != nil {
		log.Fatal().Err(err).Msg("Load inn catalog")
	}
	icons, err := innkeeping.LoadIcons(filepath.Join(*data, "icons"))
	if err != nil {
		log.Fatal().Err(err).Msg("Load inn icons")
	}
	labels := []string{"Current Income", "Patrons Seated", "Dishes Sold"}
	if *phase == "kitchen" {
		labels = []string{"Kitchen 1", "Kitchen 2"}
	}
	tokens := make([]screenocr.Token, len(labels))
	for i, label := range labels {
		tokens[i] = screenocr.Token{Text: label, Score: 1}
	}

	assumedLayout := *phase
	if *tokenFile != "" {
		raw, readErr := os.ReadFile(*tokenFile)
		if readErr != nil {
			log.Fatal().Err(readErr).Msg("Read same-image native OCR tokens")
		}
		if err := json.Unmarshal(raw, &tokens); err != nil {
			log.Fatal().Err(err).Msg("Decode native OCR tokens")
		}
		assumedLayout = ""
	}
	result := struct {
		Image           string                 `json:"image"`
		AssumedLayout   string                 `json:"assumed_layout"`
		InputAuthorized bool                   `json:"input_authorized"`
		View            innkeeping.LiveView    `json:"view"`
		CounterFoods    []innkeeping.WorldFood `json:"counter_foods"`
		DinerFoods      []innkeeping.WorldFood `json:"diner_foods"`
	}{Image: *file, AssumedLayout: assumedLayout, View: innkeeping.ReadLive(img, tokens, icons, catalog), CounterFoods: nil, DinerFoods: nil}
	if !*hudOnly {
		result.CounterFoods = innkeeping.WorldFoods(img, icons, true)
		result.DinerFoods = innkeeping.WorldFoods(img, icons, false)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		log.Fatal().Err(err).Msg("Write frame inspection")
	}
}
