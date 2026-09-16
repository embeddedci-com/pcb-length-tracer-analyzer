package server

import (
	"os"
	"path/filepath"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"math"
	"net/http"
	"slices"
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/ddr"
)

// Choosing a preset has to reach the planner: the limits the report judges by
// must be the vendor's, in the unit the vendor used.
func TestApplyingAPresetReachesTheRulesTheReportUses(t *testing.T) {
	// A length-based preset (Rockchip's 10-layer HDI tables, in mils).
	p, err := ApplyPreset(DefaultParams(), "rk3588-lpddr4-hdi")
	if err != nil {
		t.Fatal(err)
	}
	r := p.withDefaults().rules()
	if math.Abs(r.DataToStrobe.MM-0.635) > 1e-9 || r.DataToStrobe.PS != 0 {
		t.Errorf("data to strobe %s, want 0.635 mm and no delay", r.DataToStrobe)
	}
	if math.Abs(r.AddressToClock.MM-1.016) > 1e-9 || math.Abs(r.StrobeToClock.MM-6.35) > 1e-9 {
		t.Errorf("address %s, strobe to clock %s", r.AddressToClock, r.StrobeToClock)
	}
	if err := r.Validate(); err != nil {
		t.Errorf("rules from a preset are invalid: %v", err)
	}

	// A delay-based preset: the lengths it replaces must be cleared, or the
	// board is held to both, and withDefaults must not put them back.
	q, err := ApplyPreset(DefaultParams(), "rk3588-lpddr4-8layer")
	if err != nil {
		t.Fatal(err)
	}
	rq := q.withDefaults().rules()
	if rq.DataToStrobe.MM != 0 || rq.DataToStrobe.PS != 16 {
		t.Errorf("data to strobe %s, want 16 ps alone", rq.DataToStrobe)
	}
	if rq.StrobeToClock.MM != 0 || rq.StrobeToClock.PS != 40 {
		t.Errorf("strobe to clock %s, want 40 ps alone", rq.StrobeToClock)
	}
	if rq.IntraPair.MM != 0 || rq.IntraPair.PS != 1 {
		t.Errorf("intra-pair %s, want 1 ps alone", rq.IntraPair)
	}
}

// A preset that states nothing about one memory device against another still
// has to show the figure that will be in force, not a zero the server would
// quietly replace.
func TestAPresetShowsTheChipDeltaThatWillApply(t *testing.T) {
	for _, info := range KnownPresets() {
		if info.Params.MaxChipDeltaMM <= 0 {
			t.Errorf("%s: chip delta %v", info.ID, info.Params.MaxChipDeltaMM)
		}
		p, _ := ApplyPreset(DefaultParams(), info.ID)
		if got := p.withDefaults().MaxChipDeltaMM; math.Abs(got-info.Params.MaxChipDeltaMM) > 1e-9 {
			t.Errorf("%s: shows %.3f, applies %.3f", info.ID, info.Params.MaxChipDeltaMM, got)
		}
	}
}

func TestApplyPresetRefusesAnUnknownID(t *testing.T) {
	if _, err := ApplyPreset(DefaultParams(), "nope"); err == nil {
		t.Error("an unknown preset was applied")
	}
}

// The form reads them from /defaults, so they have to arrive with their
// citations and their values.
func TestDefaultsServesThePresets(t *testing.T) {
	h := newHarness(t)
	rec := h.do("GET", "/api/pcb-trace-length-analyzer/defaults", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	got := decode[struct {
		Presets []PresetInfo `json:"presets"`
	}](t, rec)
	if len(got.Presets) != len(ddr.Presets()) {
		t.Fatalf("%d presets served, %d known", len(got.Presets), len(ddr.Presets()))
	}
	var rk PresetInfo
	for _, p := range got.Presets {
		if p.ID == "rk3588-lpddr4-hdi" {
			rk = p
		}
	}
	if rk.Vendor != "Rockchip" || rk.Source == "" || rk.Params.DataToStrobeMM == 0 {
		t.Errorf("RK3588 preset arrived as %+v", rk)
	}
	// The tolerances themselves are not sent twice: the form applies params.
	if rk.DataToStrobe.MM != 0.635 && rk.Params.DataToStrobeMM != 0.635 {
		t.Errorf("neither form of the limit is right: %+v", rk)
	}
}

// A limit the guide does not state carries the tool's default, and says so.
// Otherwise the form would show a number under a vendor's citation that the
// vendor never wrote, and nothing would tell the reader which was which.
func TestUnstatedLimitsCarryTheDefaultAndAreNamed(t *testing.T) {
	d := DefaultParams()
	byID := map[string]PresetInfo{}
	for _, p := range KnownPresets() {
		byID[p.ID] = p
	}

	am64x, ok := byID["am64x-lpddr4"]
	if !ok {
		t.Fatal("no am64x preset")
	}
	if am64x.Params.StrobeToClockMM != d.StrobeToClockMM || am64x.Params.StrobeToClockPS != 0 {
		t.Errorf("am64x strobe to clock: %v mm / %v ps, want the default %v mm",
			am64x.Params.StrobeToClockMM, am64x.Params.StrobeToClockPS, d.StrobeToClockMM)
	}
	if !slices.Contains(am64x.Unstated, "strobe_to_clock_mm") {
		t.Errorf("am64x: strobe to clock is the default but unstated is %v", am64x.Unstated)
	}
	// Its own figures are not claimed to be defaults.
	if slices.Contains(am64x.Unstated, "data_to_strobe_mm") {
		t.Errorf("am64x: data to strobe is the guide's own but named unstated: %v", am64x.Unstated)
	}

	// Every preset that is silent somewhere names it, and every one that is
	// not silent names only the chip delta (which no point-to-point guide gives).
	for _, p := range KnownPresets() {
		if p.StrobeToClock.Zero() != slices.Contains(p.Unstated, "strobe_to_clock_mm") {
			t.Errorf("%s: strobe to clock zero=%v, unstated=%v", p.ID, p.StrobeToClock.Zero(), p.Unstated)
		}
		if p.MaxChipDeltaMM <= 0 && !slices.Contains(p.Unstated, "max_chip_delta_mm") {
			t.Errorf("%s: chip delta is the default but unstated is %v", p.ID, p.Unstated)
		}
	}

	// Applying one still puts a working number in force.
	got, err := ApplyPreset(DefaultParams(), "am64x-lpddr4")
	if err != nil {
		t.Fatal(err)
	}
	if got.StrobeToClockMM != d.StrobeToClockMM {
		t.Errorf("applied strobe to clock %v, want %v", got.StrobeToClockMM, d.StrobeToClockMM)
	}
}

// The board says which chip it has, and the analysis has to pass that on: it
// is what lets the form warn that the limits in force are some other vendor's.
func TestAnalysisNamesThePresetsForTheControllersPart(t *testing.T) {
	path := filepath.Join("../demo-pcb", "ai-vision.kicad_pcb")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skip(err)
	}
	b, err := board.Parse(raw, path)
	if err != nil {
		t.Fatal(err)
	}
	proj, _ := board.LoadProject(path)
	a, _, err := analyse(b, proj, "ai-vision.kicad_pcb", Params{}.withDefaults(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.Interface.ControllerValue == "" {
		t.Fatal("no controller value read from the board")
	}
	if !slices.Contains(a.Interface.PresetsForPart, "st-stm32mp25-ddr4") {
		t.Errorf("controller %q gave presets %v, want the STM32MP25 one",
			a.Interface.ControllerValue, a.Interface.PresetsForPart)
	}
}
