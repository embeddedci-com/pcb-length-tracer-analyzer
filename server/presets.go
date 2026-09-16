package server

import (
	"fmt"

	"github.com/embeddedci-com/pcb-autorouter/ddr"
)

// The published rule sets, as parameters the form can apply.
//
// A preset is nothing but a set of values with a citation: choosing one fills
// the DDR limits in from the vendor's table, and from then on they are this
// board's parameters like any other. Nothing downstream knows a preset was
// used, so a board whose rules came from a preset and one whose rules were
// typed in behave identically.

// PresetInfo is a preset and the parameters it sets.
type PresetInfo struct {
	ddr.Preset
	Params PresetParams `json:"params"`

	// Unstated names the parameters whose value here is the tool's own
	// default because the guide sets no limit for them. Without it a page
	// showing the parameters would put a number the vendor never wrote
	// beside a citation of the vendor's document.
	Unstated []string `json:"unstated,omitempty"`
}

// PresetParams are the parameters a preset decides. The names match Params,
// so the form applies them field for field.
//
// Both units of every limit are given, one of them zero: a guide states a
// limit either as a length or as a delay, and sending both would apply the
// tighter of two numbers the guide never intended to combine.
//
// A limit the guide does not state carries the tool's default, and its name is
// in PresetInfo.Unstated.
type PresetParams struct {
	DataToStrobeMM   float64 `json:"data_to_strobe_mm"`
	DataToStrobePS   float64 `json:"data_to_strobe_ps"`
	IntraPairMM      float64 `json:"intra_pair_mm"`
	IntraPairPS      float64 `json:"intra_pair_ps"`
	AddressToClockMM float64 `json:"address_to_clock_mm"`
	AddressToClockPS float64 `json:"address_to_clock_ps"`
	StrobeToClockMM  float64 `json:"strobe_to_clock_mm"`
	StrobeToClockPS  float64 `json:"strobe_to_clock_ps"`

	// MaxChipDeltaMM is the tool's default where the guide says nothing about
	// one memory device against another, so the form shows the figure that
	// will really be in force rather than a zero.
	MaxChipDeltaMM float64 `json:"max_chip_delta_mm"`

	ClockOffsetPercent float64 `json:"clock_offset_percent"`
}

// KnownPresets is every preset, with the parameters each one sets.
func KnownPresets() []PresetInfo {
	d := DefaultParams()
	out := make([]PresetInfo, 0, len(ddr.Presets()))
	for _, p := range ddr.Presets() {
		v := PresetParams{
			DataToStrobeMM:   p.DataToStrobe.MM,
			DataToStrobePS:   p.DataToStrobe.PS,
			IntraPairMM:      p.IntraPair.MM,
			IntraPairPS:      p.IntraPair.PS,
			AddressToClockMM: p.AddressToClock.MM,
			AddressToClockPS: p.AddressToClock.PS,
			StrobeToClockMM:  p.StrobeToClock.MM,
			StrobeToClockPS:  p.StrobeToClock.PS,
			MaxChipDeltaMM:   p.MaxChipDeltaMM,

			ClockOffsetPercent: p.ClockOffsetPercent,
		}
		var unstated []string
		if p.DataToStrobe.Zero() {
			v.DataToStrobeMM, unstated = d.DataToStrobeMM, append(unstated, "data_to_strobe_mm")
		}
		if p.IntraPair.Zero() {
			v.IntraPairMM, unstated = d.IntraPairMM, append(unstated, "intra_pair_mm")
		}
		if p.AddressToClock.Zero() {
			v.AddressToClockMM, unstated = d.AddressToClockMM, append(unstated, "address_to_clock_mm")
		}
		if p.StrobeToClock.Zero() {
			v.StrobeToClockMM, unstated = d.StrobeToClockMM, append(unstated, "strobe_to_clock_mm")
		}
		if v.MaxChipDeltaMM <= 0 {
			v.MaxChipDeltaMM, unstated = d.MaxChipDeltaMM, append(unstated, "max_chip_delta_mm")
		}
		out = append(out, PresetInfo{Preset: p, Params: v, Unstated: unstated})
	}
	return out
}

// ApplyPreset returns the parameters with one preset's limits in force.
//
// Every limit it decides is set, including the ones it leaves at zero: a
// preset stated as a delay has to clear the lengths it replaces, or the board
// would be held to both.
func ApplyPreset(p Params, id string) (Params, error) {
	for _, info := range KnownPresets() {
		if info.ID != id {
			continue
		}
		v := info.Params
		p.DataToStrobeMM, p.DataToStrobePS = v.DataToStrobeMM, v.DataToStrobePS
		p.IntraPairMM, p.IntraPairPS = v.IntraPairMM, v.IntraPairPS
		p.AddressToClockMM, p.AddressToClockPS = v.AddressToClockMM, v.AddressToClockPS
		p.StrobeToClockMM, p.StrobeToClockPS = v.StrobeToClockMM, v.StrobeToClockPS
		p.MaxChipDeltaMM = v.MaxChipDeltaMM
		p.ClockOffsetPercent = v.ClockOffsetPercent
		return p, nil
	}
	return p, fmt.Errorf("no such preset: %q", id)
}

// PresetIDs names every preset, for a command line's help.
func PresetIDs() []string {
	out := make([]string, 0, len(ddr.Presets()))
	for _, p := range ddr.Presets() {
		out = append(out, p.ID)
	}
	return out
}
