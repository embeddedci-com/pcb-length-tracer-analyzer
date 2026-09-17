package netlen

import (
	"slices"
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/board"
)

// A signal through a series resistor is two nets. Measuring one of them
// understates it, and understates each line of a bus by a different amount,
// which is the skew the matching exists to find.
//
// The board: a driver at x=10, 10 mm of copper to a 22R at x=20, then 5 mm on
// to the receiver at x=26. Plus a decoupling capacitor, which is also a
// two-pad part and must not be walked through into a plane.
func seriesBoard(t *testing.T) *board.Board {
	t.Helper()
	src := []byte(`(kicad_pcb
	(version 20260206)
	(generator "pcb-trace-length-analyzer-test")
	(paper "A4")
	(layers (0 "F.Cu" signal) (2 "B.Cu" signal) (25 "Edge.Cuts" user))
	(setup (stackup
		(layer "F.Cu" (type "copper") (thickness 0.035))
		(layer "dielectric 1" (type "core") (thickness 1.51) (material "FR4") (epsilon_r 4.5))
		(layer "B.Cu" (type "copper") (thickness 0.035))
	))
	(gr_rect (start 0 0) (end 40 30) (stroke (width 0.05) (type default)) (fill none) (layer "Edge.Cuts") (uuid "ffffffff-4444-4000-8000-000000000001"))
	(footprint "t:mac" (layer "F.Cu") (uuid "aaaaaaaa-4444-4000-8000-000000000002") (at 10 15) (attr smd)
		(property "Reference" "U1" (at 0 0) (layer "F.Cu") (uuid "aaaaaaaa-4444-4000-8000-000000000003"))
		(pad "1" smd circle (at 0 0) (size 0.3 0.3) (layers "F.Cu") (net "/eth/ETH1.TX_CLK") (uuid "aaaaaaaa-4444-4000-8000-000000000004"))
	)
	(footprint "t:r" (layer "F.Cu") (uuid "aaaaaaaa-4444-4000-8000-000000000005") (at 20 15) (attr smd)
		(property "Reference" "R80" (at 0 0) (layer "F.Cu") (uuid "aaaaaaaa-4444-4000-8000-000000000006"))
		(property "Value" "22" (at 0 0) (layer "F.Cu") (uuid "aaaaaaaa-4444-4000-8000-000000000007"))
		(pad "1" smd circle (at 0 0) (size 0.3 0.3) (layers "F.Cu") (net "/eth/ETH1.TX_CLK") (uuid "aaaaaaaa-4444-4000-8000-000000000008"))
		(pad "2" smd circle (at 1 0) (size 0.3 0.3) (layers "F.Cu") (net "Net-(U9-TXCLK)") (uuid "aaaaaaaa-4444-4000-8000-000000000009"))
	)
	(footprint "t:phy" (layer "F.Cu") (uuid "aaaaaaaa-4444-4000-8000-000000000010") (at 26 15) (attr smd)
		(property "Reference" "U9" (at 0 0) (layer "F.Cu") (uuid "aaaaaaaa-4444-4000-8000-000000000011"))
		(pad "1" smd circle (at 0 0) (size 0.3 0.3) (layers "F.Cu") (net "Net-(U9-TXCLK)") (uuid "aaaaaaaa-4444-4000-8000-000000000012"))
	)
	(footprint "t:c" (layer "F.Cu") (uuid "aaaaaaaa-4444-4000-8000-000000000013") (at 30 20) (attr smd)
		(property "Reference" "C1" (at 0 0) (layer "F.Cu") (uuid "aaaaaaaa-4444-4000-8000-000000000014"))
		(property "Value" "100n" (at 0 0) (layer "F.Cu") (uuid "aaaaaaaa-4444-4000-8000-000000000015"))
		(pad "1" smd circle (at 0 0) (size 0.3 0.3) (layers "F.Cu") (net "VDD_ETH") (uuid "aaaaaaaa-4444-4000-8000-000000000016"))
		(pad "2" smd circle (at 1 0) (size 0.3 0.3) (layers "F.Cu") (net "GND") (uuid "aaaaaaaa-4444-4000-8000-000000000017"))
	)
	(segment (start 10 15) (end 20 15) (width 0.2) (layer "F.Cu") (net "/eth/ETH1.TX_CLK") (uuid "bbbbbbbb-4444-4000-8000-000000000018"))
	(segment (start 21 15) (end 26 15) (width 0.2) (layer "F.Cu") (net "Net-(U9-TXCLK)") (uuid "bbbbbbbb-4444-4000-8000-000000000019"))
	(segment (start 30 20) (end 34 20) (width 0.2) (layer "F.Cu") (net "VDD_ETH") (uuid "bbbbbbbb-4444-4000-8000-000000000020"))
)
`)
	b, err := board.Parse(src, "series.kicad_pcb")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestJoinedSumsTheSegmentsAndNotThePart(t *testing.T) {
	e := New(seriesBoard(t))
	j := e.Joined("/eth/ETH1.TX_CLK")

	if !j.Split() {
		t.Fatalf("not seen as split: %+v", j)
	}
	// 10 mm up to the resistor and 5 mm after it. The 1 mm across the part
	// itself is deliberately not counted.
	if j.LengthMM < 14.99 || j.LengthMM > 15.01 {
		t.Errorf("length %.3f mm, want 15 (10 + 5, without the part)", j.LengthMM)
	}
	if !j.Found || !j.Complete {
		t.Errorf("found=%v complete=%v", j.Found, j.Complete)
	}
	if !slices.Contains(j.Through, "R80") {
		t.Errorf("through %v, want R80", j.Through)
	}
	if len(j.Segments) != 2 {
		t.Errorf("segments %v", j.Segments)
	}
	// What this replaces: the near side on its own.
	if near := e.Measure("/eth/ETH1.TX_CLK").Longest.Length; near < 9.99 || near > 10.01 {
		t.Errorf("the near segment alone is %.3f mm, want 10", near)
	}
	// The same signal, asked for from the far end.
	if back := e.Joined("Net-(U9-TXCLK)"); back.LengthMM < 14.99 || back.LengthMM > 15.01 {
		t.Errorf("from the far end: %.3f mm", back.LengthMM)
	}
}

// The guard that matters. A decoupling capacitor is a two-pad part too, and
// following one would add a power plane to a signal's length.
func TestJoinedDoesNotFollowAPartIntoARail(t *testing.T) {
	e := New(seriesBoard(t))
	if j := e.Joined("VDD_ETH"); j.Split() {
		t.Errorf("followed a decoupling capacitor: %+v", j)
	}
	for _, name := range []string{"GND", "VDD", "VSS", "+3V3", "1V8", "VBUS", "VTT", "/mp25/VDDA1V8"} {
		if !isPowerName(name) {
			t.Errorf("%q is not recognised as a rail", name)
		}
	}
	for _, name := range []string{"TX_CLK", "/ethernet/ETH1.RXD0", "Net-(U9-RXD0_RXDLY)", "DDR_A0"} {
		if isPowerName(name) {
			t.Errorf("%q is taken for a rail", name)
		}
	}
}

// A net that passes through nothing still answers, so a caller needs no branch.
func TestJoinedOnAnUnsplitNet(t *testing.T) {
	b := loadBoard(t)
	e := New(b)
	nets := ddrNets(b)
	if len(nets) == 0 {
		t.Skip("no DDR nets")
	}
	for _, n := range nets[:min(12, len(nets))] {
		j, m := e.Joined(n), e.Measure(n)
		if j.Split() {
			continue // a terminated address line legitimately is split
		}
		if j.LengthMM != m.Longest.Length {
			t.Errorf("%s: joined %.4f, plain %.4f", n, j.LengthMM, m.Longest.Length)
		}
	}
}

// Two pads is not enough. A two-pin connector and a small chip both have two,
// and walking through one joins signals that have nothing to do with each
// other -- on a USB board it made D+ continue into D-.
func TestJoinedNeedsAPassiveNotJustTwoPads(t *testing.T) {
	src := []byte(`(kicad_pcb
	(version 20260206)
	(generator "pcb-trace-length-analyzer-test")
	(paper "A4")
	(layers (0 "F.Cu" signal) (2 "B.Cu" signal) (25 "Edge.Cuts" user))
	(setup (stackup
		(layer "F.Cu" (type "copper") (thickness 0.035))
		(layer "dielectric 1" (type "core") (thickness 1.51) (material "FR4") (epsilon_r 4.5))
		(layer "B.Cu" (type "copper") (thickness 0.035))
	))
	(gr_rect (start 0 0) (end 40 30) (stroke (width 0.05) (type default)) (fill none) (layer "Edge.Cuts") (uuid "ffffffff-4444-4000-8000-000000000021"))
	(footprint "t:u" (layer "F.Cu") (uuid "aaaaaaaa-4444-4000-8000-000000000022") (at 10 15) (attr smd)
		(property "Reference" "U1" (at 0 0) (layer "F.Cu") (uuid "aaaaaaaa-4444-4000-8000-000000000023"))
		(pad "1" smd circle (at 0 0) (size 0.3 0.3) (layers "F.Cu") (net "/USB_D+") (uuid "aaaaaaaa-4444-4000-8000-000000000024"))
		(pad "2" smd circle (at 0 1) (size 0.3 0.3) (layers "F.Cu") (net "/USB_D-") (uuid "aaaaaaaa-4444-4000-8000-000000000025"))
	)
	(footprint "t:j" (layer "F.Cu") (uuid "aaaaaaaa-4444-4000-8000-000000000026") (at 30 15) (attr smd)
		(property "Reference" "J1" (at 0 0) (layer "F.Cu") (uuid "aaaaaaaa-4444-4000-8000-000000000027"))
		(pad "1" smd circle (at 0 0) (size 0.3 0.3) (layers "F.Cu") (net "/USB_D+") (uuid "aaaaaaaa-4444-4000-8000-000000000028"))
		(pad "2" smd circle (at 0 1) (size 0.3 0.3) (layers "F.Cu") (net "/USB_D-") (uuid "aaaaaaaa-4444-4000-8000-000000000029"))
	)
	(segment (start 10 15) (end 30 15) (width 0.2) (layer "F.Cu") (net "/USB_D+") (uuid "bbbbbbbb-4444-4000-8000-000000000030"))
	(segment (start 10 16) (end 30 16) (width 0.2) (layer "F.Cu") (net "/USB_D-") (uuid "bbbbbbbb-4444-4000-8000-000000000031"))
)
`)
	b, err := board.Parse(src, "usb.kicad_pcb")
	if err != nil {
		t.Fatal(err)
	}
	e := New(b)
	if j := e.Joined("/USB_D+"); j.Split() {
		t.Errorf("D+ was joined to something through a chip or a connector: %+v", j)
	}
	for _, ref := range []string{"R80", "L4", "FB2", "C17"} {
		if !seriesPart.MatchString(ref) {
			t.Errorf("%q should be a part a signal can run through", ref)
		}
	}
	for _, ref := range []string{"U1", "J1", "CN3", "P2", "D5", "Q1"} {
		if seriesPart.MatchString(ref) {
			t.Errorf("%q should not be walked through", ref)
		}
	}
}
