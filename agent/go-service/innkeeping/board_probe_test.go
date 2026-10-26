package innkeeping

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/screenocr"
	"testing"
)

func TestPhysicalBoardProbeBounds(t *testing.T) {
	valid := transitPose{Number: "3", Box: [4]int{880, 327, 26, 32}, Template: "observed-board.png"}
	if !validBoardProbe(valid) {
		t.Fatal("measured table board rejected")
	}
	for _, change := range []func(*transitPose){
		func(p *transitPose) { p.Number = "31" }, func(p *transitPose) { p.Number = "7" },
		func(p *transitPose) { p.Template = "" }, func(p *transitPose) { p.Box[1] = 32 },
		func(p *transitPose) { p.Box[0] = 1260 }, func(p *transitPose) { p.Box[2] = 90 },
	} {
		p := valid
		change(&p)
		if validBoardProbe(p) {
			t.Fatalf("invalid probe admitted: %+v", p)
		}
	}
}

func TestPhysicalBoardIdentityUsesFreshVisualEvidence(t *testing.T) {
	if !boardIdentity("3", .97, []screenocr.Token{{Text: "月", Score: .4}}) {
		t.Fatal("strong sign identity lost to uncertain OCR")
	}
	if boardIdentity("3", .97, []screenocr.Token{{Text: "5", Score: .99}}) {
		t.Fatal("contradictory digit admitted")
	}
	if boardIdentity("3", .94, nil) {
		t.Fatal("weak unconfirmed template admitted")
	}
	if !boardIdentity("3", .94, []screenocr.Token{{Text: "3", Score: .99}}) {
		t.Fatal("independent agreeing recognizers rejected")
	}
	if boardIdentity("3", .8, []screenocr.Token{{Text: "3", Score: .99}}) {
		t.Fatal("unlocalized digit admitted")
	}
}
