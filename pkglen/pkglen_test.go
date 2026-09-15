package pkglen

import (
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/board"
)

func TestBall(t *testing.T) {
	for in, want := range map[string]string{
		"DDR_A5_M19":    "DDR_A5",
		"DDR_DQS3N_H21": "DDR_DQS3N",
		"DDR_DQ3_AA22":  "DDR_DQ3",
	} {
		if got := Ball(in); got != want {
			t.Errorf("Ball(%q) = %q, want %q", in, got, want)
		}
	}
}

// The table follows ball names, so a pin swap moves the length with the ball;
// a footprint's own die length is kept.
func TestApplyFillsKnownPartsAndKeepsTheFootprint(t *testing.T) {
	mpu := &board.Footprint{Ref: "U3", Value: "STM32MP257FAI3", Pads: []*board.Pad{
		{Ref: "U3", Name: "M19", Function: "DDR_A5_M19", Net: "/ddr4/DDR_CLK_N"},
		{Ref: "U3", Name: "N19", Function: "DDR_A4_N19", Net: "/ddr4/DDR_A4", DieLength: 1.5, DieSource: "footprint"},
		{Ref: "U3", Name: "A1", Function: "VDD_A1"},
	}}
	mem := &board.Footprint{Ref: "U4", Value: "MT40A512M16", Pads: []*board.Pad{
		{Ref: "U4", Name: "K8", Function: "CK_N", Net: "/ddr4/DDR_CLK_N"},
	}}
	b := &board.Board{Footprints: []*board.Footprint{mem, mpu}}

	got := Apply(b)
	if len(got) != 1 || got[0].Ref != "U3" || got[0].Pads != 1 || got[0].FromBoard != 1 {
		t.Fatalf("Apply = %+v", got)
	}
	if l := mpu.Pads[0].DieLength; l != 7.693 {
		t.Errorf("DDR_A5 die length %v, want ST's 7.693", l)
	}
	if l := mpu.Pads[1].DieLength; l != 1.5 {
		t.Errorf("the footprint's own die length was replaced: %v", l)
	}
	if l := mem.Pads[0].DieLength; l != 0 {
		t.Errorf("an unknown part got a length: %v", l)
	}
	// Idempotent: the analysis runs more than once on one board.
	if again := Apply(b); len(again) != 1 || again[0].Pads != 1 || again[0].FromBoard != 1 || mpu.Pads[0].DieLength != 7.693 {
		t.Errorf("second Apply = %+v", again)
	}
}

// An override replaces the length on that net's pad and reports what the
// default was, so the page can offer to go back to it.
func TestOverrideReportsTheDefault(t *testing.T) {
	mpu := &board.Footprint{Ref: "U3", Value: "STM32MP257FAI3", Pads: []*board.Pad{
		{Ref: "U3", Name: "M19", Function: "DDR_A5_M19", Net: "/ddr4/DDR_CLK_N"},
		{Ref: "U3", Name: "N19", Function: "DDR_A4_N19", Net: "/ddr4/DDR_CLK_P"},
	}}
	b := &board.Board{Footprints: []*board.Footprint{mpu}}
	Apply(b)
	rows := Override(b, "U3", []string{"/ddr4/DDR_CLK_N", "/ddr4/DDR_CLK_P"}, map[string]float64{"/ddr4/DDR_CLK_P": 7})
	if len(rows) != 2 {
		t.Fatalf("rows %+v", rows)
	}
	n, p := rows[0], rows[1]
	if n.MM != 7.693 || n.DefaultMM != 7.693 || n.Source != "STM32MP25xxAI" || n.Ball != "DDR_A5" {
		t.Errorf("CLK_N %+v", n)
	}
	if p.MM != 7 || p.DefaultMM != 6.886 || p.Source != "override" || mpu.Pads[1].DieLength != 7 {
		t.Errorf("CLK_P %+v, pad %v", p, mpu.Pads[1].DieLength)
	}
}

// A footprint whose pads carry no pin function, or name the pin some other
// way, still gets the table: the ball number is the same on every board.
func TestApplyFallsBackToTheBallNumber(t *testing.T) {
	mpu := &board.Footprint{Ref: "U27", Value: "STM32MP257FAI3", Pads: []*board.Pad{
		{Ref: "U27", Name: "M19", Net: "/DDR_CLK_N"},
		{Ref: "U27", Name: "N19", Function: "PDDR_CLK_P", Net: "/DDR_CLK_P"},
		{Ref: "U27", Name: "W22", Function: "DDR_DQ0", Net: "/DDR_DQ0"},
	}}
	b := &board.Board{Footprints: []*board.Footprint{mpu}}
	if got := Apply(b); len(got) != 1 || got[0].Pads != 3 {
		t.Fatalf("Apply = %+v", got)
	}
	for i, want := range []float64{7.693, 6.886, 9.155} {
		if l := mpu.Pads[i].DieLength; l != want {
			t.Errorf("pad %s: %v, want %v", mpu.Pads[i].Name, l, want)
		}
	}
}

func TestSquashAndLibraryMatch(t *testing.T) {
	if got := squash("DDR_DQS0_P"); got != "DDR_DQS0P" {
		t.Errorf("squash = %q", got)
	}
	if lookup("U_MPU", "ST:TFBGA436_STM32MP257FAI3") == nil {
		t.Error("the footprint library name should identify the part")
	}
	if lookup("STM32MP25XXAI", "") == nil {
		t.Error("the family name ST's sheet uses should identify the part")
	}
	if lookup("STM32MP257FAK3", "") != nil {
		t.Error("the 14x14 AK package is a different table")
	}
}

// A footprint nobody recognised is given a part by hand, and "none" takes it
// away again.
func TestUsePart(t *testing.T) {
	mpu := &board.Footprint{Ref: "U27", Value: "MPU", Pads: []*board.Pad{
		{Ref: "U27", Name: "W22", Net: "/DDR_DQ0"},
		{Ref: "U27", Name: "Y21", Net: "/DDR_DQ1", DieLength: 1, DieSource: "footprint"},
	}}
	b := &board.Board{Footprints: []*board.Footprint{mpu}}
	if len(Apply(b)) != 1 || mpu.Pads[0].DieLength != 0 {
		t.Fatal("an unknown value got a table")
	}
	if a := UsePart(b, "U27", "STM32MP25xxAI"); a.Pads != 1 || mpu.Pads[0].DieLength != 9.155 {
		t.Errorf("UsePart = %+v, pad %v", a, mpu.Pads[0].DieLength)
	}
	if a := UsePart(b, "U27", "none"); a.Pads != 0 || mpu.Pads[0].DieLength != 0 || mpu.Pads[1].DieLength != 1 {
		t.Errorf("none = %+v, pads %v %v", a, mpu.Pads[0].DieLength, mpu.Pads[1].DieLength)
	}
}
