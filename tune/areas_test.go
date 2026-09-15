package tune

import (
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/geom"
)

// Where the tool may put copper is the user's call, and these are the ways that
// has to hold: nothing outside, nothing straddling the edge, and no restriction
// at all when none was asked for.

func TestNoAreasMeansAnywhere(t *testing.T) {
	s, net := straightTrack(20)
	_, _, tu := s.build(t, synthClr, synthW)

	res, err := tu.Tune(net, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Shortfall > 1e-3 {
		t.Errorf("5 mm on an unrestricted 20 mm track fell short by %.6f", res.Shortfall)
	}
}

// A meander goes in the area or it does not go at all.
func TestNothingIsAddedOutsideTheAllowedAreas(t *testing.T) {
	s, net := straightTrack(20)
	b, _, tu := s.build(t, synthClr, synthW)
	// The track runs from x=10 to x=30 at y=50. Allow only its far third.
	tu.SetAreas(Areas{{Rect: geom.Rect{MinX: 23, MinY: 45, MaxX: 31, MaxY: 55}}})

	res, err := tu.Tune(net, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added <= 0 {
		t.Fatalf("nothing was added, though a third of the track is allowed: %s", res.Notes)
	}
	// Every piece of copper on the net now has to be inside the area, or the
	// meander went where it was told not to.
	for _, tr := range b.TracksOfNet(net) {
		for _, p := range []geom.Pt{tr.Start, tr.End} {
			if p.X < 22.9 && !onTheOriginalLine(p) {
				t.Errorf("copper at (%.3f, %.3f) is outside the allowed area", p.X, p.Y)
			}
		}
	}
}

// onTheOriginalLine reports whether a point is on the track as it was drawn --
// the straight run at y=50, which is not copper that was added.
func onTheOriginalLine(p geom.Pt) bool {
	return p.Y > 49.999 && p.Y < 50.001
}

func TestAnAreaOverNoneOfTheTrackAddsNothing(t *testing.T) {
	s, net := straightTrack(20)
	_, _, tu := s.build(t, synthClr, synthW)
	// Nowhere near the track.
	tu.SetAreas(Areas{{Rect: geom.Rect{MinX: 60, MinY: 60, MaxX: 80, MaxY: 80}}})

	res, err := tu.Tune(net, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added > 1e-9 {
		t.Errorf("%.4f mm was added where no area allows it", res.Added)
	}
	if res.Shortfall < 3-1e-9 {
		t.Errorf("shortfall %.4f mm, want the whole 3 mm reported as unmet", res.Shortfall)
	}
}

// A stretch half inside an area is not half allowed. A meander wanders either
// side of the stretch along its whole length, so it would leave.
func TestAStretchStraddlingTheEdgeIsNotAllowed(t *testing.T) {
	a := Areas{{Rect: geom.Rect{MinX: 0, MinY: 0, MaxX: 10, MaxY: 10}}}
	in := a.AllowsSpan(geom.Pt{X: 2, Y: 5}, geom.Pt{X: 8, Y: 5})
	out := a.AllowsSpan(geom.Pt{X: 8, Y: 5}, geom.Pt{X: 14, Y: 5})
	if !in {
		t.Error("a stretch wholly inside was refused")
	}
	if out {
		t.Error("a stretch running out of the area was allowed")
	}
}

func TestAreasAcceptAnyOfSeveral(t *testing.T) {
	a := Areas{
		{Rect: geom.Rect{MinX: 0, MinY: 0, MaxX: 10, MaxY: 10}},
		{Rect: geom.Rect{MinX: 20, MinY: 0, MaxX: 30, MaxY: 10}},
	}
	if !a.Allows(geom.Pt{X: 5, Y: 5}) || !a.Allows(geom.Pt{X: 25, Y: 5}) {
		t.Error("a point in one of the areas was refused")
	}
	if a.Allows(geom.Pt{X: 15, Y: 5}) {
		t.Error("a point between the areas was allowed")
	}
}

// ---- proposing areas ----

// A user given a blank board and "draw where copper may go" has to hunt for
// where the length could come from. The tool has already measured that.
func TestSuggestFindsWhereTheRoomIs(t *testing.T) {
	s, net := straightTrack(20)
	_, _, tu := s.build(t, synthClr, synthW)

	spots := tu.Spots(net, nil)
	if len(spots) == 0 {
		t.Fatal("an empty board offers room, but no spot was found")
	}
	var gain float64
	for _, sp := range spots {
		gain += sp.GainMM
		if sp.Net != net {
			t.Errorf("spot on %s, want %s", sp.Net, net)
		}
	}
	if gain <= 0 {
		t.Error("the spots are worth nothing")
	}

	// And the area proposed covers the track it came from.
	got := Suggest(spots, 1.0, 4)
	if len(got) != 1 {
		t.Fatalf("%d areas for one track on an empty board", len(got))
	}
	if !got[0].Area.Overlaps(geom.RectFromPts(geom.Pt{X: 12, Y: 50}, geom.Pt{X: 28, Y: 50})) {
		t.Errorf("the proposed area %+v does not cover the track", got[0].Area)
	}
	// Drawing what was proposed has to leave the room it promised.
	tu.SetAreas(Areas{{Rect: got[0].Area}})
	if room := tu.Headroom(net, nil); room <= 0 {
		t.Error("the proposed area holds no room, though it was proposed for holding some")
	}
}

// A bus's worth of parallel stretches is one place on the board. Offering
// twenty slivers is a worse answer than offering none.
func TestSuggestMergesWhatIsNearby(t *testing.T) {
	spots := []Spot{
		{Net: "A", Box: geom.Rect{MinX: 0, MinY: 0, MaxX: 10, MaxY: 1}, GainMM: 2},
		{Net: "B", Box: geom.Rect{MinX: 0, MinY: 1.2, MaxX: 10, MaxY: 2}, GainMM: 3},
		{Net: "C", Box: geom.Rect{MinX: 0, MinY: 2.4, MaxX: 10, MaxY: 3}, GainMM: 4},
		// Somewhere else entirely.
		{Net: "D", Box: geom.Rect{MinX: 60, MinY: 60, MaxX: 70, MaxY: 61}, GainMM: 5},
	}
	got := Suggest(spots, 1.0, 10)
	if len(got) != 2 {
		t.Fatalf("%d areas, want the bus as one and the far one as another: %+v", len(got), got)
	}
	// Worth the most first, so taking only the top one takes the best.
	if got[0].GainMM < got[1].GainMM {
		t.Error("the areas are not ordered by what they are worth")
	}
	// Worth the most first, so the merged bus is the one at the top.
	bus := got[0]
	if bus.Nets != 3 {
		t.Errorf("the merged area covers %d nets, want 3", bus.Nets)
	}
	if bus.GainMM != 9 {
		t.Errorf("the merged area is worth %v, want 2+3+4", bus.GainMM)
	}
}

// Merging has to settle rather than depend on the order the spots arrived in:
// a cluster grows as it absorbs, and two that were apart can end up touching.
func TestSuggestMergesUntilItSettles(t *testing.T) {
	// Three boxes in a line, each apart from the next by less than the margin
	// but the ends far apart. Merging once leaves two; merging again leaves one.
	spots := []Spot{
		{Net: "A", Box: geom.Rect{MinX: 0, MinY: 0, MaxX: 5, MaxY: 1}, GainMM: 1},
		{Net: "B", Box: geom.Rect{MinX: 12, MinY: 0, MaxX: 17, MaxY: 1}, GainMM: 1},
		{Net: "C", Box: geom.Rect{MinX: 6, MinY: 0, MaxX: 11, MaxY: 1}, GainMM: 1},
	}
	got := Suggest(spots, 1.5, 10)
	if len(got) != 1 {
		t.Fatalf("%d areas, want one: %+v", len(got), got)
	}
	if got[0].Nets != 3 || got[0].GainMM != 3 {
		t.Errorf("merged area covers %d nets worth %v", got[0].Nets, got[0].GainMM)
	}
}

func TestSuggestIsCappedAndEmptyWhenThereIsNothing(t *testing.T) {
	if got := Suggest(nil, 1, 4); got != nil {
		t.Errorf("proposed %d areas from nothing", len(got))
	}
	var spots []Spot
	for i := 0; i < 10; i++ {
		x := float64(i) * 50
		spots = append(spots, Spot{
			Net: "N", GainMM: float64(i),
			Box: geom.Rect{MinX: x, MinY: 0, MaxX: x + 5, MaxY: 1},
		})
	}
	if got := Suggest(spots, 1, 3); len(got) != 3 {
		t.Errorf("%d areas, want the cap of 3", len(got))
	}
}

// ---- per-area spacing ----

// The spacing lives on the area because that is where the user knows it, and
// it only ever tightens: an area is somewhere they chose to put copper.
func TestAnAreaCanAskForMoreSpaceThanTheBoardDoes(t *testing.T) {
	s, net := straightTrack(20)
	// Walls both sides, a little further than the board's own clearance. One
	// side alone proves nothing: a meander simply goes the other way, which is
	// what the first version of this test measured.
	s.blocker("WALL_HI", 10, 30, 0.45)
	s.track("WALL_LO", "F.Cu", geom.Pt{X: 10, Y: 50 - 0.45 - synthW}, geom.Pt{X: 30, Y: 50 - 0.45 - synthW}, synthW)
	_, _, tu := s.build(t, synthClr, synthW)

	wide := Areas{{Rect: geom.Rect{MinX: 5, MinY: 40, MaxX: 35, MaxY: 60}}}
	tu.SetAreas(wide)
	loose := tu.Headroom(net, nil)
	if loose <= 0 {
		t.Fatal("no room at all beside a track with a wall 0.45 mm away")
	}

	wide[0].MinClearance = 0.4
	tu.SetAreas(wide)
	tight := tu.Headroom(net, nil)
	if tight >= loose {
		t.Errorf("asking for 0.4 mm of spacing left %.4f mm of room, no less than the %.4f mm at the board's 0.2",
			tight, loose)
	}
}
