package ddr

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"testing"
)

// A preset is a citation as much as a set of numbers: without the document it
// came from, nobody can check the board against the guide it claims to follow.
func TestEveryPresetCitesItsSource(t *testing.T) {
	for _, p := range Presets() {
		if p.ID == "" || p.Name == "" || p.Vendor == "" || p.Memory == "" {
			t.Errorf("%+v: incompletely named", p)
		}
		// A revision or a date, because guides are revised and the numbers
		// move with them: "V1.0" the way Rockchip writes it, "Rev. 2" the
		// way NXP does, "Rev. A" the way Microchip does, a publication date
		// for TI, or the ST sheet by the note it comes with.
		if !regexp.MustCompile(`V\d|Rev\.\s?[0-9A-Z]|\(\d{4}-\d{2}|AN5724`).MatchString(p.Source) {
			t.Errorf("%s: source %q names no document version", p.ID, p.Source)
		}
		if p.URL != "" && !strings.HasPrefix(p.URL, "https://") {
			t.Errorf("%s: url %q", p.ID, p.URL)
		}
	}
}

// Each limit is stated the way its guide states it, in length or in delay, and
// never in both: the tool takes the tighter of the two, which for numbers from
// different columns of the same table would be a limit the vendor never wrote.
//
// A limit may be unset, meaning the guide gives none, but never silently: the
// note has to tell the reader, because the tool's default applies there and it
// is not the vendor's figure.
func TestEveryLimitIsSetInExactlyOneUnit(t *testing.T) {
	for _, p := range Presets() {
		for name, tol := range map[string]Tolerance{
			"data to strobe":   p.DataToStrobe,
			"intra-pair":       p.IntraPair,
			"address to clock": p.AddressToClock,
			"strobe to clock":  p.StrobeToClock,
		} {
			if tol.Zero() {
				// Named in the note, so a reader sees which figure is the
				// tool's rather than the vendor's.
				word := map[string]string{"intra-pair": "pair", "strobe to clock": "strobe"}[name]
				if word == "" || !strings.Contains(strings.ToLower(p.Note), word) {
					t.Errorf("%s: %s is unset and the note does not say so: %q", p.ID, name, p.Note)
				}
				continue
			}
			if tol.MM > 0 && tol.PS > 0 {
				t.Errorf("%s: %s is given in both units (%s)", p.ID, name, tol)
			}
		}
	}
}

// A preset with no data-to-strobe or address-to-clock limit would not be worth
// offering: those two are what a length matcher is for, and every guide states
// them. The pair and the strobe against the clock are the ones some guides
// really do leave out.
func TestDataAndAddressLimitsAreAlwaysStated(t *testing.T) {
	for _, p := range Presets() {
		if p.DataToStrobe.Zero() || p.AddressToClock.Zero() {
			t.Errorf("%s: a limit every guide states is unset: %+v", p.ID, p)
		}
	}
}

// The form shows which preset a board is on by matching the numbers, so two
// presets may never carry the same ones.
func TestPresetsAreDistinctAndUniquelyIdentified(t *testing.T) {
	seen := map[string]string{}
	values := map[string]string{}
	for _, p := range Presets() {
		if other, ok := seen[p.ID]; ok {
			t.Errorf("duplicate id %q, also %s", p.ID, other)
		}
		seen[p.ID] = p.Name
		key := fmt.Sprintf("%s|%s|%s|%s|%g|%g", p.DataToStrobe, p.IntraPair, p.AddressToClock,
			p.StrobeToClock, p.MaxChipDeltaMM, p.ClockOffsetPercent)
		if other, ok := values[key]; ok {
			t.Errorf("%s has the same limits as %s (%s)", p.ID, other, key)
		}
		values[key] = p.ID
	}
}

