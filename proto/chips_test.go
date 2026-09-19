package proto

import (
	"regexp"
	"testing"
)

func TestChipForPartReadsTheWaysBoardsLabelTheSoC(t *testing.T) {
	for value, want := range map[string]string{
		"STM32MP257DAI3":  "STM32MP2",
		"STM32MP157CAC3":  "STM32MP15",
		"RK3588S":         "RK3588",
		"RK3566":          "RK3566/RK3568",
		"MIMX8MM6CVTKZAA": "i.MX 8M Mini",
		"i.MX 8M Plus":    "i.MX 8M Plus",
		"MIMX8MQ6DVAJZAA": "i.MX 8M Quad/Dual",
		"MIMX9352CVVXMAB": "i.MX 93",
		"i.MX93":          "i.MX 93",
		"AM6254":          "AM62x",
		"SAMA5D27":        "SAMA5D2",
		// No guide with a figure was available for these, so the family's
		// usual figures stand rather than a sibling's.
		"MIMX8MN6CVTIZAA": "",
		"STM32MP135":      "",
		"":                "",
		"10k":             "",
	} {
		c, ok := ChipForPart(value)
		if ok != (want != "") || c.Name != want {
			t.Errorf("ChipForPart(%q) = %q, %v; want %q", value, c.Name, ok, want)
		}
	}
}

// Every figure has to be traceable to a document a reviewer can open, and has
// to be plausible for the protocol it is attached to.
func TestChipTargetsAreCitedAndPlausible(t *testing.T) {
	version := regexp.MustCompile(`V\d|Rev\.? ?[0-9A-Z]|\(\d{4}-\d{2}`)
	for _, c := range Chips {
		if len(c.Targets) == 0 {
			t.Errorf("%s has no targets", c.Name)
		}
		seen := map[Kind]bool{}
		for _, x := range c.Targets {
			if seen[x.Kind] {
				t.Errorf("%s lists %s twice", c.Name, x.Kind)
			}
			seen[x.Kind] = true
			if _, ok := FamilyOf(x.Kind); !ok {
				t.Errorf("%s: unknown kind %q", c.Name, x.Kind)
			}
			if !version.MatchString(x.Source) {
				t.Errorf("%s %s: source %q has no version", c.Name, x.Kind, x.Source)
			}
			if x.SingleEnded.Nominal == 0 && x.Differential.Nominal == 0 {
				t.Errorf("%s %s: no figure", c.Name, x.Kind)
			}
			for _, o := range []Ohms{x.SingleEnded, x.Differential} {
				if o.Max != 0 && o.Max <= o.Nominal {
					t.Errorf("%s %s: range %v is backwards", c.Name, x.Kind, o)
				}
			}
			if o := x.SingleEnded.Nominal; o != 0 && (o < 30 || o > 60) {
				t.Errorf("%s %s: %v ohms single-ended is implausible", c.Name, x.Kind, o)
			}
			if o := x.Differential.Nominal; o != 0 && (o < 75 || o > 110) {
				t.Errorf("%s %s: %v ohms differential is implausible", c.Name, x.Kind, o)
			}
		}
	}
}

// The STM32MP2 is why this exists: ST asks for 55 and 100 ohms where DDR4 is
// usually drawn to 40 and 80, and the demo board carries that chip.
func TestTheDemoBoardsDDRIsHeldToST(t *testing.T) {
	b := demo(t)
	var ddrIface *Interface
	for _, i := range Detect(b) {
		if i.Kind == DDR {
			ddrIface = i
		}
	}
	if ddrIface == nil {
		t.Fatal("no DDR interface on the demo board")
	}
	z := ddrIface.CheckImpedance(b, ddrIface.MeasureGeometry(b))
	if z.Chip == nil || z.Chip.Chip.Name != "STM32MP2" || !z.Chip.Connected {
		t.Fatalf("chip %+v, want the STM32MP2 connected to the DDR nets", z.Chip)
	}
	if z.TargetSingleEnded != 55 || z.TargetDifferential != 100 {
		t.Errorf("targets %v/%v, want ST's 55/100", z.TargetSingleEnded, z.TargetDifferential)
	}
	if z.TargetSource == "" {
		t.Error("no source for ST's figures")
	}
}

// Where the chip's guide is silent on a protocol, the family's figure stands
// and the chip is still named, so the page can say whose guide was silent.
func TestAProtocolTheGuideIsSilentOnKeepsTheFamilysFigure(t *testing.T) {
	b := demo(t)
	for _, i := range Detect(b) {
		if i.Kind != USB2 {
			continue
		}
		z := i.CheckImpedance(b, i.MeasureGeometry(b))
		if z.TargetSource != "" {
			t.Errorf("%s: source %q, but AN5724 gives no USB figure", i.Name, z.TargetSource)
		}
		if z.TargetDifferential != 90 {
			t.Errorf("%s: %v ohms, want the family's 90", i.Name, z.TargetDifferential)
		}
		return
	}
	t.Skip("no USB 2.0 interface on the demo board")
}
