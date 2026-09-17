package proto

import (
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/netlen"
)

const demoPath = "../demo-pcb/ai-vision.kicad_pcb"

func demo(t *testing.T) *board.Board {
	t.Helper()
	b, err := board.Load(demoPath)
	if err != nil {
		t.Skipf("demo board missing: %v", err)
	}
	return b
}

// ---- pairs ----

func TestFindPairsRecognisesTheUsualSpellings(t *testing.T) {
	got := FindPairs([]string{
		"/x/USB_D+", "/x/USB_D-",
		"/x/PCIE.RX1_P", "/x/PCIE.RX1_N",
		"/x/DDR_CLK_T", "/x/DDR_CLK_C",
		"/x/LVDS0P", "/x/LVDS0N",
	})
	if len(got) != 4 {
		t.Fatalf("%d pairs, want 4: %+v", len(got), got)
	}
	for _, p := range got {
		if p.P == "" || p.N == "" || p.P == p.N {
			t.Errorf("bad pair %+v", p)
		}
	}
}

// The rule that keeps this from inventing pairs: both halves must exist. A
// trailing N is otherwise indistinguishable from an active-low suffix, and DDR
// is full of those.
func TestFindPairsDoesNotMistakeActiveLowForAComplement(t *testing.T) {
	got := FindPairs([]string{
		"/d/DDR_RESETN", "/d/DDR_CASN", "/d/DDR_ACTN", "/d/DDR_WEN",
		"/d/DDR_A0", "/d/DDR_A1",
	})
	if len(got) != 0 {
		t.Errorf("invented %d pair(s) out of active-low singles: %+v", len(got), got)
	}
}

func TestFindPairsClaimsEachNetOnce(t *testing.T) {
	// CK_P could be read as base "CK_" plus "P", or base "CK" plus "_P". Both
	// readings must not produce two pairs out of two nets.
	got := FindPairs([]string{"/x/CK_P", "/x/CK_N"})
	if len(got) != 1 {
		t.Fatalf("%d pairs from two nets: %+v", len(got), got)
	}
}

func TestFindPairsNeedsSomethingBeforeTheSuffix(t *testing.T) {
	if got := FindPairs([]string{"/x/P", "/x/N"}); len(got) != 0 {
		t.Errorf("made a pair out of two nets called P and N: %+v", got)
	}
}

// ---- detection ----

func TestDetectsTheDemoBoardsInterfaces(t *testing.T) {
	b := demo(t)
	found := Detect(b)
	if len(found) == 0 {
		t.Fatal("nothing detected on a board with six interfaces on it")
	}
	kinds := map[Kind]int{}
	for _, i := range found {
		kinds[i.Kind]++
		if i.Evidence == "" {
			t.Errorf("%s was detected with no evidence", i.Name)
		}
		if i.Total != len(i.Nets) {
			t.Errorf("%s counts %d nets and lists %d", i.Name, i.Total, len(i.Nets))
		}
	}
	for _, want := range []Kind{DDR, RGMII, MIPI, PCIe, USB2, SDMMC} {
		if kinds[want] == 0 {
			t.Errorf("no %s interface found; got %v", want, kinds)
		}
	}
	// The board has an eMMC and two SD slots, differently named.
	if kinds[SDMMC] < 3 {
		t.Errorf("%d SD/eMMC interfaces, want the eMMC and both SDMMC controllers", kinds[SDMMC])
	}
}

// A net may belong to one interface only. Two interfaces claiming the same net
// would each measure it against a different reference, and both would be
// reported as fact.
func TestEveryNetBelongsToAtMostOneInterface(t *testing.T) {
	b := demo(t)
	owner := map[string]string{}
	for _, i := range Detect(b) {
		for _, n := range i.Nets {
			if prev, ok := owner[n]; ok {
				t.Errorf("%s is claimed by both %s and %s", n, prev, i.Name)
				continue
			}
			owner[n] = i.Name
		}
	}
}

