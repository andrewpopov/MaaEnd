package autofight

import "testing"

func TestMemberLevelResourceVerification(t *testing.T) {
	before := memberLevelScreen{Level: 82, Currency: 791141, LowStock: 2484, HighStock: 737}
	preview := before
	preview.Delta, preview.LowCount, preview.HighCount, preview.Cost = 1, 6, 3, 13043
	if !validMemberLevelPreview(before, preview) {
		t.Fatal("captured preview rejected")
	}
	state := &memberLevelState{Before: before, LowCost: 6, HighCost: 3, Cost: 13043}
	after := memberLevelScreen{Level: 83, Currency: 778098, LowStock: 2478, HighStock: 734}
	if !validMemberLevelResult(state, after) {
		t.Fatal("verified pilot result rejected")
	}
	cases := []memberLevelScreen{after, after, after, after}
	cases[0].Level = 82
	cases[1].Currency--
	cases[2].LowStock++
	cases[3].Delta = 1
	for index, screen := range cases {
		if validMemberLevelResult(state, screen) {
			t.Fatalf("accepted changed result %d", index)
		}
	}
	preview.LowCount = -1
	if validMemberLevelPreview(before, preview) {
		t.Fatal("accepted negative consumption")
	}
}

func TestMemberLevelNameNormalization(t *testing.T) {
	if memberName("Zhuang Fangyi") != memberName("ZhuangFangyi") {
		t.Fatal("name normalization differs")
	}
	if memberOverviewMatches("Rossi/Overview", "Si") {
		t.Fatal("matched Si as a suffix of Rossi")
	}
	if !memberOverviewMatches("Si / Overview", "Si") {
		t.Fatal("did not match the exact selected member")
	}
}
