package innkeeping

import (
	"encoding/json"
	"image"
	"image/color"
	"os"
	"testing"
)

func TestRecordedSwipeWireCoordinatesAreExact(t *testing.T) {
	for _, tc := range []struct {
		kind, begin string
	}{{"walk", `[195,551,1,1]`}, {"camera", `[1000,350,1,1]`}} {
		data, err := json.Marshal(movementSwipeParam(movementCommand{Kind: tc.kind, X: 275, Y: 551, Milliseconds: 20, HoldMilliseconds: 80}))
		if err != nil {
			t.Fatal(err)
		}
		var wire map[string]json.RawMessage
		if err = json.Unmarshal(data, &wire); err != nil {
			t.Fatal(err)
		}
		for field, want := range map[string]string{"begin": tc.begin, "end": `[[275,551,1,1]]`, "duration": `[20]`, "end_hold": `[80]`} {
			if string(wire[field]) != want {
				t.Errorf("%s %s=%s, want %s", tc.kind, field, wire[field], want)
			}
		}
	}
}

func TestRecordedCameraOriginAvoidsInteractionMenu(t *testing.T) {
	data, err := json.Marshal(movementSwipeParam(movementCommand{Kind: "camera", Origin: [2]int{1000, 230}, X: 650, Y: 230, Milliseconds: 250, Pipeline: true}))
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err = json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if string(wire["begin"]) != `[1000,230,1,1]` || string(wire["end"]) != `[[650,230,1,1]]` {
		t.Fatalf("measured clear camera origin was lost: %s", data)
	}
}

