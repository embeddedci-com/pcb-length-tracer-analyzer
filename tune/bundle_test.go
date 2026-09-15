package tune

import (
	"math"
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/geom"
)

// bus lays n parallel horizontal tracks at the given pitch, centred on y=50,
// each with a pad at both ends, and returns their net names in order.
func bus(s *synth, n int, pitch, length float64) []string {
	var nets []string
	for i := range n {
		net := string(rune('A' + i))
		y := 50 + float64(i)*pitch
		s.pad(net, geom.Pt{X: 10, Y: y}, synthW)
		s.pad(net, geom.Pt{X: 10 + length, Y: y}, synthW)
		s.track(net, "F.Cu", geom.Pt{X: 10, Y: y}, geom.Pt{X: 10 + length, Y: y}, synthW)
		nets = append(nets, net)
	}
	return nets
}

func TestBundleFoundAcrossAParallelBus(t *testing.T) {
	s := newSynth()
	nets := bus(s, 6, synthClr+synthW, 20)
	_, _, tu := s.build(t, synthClr, synthW)

	bundles := tu.FindBundles(nets, DefaultBundleOptions())
	if len(bundles) != 1 {
		t.Fatalf("found %d bundles for one bus", len(bundles))
	}
	b := bundles[0]
	if len(b.Members) != 6 {
		t.Errorf("%d members, want 6", len(b.Members))
	}
	if math.Abs(b.Span()-20) > 1.0 {
		t.Errorf("span %.3f, want about 20", b.Span())
	}
	// Six tracks at a 0.29 pitch span 5 gaps plus one width.
	if want := 5*(synthClr+synthW) + synthW; math.Abs(b.Width()-want) > 1e-6 {
		t.Errorf("width %.4f, want %.4f", b.Width(), want)
	}
	// Ordered across, which is what keeps a re-spacing from crossing traces.
	for i := 1; i < len(b.Members); i++ {
		if b.Members[i].Offset <= b.Members[i-1].Offset {
			t.Errorf("members not ordered across: %v", b.Members)
			break
		}
	}
	if got := len(b.Nets()); got != 6 {
		t.Errorf("%d nets, want 6", got)
	}
	if math.Abs(b.Clearance-synthClr) > 1e-9 {
		t.Errorf("clearance %.4f, want the strictest of the members, %.4f", b.Clearance, synthClr)
	}
}

func TestTwoTracksAreNotABus(t *testing.T) {
	s := newSynth()
	// A differential pair is two parallel tracks, and re-spacing a pair is a
	// different operation with different rules, so it must not be mistaken for
	// a bus.
	nets := bus(s, 2, synthClr+synthW, 20)
	_, _, tu := s.build(t, synthClr, synthW)
	if got := tu.FindBundles(nets, DefaultBundleOptions()); len(got) != 0 {
		t.Errorf("found %d bundles for two tracks", len(got))
	}
}

func TestWidelySpacedTracksAreSeparateBuses(t *testing.T) {
	s := newSynth()
	var nets []string
	// Two groups of three, far enough apart to be unrelated.
	for g := range 2 {
		for i := range 3 {
			net := string(rune('A' + g*3 + i))
			y := 50 + float64(g)*8 + float64(i)*(synthClr+synthW)
			s.pad(net, geom.Pt{X: 10, Y: y}, synthW)
			s.pad(net, geom.Pt{X: 30, Y: y}, synthW)
			s.track(net, "F.Cu", geom.Pt{X: 10, Y: y}, geom.Pt{X: 30, Y: y}, synthW)
			nets = append(nets, net)
		}
	}
	_, _, tu := s.build(t, synthClr, synthW)
	bundles := tu.FindBundles(nets, DefaultBundleOptions())
	if len(bundles) != 2 {
		t.Fatalf("found %d bundles, want 2", len(bundles))
	}
	for _, b := range bundles {
		if len(b.Members) != 3 {
			t.Errorf("bundle has %d members, want 3", len(b.Members))
		}
	}
}

func TestNonParallelTracksAreNotABus(t *testing.T) {
	s := newSynth()
	var nets []string
	for i := range 4 {
		net := string(rune('A' + i))
		y := 50 + float64(i)*(synthClr+synthW)
		// Each fans out at a different angle: adjacent at one end, splayed at
		// the other.
		s.pad(net, geom.Pt{X: 10, Y: y}, synthW)
		end := geom.Pt{X: 30, Y: y + float64(i)*3}
		s.pad(net, end, synthW)
		s.track(net, "F.Cu", geom.Pt{X: 10, Y: y}, end, synthW)
		nets = append(nets, net)
	}
	_, _, tu := s.build(t, synthClr, synthW)
	for _, b := range tu.FindBundles(nets, DefaultBundleOptions()) {
		if len(b.Members) > 1 {
			t.Errorf("splayed tracks grouped into a bundle of %d", len(b.Members))
		}
	}
}