// DDR is described, not planned, here: its requirement is not one set against
// one reference, and a group saying otherwise would be arithmetic rather than
// an answer.
func TestDDRIsLeftToItsOwnAnalyser(t *testing.T) {
	b := demo(t)
	for _, i := range Detect(b) {
		if i.Kind != DDR {
			continue
		}
		if len(i.Groups) != 0 {
			t.Errorf("DDR was given %d generic group(s): %+v", len(i.Groups), i.Groups)
		}
		if i.Planner == "" {
			t.Error("DDR does not say which analyser handles it")
		}
		if len(i.Pairs) == 0 {
			t.Error("DDR has strobe and clock pairs, which are checked the same way as anyone's")
		}
		return
	}
	t.Fatal("no DDR interface found")
}

func TestRGMIINeedsBothDirections(t *testing.T) {
	// Transmit alone is not RGMII: it is four nets that happen to be named TXD.
	only := detectRGMII([]string{"/e/ETH1.TXD0", "/e/ETH1.TXD1", "/e/ETH1.TXD2", "/e/ETH1.TXD3", "/e/ETH1.GTX_CLK"})
	if len(only) != 0 {
		t.Errorf("claimed RGMII from the transmit half alone: %+v", only)
	}
	both := detectRGMII([]string{
		"/e/ETH1.TXD0", "/e/ETH1.TXD1", "/e/ETH1.TXD2", "/e/ETH1.TXD3", "/e/ETH1.GTX_CLK",
		"/e/ETH1.RXD0", "/e/ETH1.RXD1", "/e/ETH1.RXD2", "/e/ETH1.RXD3", "/e/ETH1.RX_CLK",
	})
	if len(both) != 1 {
		t.Fatalf("%d interfaces from a whole RGMII bus", len(both))
	}
	if n := len(both[0].Groups); n != 2 {
		t.Fatalf("%d groups, want transmit and receive separately", n)
	}
	// Each direction is matched to its own clock and to nothing else.
	for _, g := range both[0].Groups {
		if g.Reference == "" {
			t.Errorf("group %q has no clock to match against", g.Name)
		}
		if strings.Contains(g.Name, "transmit") && !strings.Contains(g.Reference, "GTX_CLK") {
			t.Errorf("transmit is matched to %s", g.Reference)
		}
		if strings.Contains(g.Name, "receive") && !strings.Contains(g.Reference, "RX_CLK") {
			t.Errorf("receive is matched to %s", g.Reference)
		}
	}
}

// A bare TX/RX pair could be anything. Only nets that say PCIe are claimed as
// PCIe, because guessing is worse than not reporting.
func TestPCIeOnlyClaimsNetsThatSaySo(t *testing.T) {
	if got := detectPCIe([]string{"/x/TX0+", "/x/TX0-", "/x/RX0+", "/x/RX0-"}); len(got) != 0 {
		t.Errorf("claimed PCIe from unnamed pairs: %+v", got)
	}
	got := detectPCIe([]string{"/x/PCIE.RX0+", "/x/PCIE.RX0-"})
	if len(got) != 1 {
		t.Fatalf("%d interfaces from a named PCIe pair", len(got))
	}
	// Each lane recovers its own clock, so there is no group to match.
	if len(got[0].Groups) != 0 {
		t.Errorf("PCIe was given a group: %+v", got[0].Groups)
	}
	if got[0].IntraPair.MM <= 0 {
		t.Error("PCIe has no intra-pair limit, which is the whole requirement")
	}
}

// The same interface written two ways: SDMMC1_CK reads as instance SDMMC1
// carrying CK, GPIO.SDMMC3_CK as instance GPIO carrying SDMMC3_CK.
func TestSDMMCIsFoundUnderEitherNamingStyle(t *testing.T) {
	b := demo(t)
	var names []string
	for _, i := range Detect(b) {
		if i.Kind == SDMMC {
			names = append(names, i.Instance)
		}
	}
	sort.Strings(names)
	joined := strings.Join(names, ",")
	for _, want := range []string{"SDMMC1", "SDMMC3"} {
		if !strings.Contains(joined, want) {
			t.Errorf("%s not found among %v", want, names)
		}
	}
}

// A pair nobody recognised is still a pair, and saying so is more use than
// guessing at what it is.
func TestUnrecognisedPairsAreStillReported(t *testing.T) {
	b := demo(t)
	var any bool
	for _, i := range Detect(b) {
		if i.Kind == DiffOnly {
			any = true
			if len(i.Pairs) == 0 {
				t.Errorf("%s has no pairs in it", i.Name)
			}
			if i.IntraPair.MM <= 0 {
				t.Errorf("%s has no intra-pair limit", i.Name)
			}
		}
	}
	if !any {
		t.Error("the board's unclaimed differential pairs were not reported")
	}
}