func TestUnverifiedRouteCannotReplaceVisualNavigation(t *testing.T) {
	f, err := os.Open("testdata/inn-fin-held.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	r := fixedRoute{Version: 1, ID: "candidate", Goal: "diner", Seat: "2", Start: fingerprint(img), End: fingerprint(img), Steps: []movementCommand{{Kind: "walk", X: 275, Y: 551, Milliseconds: 20, HoldMilliseconds: 80}}}
	v := LiveView{Phase: "active", Tray: map[string]int{"soup": 1}, Seats: []SeatView{{Seat: "2", Dishes: []string{"soup"}}}}
	s := &liveSession{Fixed: fixedRouteState{Routes: []fixedRoute{r}}}
	if s.startFixedRoute(img, v) {
		t.Fatal("unverified capture selected for normal navigation")
	}
	s.Fixed.Routes[0].Replays = 1
	if !s.startFixedRoute(img, v) {
		t.Fatal("verified compatible route not selected")
	}
	s.Fixed.Routes[0].CalibrationOnly = true
	if s.startFixedRoute(img, v) {
		t.Fatal("geometry-only probe selected for production serving")
	}
	s.Fixed.Routes[0].CalibrationOnly = false
	if s.prepareRouteProbe(nil, r.ID, img, LiveView{Phase: "active"}) {
		t.Fatal("production delivery probe accepted without its dish")
	}
}

func TestRouteSceneRejectsWrongPoseAndFlatWall(t *testing.T) {
	load := func(name string) image.Image {
		f, err := os.Open("testdata/" + name + ".png")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		img, _, err := image.Decode(f)
		if err != nil {
			t.Fatal(err)
		}
		return img
	}
	counter := fingerprint(load("inn-fin-held"))
	table := fingerprint(load("inn-table-two-approach"))
	if !sceneMatches(counter, counter) || !sceneMatches(table, table) {
		t.Fatal("captured textured anchors rejected")
	}
	if sceneMatches(counter, table) {
		t.Fatal("different camera/position accepted")
	}
	// Match the stored reference against the new frame, as replay does.
	// Independently selecting a new crop need not choose the same rectangle.
	if !sceneMatchesImage(load("inn-first-serve"), table) {
		t.Fatal("same table pose rejected after serving changed its dish marker")
	}
	flat := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	for y := 0; y < 720; y++ {
		for x := 0; x < 1280; x++ {
			flat.SetRGBA(x, y, color.RGBA{90, 90, 90, 255})
		}
	}
	if sceneMatches(fingerprint(flat), fingerprint(flat)) {
		t.Fatal("flat wall treated as a known position")
	}
}

func TestServedSeatRequiresUniqueUnchangedDiner(t *testing.T) {
	before := []SeatView{{Seat: "1", Phase: "waiting", Dishes: []string{"soup", "fin"}}, {Seat: "2", Phase: "waiting", Dishes: []string{"soup"}}}
	if got := servedSeat(before, []SeatView{{Seat: "1", Phase: "waiting", Dishes: []string{"fin"}}, {Seat: "2", Phase: "waiting", Dishes: []string{"soup"}}}, "soup"); got != "1" {
		t.Fatalf("partial delivery seat=%q", got)
	}
	for _, after := range [][]SeatView{
		{{Seat: "1", Phase: "ordering"}, {Seat: "2", Phase: "waiting", Dishes: []string{"soup"}}},
		{{Seat: "1", Phase: "waiting", Dishes: []string{"fries"}}, {Seat: "2", Phase: "waiting", Dishes: []string{"soup"}}},
		{{Seat: "1", Phase: "waiting", Dishes: []string{"fin"}}, {Seat: "2", Phase: "dining"}},
	} {
		if got := servedSeat(before, after, "soup"); got != "" {
			t.Fatalf("ambiguous/new visit promoted: %q", got)
		}
	}
}

func TestDeliveryRetainsSeatAfterCameraTraceDiscarded(t *testing.T) {
	s := &liveSession{Fixed: fixedRouteState{BeforeServe: []SeatView{{Seat: "3", Phase: "waiting", Dishes: []string{"soup"}}}}}
	if err := s.confirmRoute("serve", "soup", LiveView{Seats: []SeatView{{Seat: "3", Phase: "dining"}}}); err != nil {
		t.Fatal(err)
	}
	if s.Fixed.Seat != "3" || s.Fixed.Recording == nil || s.Fixed.Recording.Seat != "3" || s.Fixed.Recording.Goal != "counter" {
		t.Fatal("delivery destination lost when outbound camera search discarded the recording")
	}
}

func TestCapturedCheckpointSettlingDoesNotAcceptOffsetPose(t *testing.T) {
	load := func(name string) image.Image {
		f, err := os.Open("testdata/" + name + ".png")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		img, _, err := image.Decode(f)
		if err != nil {
			t.Fatal(err)
		}
		return img
	}
	ref := fingerprint(load("inn-route-counter-reference"))
	if !sceneMatchesImage(load("inn-route-counter-aligned"), ref) {
		t.Fatal("aligned checkpoint rejected after lighting/camera settling")
	}
	if sceneMatchesImage(load("inn-route-counter-offset"), ref) {
		t.Fatal("offset checkpoint accepted outside the small settling tolerance")
	}
	table := fingerprint(load("inn-route-table2-reference"))
	if !sceneMatchesImage(load("inn-route-table2-settled"), table) {
		t.Fatal("actual settled replay endpoint rejected")
	}
}

func TestRouteRejectsPartialCameraAndExcessiveMovement(t *testing.T) {
	f, err := os.Open("testdata/inn-fin-held.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	r := fixedRoute{Version: 1, ID: "measured", Goal: "diner", Seat: "2", Start: fingerprint(img), End: fingerprint(img), Steps: []movementCommand{{Kind: "walk", X: 195, Y: 471, Milliseconds: 450}}}
	if !validFixedRoute(r) {
		t.Fatal("bounded measured route rejected")
	}
	r.Steps[0].Kind = "camera"
	if validFixedRoute(r) {
		t.Fatal("camera search replay enabled")
	}
	r.Steps[0].Kind = "walk"
	r.Steps[0].Milliseconds = 30000
	if validFixedRoute(r) {
		t.Fatal("unbounded movement accepted")
	}
	r.Steps = nil
	if validFixedRoute(r) {
		t.Fatal("empty partial route accepted")
	}
}

func TestNeutralJoystickGestureSurvivesRecordedRouteJSON(t *testing.T) {
	var cmd movementCommand
	if err := json.Unmarshal([]byte(`{"Kind":"walk","X":275,"Y":551,"Milliseconds":80,"HoldMilliseconds":400,"JoystickWarmup":true,"Pipeline":true}`), &cmd); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(movementSwipeParam(cmd))
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err = json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	for field, want := range map[string]string{"end": "[[195,551,1,1],[275,551,1,1]]", "duration": "[1,80]", "end_hold": "[50,400]"} {
		if string(wire[field]) != want {
			t.Fatalf("recorded neutral gesture changed: %s=%s", field, wire[field])
		}
	}
	if movementDurationMS(cmd) != 531 {
		t.Fatal("neutral engagement missing from measured cost")
	}
	r := transitFixture(t)[0]
	r.MovementStage = "exit"
	r.Steps = []movementCommand{cmd}
	if !validTransitRoute(r) {
		t.Fatal("valid recorded neutral gesture rejected")
	}
	r.Steps[0].Milliseconds = 20
	if validTransitRoute(r) {
		t.Fatal("unmeasured fast warmup accepted")
	}
	r.Steps[0].Milliseconds = 80
	r.Steps[0].HoldMilliseconds = 1400
	if validTransitRoute(r) {
		t.Fatal("neutral touch bypassed movement duration bound")
	}
}