func TestBundleFreeSpaceIsMeasuredOnBothSides(t *testing.T) {
	s := newSynth()
	nets := bus(s, 4, synthClr+synthW, 20)
	// A wall 1 mm of clear copper below the bus, nothing above.
	lowest := 50.0
	s.track("WALL", "F.Cu", geom.Pt{X: 5, Y: lowest - 1 - synthW}, geom.Pt{X: 35, Y: lowest - 1 - synthW}, synthW)
	_, _, tu := s.build(t, synthClr, synthW)

	bundles := tu.FindBundles(nets, DefaultBundleOptions())
	if len(bundles) != 1 {
		t.Fatalf("found %d bundles", len(bundles))
	}
	b := bundles[0]
	// Below: the wall leaves 1 mm of copper gap, of which the clearance takes
	// its share.
	if b.FreeNeg > 1-synthClr+1e-3 || b.FreeNeg < 0.5 {
		t.Errorf("free space below = %.4f, expected a little under %.3f", b.FreeNeg, 1-synthClr)
	}
	// Above: open board, so the search limit.
	if b.FreePos < 4.9 {
		t.Errorf("free space above = %.4f, expected the search limit", b.FreePos)
	}
	if b.Corridor() <= b.Width() {
		t.Errorf("corridor %.3f is no wider than the bus itself %.3f", b.Corridor(), b.Width())
	}

	spare, span := tu.BundleSpare(nets)
	if math.Abs(spare-(b.Corridor()-b.Width())) > 1e-9 {
		t.Errorf("BundleSpare reported %.4f, the bundle has %.4f", spare, b.Corridor()-b.Width())
	}
	if math.Abs(span-b.Span()) > 1e-9 {
		t.Errorf("BundleSpare span %.4f, bundle span %.4f", span, b.Span())
	}
}

func TestBundleBoxedInHasNoSpare(t *testing.T) {
	s := newSynth()
	nets := bus(s, 4, synthClr+synthW, 20)
	top := 50 + 3*(synthClr+synthW)
	s.track("W1", "F.Cu", geom.Pt{X: 5, Y: 50 - synthClr - synthW}, geom.Pt{X: 35, Y: 50 - synthClr - synthW}, synthW)
	s.track("W2", "F.Cu", geom.Pt{X: 5, Y: top + synthClr + synthW}, geom.Pt{X: 35, Y: top + synthClr + synthW}, synthW)
	_, _, tu := s.build(t, synthClr, synthW)

	spare, _ := tu.BundleSpare(nets)
	if spare > 1e-6 {
		t.Errorf("a bus walled in on both sides reported %.4f mm of spare room", spare)
	}
}

func TestBundlesOnDifferentLayersAreSeparate(t *testing.T) {
	s := newSynth()
	var nets []string
	for i := range 6 {
		net := string(rune('A' + i))
		layer := "F.Cu"
		if i >= 3 {
			layer = "B.Cu"
		}
		y := 50 + float64(i%3)*(synthClr+synthW)
		s.pad(net, geom.Pt{X: 10, Y: y}, synthW)
		s.track(net, layer, geom.Pt{X: 10, Y: y}, geom.Pt{X: 30, Y: y}, synthW)
		nets = append(nets, net)
	}
	_, _, tu := s.build(t, synthClr, synthW)
	bundles := tu.FindBundles(nets, DefaultBundleOptions())
	if len(bundles) != 2 {
		t.Fatalf("found %d bundles, want one per layer", len(bundles))
	}
	seen := map[string]bool{}
	for _, b := range bundles {
		seen[b.Layer] = true
		for _, m := range b.Members {
			if m.Track.Layer != b.Layer {
				t.Errorf("bundle on %s has a member on %s", b.Layer, m.Track.Layer)
			}
		}
	}
	if !seen["F.Cu"] || !seen["B.Cu"] {
		t.Errorf("layers found: %v", seen)
	}
}

func TestFindBundlesIsDeterministic(t *testing.T) {
	s := newSynth()
	nets := bus(s, 5, synthClr+synthW, 20)
	_, _, tu := s.build(t, synthClr, synthW)
	first := tu.FindBundles(nets, DefaultBundleOptions())
	for range 3 {
		again := tu.FindBundles(nets, DefaultBundleOptions())
		if len(again) != len(first) {
			t.Fatalf("bundle count varies: %d then %d", len(first), len(again))
		}
		for i := range first {
			if len(again[i].Members) != len(first[i].Members) ||
				math.Abs(again[i].Span()-first[i].Span()) > 1e-12 {
				t.Fatalf("bundle %d differs between runs", i)
			}
		}
	}
}

func TestFindBundlesIgnoresNetsNotAskedAbout(t *testing.T) {
	s := newSynth()
	nets := bus(s, 5, synthClr+synthW, 20)
	_, _, tu := s.build(t, synthClr, synthW)
	// Leaving one out must shrink the bundle, not silently include it: a
	// bundle is something the caller may re-space, and sweeping in a net
	// nobody asked about would be the wrong kind of helpful.
	sub := nets[:4]
	bundles := tu.FindBundles(sub, DefaultBundleOptions())
	if len(bundles) != 1 || len(bundles[0].Members) != 4 {
		t.Fatalf("got %d bundles; first has %d members, want one with 4", len(bundles), len(bundles[0].Members))
	}
	for _, m := range bundles[0].Members {
		if m.Net == nets[4] {
			t.Errorf("%s was not asked about but is in the bundle", m.Net)
		}
	}
}
