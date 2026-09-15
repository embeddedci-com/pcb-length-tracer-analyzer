package board

import (
	"math"
	"path/filepath"
	"testing"
)

func TestLoadProjectFromDemoBoard(t *testing.T) {
	p, err := LoadProject(demoPath)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if len(p.Classes) < 10 {
		t.Errorf("%d net classes, want the board's full set", len(p.Classes))
	}
	if math.Abs(p.MinClearance-0.1) > 1e-9 {
		t.Errorf("MinClearance = %v, want 0.1", p.MinClearance)
	}
	if math.Abs(p.MinTrackWidth-0.089) > 1e-9 {
		t.Errorf("MinTrackWidth = %v, want 0.089", p.MinTrackWidth)
	}
	if math.Abs(p.EdgeClearance-0.2) > 1e-9 {
		t.Errorf("EdgeClearance = %v, want 0.2", p.EdgeClearance)
	}
	if !p.UseHeightForLength {
		t.Error("this board counts via height toward length")
	}
	if !p.HasCustomRules {
		t.Error("the demo board has a .kicad_dru beside it")
	}
}

func TestNetClassAssignment(t *testing.T) {
	p, err := LoadProject(demoPath)
	if err != nil {
		t.Fatal(err)
	}
	// The board assigns /ddr4/DDR_* to DDR, then the differential halves to
	// DDR_DIFF with a later pattern, which therefore wins.
	for net, want := range map[string]string{
		"/ddr4/DDR_DQ0":    "DDR",
		"/ddr4/DDR_A13":    "DDR",
		"/ddr4/DDR_DQS0_P": "DDR_DIFF",
		"/ddr4/DDR_CLK_N":  "DDR_DIFF",
		"VDD_DDR":          "DDR_POWER",
		"GND":              "GND",
		"/MP257/GPIO7":     "GPIO",
	} {
		c := p.ClassOf(net)
		if c == nil {
			t.Errorf("%s: no class", net)
			continue
		}
		if c.Name != want {
			t.Errorf("%s is in class %s, want %s", net, c.Name, want)
		}
	}
	// An unmatched net falls back to Default.
	if c := p.ClassOf("/nothing/AT_ALL"); c == nil || c.Name != "Default" {
		t.Errorf("unmatched net got %v, want Default", c)
	}
}

func TestClearanceResolution(t *testing.T) {
	p, err := LoadProject(demoPath)
	if err != nil {
		t.Fatal(err)
	}
	// DDR is 0.2, DDR_DIFF is 0.1, and the board floor is 0.1.
	if got := p.ClearanceOf("/ddr4/DDR_DQ0"); math.Abs(got-0.2) > 1e-9 {
		t.Errorf("DQ0 clearance = %v, want 0.2", got)
	}
	if got := p.ClearanceOf("/ddr4/DDR_DQS0_P"); math.Abs(got-0.1) > 1e-9 {
		t.Errorf("DQS0_P clearance = %v, want 0.1", got)
	}
	// Between two nets, the larger requirement wins.
	if got := p.ClearanceBetween("/ddr4/DDR_DQS0_P", "/ddr4/DDR_DQ0"); math.Abs(got-0.2) > 1e-9 {
		t.Errorf("between = %v, want 0.2", got)
	}
	if got := p.TrackWidthOf("/ddr4/DDR_DQ0"); math.Abs(got-0.09) > 1e-9 {
		t.Errorf("DQ0 track width = %v, want 0.09", got)
	}
	// DDR_DIFF sets only a differential width, which stands in for the track
	// width when routed as a pair.
	if got := p.TrackWidthOf("/ddr4/DDR_CLK_P"); math.Abs(got-0.09) > 1e-9 {
		t.Errorf("CLK_P width = %v, want 0.09", got)
	}
}

func TestLoadProjectWithoutFile(t *testing.T) {
	p, err := LoadProject(filepath.Join(t.TempDir(), "nothing.kicad_pcb"))
	if err != nil {
		t.Fatalf("a board with no project file must still load: %v", err)
	}
	if p.MinClearance <= 0 || p.EdgeClearance <= 0 {
		t.Errorf("defaults not applied: %+v", p)
	}
	if p.HasCustomRules {
		t.Error("no rules file should be reported")
	}
	if c := p.ClassOf("anything"); c != nil {
		t.Errorf("no classes should resolve, got %v", c)
	}
	if got := p.ClearanceOf("anything"); got != p.MinClearance {
		t.Errorf("clearance = %v, want the floor %v", got, p.MinClearance)
	}
}

func TestMatchPattern(t *testing.T) {
	cases := []struct {
		pat, s string
		want   bool
	}{
		{"/ddr4/DDR_*", "/ddr4/DDR_DQ0", true},
		{"/ddr4/DDR_*", "/ddr4/OTHER", false},
		{"/ddr4/DDR_*_P", "/ddr4/DDR_DQS0_P", true},
		{"/ddr4/DDR_*_P", "/ddr4/DDR_DQS0_N", false},
		{"GND", "GND", true},
		{"GND", "GNDA", false},
		{"*", "anything", true},
		{"D?0", "DQ0", true},
		{"D?0", "DQ10", false},
		{"a*b*c", "axxbyyc", true},
		{"a*b*c", "axxbyyd", false},
		{"***a", "xyza", true},
		{"", "", true},
		{"", "x", false},
	}
	for _, c := range cases {
		if got := matchPattern(c.pat, c.s); got != c.want {
			t.Errorf("matchPattern(%q, %q) = %v, want %v", c.pat, c.s, got, c.want)
		}
	}
}
