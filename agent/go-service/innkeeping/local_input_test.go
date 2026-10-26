package innkeeping

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestLocalControlReadsFreshTrayAndPrompt(t *testing.T) {
	now := time.Unix(1000, 0)
	lib, err := LoadIcons("../../../assets/data/Innkeeping/icons")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadCatalog("../../../assets/data/Innkeeping/recipes.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"inn-fin-held", "inn-first-serve", "inn-ready-counter"} {
		t.Run(name, func(t *testing.T) {
			img := navImage(t, name)
			full := liveFixture(t, name)
			s := &liveSession{Icons: lib, Catalog: catalog, InputPhase: full.Phase, InputHUD: newCameraSearch(img, full.AddOrderBox, nil, false, now)}
			visual := readLivePhase(img, nil, lib, catalog, full.Phase)
			s.InputVisual = &visual
			data, err := os.ReadFile("testdata/" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var tokens []screenocr.Token
			if err := json.Unmarshal(data, &tokens); err != nil {
				t.Fatal(err)
			}
			read := func(roi maa.Rect) ([]screenocr.Token, error) {
				var out []screenocr.Token
				for _, token := range tokens {
					if token.Box[0] >= roi[0] && token.Box[0] < roi[0]+roi[2] && token.Box[1] >= roi[1] && token.Box[1] < roi[1]+roi[3] {
						out = append(out, token)
					}
				}
				return out, nil
			}
			navCalls := 0
			nav, ok, err := localNavigationView(s, img, now, func(roi maa.Rect) ([]screenocr.Token, error) {
				navCalls++
				if roi != (maa.Rect{720, 390, 480, 80}) {
					t.Fatal("navigation reread unchanged order text", roi)
				}
				return read(roi)
			})
			if err != nil || !ok || !reflect.DeepEqual(nav.Seats, visual.Seats) || !reflect.DeepEqual(nav.TraySlots, full.TraySlots) || nav.SelectedSlot != full.SelectedSlot || nav.Prompt != full.Prompt || nav.PromptDish != full.PromptDish || nav.AddOrderBox != full.AddOrderBox {
				t.Fatal("focused navigation lost fresh controls or inventory", nav, full, err)
			}
			if navCalls != 1 {
				t.Fatal("navigation should read only its fresh interaction prompt", navCalls)
			}
			before := s.InputVisual
			changed := *before
			changed.TraySlots = append([]string(nil), before.TraySlots...)
			changed.TraySlots[0] = "different_dish"
			s.InputVisual = &changed
			if _, ok, _ := localNavigationView(s, img, now, func(maa.Rect) ([]screenocr.Token, error) {
				t.Fatal("changed inventory bypassed full observation")
				return nil, nil
			}); ok {
				t.Fatal("changed inventory allowed local navigation")
			}
			s.InputVisual = before
			for _, check := range []struct {
				name  string
				at    time.Time
				frame string
			}{{"stale", now.Add(3 * time.Second), name}, {"changed phase", now, "inn-live-k1"}} {
				if _, ok, _ := localNavigationView(s, navImage(t, check.frame), check.at, func(maa.Rect) ([]screenocr.Token, error) {
					t.Fatal("invalid world guard ran focused navigation OCR", check.name)
					return nil, nil
				}); ok {
					t.Fatal("invalid world guard authorized navigation", check.name)
				}
			}
			local, ok, err := localInputView(s, pendingInput{Kind: "pickup"}, img, now, read)
			if err != nil || !ok {
				t.Fatal("local validation unavailable", err)
			}
			if !reflect.DeepEqual(local.Tray, full.Tray) || !reflect.DeepEqual(local.TraySlots, full.TraySlots) || local.SelectedSlot != full.SelectedSlot || local.Prompt != full.Prompt || local.PromptDish != full.PromptDish {
				t.Fatal("local control disagrees with full fresh recognition", local, full)
			}
			ack, ok, err := localInputView(s, pendingInput{Kind: "select_tray", IssuedAt: now}, img, now, read)
			if err != nil || !ok || ack.Prompt != full.Prompt {
				t.Fatal("selection acknowledgement lost fresh Serve prompt", ack.Prompt, full.Prompt, err)
			}
			ack, ok, err = localInputView(s, pendingInput{Kind: "serve", IssuedAt: now}, img, now, read)
			if err != nil || !ok || !reflect.DeepEqual(ack.Seats, full.Seats) {
				t.Fatal("serve acknowledgement lost actual dining states", ack.Seats, full.Seats, err)
			}
			_, ok, err = localInputView(s, pendingInput{Kind: "select_tray"}, img, now, func(maa.Rect) ([]screenocr.Token, error) {
				t.Fatal("tray selection click unnecessarily ran OCR")
				return nil, nil
			})
			if err != nil || !ok {
				t.Fatal("tray selection local validation rejected", err)
			}
			if _, ok, _ := localInputView(s, pendingInput{Kind: "pickup"}, img, now.Add(3*time.Second), read); ok {
				t.Fatal("stale phase authorized control")
			}
			if _, ok, _ := localInputView(s, pendingInput{Kind: "queue"}, img, now, read); ok {
				t.Fatal("queue bypassed full validation")
			}
			if _, ok, _ := localInputView(s, pendingInput{Kind: "pickup"}, navImage(t, "inn-live-k1"), now, read); ok {
				t.Fatal("changed phase authorized control")
			}
		})
	}
}
