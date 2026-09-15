package board

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/geom"
)

const demoPath = "../demo-pcb/ai-vision.kicad_pcb"

func load(t *testing.T) *Board {
	t.Helper()
	b, err := Load(demoPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("demo board not available")
		}
		t.Fatalf("Load: %v", err)
	}
	return b
}

func TestLoadDemoBoardCounts(t *testing.T) {
	b := load(t)
	// Counted straight out of the file with grep, so a change here means the
	// parser started missing or duplicating items.
	var seg, arc, via int
	for _, tr := range b.Tracks {
		switch tr.Kind {
		case KindSegment:
			seg++
		case KindArc:
			arc++
		case KindVia:
			via++
		}
	}
	if seg != 2630 || arc != 280 || via != 210 {
		t.Errorf("tracks: %d segments, %d arcs, %d vias; want 2630/280/210", seg, arc, via)
	}
	if len(b.Footprints) != 370 {
		t.Errorf("footprints = %d, want 370", len(b.Footprints))
	}
	if len(b.Pads) != 1968 {
		t.Errorf("pads = %d, want 1968", len(b.Pads))
	}
	if got := len(b.CopperLayers); got != 6 {
		t.Errorf("copper layers = %d, want 6", got)
	}
	if b.CopperLayers[0] != "F.Cu" || b.CopperLayers[5] != "B.Cu" {
		t.Errorf("layer order = %v", b.CopperLayers)
	}
}