// ---- measurement ----

func TestAssessMeasuresPairsAndSaysWhatItCannot(t *testing.T) {
	b := demo(t)
	e := netlen.New(b)
	var ddr *Interface
	for _, i := range Detect(b) {
		if i.Kind == DDR {
			ddr = i
		}
	}
	if ddr == nil {
		t.Fatal("no DDR interface")
	}
	a := ddr.Assess(e)
	if len(a.Pairs) != len(ddr.Pairs) {
		t.Errorf("%d pair measurements for %d pairs", len(a.Pairs), len(ddr.Pairs))
	}
	// Worst first, so the list leads with what to act on.
	for k := 1; k < len(a.Pairs); k++ {
		if a.Pairs[k].SkewMM > a.Pairs[k-1].SkewMM {
			t.Error("pairs are not ordered worst first")
			break
		}
	}
	// The demo board's strobe pairs are skewed (DQS3 by 1.479 mm), but a DDR
	// pair has no limit of its own (AN5724): measured, never out.
	if a.Pairs[0].SkewMM < 1 {
		t.Errorf("worst DDR pair skew %.3f mm; DQS3 is about 1.5", a.Pairs[0].SkewMM)
	}
	if a.PairsOut != 0 {
		t.Errorf("%d DDR pair(s) counted out, but DDR pairs have no limit", a.PairsOut)
	}
	if a.Summary == "" {
		t.Error("no summary")
	}
}

// A board where nothing is joined up has nothing to measure, and the honest
// answer is to say so rather than to report a skew of zero.
func TestAnInterfaceWithNoCompleteRouteSaysSo(t *testing.T) {
	b := demo(t)
	e := netlen.New(b)
	for _, i := range Detect(b) {
		if i.Kind != SDMMC {
			continue
		}
		a := i.Assess(e)
		if a.Unroutable != i.Total {
			continue // this one has something routed; not the case under test
		}
		if !strings.Contains(a.Summary, "joined end to end") && !strings.Contains(a.Summary, "not routed") {
			t.Errorf("%s: summary %q hides that nothing can be measured", i.Name, a.Summary)
		}
		if a.Actionable() {
			t.Errorf("%s claims work to do with nothing measurable", i.Name)
		}
		return
	}
}

// ---- assignment ----

// A board that names its camera link after the sensor is not a board this can
// recognise, and the user knows what it is. Assigning the family has to apply
// the family's limits either way, and say which case it is in.
func TestReassignFindsTheStructureWhenItIsThere(t *testing.T) {
	nets := []string{
		"/e/ETH1.TXD0", "/e/ETH1.TXD1", "/e/ETH1.TXD2", "/e/ETH1.TXD3", "/e/ETH1.GTX_CLK",
		"/e/ETH1.RXD0", "/e/ETH1.RXD1", "/e/ETH1.RXD2", "/e/ETH1.RXD3", "/e/ETH1.RX_CLK",
	}
	got := Reassign(nets, RGMII, "")
	if got.Kind != RGMII {
		t.Fatalf("kind %s", got.Kind)
	}
	if len(got.Groups) != 2 {
		t.Errorf("%d groups, want transmit and receive", len(got.Groups))
	}
	if !strings.Contains(got.Evidence, "do carry this interface's structure") {
		t.Errorf("evidence %q", got.Evidence)
	}
}

func TestReassignSaysWhenTheStructureIsNotInTheNames(t *testing.T) {
	// Named after a connector. The pairs are still pairs.
	nets := []string{"/x/J7_1_P", "/x/J7_1_N", "/x/J7_2_P", "/x/J7_2_N"}
	got := Reassign(nets, MIPI, "Camera link")
	if got.Name != "Camera link" {
		t.Errorf("name %q", got.Name)
	}
	if len(got.Pairs) != 2 {
		t.Errorf("%d pairs, want 2: a complement suffix is a complement suffix", len(got.Pairs))
	}
	if got.IntraPair.MM <= 0 {
		t.Error("the family's intra-pair limit was not applied")
	}
	if len(got.Groups) != 0 {
		t.Errorf("groups were invented: %+v", got.Groups)
	}
	if !strings.Contains(got.Evidence, "do not carry this interface's structure") {
		t.Errorf("evidence %q", got.Evidence)
	}
}

