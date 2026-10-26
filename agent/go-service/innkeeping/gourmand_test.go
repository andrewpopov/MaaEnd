package innkeeping

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
)

func recordedGourmandTokens() []screenocr.Token {
	return []screenocr.Token{
		{Text: "0/12or spicy. Make it delicious.", Score: .98776, Box: [4]int{411, 274, 263, 26}},
		{Text: "My teeth hurt, so nothing", Score: .989241, Box: [4]int{449, 201, 258, 28}},
		{Text: "sour, please.", Score: .99787, Box: [4]int{449, 229, 131, 25}},
		{Text: "I can have anything salty and-", Score: .975328, Box: [4]int{449, 257, 258, 21}},
	}
}

func TestRecordedGourmandRequestsStaySeparate(t *testing.T) {
	requests := readGourmandRequests(recordedGourmandTokens())
	if len(requests) != 2 {
		t.Fatalf("expected the two separately observed servings: %+v", requests)
	}
	salty := DishDefinition{Traits: []string{"salty"}, TraitsComplete: true}
	spicySour := DishDefinition{Traits: []string{"spicy", "sour"}, TraitsComplete: true}
	if !requests[0].Requirements.Accepts(salty) || requests[0].Requirements.Accepts(spicySour) {
		t.Fatal("first serving must exclude sour")
	}
	if !requests[1].Requirements.Accepts(salty) || !requests[1].Requirements.Accepts(spicySour) {
		t.Fatal("second serving accepts either salty or spicy, independent of the first serving")
	}
	if requests[1].Requirements.Accepts(DishDefinition{Traits: []string{"savory"}}) {
		t.Fatal("unrelated trait must not satisfy an alternative")
	}
	if requests[0].Requirements.Accepts(DishDefinition{}) {
		t.Fatal("unknown traits cannot prove absence of sour")
	}
}

func TestGourmandRequestFromFullScreenOCR(t *testing.T) {
	v := liveFixture(t, "inn-gourmand-request")
	if v.Phase != "active" || len(v.GourmandRequests) != 2 {
		t.Fatalf("actual full-screen OCR must retain both requests: %+v", v)
	}
	if len(v.Seats) != 6 || v.Seats[4].Phase != "gourmand" {
		t.Fatal("the recorded gold seat must remain distinct from ordinary food orders")
	}
	catalog, err := LoadCatalog("../../../assets/data/Innkeeping/recipes.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, dish := range catalog {
		switch dish.ID {
		case "salted_cheese_crackers", "meaty_pilaf":
			if !v.GourmandRequests[0].Requirements.Accepts(dish) || !v.GourmandRequests[1].Requirements.Accepts(dish) {
				t.Fatalf("live-verified %s satisfies either plate, so it cannot be assigned to the wrong condition", dish.ID)
			}
		case "hot_sour_soup":
			if v.GourmandRequests[0].Requirements.Accepts(dish) || !v.GourmandRequests[1].Requirements.Accepts(dish) {
				t.Fatal("verified spicy/sour soup satisfies only the second plate")
			}
		}
	}
}

func TestGourmandPartialOrChangedRequestDoesNotShiftSlots(t *testing.T) {
	for _, change := range []string{"missing first", "unknown first", "low confidence", "different sour", "wrong region", "extra constraint", "extra line"} {
		t.Run(change, func(t *testing.T) {
			tokens := recordedGourmandTokens()
			switch change {
			case "missing first":
				tokens = []screenocr.Token{tokens[0], tokens[3]}
			case "unknown first":
				tokens[1].Text = "I want something else"
			case "low confidence":
				tokens[2].Score = .6
			case "different sour":
				tokens[2].Text = "spicy, please."
			case "wrong region":
				tokens[1].Box[1] = 40
			case "extra constraint":
				tokens[2].Text += " No flour."
			case "extra line":
				tokens = append(tokens, screenocr.Token{Text: "No flour.", Score: .99, Box: [4]int{449, 305, 100, 20}})
			}
			if got := readGourmandRequests(tokens); len(got) != 0 {
				t.Fatalf("cannot authorize incomplete or changed request: %+v", got)
			}
		})
	}
	tokens := recordedGourmandTokens()
	tokens[0].Text = "0/12 or spicy. Make it delicious."
	if len(readGourmandRequests(tokens)) != 2 {
		t.Fatal("whitespace after overlapping theme counter is harmless")
	}
}

func TestWelcomeIntentCannotClickReplacementServe(t *testing.T) {
	v := LiveView{Phase: "active", Prompt: "welcome", PromptBox: [4]int{800, 410, 100, 25}}
	p := pendingInput{Kind: "welcome"}
	if _, ok := eligibleInput(v, p); !ok {
		t.Fatal("fresh Welcome control rejected")
	}
	v.Prompt = "serve"
	if _, ok := eligibleInput(v, p); ok {
		t.Fatal("stale Welcome intent became an incompatible Serve click")
	}
	v.Prompt = "welcome"
	v.Phase = "kitchen"
	if _, ok := eligibleInput(v, p); ok {
		t.Fatal("Welcome survived a changed screen")
	}
}

func TestRecordedDessertAndNoSourRequestsKeepTheirOwnConstraints(t *testing.T) {
	data, err := os.ReadFile("testdata/inn-gourmand-dessert-nosour.json")
	if err != nil {
		t.Fatal(err)
	}
	var tokens []screenocr.Token
	if err = json.Unmarshal(data, &tokens); err != nil {
		t.Fatal(err)
	}
	requests := readGourmandRequests(tokens)
	if len(requests) != 2 {
		t.Fatalf("actual saved-image OCR lost the two requests: %+v", requests)
	}
	catalog, err := LoadCatalog("../../../assets/data/Innkeeping/recipes.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, dish := range catalog {
		switch dish.ID {
		case "salted_cheese_crackers":
			if !requests[0].Requirements.Accepts(dish) || !requests[1].Requirements.Accepts(dish) {
				t.Fatal("verified salty crackers should satisfy either recorded serving")
			}
		case "hot_sour_soup":
			if requests[0].Requirements.Accepts(dish) || requests[1].Requirements.Accepts(dish) {
				t.Fatal("sour soup must not satisfy either recorded request")
			}
		}
	}
	tokens = append(tokens, screenocr.Token{Text: "No flour.", Score: .99, Box: [4]int{690, 315, 100, 18}})
	if len(readGourmandRequests(tokens)) != 0 {
		t.Fatal("an extra unparsed condition was ignored")
	}
}