// Every number comes from a vendor's table; these are the bounds inside which
// a typo would still look plausible, so they are the ones worth asserting.
func TestPresetNumbersAreInAPlausibleRange(t *testing.T) {
	for _, p := range Presets() {
		for name, tol := range map[string]Tolerance{
			"data to strobe": p.DataToStrobe, "intra-pair": p.IntraPair,
			"address to clock": p.AddressToClock, "strobe to clock": p.StrobeToClock,
		} {
			if tol.MM > 0 && (tol.MM < 0.05 || tol.MM > 50) {
				t.Errorf("%s: %s is %.3f mm", p.ID, name, tol.MM)
			}
			if tol.PS > 0 && (tol.PS < 0.2 || tol.PS > 5000) {
				t.Errorf("%s: %s is %.1f ps", p.ID, name, tol.PS)
			}
		}
		// A pair is always held tighter than a byte lane, and a byte lane
		// tighter than strobe against clock. A preset that breaks that has a
		// column swapped.
		if !tighter(p.IntraPair, p.DataToStrobe) {
			t.Errorf("%s: intra-pair %s is not tighter than data to strobe %s", p.ID, p.IntraPair, p.DataToStrobe)
		}
		if !tighter(p.DataToStrobe, p.StrobeToClock) {
			t.Errorf("%s: data to strobe %s is not tighter than strobe to clock %s", p.ID, p.DataToStrobe, p.StrobeToClock)
		}
	}
}

func tighter(a, b Tolerance) bool {
	if a.MM > 0 && b.MM > 0 {
		return a.MM <= b.MM
	}
	if a.PS > 0 && b.PS > 0 {
		return a.PS <= b.PS
	}
	return true // different units: not comparable without a stackup
}

// The mil figures the Rockchip tables give, in the millimetres the report
// works in. Wrong by 25.4 is the mistake this catches.
func TestMilsAreConvertedExactly(t *testing.T) {
	for _, c := range []struct {
		id   string
		want float64
	}{
		{"rk3588-lpddr4-hdi", 25 * 0.0254}, // 0.635 mm
		{"rk3588-lpddr5-hdi", 25 * 0.0254}, // 0.635 mm
		{"rk3568-lpddr4", 600 * 0.0254},    // 15.24 mm
		{"sama5d3-ddr2", 100 * 0.0254},     // 2.54 mm
		{"sama5d2-ddr3l", 100 * 0.0254},    // 2.54 mm
		{"stm32mp1-ddr3l", 40 * 0.0254},    // 1.016 mm
	} {
		p, ok := PresetByID(c.id)
		if !ok {
			t.Fatalf("no preset %q", c.id)
		}
		if math.Abs(p.DataToStrobe.MM-c.want) > 1e-9 {
			t.Errorf("%s: data to strobe %.4f mm, want %.4f", c.id, p.DataToStrobe.MM, c.want)
		}
	}
	// And the numbers themselves, against the guides.
	rk, _ := PresetByID("rk3588-lpddr4-hdi")
	if math.Abs(rk.StrobeToClock.MM-6.35) > 1e-9 || math.Abs(rk.AddressToClock.MM-1.016) > 1e-9 {
		t.Errorf("rk3588 hdi: %s / %s, want 1.016 mm and 6.350 mm", rk.AddressToClock, rk.StrobeToClock)
	}
	eight, _ := PresetByID("rk3588-lpddr4-8layer")
	if eight.DataToStrobe.PS != 16 || eight.StrobeToClock.PS != 40 || eight.IntraPair.PS != 1 {
		t.Errorf("rk3588 8-layer: %+v", eight)
	}
	lp3, _ := PresetByID("rk3399-lpddr3")
	if lp3.DataToStrobe.PS != 5 || lp3.AddressToClock.PS != 5 || lp3.StrobeToClock.PS != 150 {
		t.Errorf("rk3399 lpddr3: %+v", lp3)
	}
}

func TestPresetByIDRefusesAnUnknownOne(t *testing.T) {
	if _, ok := PresetByID("rk9999"); ok {
		t.Error("an unknown preset was found")
	}
}