func TestSaveIsByteExactWhenNothingChanged(t *testing.T) {
	b := load(t)
	src, err := os.ReadFile(demoPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(src, b.Bytes()) {
		t.Fatal("loading and saving changed the file")
	}
}

func TestStackupViaLengthMatchesKiCad(t *testing.T) {
	b := load(t)
	// Established by running kicad-cli DRC with and without
	// use_height_for_length_calcs: an F.Cu-to-B.Cu via on this board is worth
	// exactly 1.594 mm of net length.
	got := b.Stackup.ViaLength("F.Cu", "B.Cu")
	if math.Abs(got-1.594) > 1e-9 {
		t.Errorf("ViaLength(F.Cu, B.Cu) = %v, want 1.594", got)
	}
	if math.Abs(b.Stackup.Thickness-1.594) > 1e-9 {
		t.Errorf("Thickness = %v, want 1.594", b.Stackup.Thickness)
	}
	// A blind via spanning less of the stack must measure less.
	if inner := b.Stackup.ViaLength("F.Cu", "In2.Cu"); inner >= got || inner <= 0 {
		t.Errorf("ViaLength(F.Cu, In2.Cu) = %v, want between 0 and %v", inner, got)
	}
	// Symmetric.
	if a, c := b.Stackup.ViaLength("In1.Cu", "In3.Cu"), b.Stackup.ViaLength("In3.Cu", "In1.Cu"); a != c {
		t.Errorf("ViaLength not symmetric: %v vs %v", a, c)
	}
}

func TestStackupLayerClassification(t *testing.T) {
	b := load(t)
	for _, name := range []string{"F.Cu", "B.Cu"} {
		l, ok := b.Stackup.Layer(name)
		if !ok || !l.Outer() {
			t.Errorf("%s should be an outer layer (ok=%v)", name, ok)
		}
	}
	for _, name := range []string{"In1.Cu", "In2.Cu", "In3.Cu", "In4.Cu"} {
		l, ok := b.Stackup.Layer(name)
		if !ok || l.Outer() {
			t.Errorf("%s should be an inner layer (ok=%v)", name, ok)
		}
	}
}

func TestPropagationDelayMicrostripVsStripline(t *testing.T) {
	b := load(t)
	ms := b.Stackup.DelayPerMM("F.Cu", 0.09)
	sl := b.Stackup.DelayPerMM("In3.Cu", 0.09)
	// Microstrip is faster than stripline: part of its field returns through
	// air. Both must land in the range real FR4 boards show, roughly
	// 5.5 to 7.5 ps/mm.
	if ms >= sl {
		t.Errorf("microstrip %v ps/mm should be faster than stripline %v ps/mm", ms, sl)
	}
	for name, v := range map[string]float64{"microstrip": ms, "stripline": sl} {
		if v < 5.0 || v > 8.0 {
			t.Errorf("%s delay %v ps/mm is outside the plausible FR4 range", name, v)
		}
	}
}

func TestPadPositionsAgainstKnownGeometry(t *testing.T) {
	b := load(t)
	// Two pads whose board positions were read out of the file by hand. U4 is
	// placed at -90 degrees, so it also pins down the rotation convention: get
	// the sign wrong and this pad lands at (84.47, 66.28) instead.
	want := map[string]geom.Pt{
		"U3.W22": {X: 72.12, Y: 67.41},
		"U4.F7":  {X: 88.47, Y: 69.88},
	}
	found := map[string]bool{}
	for _, p := range b.Pads {
		if w, ok := want[p.ID()]; ok {
			found[p.ID()] = true
			if p.Centre.Dist(w) > 1e-6 {
				t.Errorf("%s centre = %v, want %v", p.ID(), p.Centre, w)
			}
		}
	}
	for id := range want {
		if !found[id] {
			t.Errorf("pad %s not found", id)
		}
	}
}

func TestPadRotationConventionHoldsForEveryDDRBGAPad(t *testing.T) {
	b := load(t)
	// A far stronger check than two hand-picked pads: for every DDR net, each
	// BGA pad must have a track endpoint of the same net landing on it. That
	// only holds if the footprint transform is right for every rotation on the
	// board -- U3 sits at 0 degrees and U4/U5 at -90.
	//
	// The termination resistors are excluded on purpose: their legs are not
	// routed on this board (see TestFlyByTerminationLegsAreUnrouted), so a pad
	// with no copper on it there is the truth, not a parser bug.
	type key struct {
		layer string
		net   string
	}
	ends := map[key][]geom.Pt{}
	for _, tr := range b.Tracks {
		if tr.Kind == KindVia {
			continue
		}
		k := key{tr.Layer, tr.Net}
		ends[k] = append(ends[k], tr.Start, tr.End)
	}
	checked, bad := 0, 0
	for _, net := range b.Nets() {
		if !isDDR(net) {
			continue
		}
		for _, p := range b.PadsOfNet(net) {
			if p.Ref != "U3" && p.Ref != "U4" && p.Ref != "U5" {
				continue
			}
			checked++
			hit := false
			for _, l := range b.CopperLayers {
				if !p.OnLayer(l) {
					continue
				}
				for _, e := range ends[key{l, net}] {
					if p.Shape.Contains(e) {
						hit = true
						break
					}
				}
				if hit {
					break
				}
			}
			if !hit {
				bad++
				if bad <= 5 {
					t.Errorf("no track of %s lands on pad %s at %v", net, p.ID(), p.Centre)
				}
			}
		}
	}
	// 44 point-to-point nets with 2 BGA pads plus 27 fly-by nets with 3.
	if want := 44*2 + 27*3; checked != want {
		t.Fatalf("checked %d BGA pads, want %d", checked, want)
	}
	if bad != 0 {
		t.Errorf("%d of %d DDR BGA pads have no track endpoint on them", bad, checked)
	}
}

// TestFlyByTerminationLegsAreUnrouted pins down a property of the demo board
// that the length engine has to cope with, and that KiCad's own DRC does not
// report: on 25 of the 27 fly-by nets the leg from the far DDR device to the
// termination resistor is not routed. Most are metres away in board terms -- 8 mm
// of bare laminate -- and DDR_RESETN is a near miss, its copper stopping 0.47 mm
// short of R16.1. Only the clock pair reaches its terminator R17.
//
// None of those pads appears anywhere in a full
// `kicad-cli pcb drc --severity-all` report, which is why the tool decides
// routing completeness from geometry rather than from an empty unconnected
// list. It is also exactly why KiCad measures three via lengths on CLK and only
// two on every address net: the third via is on the unrouted leg.
//
// The lesson encoded here is that the tool must decide for itself whether a net
// is routed, from the geometry, instead of trusting an empty unconnected list.
func TestFlyByTerminationLegsAreUnrouted(t *testing.T) {
	b := load(t)
	unrouted, total := 0, 0
	var routed []string
	for _, net := range b.Nets() {
		if !isDDR(net) {
			continue
		}
		for _, p := range b.PadsOfNet(net) {
			if p.Ref == "U3" || p.Ref == "U4" || p.Ref == "U5" {
				continue
			}
			total++
			// Nearest copper of the same net, on any layer.
			best := math.Inf(1)
			for _, tr := range b.TracksOfNet(net) {
				for _, sh := range tr.Shape(b.MaxError) {
					if d := sh.Dist(p.Shape); d < best {
						best = d
					}
				}
			}
			// Copper is connected only where it actually overlaps the pad.
			if best > geom.Eps {
				unrouted++
			} else {
				routed = append(routed, net)
			}
		}
	}
	if total != 27 {
		t.Fatalf("found %d termination pads, want 27", total)
	}
	if unrouted != 25 {
		t.Errorf("%d of %d termination legs unrouted; want 25", unrouted, total)
	}
	sort.Strings(routed)
	want := []string{"/ddr4/DDR_CLK_N", "/ddr4/DDR_CLK_P"}
	if !slices.Equal(routed, want) {
		t.Errorf("routed termination legs = %v, want %v", routed, want)
	}
}

func isDDR(net string) bool { return len(net) > 7 && net[:7] == "/ddr4/D" }

func TestZonesHaveFilledPolygons(t *testing.T) {
	b := load(t)
	if len(b.Zones) == 0 {
		t.Fatal("no zones parsed")
	}
	total := 0
	for _, z := range b.Zones {
		for _, polys := range z.Filled {
			total += len(polys)
		}
	}
	if total == 0 {
		t.Error("no filled polygons parsed; clearance against pours would be blind")
	}
}

func TestBoardOutlineParsed(t *testing.T) {
	b := load(t)
	if len(b.Outline) < 4 {
		t.Errorf("board outline has %d segments, want at least 4", len(b.Outline))
	}
	box := geom.Rect{MinX: math.Inf(1), MinY: math.Inf(1), MaxX: math.Inf(-1), MaxY: math.Inf(-1)}
	for _, s := range b.Outline {
		box = box.Union(s.Box())
	}
	if box.MaxX-box.MinX < 10 || box.MaxY-box.MinY < 10 {
		t.Errorf("outline bounding box looks wrong: %+v", box)
	}
}

func TestTrackLengthsAndArcs(t *testing.T) {
	b := load(t)
	var arcTotal float64
	arcs := 0
	for _, tr := range b.Tracks {
		l := tr.Length(b.Stackup)
		if l < 0 || math.IsNaN(l) {
			t.Fatalf("track %v has length %v", tr.UUID, l)
		}
		if tr.Kind == KindArc {
			arcs++
			arcTotal += l
			// A tuning arc is a small rounded corner; anything huge means the
			// sweep came out on the wrong side of the circle.
			if l > 5 {
				t.Errorf("arc %s length %v mm looks like a wrong-way sweep", tr.UUID, l)
			}
		}
	}
	if arcs == 0 || arcTotal == 0 {
		t.Fatal("no arc length measured")
	}
}

func TestReplaceTrackRewritesInPlace(t *testing.T) {
	b := load(t)
	var victim *Track
	for _, tr := range b.Tracks {
		if tr.Kind == KindSegment && tr.Net == "/ddr4/DDR_DQ0" {
			victim = tr
			break
		}
	}
	if victim == nil {
		t.Skip("no DQ0 segment")
	}
	idx := b.Root.IndexOf(victim.Node)
	mid := victim.Start.Add(victim.End.Sub(victim.Start).Mul(0.5)).Snap()
	a := &Track{Kind: KindSegment, Net: victim.Net, Start: victim.Start, End: mid, Width: victim.Width, Layer: victim.Layer}
	c := &Track{Kind: KindSegment, Net: victim.Net, Start: mid, End: victim.End, Width: victim.Width, Layer: victim.Layer}
	before := len(b.Tracks)
	b.ReplaceTrack(victim, []*Track{a, c})

	if len(b.Tracks) != before+1 {
		t.Errorf("track count = %d, want %d", len(b.Tracks), before+1)
	}
	if got := b.Root.IndexOf(a.Node); got != idx {
		t.Errorf("replacement landed at index %d, want %d (in place)", got, idx)
	}
	// The rewritten file must still parse, and the two halves must measure the
	// same as the original.
	b2, err := Parse(b.Bytes(), "reparsed")
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	var sum float64
	for _, tr := range b2.TracksOfNet("/ddr4/DDR_DQ0") {
		if tr.Start.Near(a.Start) && tr.End.Near(a.End) || tr.Start.Near(c.Start) && tr.End.Near(c.End) {
			sum += tr.Length(b2.Stackup)
		}
	}
	if math.Abs(sum-victim.Length(b.Stackup)) > 1e-6 {
		t.Errorf("split halves measure %v, original was %v", sum, victim.Length(b.Stackup))
	}
}

func TestRemoveTracks(t *testing.T) {
	b := load(t)
	victims := b.TracksOfNet("/ddr4/DDR_DQ0")
	if len(victims) == 0 {
		t.Skip("no DQ0")
	}
	n := b.RemoveTracks(victims[:2])
	if n != 2 {
		t.Errorf("removed %d, want 2", n)
	}
	if got := len(b.TracksOfNet("/ddr4/DDR_DQ0")); got != len(victims)-2 {
		t.Errorf("net has %d tracks, want %d", got, len(victims)-2)
	}
	if _, err := Parse(b.Bytes(), "reparsed"); err != nil {
		t.Fatalf("reparse after removal: %v", err)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	b := load(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "out.kicad_pcb")
	if err := b.Save(out); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	src, _ := os.ReadFile(demoPath)
	if !bytes.Equal(src, got) {
		t.Error("saved file differs from the original")
	}
}

func TestParseRejectsNonBoard(t *testing.T) {
	if _, err := Parse([]byte("(kicad_sch (version 1))"), "x"); err == nil {
		t.Error("accepted a schematic as a board")
	}
}
