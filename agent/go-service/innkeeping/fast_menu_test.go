package innkeeping

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
)

func TestFastMenuTrayIcons(t *testing.T) {
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	img := navImage(t, "inn-fast-menu-tray")
	for i, want := range []string{"fin_dumpling", "salted_cheese_crackers", "tonic_congee_greens", "shroom_dumpling"} {
		id, score := lib.Identify(img, 264+72*i, 608, 24)
		if id != want {
			t.Errorf("slot %d: want %s, got %s (%.3f)", i+1, want, id, score)
		}
	}
}

func TestFastSavedMenuIsPreservedWithVerifiedStock(t *testing.T) {
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/inn-fast-menu.json")
	if err != nil {
		t.Fatal(err)
	}
	var tokens []screenocr.Token
	if err = json.Unmarshal(data, &tokens); err != nil {
		t.Fatal(err)
	}
	img := navImage(t, "inn-fast-menu")
	if choice := planMenu(img, tokens, lib); !choice.Ready {
		t.Fatalf("actual saved faster menu is valid: %+v", choice)
	}
	// Unselected Soup may be exhausted without invalidating the four selected foods.
	for i := range tokens {
		if strings.HasPrefix(tokens[i].Text, "Preparable:") && tokens[i].Box[0] < 840 && tokens[i].Box[1] > 220 && tokens[i].Box[1] < 265 {
			tokens[i].Text = "Preparable: 0"
		}
	}
	if choice := planMenu(img, tokens, lib); !choice.Ready {
		t.Fatalf("unselected soup stock must not overwrite the saved menu: %+v", choice)
	}
	// A selected Congee with no stock must stop without removing another recipe.
	for i := range tokens {
		if strings.HasPrefix(tokens[i].Text, "Preparable:") && tokens[i].Box[0] < 840 && tokens[i].Box[1] > 460 && tokens[i].Box[1] < 505 {
			tokens[i].Text = "Preparable: 0"
		}
	}
	if choice := planMenu(img, tokens, lib); choice.Ready || choice.Box[2] > 0 || !strings.Contains(choice.Reason, "Tonic Congee") {
		t.Fatalf("selected missing stock must stop without menu mutation: %+v", choice)
	}
}

func TestFastKitchenMergedTitleAndPrice(t *testing.T) {
	v := liveFixture(t, "inn-fast-kitchen")
	if v.Phase != "kitchen" || v.Kitchen != "2" || len(v.Recipes) != 4 {
		t.Fatalf("all four actual recipes must remain available: %+v", v)
	}
	for _, recipe := range v.Recipes {
		if recipe.Dish == "shroom_dumpling" {
			if !recipe.Available || recipe.Seconds != 13 {
				t.Fatalf("actual Shroom title/price token lost its stock or time: %+v", recipe)
			}
			return
		}
	}
	t.Fatal("Shroom Dumpling32 must identify the full dish name and adjacent price")
}

func TestRecipePriceSuffixMustConsumeTheWholeTitleField(t *testing.T) {
	data, err := os.ReadFile("testdata/inn-fast-kitchen.json")
	if err != nil {
		t.Fatal(err)
	}
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadCatalog("../../../assets/data/Innkeeping/recipes.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"Shroom32", "Shroom Dumpling32 spicy", "Shroom Dumpling32000", "Shroom Dumpling0"} {
		t.Run(invalid, func(t *testing.T) {
			var tokens []screenocr.Token
			if err := json.Unmarshal(data, &tokens); err != nil {
				t.Fatal(err)
			}
			for i := range tokens {
				if tokens[i].Text == "Shroom Dumpling32" {
					tokens[i].Text = invalid
				}
			}
			v := ReadLive(navImage(t, "inn-fast-kitchen"), tokens, lib, catalog)
			for _, recipe := range v.Recipes {
				if recipe.Dish == "shroom_dumpling" {
					t.Fatalf("incomplete or additional text must not become a dish: %q", invalid)
				}
			}
		})
	}
}