// The delay figures the NXP and TI tables give. A preset is only worth having
// if it is the guide's number, so the guide's number is what is asserted.
func TestDelayPresetsMatchTheirTables(t *testing.T) {
	for _, c := range []struct {
		id                            string
		data, pair, addr, strobeClock float64
	}{
		// i.MX 93 table 16, i.MX 8M Plus table 18, i.MX 8M Nano table 22.
		{"imx93-imx8mp-imx8mn-lpddr4", 50, 1, 50, 75},
		// i.MX 8M Nano table 25: tighter on address than the Quad's table 21.
		{"imx8mn-ddr4", 10, 1, 10, 833},
		// i.MX 8M Mini table 21, i.MX 8MDQLQ table 16.
		{"imx8m-lpddr4", 10, 1, 25, 85},
		// One clock period at 1600 and 2400 MT/s.
		{"imx8m-ddr3l", 10, 1, 25, 1250},
		{"imx8mq-ddr4", 10, 1, 25, 833},
		// TI tables 3-6 and 3-7, at LPDDR4-1600: three clock periods.
		{"am62x-lpddr4", 49, 0.75, 312.5, 3750},
		// TI's AM64x tables 3-6 and 3-7 give no strobe against clock limit.
		{"am64x-lpddr4", 2, 0.4, 3, 0},
	} {
		p, ok := PresetByID(c.id)
		if !ok {
			t.Fatalf("no preset %q", c.id)
		}
		got := [4]float64{p.DataToStrobe.PS, p.IntraPair.PS, p.AddressToClock.PS, p.StrobeToClock.PS}
		want := [4]float64{c.data, c.pair, c.addr, c.strobeClock}
		if got != want {
			t.Errorf("%s: %v ps, want %v ps", c.id, got, want)
		}
		if p.DataToStrobe.MM != 0 || p.AddressToClock.MM != 0 || p.StrobeToClock.MM != 0 {
			t.Errorf("%s: a delay preset also carries lengths: %+v", c.id, p)
		}
	}
}

// Every preset a user can pick has to be one this tool can express. Where a
// guide centers a window somewhere other than on the clock, it cannot, and the
// preset is left out rather than shipped as a rule that flags every net.
func TestEveryPresetHasANoteWhereItSimplifies(t *testing.T) {
	for _, p := range Presets() {
		if p.Vendor == "NXP" || p.Vendor == "Texas Instruments" {
			if p.Note == "" {
				t.Errorf("%s: no note saying what the table says and this does not", p.ID)
			}
		}
	}
}

// The lengths AN5122 gives for the STM32MP1 Series, in the millimetres it
// prints beside its own mils.
func TestSTM32MP1MatchesAN5122(t *testing.T) {
	p, ok := PresetByID("stm32mp1-ddr3l")
	if !ok {
		t.Fatal("no stm32mp1 preset")
	}
	for _, c := range []struct {
		name string
		got  float64
		want float64 // as AN5122 prints it
	}{
		{"data to strobe", p.DataToStrobe.MM, 1.016},
		{"address to clock", p.AddressToClock.MM, 1.016},
		{"strobe to clock", p.StrobeToClock.MM, 14.986},
		{"chip to chip", p.MaxChipDeltaMM, 33.02},
	} {
		if math.Abs(c.got-c.want) > 1e-9 {
			t.Errorf("%s: %.4f mm, want %.4f", c.name, c.got, c.want)
		}
	}
	// ST states no pair limit in either note.
	if !p.IntraPair.Zero() {
		t.Errorf("intra-pair is %s, but AN5122 gives none", p.IntraPair)
	}
	st, _ := PresetByID("st-stm32mp25-ddr4")
	if !st.IntraPair.Zero() {
		t.Errorf("stm32mp25 intra-pair is %s, but AN5724 gives none", st.IntraPair)
	}
}

