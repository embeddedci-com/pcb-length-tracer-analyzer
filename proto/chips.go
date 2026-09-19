package proto

import (
	"regexp"
	"sort"
	"strings"

	"github.com/embeddedci-com/pcb-autorouter/board"
)

// The impedances a controller's own layout guide asks for.
//
// A family's typical figure is a rule of thumb, and for some parts it is the
// wrong one: ST draws STM32MP2 DDR4 to 55 ohms where DDR4 is usually 40, and a
// board built to ST's number would be flagged against the generic one. So
// where the controller is recognised, its guide's figure replaces the family's.
//
// Every figure is copied from the vendor's document, with the document,
// version and table beside it. A protocol the guide gives no figure for is left
// out, and the family's typical figure applies there, labelled as such. The
// guides all state a tolerance of plus or minus 10 percent.

// Ohms is an impedance as a guide states it: one figure, or a range where the
// guide gives one ("80 to 90 ohms"). Zero is no figure.
type Ohms struct {
	Nominal float64
	// Max is the top of a range, zero for a single figure.
	Max float64
}

// ChipTarget is one protocol's impedance on one chip.
type ChipTarget struct {
	Kind         Kind
	SingleEnded  Ohms
	Differential Ohms

	// Note is what the figures do not say: a signal group the guide holds
	// to a different figure, say.
	Note string

	// Source is the document, version and table.
	Source string
}

// Chip is a controller whose layout guide states impedances.
type Chip struct {
	// Name is the chip family as a person writes it.
	Name   string
	Vendor string

	// match recognises it from a footprint's value, upper cased with
	// everything but letters and digits removed, the same way the DDR
	// presets do.
	match *regexp.Regexp

	Targets []ChipTarget
}

// Target returns the chip's figures for one protocol.
func (c Chip) Target(k Kind) (ChipTarget, bool) {
	for _, t := range c.Targets {
		if t.Kind == k {
			return t, true
		}
	}
	return ChipTarget{}, false
}

const (
	srcAN5724  = "ST AN5724 Rev 4 (2025-12), section 6.3"
	srcAN5122  = "ST AN5122 Rev 3 (2019-02), section 5.3"
	srcRK3588  = "RK3588 Hardware Design Guide V1.0 (2022-01-06)"
	srcRK3568  = "RK3568 High Speed PCB Design Guide V1.0 (2021-04-12)"
	srcRK3399  = "RK3399 Design Guide V1.0 (2017-04-20)"
	srcIMX8MM  = "NXP i.MX 8M Mini Hardware Developer's Guide Rev. 1 (2019-08), table 32"
	srcIMX8MP  = "NXP i.MX 8M Plus Hardware Developer's Guide Rev. 1 (2024-03-26), table 22"
	srcIMX8MQ  = "NXP i.MX 8MDQLQ Hardware Developer's Guide Rev. 2 (2019-06), table 25"
	srcIMX93   = "NXP i.MX 93 Hardware Design Guide Rev. 1 (2023-04-24), table 22"
	srcAM62    = "TI SPRAD06C (2025-03), table 1-1"
	srcAM64    = "TI SPRACU1A (2021-06), table 1-1"
	srcSAMA5D2 = "Microchip AN2814 Rev. A (2018-10), section 5.1.2"
)

func se(n float64) Ohms { return Ohms{Nominal: n} }

// nxp is the shape every NXP guide's table takes: 50 ohms for every
// single-ended signal not listed otherwise, 100 differential for the pairs
// not listed otherwise, which includes Ethernet and MIPI.
func nxp(src string, ddrDiff float64, ddrNote string, extra ...ChipTarget) []ChipTarget {
	out := []ChipTarget{
		{Kind: DDR, SingleEnded: se(50), Differential: se(ddrDiff), Note: ddrNote, Source: src},
		{Kind: RGMII, SingleEnded: se(50), Source: src},
		{Kind: MIIRMII, SingleEnded: se(50), Source: src},
		{Kind: SDMMC, SingleEnded: se(50), Source: src},
		{Kind: USB2, Differential: se(90), Source: src},
		{Kind: MIPI, SingleEnded: se(50), Differential: se(100), Source: src},
		{Kind: DiffOnly, Differential: se(100), Note: "The guide's figure for differential signals it does not list by name, Ethernet among them", Source: src},
	}
	return append(out, extra...)
}

