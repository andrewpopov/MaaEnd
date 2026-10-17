package goods

import (
	"errors"
	"image"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/outposttrading/internal/ocrmatch"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// 一格库存读不出来时只把这一格记为库存未知，其余格子照常可用；
// 以前整页报错，选择与耗尽两个分支都拿不到结果，只能等超时
func TestBuildStockPageItemsKeepsUnreadableStockAsUnknown(t *testing.T) {
	offsets := stockCellOffsets{
		Name:     maa.Rect{34, -59, 164, 16},
		Quantity: maa.Rect{33, -6, 80, 7},
		Click:    maa.Rect{42, -58, 100, 34},
	}
	anchors := []maa.Rect{{100, 100, 17, 17}, {500, 100, 17, 17}, {900, 100, 17, 17}}
	groups := []itemPriorityGroup{
		{ItemID: "a", Candidates: []string{"甲货"}},
		{ItemID: "b", Candidates: []string{"乙货"}},
		{ItemID: "c", Candidates: []string{"丙货"}},
	}
	ocrItems := []ocrmatch.Item{
		{Text: "甲货", Box: maa.Rect{140, 45, 50, 20}},
		{Text: "16", Box: maa.Rect{140, 98, 20, 15}},
		{Text: "乙货", Box: maa.Rect{540, 45, 50, 20}},
		{Text: "丙货", Box: maa.Rect{940, 45, 50, 20}},
		{Text: "0", Box: maa.Rect{940, 98, 10, 15}},
	}
	fallbackCalls := 0
	fallback := func(maa.Rect) (string, error) {
		fallbackCalls++
		return "", errors.New("no stock quantity recognized")
	}

	items, err := buildStockPageItems(ocrItems, anchors, groups, offsets, image.Rect(0, 0, 1280, 720), fallback)
	if err != nil {
		t.Fatalf("buildStockPageItems() error = %v", err)
	}
	if fallbackCalls != 1 {
		t.Fatalf("fallback calls = %d, want 1", fallbackCalls)
	}
	got := make(map[string]stockPageItem, len(items))
	for _, item := range items {
		got[item.ItemID] = item
	}
	if item := got["a"]; !item.StockKnown || item.Quantity != 16 {
		t.Fatalf("item a = %+v, want known stock 16", item)
	}
	if item := got["b"]; item.StockKnown || item.ClickBox.Width() <= 0 {
		t.Fatalf("item b = %+v, want unknown stock with a click box", item)
	}
	if item := got["c"]; !item.StockKnown || item.Quantity != 0 {
		t.Fatalf("item c = %+v, want known stock 0", item)
	}
}

func TestStockNamesRecognizeEnglishRarityMarker(t *testing.T) {
	groups := []itemPriorityGroup{
		{ItemID: "battery", Candidates: []string{"SC Wuling Battery"}},
		{ItemID: "part", Candidates: []string{"Hetonite Part"}},
		{ItemID: "xiranite", Candidates: []string{"Xiranite"}},
	}
	items := []ocrmatch.Item{
		{Text: "I SC Wuling Battery", Box: maa.Rect{204, 153, 180, 26}},
		{Text: "IHetonite Part", Box: maa.Rect{593, 155, 138, 21}},
		{Text: "IXiranite", Box: maa.Rect{980, 426, 88, 25}},
	}
	if got := matchStockPageNames(items, groups); len(got) != 3 {
		t.Fatalf("matched %d names, want 3: %+v", len(got), got)
	}
	if items[0].Text != "I SC Wuling Battery" {
		t.Fatal("input OCR was mutated")
	}
}

func TestStockNamesKeepExactNamesAndRejectOtherPrefixes(t *testing.T) {
	groups := []itemPriorityGroup{
		{ItemID: "iron", Candidates: []string{"Iron"}},
		{ItemID: "ron", Candidates: []string{"ron"}},
		{ItemID: "part", Candidates: []string{"Hetonite Part"}},
	}
	got := matchStockPageNames([]ocrmatch.Item{
		{Text: "Iron", Box: maa.Rect{0, 0, 40, 20}},
		{Text: "JHetonite Part", Box: maa.Rect{100, 0, 120, 20}},
	}, groups)
	if len(got) != 1 || got[0].itemID != "iron" {
		t.Fatalf("unexpected matches: %+v", got)
	}
}

func TestStockNamesCorrectSpuriousLatinAccentOnlyForKnownFullName(t *testing.T) {
	groups := []itemPriorityGroup{{ItemID: "tea", Candidates: []string{"Jincao Tea"}}, {ItemID: "accented", Candidates: []string{"Café"}}, {ItemID: "plain", Candidates: []string{"Cafe"}}}
	for _, text := range []string{"Jincão Tea", "IJincão Tea"} {
		input := []ocrmatch.Item{{Text: text, Box: maa.Rect{981, 289, 112, 26}}}
		got := matchStockPageNames(input, groups)
		if len(got) != 1 || got[0].itemID != "tea" {
			t.Fatalf("%q: got %+v", text, got)
		}
		if input[0].Text != text {
			t.Fatal("input OCR mutated")
		}
	}
	got := matchStockPageNames([]ocrmatch.Item{{Text: "Café", Box: maa.Rect{0, 0, 30, 20}}}, groups)
	if len(got) != 1 || got[0].itemID != "accented" {
		t.Fatalf("exact accented catalog name changed: %+v", got)
	}
	for _, text := range []string{"Jincão Drink", "Jincão Tea Extra"} {
		if got := matchStockPageNames([]ocrmatch.Item{{Text: text, Box: maa.Rect{0, 0, 100, 20}}}, groups); len(got) != 0 {
			t.Fatalf("unknown name %q matched: %+v", text, got)
		}
	}
	for _, text := range []string{"ガラス", "晶体", "무릉"} {
		if got := foldStockLatinAccents(text); got != text {
			t.Fatalf("non-Latin text %q changed to %q", text, got)
		}
	}
}
