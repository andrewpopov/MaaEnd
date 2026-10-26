package innkeeping

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"image"
	"image/color"
	"image/draw"
	"testing"
)

// Boxes and focused strings are from the recorded two-request quest-overlay failure.
func TestFocusedGourmandRecoversObscuredExplanation(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	draw.Draw(img, image.Rect(350, 235, 640, 337), image.NewUniform(color.RGBA{240, 205, 140, 255}), image.Point{}, draw.Src)
	tokens := []screenocr.Token{
		{Text: "My teeth hurt, so nothing", Score: .989174, Box: [4]int{390, 236, 227, 31}},
		{Text: "sour", Score: .996684, Box: [4]int{396, 267, 42, 14}},
		{Text: "please.", Score: .995284, Box: [4]int{440, 266, 62, 18}},
		{Text: "0/", Score: .963656, Box: [4]int{422, 277, 24, 15}},
		{Text: "rt", Score: .777221, Box: [4]int{444, 292, 16, 11}},
		{Text: ", but not too sweet", Score: .996802, Box: [4]int{458, 287, 155, 18}},
		{Text: "I'm trying to tone my bod.", Score: .988371, Box: [4]int{392, 304, 217, 26}},
	}
	texts := []string{"My teeth hurt, so nothing", "sourplease.", "Dessert, but not too sweet.", "I'm trying to tone my bod."}
	regions := []maa.Rect{{394, 233, 240, 29}, {394, 263, 240, 22}, {394, 285, 240, 21}, {394, 306, 240, 27}}
	i := 0
	got := focusedGourmandRequests(img, tokens, []SeatView{{Seat: "2", Phase: "gourmand"}}, nil, func(r maa.Rect) ([]screenocr.Token, error) {
		if i >= len(texts) {
			t.Fatal("unexpected additional OCR")
		}
		if r != regions[i] {
			t.Fatalf("crop %d: got %v want %v", i, r, regions[i])
		}
		s := texts[i]
		i++
		return []screenocr.Token{{Text: s, Score: .97, Box: [4]int(r)}}, nil
	})
	if len(got) != 2 || i != 4 {
		t.Fatalf("requests=%+v lines=%d", got, i)
	}
	plain := DishDefinition{Category: "dessert", TraitsComplete: true}
	if !got[0].Requirements.Accepts(plain) || !got[1].Requirements.Accepts(plain) {
		t.Fatal("plain dessert should satisfy either serving")
	}
	sweet := plain
	sweet.Traits = []string{"sweet"}
	if !got[0].Requirements.Accepts(sweet) || got[1].Requirements.Accepts(sweet) {
		t.Fatal("sweet is excluded only by the second request")
	}
}

func TestGourmandNoRiceExcludesEveryRiceGrade(t *testing.T) {
	r := ParseGourmandRequest("Anything, as long as there are no rice grains in it.")
	if !r.Parsed {
		t.Fatal("recorded request was not parsed")
	}
	for _, name := range []string{"Rice", "Rice [A]", "Amber Rice"} {
		if r.Accepts(DishDefinition{Ingredients: map[string]int{name: 1}, IngredientsComplete: true}) {
			t.Fatalf("allowed %s", name)
		}
	}
	if !r.Accepts(DishDefinition{Ingredients: map[string]int{"Pingtuber": 2}, IngredientsComplete: true}) {
		t.Fatal("fries should qualify")
	}
}

func TestGoldCardPreservesObservedGuestLifecycle(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	draw.Draw(img, image.Rect(234, 20, 352, 120), image.NewUniform(color.RGBA{255, 220, 140, 255}), image.Point{}, draw.Src)
	for text, want := range map[string]string{"Dining": "dining", "Bill Ready": "bill_ready", "Ordering": "ordering"} {
		v := readLivePhase(img, []screenocr.Token{{Text: text, Score: .99, Box: [4]int{250, 90, 90, 20}}}, IconLibrary{}, nil, "active")
		if v.Seats[0].Phase != want {
			t.Fatalf("gold %s became %s", text, v.Seats[0].Phase)
		}
	}
}

func TestCompleteGrammarCannotHideMissingExclusionLine(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	bubble := image.Rect(350, 235, 640, 335)
	draw.Draw(img, bubble, image.NewUniform(color.RGBA{240, 205, 140, 255}), image.Point{}, draw.Src)
	for _, line := range []image.Rectangle{image.Rect(410, 245, 500, 255), image.Rect(410, 275, 520, 285)} {
		draw.Draw(img, line, image.NewUniform(color.RGBA{110, 95, 60, 255}), image.Point{}, draw.Src)
	}
	tokens := []screenocr.Token{{Text: "I want a dessert", Score: .99, Box: [4]int{400, 240, 140, 20}}}
	if !ParseGourmandRequest(tokens[0].Text).Parsed || gourmandTextCovered(img, bubble, tokens) {
		t.Fatal("parsable prefix hid the visible missing exclusion line")
	}
	tokens = append(tokens, screenocr.Token{Text: "without milk", Score: .99, Box: [4]int{400, 270, 140, 20}})
	if !gourmandTextCovered(img, bubble, tokens) {
		t.Fatal("both independently observed text lines were rejected")
	}
}

func TestMixedGoldCountsUseThePopupTable(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	disk := color.RGBA{227, 197, 114, 255}
	img.Set(234+59, 54, disk)
	img.Set(352+30, 54, disk)
	img.Set(352+85, 54, disk)
	seats := []SeatView{{Seat: "1", Phase: "gourmand"}, {Seat: "2", Phase: "gourmand"}}
	bubble := [4]int{450, 200, 250, 100}
	tokens := []screenocr.Token{{Text: "1", Score: .99, Box: [4]int{500, 312, 30, 40}}}
	current := gourmandRequestSeats(seats, tokens, bubble)
	if len(current) != 1 || current[0].Seat != "1" || gourmandPortions(img, current) != 1 {
		t.Fatal("another gold card supplied the serving count")
	}
	requests := readGourmandRequestsCount([]screenocr.Token{{Text: "Dessert, please.", Score: .99, Box: [4]int{480, 230, 150, 30}}}, gourmandPortions(img, current))
	if len(requests) != 1 {
		t.Fatal("single remaining request rejected because another diner has two plates")
	}
}
