package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/ddr"
	"github.com/embeddedci-com/pcb-autorouter/netlen"
	"github.com/embeddedci-com/pcb-autorouter/tune"
)

const demoPath = "../demo-pcb/ai-vision.kicad_pcb"

func fixture(t *testing.T) (*board.Board, *board.Project, *ddr.Interface, *netlen.Engine, *ddr.Plan) {
	t.Helper()
	b, err := board.Load(demoPath)
	if err != nil {
		t.Skipf("demo board missing: %v", err)
	}
	p, err := board.LoadProject(demoPath)
	if err != nil {
		t.Fatal(err)
	}
	iface, err := ddr.Classify(b, ddr.Options{NetPrefix: "/ddr4/"})
	if err != nil {
		t.Fatal(err)
	}
	e := netlen.New(b)
	plan, err := ddr.BuildPlan(iface, e, ddr.DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	return b, p, iface, e, plan
}

func TestInterfaceReport(t *testing.T) {
	b, p, iface, _, _ := fixture(t)
	var out bytes.Buffer
	Interface(&out, b, p, iface)
	s := out.String()
	for _, want := range []string{
		"Controller:  U3", "U4, U5", "x32 in 4 byte lanes",
		"custom .kicad_dru", "Verify with KiCad's own DRC",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("report is missing %q:\n%s", want, s)
		}
	}
}

// TestRoutingReportGroupsOneFindingOnce checks the readability property that
// matters most here: 25 nets with the same unrouted leg must read as one
// finding, not 25, or the thing the user has to notice gets lost in a list.
func TestRoutingReportGroupsOneFindingOnce(t *testing.T) {
	_, _, iface, e, _ := fixture(t)
	var out bytes.Buffer
	n := Routing(&out, e, iface)
	s := out.String()
	if n != 27 {
		t.Errorf("reported %d incomplete nets, want 27", n)
	}
	if !strings.Contains(s, "44 of 71 nets fully routed") {
		t.Errorf("missing the headline count:\n%s", s)
	}
	if !strings.Contains(s, "25 net(s) split into") {
		t.Errorf("the 25 address nets were not grouped together:\n%s", s)
	}
	// Termination resistors must be generalised, not listed by designator.
	if !strings.Contains(s, "termination") {
		t.Errorf("termination resistors not generalised:\n%s", s)
	}
	for _, ref := range []string{"R20", "R21", "R33"} {
		if strings.Contains(s, ref) {
			t.Errorf("individual resistor %s leaked into the report:\n%s", ref, s)
		}
	}
	if !strings.Contains(s, "KiCad's DRC does not report these gaps") {
		t.Error("the report should say that KiCad will not warn about this")
	}
}

func TestPlanReport(t *testing.T) {
	_, _, _, _, plan := fixture(t)
	var out bytes.Buffer
	Plan(&out, plan)
	s := out.String()
	for _, want := range []string{
		"BYTE LANE 0", "BYTE LANE 3", "ADDRESS/COMMAND U3->U4",
		"out of tolerance", "delay ps",
		// The target and what it is made of, so every offset can be read.
		"target", "the mean of DQS0_N", "DQS0_P",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q:\n%s", want, s)
		}
	}
	// Both units must be shown: the length to check in pcbnew and the delay
	// the timing budget is written in.
	if !strings.Contains(s, "length mm") || !strings.Contains(s, "delay ps") {
		t.Error("both length and delay columns should be present")
	}
	// Full net paths are noise in a table; the suffix is what people say.
	if strings.Contains(s, "/ddr4/DDR_DQ0") {
		t.Error("net names should be shortened in the table")
	}
	if !strings.Contains(s, "Not matched:") {
		t.Error("nets left out should be listed with a reason")
	}
}

func TestIntraPairSkewReport(t *testing.T) {
	_, _, _, _, plan := fixture(t)
	var out bytes.Buffer
	IntraPairSkew(&out, plan)
	s := out.String()
	if !strings.Contains(s, "Differential pair skew") {
		t.Fatalf("missing heading:\n%s", s)
	}
	// Listed for information, never flagged: AN5724 sets no pair limit.
	if strings.Contains(s, "out of tolerance") || !strings.Contains(s, "for information") {
		t.Errorf("DDR pair skew should be information only:\n%s", s)
	}
}

func TestChangesReport(t *testing.T) {
	var out bytes.Buffer
	met, shortCount, added, missing := Changes(&out, []*tune.Result{
		{Net: "/ddr4/DDR_DQ3", Requested: 6, Added: 6, Meanders: 3},
		{Net: "/ddr4/DDR_DQ24", Requested: 7.6, Added: 0, Shortfall: 7.6,
			Notes: []string{"no room"}},
		{Net: "/ddr4/DDR_DQ26", Requested: 4, Added: 1.5, Shortfall: 2.5},
	})
	if met != 1 || shortCount != 2 {
		t.Errorf("met=%d short=%d, want 1 and 2", met, shortCount)
	}
	if added != 7.5 || missing != 10.1 {
		t.Errorf("added=%v missing=%v, want 7.5 and 10.1", added, missing)
	}
	s := out.String()
	// The worst shortfall first: it is the thing the user has to act on.
	iDQ24 := strings.Index(s, "DQ24")
	iDQ26 := strings.Index(s, "DQ26")
	if iDQ24 < 0 || iDQ26 < 0 || iDQ24 > iDQ26 {
		t.Errorf("shortfalls not ordered worst first:\n%s", s)
	}
	if !strings.Contains(s, "1 net(s) met, 2 short") {
		t.Errorf("missing the summary line:\n%s", s)
	}
}

func TestChangesReportWithNothingToDo(t *testing.T) {
	var out bytes.Buffer
	met, shortCount, added, missing := Changes(&out, nil)
	if met != 0 || shortCount != 0 || added != 0 || missing != 0 {
		t.Errorf("got %d/%d/%v/%v for an empty run", met, shortCount, added, missing)
	}
}

func TestShortNetNames(t *testing.T) {
	for in, want := range map[string]string{
		"/ddr4/DDR_DQ0":        "DQ0",
		"DDR_DQS1_P":           "DQS1_P",
		"/a/b/DDR_CLK_N":       "CLK_N",
		"GND":                  "GND",
		"/ddr4/DDR_DQ0 U3->U4": "DQ0 U3->U4",
	} {
		if got := short(in); got != want {
			t.Errorf("short(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWrapKeepsLinesShort(t *testing.T) {
	items := []string{"A0", "A1", "A10", "A11", "A12", "A13", "ACTN", "BA0", "BA1", "BG0",
		"CASN", "CKE", "CSN", "ODT", "RASN", "RESETN", "WEN", "A2", "A3", "A4", "A5"}
	got := wrap(items, 40, "    ")
	for _, line := range strings.Split(got, "\n") {
		if len(strings.TrimLeft(line, " ")) > 42 {
			t.Errorf("line too long (%d): %q", len(line), line)
		}
	}
	// Nothing may be dropped.
	for _, it := range items {
		if !strings.Contains(got, it) {
			t.Errorf("%s missing from the wrapped output", it)
		}
	}
}
