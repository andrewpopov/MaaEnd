package innkeeping

import (
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"os"
	"strings"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
)

func menuTokens(t *testing.T, name string) []screenocr.Token {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var tokens []screenocr.Token
	if err = json.Unmarshal(data, &tokens); err != nil {
		t.Fatal(err)
	}
	return tokens
}

func TestSavedFoodMenuCannotBypassSelectedDrinkStock(t *testing.T) {
	icons, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	a := InnMenuAction{Icons: icons}
	choice := a.verifiedMenuChoice(navImage(t, "inn-fast-menu"), menuTokens(t, "inn-fast-menu"))
	if choice.Ready || choice.Box != [4]int{1120, 90, 120, 40} || !a.FoodVerified {
		t.Fatalf("verified food must lead to current drink-stock inspection: %+v", choice)
	}
	// The captured drink page supplies actual Stock fields. Mark the first
	// three observed card borders selected, and update the displayed count.
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	draw.Draw(img, img.Bounds(), navImage(t, "inn-menu-drinks"), image.Point{}, draw.Src)
	for _, point := range []image.Point{{460, 175}, {845, 175}, {460, 293}} {
		for y := point.Y; y < point.Y+25; y++ {
			img.Set(point.X, y, color.RGBA{R: 200, A: 255})
		}
	}
	tokens := menuTokens(t, "inn-menu-drinks")
	for i := range tokens {
		if tokens[i].Box[0] > 250 && tokens[i].Box[0] < 950 && tokens[i].Box[1] > 600 && strings.Contains(tokens[i].Text, "/6") {
			tokens[i].Text = "3/6"
		}
	}
	if choice = a.verifiedMenuChoice(img, tokens); !choice.Ready {
		t.Fatalf("all three selected drinks have stock: %+v", choice)
	}
	for i := range tokens {
		if strings.HasPrefix(tokens[i].Text, "Stock:") && tokens[i].Box[0] > 840 && tokens[i].Box[1] > 450 {
			tokens[i].Text = "Stock: 0"
		}
	}
	if choice = a.verifiedMenuChoice(img, tokens); !choice.Ready {
		t.Fatalf("unselected exhausted drink must not invalidate selection: %+v", choice)
	}
	for i := range tokens {
		if strings.HasPrefix(tokens[i].Text, "Stock:") && tokens[i].Box[0] > 840 && tokens[i].Box[1] < 270 {
			tokens[i].Text = "Stock: 0"
		}
	}
	if choice = a.verifiedMenuChoice(img, tokens); choice.Ready || choice.Box[2] > 0 || !strings.Contains(choice.Reason, "selected drink") {
		t.Fatalf("selected exhausted drink must stop without mutation: %+v", choice)
	}
}

func TestSelectedDrinkStockAcceptsGlyphOverhangWithoutBorrowingNeighbors(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	for _, p := range []image.Point{{460, 175}, {845, 175}, {460, 293}} {
		for y := p.Y; y < p.Y+25; y++ {
			img.Set(p.X, y, color.RGBA{R: 200, A: 255})
		}
	}
	tokens := []screenocr.Token{
		{Text: "Stock: 31", Box: [4]int{461, 234, 77, 22}, Score: .99},
		{Text: "Stock: 24", Box: [4]int{852, 234, 76, 22}, Score: .99},
		{Text: "Stock: 25", Box: [4]int{458, 352, 80, 22}, Score: .99},
	}
	if d := selectedDrinkStock(img, tokens, 3); !d.Ready {
		t.Fatalf("actual Corn Juice glyph overhang rejected: %+v", d)
	}
	tokens[2].Text = "Stock: 0"
	if d := selectedDrinkStock(img, tokens, 3); d.Ready {
		t.Fatal("selected empty drink accepted")
	}
	tokens[2].Text = "Stock: 47"
	tokens[2].Box = [4]int{852, 353, 76, 22}
	if d := selectedDrinkStock(img, tokens, 3); d.Ready {
		t.Fatal("neighbor stock used for missing Corn Juice stock")
	}
}