// ST writes its limits in mils and prints a rounded millimetre beside each,
// and its two documents do not round alike: the length equalization sheet says
// 12,07 mm where AN5724 says 475 mils (12.06 mm). Neither rounding is used.
// The mil figure is, converted exactly, so that a millimetre on the page can
// be taken back to the document a reviewer is holding.
func TestSTM32MP25UsesTheMilFigureNotEitherRounding(t *testing.T) {
	p, ok := PresetByID("st-stm32mp25-ddr4")
	if !ok {
		t.Fatal("no stm32mp25 preset")
	}
	for _, c := range []struct {
		name string
		got  float64
		mils float64
	}{
		{"data to strobe", p.DataToStrobe.MM, 56},
		{"address to clock", p.AddressToClock.MM, 140},
		{"strobe to clock", p.StrobeToClock.MM, 475},
		{"chip to chip", p.MaxChipDeltaMM, 1378},
	} {
		if math.Abs(c.got-c.mils*MMPerMil) > 1e-12 {
			t.Errorf("%s: %.4f mm, want %g mil exactly (%.4f mm)", c.name, c.got, c.mils, c.mils*MMPerMil)
		}
	}
	// The one that started it: neither 12.06 nor 12.07.
	if p.StrobeToClock.MM != 12.065 {
		t.Errorf("strobe to clock %.4f mm, want 12.065", p.StrobeToClock.MM)
	}
	// The defaults move with it, or a board nobody has touched would stop
	// matching this preset in the form.
	if DefaultRules().StrobeToClock.MM != p.StrobeToClock.MM {
		t.Errorf("defaults %v, preset %v", DefaultRules().StrobeToClock, p.StrobeToClock)
	}
}

// Recognising the chip from the controller footprint's value. The cost of a
// wrong answer is higher than the cost of none -- a preset claiming the wrong
// vendor is worse than a board with no preset at all -- so these check the
// part numbers boards really carry, and that nothing matches across families.
func TestPresetsForPart(t *testing.T) {
	for _, c := range []struct {
		value string
		want  []string
	}{
		// The demo board, and the orderable codes each vendor ships.
		{"STM32MP257DAI3", []string{"st-stm32mp25-ddr4"}},
		{"STM32MP157AAC3", []string{"stm32mp1-ddr3l"}},
		{"RK3588", []string{"rk3588-lpddr5-hdi", "rk3588-lpddr4-hdi", "rk3588-lpddr4-8layer"}},
		{"RK3588S", []string{"rk3588-lpddr5-hdi", "rk3588-lpddr4-hdi", "rk3588-lpddr4-8layer"}},
		{"RK3566", []string{"rk3568-lpddr4", "rk3568-ddr4"}},
		{"RK3568B2", []string{"rk3568-lpddr4", "rk3568-ddr4"}},
		{"RK3399", []string{"rk3399-ddr3", "rk3399-lpddr3"}},
		{"MIMX8MN6CVTIZAA", []string{"imx93-imx8mp-imx8mn-lpddr4", "imx8mn-ddr4"}},
		{"MIMX8MM6DVTLZAA", []string{"imx8m-lpddr4", "imx8m-ddr3l"}},
		{"MIMX9352CVVXK", []string{"imx93-imx8mp-imx8mn-lpddr4"}},
		{"AM6254", []string{"am62x-lpddr4"}},
		{"AM6442", []string{"am64x-lpddr4"}},
		{"ATSAMA5D27C-CU", []string{"sama5d2-ddr3l"}},
		// Written the way a schematic often does, with punctuation.
		{"i.MX 8M Nano", []string{"imx93-imx8mp-imx8mn-lpddr4", "imx8mn-ddr4"}},
		{"i.MX8M-Plus", []string{"imx93-imx8mp-imx8mn-lpddr4"}},
		// Nothing to go on.
		{"", nil},
		{"U3", nil},
		{"BCM2711", nil},
		{"MT7621", nil},
	} {
		var got []string
		for _, p := range PresetsForPart(c.value) {
			got = append(got, p.ID)
		}
		if len(got) != len(c.want) {
			t.Errorf("%q: %v, want %v", c.value, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q: %v, want %v", c.value, got, c.want)
				break
			}
		}
	}
}

// Every preset can be recognised, or the warning would tell a user to pick
// something the board could never point at.
func TestEveryPresetIsRecognisableFromAPart(t *testing.T) {
	for _, p := range Presets() {
		if p.match == "" {
			t.Errorf("%s: no part to recognise it by", p.ID)
		}
	}
}
