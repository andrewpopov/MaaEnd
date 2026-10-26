package innkeeping

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	"testing"
)

func TestGourmandCombinedIngredientAndTraitConstraints(t *testing.T) {
	catalog, e := LoadCatalog("../../../assets/data/Innkeeping/recipes.json")
	if e != nil {
		t.Fatal(e)
	}
	r := ParseGourmandRequest("I want a sour dish that doesn't have flour")
	if !r.Parsed {
		t.Fatal("combined request not parsed")
	}
	accepted := map[string]bool{}
	for _, d := range catalog {
		if r.Accepts(d) {
			accepted[d.ID] = true
		}
	}
	if !accepted["hot_sour_soup"] || !accepted["sourberry_pudding"] || accepted["roll_of_fin_supreme"] || accepted["salted_cheese_crackers"] {
		t.Fatalf("wrong sour/no-flour candidates: %v", accepted)
	}
	r = ParseGourmandRequest("I'd like a sour dessert without milk, please.")
	if !r.Parsed {
		t.Fatal("dessert exclusion not parsed")
	}
	for _, d := range catalog {
		if d.ID == "sourberry_pudding" && r.Accepts(d) {
			t.Fatal("ordinary milk exclusion missed Milk [A]")
		}
	}
	if ingredientFamily("Starch") == ingredientFamily("Flour") {
		t.Fatal("starch is not flour")
	}
	for _, name := range []string{"Meat [A]", "Egg [A]", "Milk [A]", "Relishroom [A]", "Cabbage [A]", "Sourberry [A]"} {
		family := ingredientFamily(name)
		r = Requirements{Parsed: true, ExcludeIngredients: []string{family}}
		if r.Accepts(DishDefinition{Ingredients: map[string]int{name: 1}, IngredientsComplete: true}) {
			t.Fatalf("exclusion missed %s", name)
		}
	}
}

func TestGourmandGrammarRejectsUnknownOrAmbiguousConditions(t *testing.T) {
	for _, text := range []string{"I want a sour dish without flour and moonlight", "I want a sour dish without flour. Also no milk.", "I want a sour dish with milk or egg", "I want a sour and salty or sweet dish", "I want a sour dish without sour", "I want a soup dessert", "I want a dish", "I want a sour dish that doesn't have flour and is cheap"} {
		if ParseGourmandRequest(text).Parsed {
			t.Fatalf("unconsumed or ambiguous wording accepted: %s", text)
		}
	}
	for _, text := range []string{"I want a sour dish without flour and rice", "I want a sour soup with fish but no flour", "I want a dish without flour or milk", "I want something salty or spicy"} {
		if !ParseGourmandRequest(text).Parsed {
			t.Fatalf("supported full constraint rejected: %s", text)
		}
	}
	r := ParseGourmandRequest("I want a sour dish without flour")
	if r.Accepts(DishDefinition{Traits: []string{"sour"}, Ingredients: map[string]int{"Starch": 1}}) {
		t.Fatal("unknown full ingredient list proves no flour")
	}
}

func TestSingleLineGourmandRequestsKeepPhysicalServingSlots(t *testing.T) {
	tokens := []screenocr.Token{{Text: "I want a sour dish without flour", Score: .99, Box: [4]int{449, 201, 290, 28}}, {Text: "I want a dessert without milk", Score: .99, Box: [4]int{449, 257, 290, 28}}}
	r := readGourmandRequests(tokens)
	if len(r) != 2 || !includes(r[0].Requirements.IncludeTraits, "sour") || !includes(r[1].Requirements.IncludeTypes, "dessert") {
		t.Fatalf("physical serving slots lost: %+v", r)
	}
	if len(readGourmandRequests(tokens[1:])) != 0 {
		t.Fatal("second request shifted into first slot")
	}
}

func TestAllRecipeFactsHaveAuditedCoverage(t *testing.T) {
	recipes, e := LoadCatalog("../../../assets/data/Innkeeping/recipes.json")
	if e != nil {
		t.Fatal(e)
	}
	if len(recipes) != 44 {
		t.Fatalf("catalog coverage changed: %d", len(recipes))
	}
	seen := map[string]bool{}
	for _, d := range recipes {
		if seen[d.ID] || d.ID == "" || !d.IngredientsComplete || !d.TraitsComplete || len(d.Ingredients) == 0 || d.Category == "" {
			t.Fatalf("incomplete recipe %s", d.ID)
		}
		seen[d.ID] = true
		for _, n := range d.Ingredients {
			if n <= 0 {
				t.Fatalf("invalid ingredient quantity: %s", d.ID)
			}
		}
	}
}

func TestLowConfidenceExtraConstraintCannotDisappear(t *testing.T) {
	tokens := recordedGourmandTokens()
	tokens = append(tokens, screenocr.Token{Text: "No flour.", Score: .6, Box: [4]int{449, 305, 100, 18}})
	if len(readGourmandRequests(tokens)) != 0 {
		t.Fatal("unreadable possible constraint was ignored")
	}
}
func TestGourmandMatcherFeedsAvailableKitchenPlanner(t *testing.T) {
	catalog, e := LoadCatalog("../../../assets/data/Innkeeping/recipes.json")
	if e != nil {
		t.Fatal(e)
	}
	r := ParseGourmandRequest("I want a sour dish that doesn't have flour")
	matches := MatchGourmandRecipes(r, catalog)
	ids := []string{}
	for _, d := range matches {
		ids = append(ids, d.ID)
	}
	s := ReactiveState{Phase: "kitchen", Verified: true, TrayCapacity: 5, RemainingSeconds: 180, Dishes: catalog, Tray: map[string]int{}, Ready: map[string]int{}, Demands: []Demand{{ID: "1/gourmand0", Seat: "1", Gourmand: true, Allowed: ids, Requirements: r}}, Kitchens: []ChefKitchen{{ID: "2", Verified: true, Capacity: 3, Available: map[string]bool{"hot_sour_soup": true, "tuber_fries": true, "fin_dumpling": true, "salted_cheese_crackers": true}, CookSeconds: map[string]float64{"hot_sour_soup": 34, "tuber_fries": 23, "fin_dumpling": 25, "salted_cheese_crackers": 15}}}}
	d := PlanReactive(s)
	if d.Kind != "queue" || d.Dish != "hot_sour_soup" || d.Kitchen != "2" {
		t.Fatalf("wrong verified-menu cook choice: %+v", d)
	}
	s.Kitchens[0].Available["hot_sour_soup"] = false
	if d := PlanReactive(s); d.Kind == "queue" {
		t.Fatalf("catalog match authorized an unavailable dish: %+v", d)
	}
}

func TestKnownGourmandWordingPreservesNegationAcrossPunctuation(t *testing.T) {
	for _, text := range []string{"MY TEETH HURT SO NOTHING SOUR PLEASE", "Dessert—but not too sweet! I’m trying to tone my bod."} {
		if !ParseGourmandRequest(text).Parsed {
			t.Fatalf("punctuation changed known request: %s", text)
		}
	}
	for _, text := range []string{"My teeth hurt, so nothing sour, please, and no flour.", "Dessert, but not too sour. I'm trying to tone my bod."} {
		if ParseGourmandRequest(text).Parsed {
			t.Fatalf("unobserved changed constraint was accepted: %s", text)
		}
	}
}