func TestReassignAdmitsWhenThereIsNothingToGoOn(t *testing.T) {
	got := Reassign([]string{"/x/SIG_A", "/x/SIG_B", "/x/SIG_C"}, SDMMC, "")
	if len(got.Pairs) != 0 || len(got.Groups) != 0 {
		t.Errorf("found structure in three unrelated names: %+v", got)
	}
	if !strings.Contains(got.Evidence, "nothing here to match automatically") {
		t.Errorf("evidence %q", got.Evidence)
	}
	// And naming the reference is what makes it actionable.
	got.WithReference("/x/SIG_A", Tolerance{MM: 2.5}, "")
	if len(got.Groups) != 1 || got.Groups[0].Reference != "/x/SIG_A" {
		t.Fatalf("naming the reference did not produce a group: %+v", got.Groups)
	}
	if n := len(got.Groups[0].Members); n != 2 {
		t.Errorf("%d members, want the other two nets", n)
	}
}

// ---- geometry ----

func TestGeometryIsMeasuredWhereTheBoardHasIt(t *testing.T) {
	b := demo(t)
	var usb, mipi *Interface
	for _, i := range Detect(b) {
		switch i.Kind {
		case USB2:
			usb = i
		case MIPI:
			mipi = i
		}
	}
	if usb == nil || mipi == nil {
		t.Skip("the demo board lacks one of these")
	}

	// USB is routed: both numbers come off the board.
	g := usb.MeasureGeometry(b)
	if g.WidthMM <= 0 {
		t.Error("USB is routed but no width was measured")
	}
	if g.GapMM <= 0 {
		t.Error("USB is a coupled pair but no spacing was measured")
	}
	if len(g.Measured) != 2 {
		t.Errorf("measured %v, want both width and spacing", g.Measured)
	}
	// The board's own net class says what the pair was drawn to, which is an
	// independent check on the measurement.
	if g.GapMM < 0.1 || g.GapMM > 0.2 {
		t.Errorf("USB pair spacing measured %.3f mm, which is not a plausible pair gap", g.GapMM)
	}

	// MIPI is not routed: nothing can be measured and the tool has to say so
	// rather than offering a default as though it were a reading.
	gm := mipi.MeasureGeometry(b)
	if len(gm.Measured) != 0 {
		t.Errorf("measured %v on an interface with no copper", gm.Measured)
	}
	if !strings.Contains(gm.Note, "have to be given") {
		t.Errorf("note %q does not say the numbers are needed", gm.Note)
	}
}

func TestImpedanceIsComputedAgainstTheFamilysTarget(t *testing.T) {
	b := demo(t)
	for _, i := range Detect(b) {
		if i.Kind != USB2 {
			continue
		}
		g := i.MeasureGeometry(b)
		z := i.CheckImpedance(b, g)
		if z.TargetDifferential != 90 {
			t.Errorf("USB differential target %v, want 90", z.TargetDifferential)
		}
		if z.Differential.Ohms < 60 || z.Differential.Ohms > 130 {
			t.Errorf("USB differential impedance came out %.1f ohms, which is not plausible", z.Differential.Ohms)
		}
		if !z.SingleEnded.Microstrip {
			t.Error("USB is on an outer layer here, so it is microstrip")
		}
		return
	}
	t.Skip("no USB on this board")
}

// A width with no copper to measure gives no impedance, rather than a figure
// computed from a default the user never saw.
func TestNoGeometryMeansNoImpedance(t *testing.T) {
	b := demo(t)
	for _, i := range Detect(b) {
		g := i.MeasureGeometry(b)
		if g.WidthMM > 0 {
			continue
		}
		z := i.CheckImpedance(b, g)
		if z.SingleEnded.Ohms != 0 || z.Differential.Ohms != 0 {
			t.Errorf("%s has no width but got %v / %v ohms", i.Name, z.SingleEnded.Ohms, z.Differential.Ohms)
		}
		if z.TargetDifferential == 0 && z.TargetSingleEnded == 0 {
			continue
		}
		return
	}
}

