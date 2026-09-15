// Package pkglen supplies the length of copper inside a package: from a
// ball to the die.
//
// DDR limits are on the whole path, package plus board. ST's length
// equalization sheets add a per-ball package length to the PCB length before
// comparing anything, and that length differs by several millimetres between
// balls of the same byte lane -- more than the lane's whole tolerance. A
// check on board copper alone matches the wrong thing.
//
// KiCad carries this as a pad's die length, and its length tuner counts it.
// Few footprints set it. So where a footprint has none, a known part's table
// fills it in, keyed by ball name -- the schematic pin function without its
// ball number, "DDR_A5" of "DDR_A5_M19" -- or, where the pin function does not
// say, by ball number: a package has the same balls on every board.
package pkglen

import (
	"regexp"
	"sort"
	"strings"

	"github.com/embeddedci-com/pcb-autorouter/board"
)

// Table is the package lengths of one part.
type Table struct {
	// Part is the name as the vendor's sheet gives it.
	Part string
	// Source says where the numbers come from.
	Source string
	// matches reports whether a footprint value is this part.
	matches *regexp.Regexp
	// Balls maps a ball name to its package length in mm.
	Balls map[string]float64
	// Numbers maps a ball number, the pad name, to its ball name. It is
	// the fallback for a footprint whose pads carry no pin function, or
	// name it some other way.
	Numbers map[string]string
}

// Applied is what Apply did to one footprint.
type Applied struct {
	Ref    string `json:"ref"`
	Part   string `json:"part"`
	Source string `json:"source"`
	// Pads is how many pads were given a length from the table.
	Pads int `json:"pads"`
	// FromBoard is how many pads already had a die length in the footprint,
	// which is kept.
	FromBoard int `json:"from_board"`
}

var tables = []*Table{stm32mp25AI}

var ballSuffix = regexp.MustCompile(`_[A-Z]{1,2}[0-9]{1,2}$`)

// Ball drops the ball number from a pin function: "DDR_A5_M19" is "DDR_A5".
// A function without one is looked up as it is before this is tried.
func Ball(function string) string {
	return ballSuffix.ReplaceAllString(strings.ToUpper(function), "")
}