// Chips are the controllers with published impedance figures.
var Chips = []Chip{
	{
		Name: "STM32MP2", Vendor: "ST", match: regexp.MustCompile(`STM32MP2\d`),
		Targets: []ChipTarget{
			{Kind: DDR, SingleEnded: se(55), Differential: se(100), Note: "For DDR3L, DDR4 and LPDDR4", Source: srcAN5724},
		},
	},
	{
		Name: "STM32MP15", Vendor: "ST", match: regexp.MustCompile(`STM32MP15`),
		Targets: []ChipTarget{
			{Kind: DDR, SingleEnded: se(55), Differential: se(100), Note: "For DDR3, DDR3L, LPDDR2 and LPDDR3", Source: srcAN5122},
		},
	},
	{
		Name: "RK3588", Vendor: "Rockchip", match: regexp.MustCompile(`RK3588`),
		Targets: []ChipTarget{
			{Kind: DDR, SingleEnded: se(40), Differential: Ohms{80, 90},
				Note:   "Address and command may be 40 to 50 ohms on LPDDR5; CKE is 50 ohms on LPDDR4 and LPDDR4X",
				Source: srcRK3588 + ", tables 3-7 to 3-11"},
			{Kind: PCIe, Differential: se(85), Note: "The reference clock is 100 ohms", Source: srcRK3588 + ", tables 3-13 and 3-14"},
			{Kind: USB2, Differential: se(90), Source: srcRK3588 + ", table 3-18"},
		},
	},
	{
		Name: "RK3566/RK3568", Vendor: "Rockchip", match: regexp.MustCompile(`RK356[68]`),
		Targets: []ChipTarget{
			{Kind: DDR, SingleEnded: se(50), Differential: se(100),
				Note: "ECC control signals are 43 ohms in the region under the chip", Source: srcRK3568 + ", tables 20 to 34"},
			{Kind: USB2, Differential: se(90), Source: srcRK3568 + ", table 1"},
			{Kind: USBSS, Differential: se(90), Source: srcRK3568 + ", table 2"},
			{Kind: PCIe, Differential: se(85), Source: srcRK3568 + ", tables 5 and 6"},
			{Kind: MIPI, Differential: se(100), Source: srcRK3568 + ", table 10"},
			{Kind: SDMMC, SingleEnded: se(50), Source: srcRK3568 + ", tables 11 to 13"},
			{Kind: RGMII, SingleEnded: se(50), Source: srcRK3568 + ", table 19"},
		},
	},
	{
		Name: "RK3399", Vendor: "Rockchip", match: regexp.MustCompile(`RK3399`),
		Targets: []ChipTarget{
			{Kind: DDR, SingleEnded: se(50), Differential: se(100), Source: srcRK3399 + ", tables 4-1 to 4-8"},
			{Kind: SDMMC, SingleEnded: se(50), Source: srcRK3399 + ", tables 4-9 and 4-17"},
			{Kind: PCIe, Differential: se(100), Source: srcRK3399 + ", table 4-10"},
			{Kind: USB2, Differential: se(90), Source: srcRK3399 + ", table 4-11"},
			{Kind: USBSS, Differential: se(90), Source: srcRK3399 + ", table 4-12"},
			{Kind: MIPI, Differential: se(100), Source: srcRK3399 + ", table 4-16"},
		},
	},
	{
		Name: "i.MX 8M Mini", Vendor: "NXP", match: regexp.MustCompile(`IMX8MM`),
		Targets: nxp(srcIMX8MM, 85, "85 ohms is for the strobes and the clock",
			ChipTarget{Kind: PCIe, Differential: se(85), Note: "The reference clock is 85 ohms too", Source: srcIMX8MM}),
	},
	{
		Name: "i.MX 8M Plus", Vendor: "NXP", match: regexp.MustCompile(`IMX8MP`),
		Targets: nxp(srcIMX8MP, 85, "85 ohms is for the strobes and the clock",
			ChipTarget{Kind: PCIe, Differential: se(85), Note: "The reference clock is 100 ohms", Source: srcIMX8MP},
			ChipTarget{Kind: USBSS, Differential: se(90), Source: srcIMX8MP}),
	},
	{
		Name: "i.MX 8M Quad/Dual", Vendor: "NXP", match: regexp.MustCompile(`IMX8MQ|IMX8MD`),
		Targets: nxp(srcIMX8MQ, 85, "85 ohms is for the strobes and the clock. LPDDR4 DQ and DMI are 42 ohms",
			ChipTarget{Kind: PCIe, Differential: se(85), Note: "The reference clock is 100 ohms", Source: srcIMX8MQ},
			ChipTarget{Kind: USBSS, Differential: se(90), Source: srcIMX8MQ}),
	},
	{
		Name: "i.MX 93", Vendor: "NXP", match: regexp.MustCompile(`IMX93`),
		Targets: nxp(srcIMX93, 85, "85 ohms is for the strobes and the clock"),
	},
	{
		Name: "AM62x", Vendor: "TI", match: regexp.MustCompile(`AM62`),
		Targets: []ChipTarget{
			{Kind: DDR, SingleEnded: se(40), Differential: se(80), Source: srcAM62},
		},
	},
	{
		Name: "AM64x/AM243x", Vendor: "TI", match: regexp.MustCompile(`AM64|AM243`),
		Targets: []ChipTarget{
			{Kind: DDR, SingleEnded: se(40), Differential: se(80), Source: srcAM64},
		},
	},
	{
		Name: "SAMA5D2", Vendor: "Microchip", match: regexp.MustCompile(`SAMA5D2`),
		Targets: []ChipTarget{
			{Kind: DDR, SingleEnded: se(50), Differential: Ohms{90, 100},
				Note: "The note says 90 or 100 ohms differential, for the board as a whole", Source: srcSAMA5D2},
		},
	},
}