// Each direction of an RGMII bus is matched to its own clock, so losing the
// clock is not a cosmetic failure: the group falls back to its longest member
// and the data is matched to data. The general signal parser reads everything
// before the first underscore as an instance, which turns TX_CTL into CTL and
// GTX_CLK into CLK, so these names have to be read signal-first.
func TestRGMIIFindsItsClockInEveryNamingStyle(t *testing.T) {
	for _, c := range []struct {
		style          string
		nets           []string
		wantTX, wantRX string
	}{
		{
			"hierarchical sheet",
			[]string{"/eth/TXD0", "/eth/TXD1", "/eth/TXD2", "/eth/TXD3", "/eth/TX_CTL", "/eth/GTX_CLK",
				"/eth/RXD0", "/eth/RXD1", "/eth/RXD2", "/eth/RXD3", "/eth/RX_CTL", "/eth/RX_CLK"},
			"/eth/GTX_CLK", "/eth/RX_CLK",
		},
		{
			"instance prefix",
			[]string{"ETH1_TXD0", "ETH1_TXD1", "ETH1_TXD2", "ETH1_TXD3", "ETH1_TX_CTL", "ETH1_GTX_CLK",
				"ETH1_RXD0", "ETH1_RXD1", "ETH1_RXD2", "ETH1_RXD3", "ETH1_RX_CTL", "ETH1_RX_CLK"},
			"ETH1_GTX_CLK", "ETH1_RX_CLK",
		},
		{
			"no prefix at all",
			[]string{"TXD0", "TXD1", "TXD2", "TXD3", "TX_CTL", "GTX_CLK",
				"RXD0", "RXD1", "RXD2", "RXD3", "RX_CTL", "RX_CLK"},
			"GTX_CLK", "RX_CLK",
		},
		{
			"TXC and RXC, as many PHYs name them",
			[]string{"RGMII_TXD0", "RGMII_TXD1", "RGMII_TXD2", "RGMII_TXD3", "RGMII_TX_CTL", "RGMII_TXC",
				"RGMII_RXD0", "RGMII_RXD1", "RGMII_RXD2", "RGMII_RXD3", "RGMII_RX_CTL", "RGMII_RXC"},
			"RGMII_TXC", "RGMII_RXC",
		},
	} {
		got := detectRGMII(c.nets)
		if len(got) != 1 {
			t.Errorf("%s: %d interfaces, want 1", c.style, len(got))
			continue
		}
		for _, g := range got[0].Groups {
			want := c.wantTX
			if g.Name == "receive" {
				want = c.wantRX
			}
			if g.Reference != want {
				t.Errorf("%s: %s reference %q, want %q", c.style, g.Name, g.Reference, want)
			}
			// Four data lines, the control line and the clock.
			if len(g.Members) != 6 {
				t.Errorf("%s: %s has %d members, want 6: %v", c.style, g.Name, len(g.Members), g.Members)
			}
			if !slices.Contains(g.Members, want) {
				t.Errorf("%s: %s does not include its own clock: %v", c.style, g.Name, g.Members)
			}
		}
	}
}

// The instance is whatever precedes the signal, so two buses stay apart.
func TestRGMIIKeepsTwoBusesApart(t *testing.T) {
	nets := []string{}
	for _, inst := range []string{"ETH1", "ETH2"} {
		for _, sig := range []string{"TXD0", "TXD1", "TXD2", "TXD3", "TX_CTL", "GTX_CLK",
			"RXD0", "RXD1", "RXD2", "RXD3", "RX_CTL", "RX_CLK"} {
			nets = append(nets, inst+"_"+sig)
		}
	}
	got := detectRGMII(nets)
	if len(got) != 2 {
		t.Fatalf("%d interfaces, want 2", len(got))
	}
	if got[0].Instance != "ETH1" || got[1].Instance != "ETH2" {
		t.Errorf("instances %q and %q", got[0].Instance, got[1].Instance)
	}
	for _, i := range got {
		for _, g := range i.Groups {
			if !strings.HasPrefix(g.Reference, i.Instance) {
				t.Errorf("%s %s matched to %q, another bus's clock", i.Instance, g.Name, g.Reference)
			}
		}
	}
}
