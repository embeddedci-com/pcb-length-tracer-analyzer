package ddr

import (
	"fmt"
	"math"
)

// Tolerance is an allowed mismatch, expressible as a length, a delay, or both.
//
// Both units are kept because both are used in practice. A vendor layout guide
// quotes millimetres or mils and that is what a reviewer measures in pcbnew; a
// timing budget is in picoseconds and that is what actually closes. Where both
// are set, the tighter one governs: it is the one the design has to satisfy.
type Tolerance struct {
	MM float64
	PS float64
}

// Zero reports whether no tolerance was given.
func (t Tolerance) Zero() bool { return t.MM <= 0 && t.PS <= 0 }

// LimitMM converts the tolerance to a length limit at the given propagation
// rate, taking the tighter of the two if both are set.
func (t Tolerance) LimitMM(psPerMM float64) float64 {
	lim := math.Inf(1)
	if t.MM > 0 {
		lim = t.MM
	}
	if t.PS > 0 && psPerMM > 0 {
		if v := t.PS / psPerMM; v < lim {
			lim = v
		}
	}
	return lim
}

func (t Tolerance) String() string {
	switch {
	case t.MM > 0 && t.PS > 0:
		return fmt.Sprintf("%.3f mm / %.1f ps", t.MM, t.PS)
	case t.MM > 0:
		return fmt.Sprintf("%.3f mm", t.MM)
	case t.PS > 0:
		return fmt.Sprintf("%.1f ps", t.PS)
	}
	return "unset"
}

// Rules are the matching requirements for a DDR interface.
//
// The defaults are the widely published starting point for DDR3/DDR4 at
// moderate data rates -- 25 mil within a byte lane, 5 mil within a
// differential pair, 50 mil from the address and command group to the clock.
// They are a starting point and nothing more: the number that matters is the
// one in the memory controller's own layout guide for the speed grade being
// built, and it tightens quickly above 1600 MT/s. Set them explicitly for real
// work.
type Rules struct {
	// DataToStrobe is how far a DQ or DM line may sit from its byte lane's
	// strobe.
	DataToStrobe Tolerance

	// IntraPair is how far the two halves of a differential pair may sit from
	// each other.
	IntraPair Tolerance

	// AddressToClock is how far an address or command line may sit from the
	// clock.
	AddressToClock Tolerance

	// ClockOffsetPercent makes the address and command group deliberately
	// longer (positive) or shorter (negative) than the clock, as a percentage
	// of the clock's own length.
	//
	// This is a real design knob rather than a fudge factor: in a fly-by
	// topology the controller trains out a fixed clock-to-strobe offset, and
	// some controllers ask for the command group to lead or lag the clock by a
	// set proportion. Zero means match the clock.
	ClockOffsetPercent float64

	// IncludeControl brings reset-like lines into the address and command
	// group. They are usually left out: RESET is asynchronous and matching it
	// buys nothing.
	IncludeControl bool

	// MaxIntraPairFix caps how much length a differential pair's skew
	// correction may add to one half alone. Beyond this the mismatch is a
	// routing problem, not a tuning one, and padding one leg of a pair that
	// far would spoil the coupling it exists to provide. Zero disables the cap.
	MaxIntraPairFix float64

	// GroupToleranceMM overrides the tolerance for one group, by its name
	// ("byte lane 0", "address/command U3->U4"), in millimetres.
	//
	// The family defaults are a starting point, and a controller's layout guide
	// often is not uniform: a lane routed on an inner layer next to a noisy
	// supply may be given less than the others, or a slower address leg more.
	// A group named here is held to this and nothing else; the picosecond form
	// of the family rule does not also apply to it.
	GroupToleranceMM map[string]float64

	// StrobeToClock is how far each byte lane's strobe pair may sit from the
	// clock pair that reaches the same device -- the mean of the strobe pair
	// against the clock's mean over every fly-by leg up to that device.
	//
	// Write levelling trains this out at start-up, which is why the band is
	// wide; it is not infinite, and a lane past it is one the controller
	// cannot align.
	StrobeToClock Tolerance

	// MaxChipDeltaMM bounds how far one device's byte lanes may sit from
	// another's: the mean lane length per device, compared between devices.
	// A fly-by board routes the far device's lanes longer to follow its clock;
	// this caps how much. Zero disables the check.
	MaxChipDeltaMM float64
}

// DefaultRules returns the starting-point rules described on Rules.
func DefaultRules() Rules {
	return Rules{
		DataToStrobe:    Tolerance{MM: 1.42},  // DQ/DM to their byte lane's DQS
		IntraPair:       Tolerance{MM: 0.127}, // 5 mil
		AddressToClock:  Tolerance{MM: 3.55},  // A/C to CLK, per fly-by leg
		StrobeToClock:   Tolerance{MM: 12.07}, // each lane's DQS to the CLK at its device (ST's sheet)
		MaxChipDeltaMM:  35,                   // one device's lanes against another's
		MaxIntraPairFix: 1.0,
	}
}

// Validate reports anything obviously wrong with the rules.
func (r Rules) Validate() error {
	if r.DataToStrobe.Zero() {
		return fmt.Errorf("ddr: data-to-strobe tolerance is unset")
	}
	if r.IntraPair.Zero() {
		return fmt.Errorf("ddr: intra-pair tolerance is unset")
	}
	if r.AddressToClock.Zero() {
		return fmt.Errorf("ddr: address-to-clock tolerance is unset")
	}
	if r.ClockOffsetPercent <= -100 {
		return fmt.Errorf("ddr: clock offset of %.1f%% would ask for a negative length", r.ClockOffsetPercent)
	}
	if math.Abs(r.ClockOffsetPercent) > 50 {
		return fmt.Errorf("ddr: clock offset of %.1f%% is implausible; expected a few percent", r.ClockOffsetPercent)
	}
	return nil
}