// ChipForPart returns the chip a footprint's value names.
func ChipForPart(value string) (Chip, bool) {
	v := normalizePart(value)
	if v == "" {
		return Chip{}, false
	}
	for _, c := range Chips {
		if c.match.MatchString(v) {
			return c, true
		}
	}
	return Chip{}, false
}

// ChipMatch is the chip an interface's figures come from, and why.
type ChipMatch struct {
	Chip Chip
	// Ref is the footprint it was recognised on.
	Ref string
	// Connected is true when that footprint has a pad on the interface's
	// nets. False means it is the only recognised chip on the board, and the
	// interface was taken to be its.
	Connected bool
}

// ChipFor finds the controller whose guide governs these nets: the recognised
// part with a pad on one of them. Where none has, and the board carries exactly
// one recognised chip, that one, since an interface reaches its SoC through a
// series resistor or a connector often enough. Two candidates and no way to
// choose is no answer: a wrong vendor figure is worse than the generic one.
func ChipFor(b *board.Board, nets []string) (ChipMatch, bool) {
	on := map[string]bool{}
	for _, n := range nets {
		on[n] = true
	}
	var connected, all []ChipMatch
	for _, fp := range b.Footprints {
		c, ok := ChipForPart(fp.Value)
		if !ok {
			continue
		}
		m := ChipMatch{Chip: c, Ref: fp.Ref}
		all = append(all, m)
		for _, p := range fp.Pads {
			if on[p.Net] {
				m.Connected = true
				connected = append(connected, m)
				break
			}
		}
	}
	if m, ok := oneChip(connected); ok {
		return m, true
	}
	if len(connected) > 0 {
		return ChipMatch{}, false
	}
	return oneChip(all)
}

// oneChip returns the match when every candidate is the same chip.
func oneChip(ms []ChipMatch) (ChipMatch, bool) {
	if len(ms) == 0 {
		return ChipMatch{}, false
	}
	sort.SliceStable(ms, func(i, j int) bool { return ms[i].Ref < ms[j].Ref })
	for _, m := range ms[1:] {
		if m.Chip.Name != ms[0].Chip.Name {
			return ChipMatch{}, false
		}
	}
	return ms[0], true
}

// normalizePart strips a part number down to what is comparable: "i.MX 8M
// Plus", "i.MX8M-Plus" and "IMX8MPLUS" are the same chip written three ways.
func normalizePart(value string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(value) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
