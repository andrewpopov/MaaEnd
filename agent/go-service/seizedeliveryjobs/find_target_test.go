package seizedeliveryjobs

import "testing"

func TestAndroidAcceptRegionContainsEnglishLabel(t *testing.T) {
	// Boxes read by native OCR from the saved 1280x720 English delivery list.
	rows := []struct{ reward, label []int }{
		{[]int{846, 273, 53, 24}, []int{1036, 270, 170, 31}},
		{[]int{846, 380, 53, 24}, []int{1035, 378, 171, 31}},
		{[]int{846, 488, 53, 24}, []int{1037, 486, 168, 28}},
		{[]int{846, 595, 53, 24}, []int{1041, 598, 161, 20}},
	}
	for i, row := range rows {
		roi := offsetBox(row.reward, rowOffsetsForController("Adb").rewardToAccept)
		b := row.label
		if roi[0] > b[0] || roi[1] > b[1] || roi[0]+roi[2] < b[0]+b[2] || roi[1]+roi[3] < b[1]+b[3] {
			t.Errorf("row %d: accept region %v clips English label %v", i+1, roi, b)
		}
	}
}