// Apply sets the die length of every pad of a known part that has none, and
// reports what it did, one entry per footprint touched or already carrying
// die lengths. Pads with a die length of their own are left alone: the
// footprint is the designer's statement and wins over a table. Running it
// again on the same board changes nothing.
func Apply(b *board.Board) []Applied {
	var out []Applied
	for _, fp := range b.Footprints {
		a := fill(fp, lookup(fp.Value, fp.Library))
		if a.Pads == 0 && a.FromBoard == 0 {
			continue
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out
}

// UsePart applies the named part's table to footprint ref whatever its value
// says, for a footprint Apply did not recognise. "none", or any name without
// a table, removes table lengths from it instead; the footprint's own die
// lengths stay either way.
func UsePart(b *board.Board, ref, part string) Applied {
	fp := b.Footprint(ref)
	if fp == nil {
		return Applied{Ref: ref}
	}
	for _, p := range fp.Pads {
		if p.DieSource != "footprint" && p.DieSource != "" {
			p.DieLength, p.DieSource = 0, ""
		}
	}
	var t *Table
	for _, x := range tables {
		if x.Part == part {
			t = x
		}
	}
	return fill(fp, t)
}

// Parts is the name of every part with a table.
func Parts() []string {
	out := make([]string, 0, len(tables))
	for _, t := range tables {
		out = append(out, t.Part)
	}
	return out
}

func fill(fp *board.Footprint, t *Table) Applied {
	a := Applied{Ref: fp.Ref}
	for _, p := range fp.Pads {
		if p.DieSource == "footprint" {
			a.FromBoard++
			continue
		}
		if t == nil {
			continue
		}
		if l, ok := t.length(p); ok {
			p.DieLength, p.DieSource = l, t.Part
			a.Pads++
		}
	}
	if a.Pads > 0 {
		a.Part, a.Source = t.Part, t.Source
	}
	return a
}

// length finds a pad's package length: by pin function, then by the ball
// name inside it, then by ball number.
func (t *Table) length(p *board.Pad) (float64, bool) {
	if p.Function != "" {
		for _, k := range []string{strings.ToUpper(p.Function), Ball(p.Function), squash(Ball(p.Function))} {
			if l, ok := t.Balls[k]; ok {
				return l, true
			}
		}
	}
	if name, ok := t.Numbers[strings.ToUpper(p.Name)]; ok {
		return t.Balls[name], true
	}
	return 0, false
}

// squash writes a strobe or clock half the way ST's sheet does: "DDR_DQS0_P"
// is "DDR_DQS0P".
func squash(ball string) string {
	if n := len(ball); n > 2 && ball[n-2] == '_' && (ball[n-1] == 'P' || ball[n-1] == 'N') {
		return ball[:n-2] + ball[n-1:]
	}
	return ball
}

func lookup(value, library string) *Table {
	for _, t := range tables {
		if t.matches.MatchString(strings.ToUpper(value)) || t.matches.MatchString(strings.ToUpper(library)) {
			return t
		}
	}
	return nil
}

// stm32mp25AI is the STM32MP25xxAI (TFBGA, 18x18 mm): ST's "2DDR4 memory
// length equalization in mm" sheet, column "STM32MP25xxAI LENGTH (mm)". The
// same numbers appear in the blank template and in ST's filled-in example.
var stm32mp25AI = &Table{
	Part:   "STM32MP25xxAI",
	Source: "ST DDR4 length equalization sheet for STM32MP25xxAI (18x18)",
	// STM32MP257FAI3, STM32MP255DAI3, STM32MP25xxAI.
	matches: regexp.MustCompile(`STM32MP25[0-9X][A-Z0-9X]AI`),
	Balls: map[string]float64{
		"DDR_A0":    2.166,
		"DDR_A1":    6.966,
		"DDR_A2":    3.355,
		"DDR_A3":    3.353,
		"DDR_A4":    6.886,
		"DDR_A5":    7.693,
		"DDR_A6":    7.302,
		"DDR_A7":    8.026,
		"DDR_A8":    5.945,
		"DDR_A9":    5.905,
		"DDR_A10":   5.194,
		"DDR_A11":   4.688,
		"DDR_A12":   6.083,
		"DDR_A13":   5.926,
		"DDR_A14":   4.07,
		"DDR_A15":   3.229,
		"DDR_A16":   11.463,
		"DDR_A17":   10.864,
		"DDR_A18":   7.966,
		"DDR_A19":   4.262,
		"DDR_A20":   7.975,
		"DDR_A21":   6.599,
		"DDR_A22":   5.66,
		"DDR_A23":   4.886,
		"DDR_A25":   6.512,
		"DDR_A26":   8.536,
		"DDR_A27":   9.271,
		"DDR_A28":   6.308,
		"DDR_A29":   10.403,
		"DDR_A30":   6.702,
		"DDR_A31":   5.927,
		"DDR_DQ0":   9.155,
		"DDR_DQ1":   8.851,
		"DDR_DQ2":   9.715,
		"DDR_DQ3":   9.924,
		"DDR_DQ4":   8.137,
		"DDR_DQ5":   6.654,
		"DDR_DQ6":   7.881,
		"DDR_DQ7":   7.04,
		"DDR_DQ8":   9.391,
		"DDR_DQ9":   9.659,
		"DDR_DQ10":  9.547,
		"DDR_DQ11":  10.288,
		"DDR_DQ12":  8.699,
		"DDR_DQ13":  6.692,
		"DDR_DQ14":  7.574,
		"DDR_DQ15":  8.592,
		"DDR_DQ16":  9.317,
		"DDR_DQ17":  11.236,
		"DDR_DQ18":  8.545,
		"DDR_DQ19":  10.03,
		"DDR_DQ20":  6.837,
		"DDR_DQ21":  6.19,
		"DDR_DQ22":  6.613,
		"DDR_DQ23":  7.258,
		"DDR_DQ24":  5.59,
		"DDR_DQ25":  8.082,
		"DDR_DQ26":  7.05,
		"DDR_DQ27":  6.413,
		"DDR_DQ28":  6.631,
		"DDR_DQ29":  6.045,
		"DDR_DQ30":  6.002,
		"DDR_DQ31":  6.727,
		"DDR_DQM0":  8.652,
		"DDR_DQM1":  7.119,
		"DDR_DQM2":  8.011,
		"DDR_DQM3":  5.834,
		"DDR_DQS0N": 7.866,
		"DDR_DQS0P": 7.808,
		"DDR_DQS1N": 8.587,
		"DDR_DQS1P": 8.666,
		"DDR_DQS2N": 9.702,
		"DDR_DQS2P": 9.948,
		"DDR_DQS3N": 7.811,
		"DDR_DQS3P": 8.159,
	},
	Numbers: map[string]string{
		"AA22": "DDR_DQ3",
		"B22":  "DDR_DQ19",
		"C21":  "DDR_DQ18",
		"C22":  "DDR_DQ17",
		"D20":  "DDR_DQ16",
		"D21":  "DDR_DQS2P",
		"D22":  "DDR_DQS2N",
		"E19":  "DDR_DQ22",
		"E20":  "DDR_DQ23",
		"E21":  "DDR_DQM2",
		"F19":  "DDR_DQ21",
		"F20":  "DDR_DQ20",
		"G18":  "DDR_A20",
		"G19":  "DDR_DQ24",
		"G20":  "DDR_DQ27",
		"G21":  "DDR_DQ26",
		"G22":  "DDR_DQ25",
		"H17":  "DDR_A22",
		"H18":  "DDR_A17",
		"H19":  "DDR_A18",
		"H20":  "DDR_DQM3",
		"H21":  "DDR_DQS3N",
		"H22":  "DDR_DQS3P",
		"J17":  "DDR_A23",
		"J18":  "DDR_A16",
		"J19":  "DDR_A21",
		"J20":  "DDR_DQ29",
		"J21":  "DDR_DQ28",
		"K17":  "DDR_A19",
		"K18":  "DDR_A13",
		"K19":  "DDR_A12",
		"K20":  "DDR_DQ30",
		"L17":  "DDR_A15",
		"L18":  "DDR_A14",
		"L19":  "DDR_DQ31",
		"L21":  "DDR_DQ10",
		"L22":  "DDR_DQ11",
		"M16":  "DDR_A0",
		"M17":  "DDR_A3",
		"M18":  "DDR_A28",
		"M19":  "DDR_A5",
		"M21":  "DDR_DQ8",
		"M22":  "DDR_DQ9",
		"N16":  "DDR_A2",
		"N17":  "DDR_A11",
		"N18":  "DDR_A10",
		"N19":  "DDR_A4",
		"N20":  "DDR_DQS1P",
		"N21":  "DDR_DQS1N",
		"P16":  "DDR_A31",
		"P17":  "DDR_A30",
		"P18":  "DDR_A9",
		"P19":  "DDR_A8",
		"P20":  "DDR_DQM1",
		"R17":  "DDR_A25",
		"R18":  "DDR_A27",
		"R19":  "DDR_A26",
		"R20":  "DDR_DQ13",
		"R21":  "DDR_DQ14",
		"R22":  "DDR_DQ15",
		"T18":  "DDR_A6",
		"T19":  "DDR_A29",
		"T20":  "DDR_DQ5",
		"T21":  "DDR_DQ4",
		"T22":  "DDR_DQ12",
		"U18":  "DDR_A1",
		"U19":  "DDR_A7",
		"U20":  "DDR_DQ7",
		"U21":  "DDR_DQ6",
		"V20":  "DDR_DQS0P",
		"W20":  "DDR_DQS0N",
		"W21":  "DDR_DQM0",
		"W22":  "DDR_DQ0",
		"Y21":  "DDR_DQ1",
		"Y22":  "DDR_DQ2",
	},
}

// PadLength is the package length on one pad, as the analysis used it.
type PadLength struct {
	Net string `json:"net"`
	// Pad is the pad's board-unique name, "U3.M19"; Ball its ball name
	// from the pin function, "DDR_A5", empty when the pad has none.
	Pad  string `json:"pad"`
	Ball string `json:"ball,omitempty"`
	// DefaultMM is the length from the footprint or the part's table, and
	// MM the one in force: the same unless it was overridden.
	DefaultMM float64 `json:"default_mm"`
	MM        float64 `json:"mm"`
	// Source is "footprint", the table's part name, "override", or empty
	// when nothing gave this pad a length.
	Source string `json:"source,omitempty"`
}

// Override sets the package length of footprint ref's pad on each net in
// byNet, and returns every pad of ref on nets, overridden or not, sorted by
// net. Call it after Apply, so the defaults it reports are the ones Apply
// set. A net with no pad on ref is ignored.
func Override(b *board.Board, ref string, nets []string, byNet map[string]float64) []PadLength {
	fp := b.Footprint(ref)
	if fp == nil {
		return nil
	}
	want := make(map[string]bool, len(nets))
	for _, n := range nets {
		want[n] = true
	}
	var out []PadLength
	for _, p := range fp.Pads {
		if !want[p.Net] {
			continue
		}
		row := PadLength{Net: p.Net, Pad: p.ID(), DefaultMM: p.DieLength, Source: p.DieSource}
		if p.Function != "" {
			row.Ball = Ball(p.Function)
		}
		if mm, ok := byNet[p.Net]; ok {
			p.DieLength, p.DieSource = mm, "override"
			row.Source = "override"
		}
		row.MM = p.DieLength
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Net < out[j].Net })
	return out
}
