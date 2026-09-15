package server

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/ddr"
	"github.com/embeddedci-com/pcb-autorouter/drc"
	"github.com/embeddedci-com/pcb-autorouter/geom"
	"github.com/embeddedci-com/pcb-autorouter/netlen"
	"github.com/embeddedci-com/pcb-autorouter/pkglen"
	"github.com/embeddedci-com/pcb-autorouter/preview"
	"github.com/embeddedci-com/pcb-autorouter/proto"
	"github.com/embeddedci-com/pcb-autorouter/tune"
)

// Params are the matching parameters a user can set. They are the same knobs
// the command line takes, named the way the form asks for them.
type Params struct {
	// NetPrefix restricts which nets are considered, e.g. "/ddr4/". Empty
	// means the server guesses from the board.
	NetPrefix string `json:"net_prefix"`

	// Controller is the reference of the memory controller. Empty means infer.
	Controller string `json:"controller"`

	// ClockOffsetPercent makes the address and command group deliberately
	// longer than the clock, as a percentage of the clock's own length. This
	// is the "percentage of offset between clock and traces" knob.
	ClockOffsetPercent float64 `json:"clock_offset_percent"`

	// Tolerances, in millimetres. Zero falls back to the defaults.
	DataToStrobeMM   float64 `json:"data_to_strobe_mm"`
	IntraPairMM      float64 `json:"intra_pair_mm"`
	AddressToClockMM float64 `json:"address_to_clock_mm"`

	// StrobeToClockMM bounds each byte lane's strobe against the clock at its
	// device, and MaxChipDeltaMM one device's lanes against another's. Checks
	// across groups rather than tolerances inside one. Zero takes the default,
	// like every other tolerance here.
	StrobeToClockMM float64 `json:"strobe_to_clock_mm"`
	MaxChipDeltaMM  float64 `json:"max_chip_delta_mm"`

	// Tolerances in picoseconds. Where both units are given for a rule, the
	// tighter one governs.
	DataToStrobePS   float64 `json:"data_to_strobe_ps"`
	IntraPairPS      float64 `json:"intra_pair_ps"`
	AddressToClockPS float64 `json:"address_to_clock_ps"`

	// IncludeControl brings reset-like lines into the address group.
	IncludeControl bool `json:"include_control"`

	// PackageLengthsMM overrides the length inside the controller's package
	// for a net, in mm, keyed by the full net name. A net not listed keeps the
	// length from its footprint or the part's table.
	PackageLengthsMM map[string]float64 `json:"package_lengths_mm,omitempty"`

	// PackagePart picks the package length table for the controller by
	// hand: a part name from pkglen.Parts, or "none". Empty recognises the
	// part from the footprint.
	PackagePart string `json:"package_part,omitempty"`

	// MaxIntraPairFixMM caps how much length may be padded into one half of a
	// differential pair before it is called a routing problem instead.
	MaxIntraPairFixMM float64 `json:"max_intra_pair_fix_mm"`

	// GroupToleranceMM holds a group to its own tolerance, in millimetres,
	// instead of its family's. Keyed by the group's name as the report shows
	// it: "byte lane 0" or "address/command U3->U4" for DDR, and
	// "<interface> / <group>" for everything else, e.g.
	// "Ethernet RGMII (ETH1) / transmit".
	GroupToleranceMM map[string]float64 `json:"group_tolerance_mm,omitempty"`

	// MeanderAreas are the regions the user has allowed copper to be added in,
	// in the viewer's coordinates: origin at the bottom-left of the board,
	// Y up, which is what the preview draws and what the user drew on.
	//
	// Empty means anywhere, which is right for a first look. Once areas are
	// given, nothing is added outside them -- not a meander, not a bus being
	// spread. Deciding how much length a net needs is arithmetic; deciding
	// where to put it is a judgement about the board, and this is where the
	// person who drew it makes it.
	MeanderAreas []Area `json:"meander_areas,omitempty"`

	// Interfaces is what the user has said about the interfaces on the board:
	// which family each one really is, which net is its reference, and the
	// width and pair spacing where nothing is routed for the tool to measure.
	//
	// Detection is a reading of the net names, and the user is the authority on
	// their own board.
	Interfaces []InterfaceOverride `json:"interfaces,omitempty"`

	// OpenClearanceMM is the clearance to hold to away from the components,
	// overriding whatever the board's own rules say there.
	//
	// A fine-pitch BGA forces a tight clearance onto the whole board because
	// that is what makes the escape possible. Between the components there is
	// usually far more room, and using it means less coupling and more space
	// to meander into. Zero holds to the board's rules everywhere; a
	// differential pair is exempt either way.
	OpenClearanceMM float64 `json:"open_clearance_mm"`

	// OpenMarginMM is how far outside a component's own pads still counts as
	// being around it.
	OpenMarginMM float64 `json:"open_margin_mm"`

	// Meander shape.
	MaxAmplitudeMM  float64 `json:"max_amplitude_mm"`
	MinAmplitudeMM  float64 `json:"min_amplitude_mm"`
	MeanderGapW     float64 `json:"meander_gap_widths"`
	MeanderChamferW float64 `json:"meander_chamfer_widths"`
	MinRunMM        float64 `json:"min_run_mm"`
	PadKeepoutMM    float64 `json:"pad_keepout_mm"`
}

// DefaultParams are the starting point a freshly uploaded board is analysed
// with, so the first report a user sees is a sensible one.
func DefaultParams() Params {
	r := ddr.DefaultRules()
	st := tune.DefaultStyle()
	return Params{
		DataToStrobeMM:    r.DataToStrobe.MM,
		IntraPairMM:       r.IntraPair.MM,
		AddressToClockMM:  r.AddressToClock.MM,
		StrobeToClockMM:   r.StrobeToClock.MM,
		MaxChipDeltaMM:    r.MaxChipDeltaMM,
		MaxIntraPairFixMM: r.MaxIntraPairFix,
		MaxAmplitudeMM:    st.MaxAmplitude,
		MinAmplitudeMM:    st.MinAmplitude,
		MeanderGapW:       st.Gap,
		MeanderChamferW:   st.Chamfer,
		MinRunMM:          st.MinRunLength,
		PadKeepoutMM:      st.KeepClearOfPads,
		OpenClearanceMM:   0.2,
		OpenMarginMM:      1.0,
	}
}

// withDefaults fills in anything the caller left at zero, so a form that
// posts only the fields it cares about still gets sensible rules.
func (p Params) withDefaults() Params {
	d := DefaultParams()
	if p.DataToStrobeMM <= 0 && p.DataToStrobePS <= 0 {
		p.DataToStrobeMM = d.DataToStrobeMM
	}
	if p.IntraPairMM <= 0 && p.IntraPairPS <= 0 {
		p.IntraPairMM = d.IntraPairMM
	}
	if p.AddressToClockMM <= 0 && p.AddressToClockPS <= 0 {
		p.AddressToClockMM = d.AddressToClockMM
	}
	for _, f := range []struct {
		v *float64
		d float64
	}{
		{&p.MaxIntraPairFixMM, d.MaxIntraPairFixMM},
		{&p.StrobeToClockMM, d.StrobeToClockMM},
		{&p.MaxChipDeltaMM, d.MaxChipDeltaMM},
		{&p.MaxAmplitudeMM, d.MaxAmplitudeMM},
		{&p.MinAmplitudeMM, d.MinAmplitudeMM},
		{&p.MeanderGapW, d.MeanderGapW},
		{&p.MeanderChamferW, d.MeanderChamferW},
		{&p.MinRunMM, d.MinRunMM},
		{&p.PadKeepoutMM, d.PadKeepoutMM},
		// Not OpenClearanceMM: zero is a choice there, meaning "use the
		// board's own rules", so it must survive withDefaults.
		{&p.OpenMarginMM, d.OpenMarginMM},
	} {
		if *f.v <= 0 {
			*f.v = f.d
		}
	}
	return p
}

// Area is a rectangle on the board, in the viewer's coordinates.
type Area struct {
	MinX float64 `json:"min_x"`
	MinY float64 `json:"min_y"`
	MaxX float64 `json:"max_x"`
	MaxY float64 `json:"max_y"`

	// MinClearanceMM is the least space between different nets' copper inside
	// this area. Zero leaves the board's own rules in force.
	//
	// It lives on the area because that is where the user knows it. "Keep
	// 0.25 mm apart away from the components" needs the tool to work out what
	// "away from the components" means, and it guesses with a margin around
	// every courtyard; "keep 0.25 mm apart in here" needs nothing guessed,
	// because the user drew the here.
	MinClearanceMM float64 `json:"min_clearance_mm,omitempty"`

	// Label is what the user called it, for the report.
	Label string `json:"label,omitempty"`
}

// Empty reports whether an area has no extent, which is what a stray click
// produces and what must not be taken for a restriction.
func (a Area) Empty() bool { return a.MaxX-a.MinX < 1e-6 || a.MaxY-a.MinY < 1e-6 }

// areasFor converts the user's areas into the board's own coordinates.
//
// The conversion is the preview's, in reverse, and it happens here alone. The
// front end draws on a picture whose origin is the bottom-left of the board
// with Y upward; the board file counts from the top of the page downward. Two
// implementations of that flip is one too many, and the wrong one is silent: it
// mirrors the region, which still looks like a rectangle.
func areasFor(b *board.Board, in []Area) (tune.Areas, error) {
	var out tune.Areas
	if len(in) == 0 {
		return nil, nil
	}
	tf, ok := preview.ExtentOf(b)
	if !ok {
		return nil, fmt.Errorf("this board has no extent to place an area on")
	}
	for _, a := range in {
		if a.Empty() {
			continue
		}
		out = append(out, tune.Area{
			Rect: tf.RectToKiCad(geom.Rect{
				MinX: math.Min(a.MinX, a.MaxX), MinY: math.Min(a.MinY, a.MaxY),
				MaxX: math.Max(a.MinX, a.MaxX), MaxY: math.Max(a.MinY, a.MaxY),
			}),
			MinClearance: a.MinClearanceMM,
			Label:        a.Label,
		})
	}
	return out, nil
}

// Validate rejects parameters that would produce nonsense rather than letting
// the engine fail deeper down where the message would mean less.
func (p Params) Validate() error {
	if p.ClockOffsetPercent <= -100 {
		return fmt.Errorf("clock offset of %.1f%% would ask for a negative length", p.ClockOffsetPercent)
	}
	if math.Abs(p.ClockOffsetPercent) > 50 {
		return fmt.Errorf("clock offset of %.1f%% is implausible; a few percent is the usual range", p.ClockOffsetPercent)
	}
	if p.MeanderGapW < 1 {
		return fmt.Errorf("meander gap of %.2f track widths would couple the legs to each other; 3 is the usual guidance", p.MeanderGapW)
	}
	if p.OpenClearanceMM < 0 {
		return fmt.Errorf("open-field clearance of %.3f mm is negative", p.OpenClearanceMM)
	}
	if p.OpenClearanceMM > 5 {
		return fmt.Errorf("open-field clearance of %.2f mm is implausible for signal routing", p.OpenClearanceMM)
	}
	for name, mm := range p.GroupToleranceMM {
		if mm <= 0 || mm > 50 {
			return fmt.Errorf("tolerance of %.3f mm for %s is outside 0 to 50 mm", mm, name)
		}
	}
	if p.PackagePart != "" && p.PackagePart != "none" && !slices.Contains(pkglen.Parts(), p.PackagePart) {
		return fmt.Errorf("no package length table for %q", p.PackagePart)
	}
	for net, mm := range p.PackageLengthsMM {
		if mm < 0 || mm > 50 {
			return fmt.Errorf("package length of %.3f mm for %s is outside 0 to 50 mm", mm, net)
		}
	}
	for i, a := range p.MeanderAreas {
		if a.Empty() {
			return fmt.Errorf("area %d has no extent; drag out a region rather than clicking", i+1)
		}
	}
	if p.MaxAmplitudeMM < p.MinAmplitudeMM {
		return fmt.Errorf("maximum meander amplitude %.3f mm is below the minimum %.3f mm", p.MaxAmplitudeMM, p.MinAmplitudeMM)
	}
	return nil
}

func (p Params) rules() ddr.Rules {
	return ddr.Rules{
		DataToStrobe:       ddr.Tolerance{MM: p.DataToStrobeMM, PS: p.DataToStrobePS},
		IntraPair:          ddr.Tolerance{MM: p.IntraPairMM, PS: p.IntraPairPS},
		AddressToClock:     ddr.Tolerance{MM: p.AddressToClockMM, PS: p.AddressToClockPS},
		ClockOffsetPercent: p.ClockOffsetPercent,
		IncludeControl:     p.IncludeControl,
		MaxIntraPairFix:    p.MaxIntraPairFixMM,
		GroupToleranceMM:   p.GroupToleranceMM,
		StrobeToClock:      ddr.Tolerance{MM: p.StrobeToClockMM},
		MaxChipDeltaMM:     p.MaxChipDeltaMM,
	}
}

func (p Params) style() tune.Style {
	return tune.Style{
		MaxAmplitude:    p.MaxAmplitudeMM,
		MinAmplitude:    p.MinAmplitudeMM,
		Chamfer:         p.MeanderChamferW,
		Gap:             p.MeanderGapW,
		MinRunLength:    p.MinRunMM,
		KeepClearOfPads: p.PadKeepoutMM,
	}
}

// ---- report shapes ----

// BoardInfo is what the board is.
type BoardInfo struct {
	Filename     string   `json:"filename"`
	CopperLayers []string `json:"copper_layers"`
	StackupMM    float64  `json:"stackup_mm"`
	Footprints   int      `json:"footprints"`
	Pads         int      `json:"pads"`
	Tracks       int      `json:"tracks"`
	Vias         int      `json:"vias"`
	NetClasses   []string `json:"net_classes"`
	HasCustomDRU bool     `json:"has_custom_dru"`

	// CustomRules says what became of that .kicad_dru: how many of its rules
	// are applied, and which were left alone because they use something this
	// does not implement. A rule left alone is not ignored quietly -- the net
	// classes at their strictest stand in for it, which can only be stricter
	// than the board asks.
	CustomRules   *CustomRulesInfo `json:"custom_rules,omitempty"`
	ViaLengthUsed bool             `json:"via_length_counted"`

	// HasProjectFile is false when no .kicad_pro was supplied, in which case
	// every clearance resolved to the board minimum rather than to the net
	// class the designer set. The UI says so, because it changes what a clean
	// clearance check means.
	HasProjectFile bool `json:"has_project_file"`
}

// InterfaceInfo is what the DDR interface is.
type InterfaceInfo struct {
	NetPrefix  string `json:"net_prefix"`
	Controller string `json:"controller"`
	// ControllerValue is the controller footprint's value, the part number
	// a package length table is recognised by.
	ControllerValue string   `json:"controller_value,omitempty"`
	Devices         []string `json:"devices"`
	WidthBits       int      `json:"width_bits"`
	Lanes           int      `json:"lanes"`
	NetsFound       int      `json:"nets_found"`
	Unclassified    []string `json:"unclassified,omitempty"`
	Notes           []string `json:"notes,omitempty"`
}

// RoutingGap is a group of nets with the same incompleteness.
type RoutingGap struct {
	// Islands describes the split, e.g. ["U3+U4", "U5", "termination"].
	Islands []string `json:"islands"`
	Nets    []string `json:"nets"`
}

// CustomRulesInfo is what the board's own .kicad_dru amounted to.
type CustomRulesInfo struct {
	Applied int `json:"applied"`
	Skipped int `json:"skipped"`

	// SkippedRules names the rules that were left alone, with the reason.
	SkippedRules []SkippedRule `json:"skipped_rules,omitempty"`
}

// SkippedRule is one rule this does not implement.
type SkippedRule struct {
	Name string `json:"name"`
	Why  string `json:"why"`
}

// MissingConnection is one pad-to-pad join the board has not got.
type MissingConnection struct {
	Net   string `json:"net"`
	Label string `json:"label"`

	// From and To are the pads, as "U4.K3", and Hop the span they belong to.
	From string `json:"from"`
	To   string `json:"to"`
	Hop  string `json:"hop"`
}

// RoutingInfo is how much of the interface is actually routed.
type RoutingInfo struct {
	Complete   int          `json:"complete"`
	Incomplete int          `json:"incomplete"`
	Gaps       []RoutingGap `json:"gaps,omitempty"`

	// Missing is every connection the fly-by chain has not got, pad to pad.
	// This is the work list: a length cannot be matched over copper that is
	// not there, and KiCad reports none of these as unconnected because each
	// of those nets has copper on it.
	Missing []MissingConnection `json:"missing,omitempty"`

	// Chain is the fly-by topology the address, command, control and clock
	// nets are routed in, and what the board has of it.
	Chain *ChainInfo `json:"chain,omitempty"`
}

// ChainInfo is the fly-by chain: the order the devices are visited and which
// hops exist.
//
// It is reported separately from the gaps because it is not a gap in the same
// sense. DDR3 and DDR4 address, command, control and clock have to be fly-by --
// through each device in turn, terminated at the end -- and a board missing a
// hop has not routed that bus, however finished the copper on it looks. Which
// is easy to miss: KiCad reports no unconnected item for any of those nets,
// because each one is a net with copper on it.
type ChainInfo struct {
	// Order is the controller followed by the devices, outward.
	Order []string `json:"order"`

	// Hops are the spans between them, in order, ending at the termination.
	Hops []HopInfo `json:"hops"`

	// OrderFrom says how the order was established -- a conclusion, not a
	// reading, and the one every per-hop length rests on.
	OrderFrom string `json:"order_from,omitempty"`

	// Note records a disagreement worth acting on.
	Note string `json:"note,omitempty"`

	// Complete is true when the board has every hop on every net.
	Complete bool `json:"complete"`
}

// HopInfo is one span of the chain.
type HopInfo struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Nets   int    `json:"nets"`
	Of     int    `json:"of"`
	Routed bool   `json:"routed"`
}

// MemberInfo is one net in a group.
type MemberInfo struct {
	Net         string  `json:"net"`
	Label       string  `json:"label"`
	Role        string  `json:"role"`
	Routed      bool    `json:"routed"`
	LengthMM    float64 `json:"length_mm"`
	DelayPS     float64 `json:"delay_ps"`
	DeviationMM float64 `json:"deviation_mm"`
	NeedMM      float64 `json:"need_mm"`
	NeedPS      float64 `json:"need_ps"`
	InTolerance bool    `json:"in_tolerance"`
	From        string  `json:"from,omitempty"`
	To          string  `json:"to,omitempty"`

	// HeadroomMM is the most length the space beside this route could hold.
	// Zero when it was not measured.
	//
	// As a candidate -- where one entry stands for a whole net rather than one
	// leg of it -- this is the room that can actually be used: each leg's room
	// capped at what that leg needs, summed. Room beside one leg cannot be
	// lent to another any more than it can be lent to another net, and a net
	// with 60 mm of space on a leg that needs 5 has 5 mm of use for it.
	HeadroomMM float64 `json:"headroom_mm"`

	// NeedsReroute marks a net asking for more length than a meander could
	// ever supply, whatever room were opened beside it -- or one that is longer
	// than its reference, which nothing here can shorten.
	NeedsReroute bool `json:"needs_reroute"`

	// ExcessMM is how far past the tolerance band a member is on the long
	// side: the amount it would have to be routed shorter. Zero otherwise.
	ExcessMM float64 `json:"excess_mm,omitempty"`

	// Reference marks a half of the group's own reference pair. Its offset is
	// against the pair's mean; it is not tuned towards it.
	Reference bool `json:"reference,omitempty"`

	// NetTotalMM is the whole net's length as KiCad reports it, set only when
	// it differs from LengthMM: a fly-by leg is matched on part of a net, but
	// KiCad shows the net as one number, so that is the figure to tune towards.
	NetTotalMM float64 `json:"net_total_mm,omitempty"`

	// NetAimMM is the whole-net length once every leg of the net is matched:
	// NetTotalMM plus what each out-of-tolerance leg needs, less what each has
	// to lose. Set with NetTotalMM.
	NetAimMM float64 `json:"net_aim_mm,omitempty"`

	// RunNeededMM and SpaceNeededMM2 are roughly what adding NeedMM would
	// take: how much straight track the meander has to be folded into, and
	// how much board the meander would then occupy. Zero when the room was
	// not measured.
	//
	// They are the best case -- the meander getting the whole amplitude it is
	// allowed -- and they are here so that "this net needs 13.7 mm" can be
	// read as a decision about the board rather than as a number.
	RunNeededMM    float64 `json:"run_needed_mm,omitempty"`
	SpaceNeededMM2 float64 `json:"space_needed_mm2,omitempty"`

	// Legs is where the requirement came from: one entry per group this net is
	// short in. Usually one, but an address or command line is measured over
	// each span of the fly-by chain separately -- controller to first device,
	// device to device -- each against the clock over that same span, each with
	// its own copper to fold length into.
	//
	// Always present, even for the single-leg case, because it is what says
	// which group a requirement belongs to: without it a net that appears in
	// two groups has its whole requirement charged to both, and a per-group
	// table adds up to more than the board needs.
	Legs []CandidateLeg `json:"legs,omitempty"`

	// pathTracks is the copper the measured route runs over. It is not sent to
	// the client -- a uuid list is of no use in a browser -- but the apply
	// step needs it so a meander lands on the path being matched.
	pathTracks []string
}

// CandidateLeg is one span a net is short over: the group that measured it,
// and everything about the requirement that belongs to that span alone.
//
// LengthMM is the length of that span, not of the net -- which is why the
// breakdown has to exist at all. A net short on two legs has two current
// lengths and two targets, and adding a two-leg requirement to a one-leg
// length produces a figure that means nothing.
type CandidateLeg struct {
	// Group is the group that measured this span, and Leg the span itself,
	// as "U3->U4".
	Group string `json:"group"`
	Leg   string `json:"leg,omitempty"`

	LengthMM float64 `json:"length_mm"`
	NeedMM   float64 `json:"need_mm"`

	// ExcessMM is how far past the tolerance band this leg is on the long
	// side, zero when it is not. Nothing can be added to fix that: it needs
	// routing shorter, or its reference longer.
	ExcessMM float64 `json:"excess_mm,omitempty"`

	// TargetMM is the length this leg is matched to: its group's reference.
	TargetMM float64 `json:"target_mm,omitempty"`

	// HeadroomMM is the room beside this leg's copper, uncapped.
	HeadroomMM   float64 `json:"headroom_mm"`
	NeedsReroute bool    `json:"needs_reroute"`

	// RunNeededMM and SpaceNeededMM2 are what this leg's requirement would
	// take up. Zero when the room was not measured.
	RunNeededMM    float64 `json:"run_needed_mm,omitempty"`
	SpaceNeededMM2 float64 `json:"space_needed_mm2,omitempty"`

	pathTracks []string
}

// CheckInfo is one requirement across groups, and whether the board meets it.
type CheckInfo struct {
	Kind    string  `json:"kind"`
	Name    string  `json:"name"`
	ValueMM float64 `json:"value_mm"`
	LimitMM float64 `json:"limit_mm"`
	OK      bool    `json:"ok"`
	Detail  string  `json:"detail"`
}

// ReferenceHalf is one half of a group's reference pair.
type ReferenceHalf struct {
	Net      string  `json:"net"`
	Label    string  `json:"label"`
	LengthMM float64 `json:"length_mm"`
}

// GroupInfo is a set of nets that have to match.
type GroupInfo struct {
	Name              string  `json:"name"`
	Kind              string  `json:"kind"`
	Lane              int     `json:"lane"`
	Leg               string  `json:"leg,omitempty"`
	Reference         string  `json:"reference"`
	ReferenceLengthMM float64 `json:"reference_length_mm"`
	// ReferenceMembers are the halves the reference length is the mean of.
	ReferenceMembers      []ReferenceHalf `json:"reference_members,omitempty"`
	TargetMM              float64         `json:"target_mm"`
	TargetFromReferenceMM float64         `json:"target_from_reference_mm"`
	ToleranceMM           float64         `json:"tolerance_mm"`
	SpreadMM              float64         `json:"spread_mm"`
	TotalNeedMM           float64         `json:"total_need_mm"`
	OutOfTolerance        int             `json:"out_of_tolerance"`
	Members               []MemberInfo    `json:"members"`
	Notes                 []string        `json:"notes,omitempty"`
}

// LayerFinding is a layer rule some DDR nets do not follow, for information.
type LayerFinding struct {
	// Rule is "data-top" or "address-bottom".
	Rule string `json:"rule"`
	// Expect is what the guide expects, in words.
	Expect string            `json:"expect"`
	Nets   []LayerFindingNet `json:"nets"`
}

// LayerFindingNet is one net's route copper per layer.
type LayerFindingNet struct {
	Net     string             `json:"net"`
	Label   string             `json:"label"`
	ByLayer map[string]float64 `json:"by_layer_mm"`
	// Share is the share of the copper on the expected layer, 0 to 1.
	Share float64 `json:"share"`
}

// PairSkew is the measured skew inside one differential pair.
type PairSkew struct {
	Pair    string  `json:"pair"`
	SkewMM  float64 `json:"skew_mm"`
	LimitMM float64 `json:"limit_mm"`
	OK      bool    `json:"ok"`
}

// Analysis is everything the report page needs.
type Analysis struct {
	Board     BoardInfo     `json:"board"`
	Interface InterfaceInfo `json:"interface"`
	Routing   RoutingInfo   `json:"routing"`
	Params    Params        `json:"params"`
	Groups    []GroupInfo   `json:"groups"`
	PairSkew  []PairSkew    `json:"pair_skew,omitempty"`

	// Layers are layer rules some DDR nets do not follow: observations for
	// the designer, never counted as out of tolerance.
	Layers []LayerFinding `json:"layers,omitempty"`

	// Notes are things the reader needs to know before trusting the rest: that
	// there is no DDR here, or that something was assumed.
	Notes []string `json:"notes,omitempty"`

	// PackageLengths lists the footprints whose pads carry a length inside
	// the package, added to every length measured to or from them.
	PackageLengths []pkglen.Applied `json:"package_lengths,omitempty"`

	// PackagePads is the package length on each of the controller's DDR
	// pads: the one in force and the default it replaced, if any.
	PackagePads []pkglen.PadLength `json:"package_pads,omitempty"`

	// Rules is every figure this tool works to and where it came from.
	Rules *DesignRules `json:"rules,omitempty"`

	// Checks are the requirements across groups: each byte lane's strobe
	// against the clock at its device, and one device's lanes against
	// another's.
	Checks []CheckInfo `json:"checks,omitempty"`

	// Interfaces is everything on the board this tool recognises, DDR
	// included, with what is wrong with each. The user picks which to work on.
	Interfaces []DetectedInterface `json:"interfaces,omitempty"`

	// Skipped lists nets in no group, with the reason.
	Skipped map[string]string `json:"skipped,omitempty"`

	// areas are the regions the user allowed copper to be added in, converted
	// to the board's own coordinates. Held here so the apply uses exactly what
	// the report measured against.
	areas tune.Areas

	// spacing is the open-field clearance these parameters resolved to for
	// this board's differential pairs. It is held here so that whatever edits
	// the board is held to the same rule the room was measured under.
	//
	// Deliberately not serialised: it is derived from Params and the board, so
	// a handler that has an Analysis from the wire has to re-derive it rather
	// than trust a copy that may not match the board in front of it.
	spacing drc.OpenSpacing

	// Candidates are the nets that would be changed, worst first. This is the
	// list the user ticks.
	Candidates []MemberInfo `json:"candidates"`

	// TotalNeedMM is what the candidates add up to.
	TotalNeedMM float64 `json:"total_need_mm"`

	// GettableMM is how much of that the board can actually hold: each net's
	// room capped at what it needs, summed. Room beside one net cannot be lent
	// to another, so this is not the sum of the room figures.
	GettableMM float64 `json:"gettable_mm"`

	// RerouteCount is how many candidates need routing differently rather than
	// tuning.
	RerouteCount int `json:"reroute_count"`

	// BusSpareMM is the clear space the buses these nets run in could be spread
	// into, which bounds what re-spacing them could add.
	BusSpareMM float64 `json:"bus_spare_mm"`

	// RunNeededMM and SpaceNeededMM2 are what the whole requirement would take
	// up: the straight track the meanders have to be folded into, and the
	// board area they would occupy. Zero until the room is measured.
	//
	// Not a single region -- the length has to go beside the net that needs
	// it, so this is a total and not somewhere to look. It is here because
	// "336.944 mm to add" is a number nobody can picture and "about 90 mm2 of
	// clear board, in the right places" is a decision.
	RunNeededMM    float64 `json:"run_needed_mm,omitempty"`
	SpaceNeededMM2 float64 `json:"space_needed_mm2,omitempty"`
}

func label(net string) string {
	if i := strings.LastIndexByte(net, '/'); i >= 0 {
		net = net[i+1:]
	}
	return strings.TrimPrefix(net, "DDR_")
}

// analyse runs the whole read-and-report path on a board. It is the function
// both the report and the plan endpoints call, so what the user is shown and
// what is applied can never diverge.
// Surveyor measures how much room the board has beside a route, and how much
// spare space its buses have. tune.Tuner satisfies it.
//
// The analysis takes it as an interface and tolerates nil, because measuring it
// probes the design rules along every candidate track and is much the slower
// half. A caller that only wants lengths passes nil; one that is about to ask
// the user to decide something passes a real one.
type Surveyor interface {
	Headroom(net string, on map[string]bool) float64
	BundleSpare(nets []string) (spare, span float64)
}

// openSpacer is a surveyor that can be held to an open-field clearance. It is
// optional so that a test double does not have to implement it.
type openSpacer interface {
	SetOpenSpacing(drc.OpenSpacing)
}

// areaSetter is a surveyor that can be restricted to the areas the user has
// allowed. Optional for the same reason.
type areaSetter interface {
	SetAreas(tune.Areas)
}

// measureInterfaces fills in the room beside the nets of every interface the
// DDR planner does not cover.
//
// The same three answers the DDR half gives -- match it here, open some room,
// route it differently -- need the same measurement, and there is nothing DDR
// about it: the room beside a track is the room beside a track. What differs is
// which copper counts as the route, and for a point-to-point net that is simply
// all of it.
func measureInterfaces(b *board.Board, sv Surveyor, ifaces []DetectedInterface) {
	if sv == nil {
		return
	}
	sp, canSize := sv.(spacer)
	for i := range ifaces {
		if ifaces[i].Planner != "" {
			// The DDR plan measures its own, per leg of the chain.
			continue
		}
		for j := range ifaces[i].Candidates {
			c := &ifaces[i].Candidates[j]
			c.HeadroomMM = 0
			for k := range c.Legs {
				l := &c.Legs[k]
				on := make(map[string]bool, len(l.pathTracks))
				for _, u := range l.pathTracks {
					on[u] = true
				}
				l.HeadroomMM = sv.Headroom(c.Net, on)
				c.HeadroomMM += math.Min(l.HeadroomMM, l.NeedMM)
				if canSize {
					ls := sp.SpaceFor(c.Net, l.NeedMM)
					l.RunNeededMM, l.SpaceNeededMM2 = ls.RunMM, ls.AreaMM2
				}
			}
			if canSize {
				s := sp.SpaceFor(c.Net, c.NeedMM)
				c.RunNeededMM, c.SpaceNeededMM2 = s.RunMM, s.AreaMM2
			}
		}
	}
}

// designRules collects what the tool works to, and what set each figure.
func designRules(b *board.Board, proj *board.Project, p Params, nets []string, iface *ddr.Interface) *DesignRules {
	out := &DesignRules{
		MinClearanceMM:  proj.MinClearance,
		MinTrackWidthMM: proj.MinTrackWidth,
		EdgeClearanceMM: proj.EdgeClearance,
	}
	// Net classes, with how many of the nets in hand fall in each: a class
	// nothing here belongs to is noise on this page.
	count := map[string]int{}
	for _, n := range nets {
		if c := proj.ClassOf(n); c != nil {
			count[c.Name]++
		}
	}
	for _, c := range proj.Classes {
		out.Classes = append(out.Classes, NetClassInfo{
			Name: c.Name, ClearanceMM: c.Clearance, TrackWidthMM: c.TrackWidth,
			DiffPairWidthMM: c.DiffPairWidth, DiffPairGapMM: c.DiffPairGap,
			ViaDiameterMM: c.ViaDiameter, ViaDrillMM: c.ViaDrill,
			Nets: count[c.Name],
		})
	}
	sort.Slice(out.Classes, func(i, j int) bool {
		if out.Classes[i].Nets != out.Classes[j].Nets {
			return out.Classes[i].Nets > out.Classes[j].Nets
		}
		return out.Classes[i].Name < out.Classes[j].Name
	})

	if r := customRules(b, proj); r != nil {
		for _, rule := range r.List {
			out.Custom = append(out.Custom, CustomRuleInfo{
				Name: rule.Name, Condition: rule.Condition, ClearanceMM: rule.ClearanceMin,
				Applied: rule.Understood, Why: rule.Why,
			})
		}
	}

	// And what all of that comes to for the nets in hand. The width and the
	// clearance are the board's; the rest are this tool's own, and saying so
	// is the point -- they are the only ones the user can change here.
	if len(nets) > 0 {
		widest, clear, class := 0.0, 0.0, ""
		if c := proj.ClassOf(nets[0]); c != nil {
			widest, clear, class = c.TrackWidth, c.Clearance, c.Name
		}
		from := "the board minimum"
		if class != "" {
			from = "net class " + class
		}
		if widest > 0 {
			out.Effective = append(out.Effective, EffectiveRule{
				What: "track width", ValueMM: widest, Source: from,
				Note: "a meander is drawn at the width of the track it replaces",
			})
		}
		if clear > 0 {
			out.Effective = append(out.Effective, EffectiveRule{
				What: "clearance to other nets", ValueMM: clear, Source: from,
			})
		}
	}
	if sp := p.openSpacing(iface); sp.Clearance > 0 {
		out.Effective = append(out.Effective, EffectiveRule{
			What: "clearance away from the components", ValueMM: sp.Clearance,
			Source: "your setting",
			Note: fmt.Sprintf("applies more than %.3f mm outside a component's pads, where there "+
				"is usually more room than the fanout rules assume", sp.Margin),
		})
	}
	out.Effective = append(out.Effective,
		EffectiveRule{
			What: "gap between meander legs", ValueMM: p.MeanderGapW * widthOf(proj, nets),
			Source: fmt.Sprintf("your setting, %g track widths", p.MeanderGapW),
			Note:   "parallel runs of the same signal couple to each other, which eats into the delay the meander was added for",
		},
		EffectiveRule{
			What: "how far a meander may stray", ValueMM: p.MaxAmplitudeMM, Source: "your setting",
		},
		EffectiveRule{
			What: "keep-out around pads", ValueMM: p.PadKeepoutMM, Source: "your setting",
		},
	)
	return out
}

// widthOf is the track width the nets in hand are drawn at, for turning a
// setting given in track widths into millimetres.
func widthOf(proj *board.Project, nets []string) float64 {
	if len(nets) == 0 {
		return 0
	}
	if c := proj.ClassOf(nets[0]); c != nil && c.TrackWidth > 0 {
		return c.TrackWidth
	}
	return proj.MinTrackWidth
}

// surveyor is the tuner used to measure rather than to write: the same
// arithmetic, the same rules, no copper changed.
func surveyor(b *board.Board, proj *board.Project, p Params) Surveyor {
	t := tune.NewTuner(b, proj, p.style())
	t.SetRules(customRules(b, proj))
	return t
}

// spacer is a surveyor that can say what a length would take up. Optional for
// the same reason.
type spacer interface {
	SpaceFor(net string, gain float64) tune.Space
}

// openSpacing is the away-from-the-components rule these parameters ask for.
// A zero clearance produces a zero rule, which changes nothing.
// interfaceNets is every net of every interface found, for the rules summary
// on a board with no DDR on it.
func interfaceNets(ifaces []DetectedInterface) []string {
	var out []string
	for _, i := range ifaces {
		for _, c := range i.Candidates {
			out = append(out, c.Net)
		}
	}
	return out
}

// withPairs adds every differential pair the detector found to an open-field
// rule, so the two halves of a USB or PCIe pair are held to their own coupled
// gap rather than to the away-from-components clearance -- which is wider than
// any pair is drawn, and would call every routed pair a violation.
func withPairs(o drc.OpenSpacing, b *board.Board) drc.OpenSpacing {
	if o.Clearance <= 0 {
		return o
	}
	if o.Coupled == nil {
		o.Coupled = map[string]string{}
	}
	for _, i := range proto.Detect(b) {
		for _, pr := range i.Pairs {
			o.Coupled[pr.P] = pr.N
			o.Coupled[pr.N] = pr.P
		}
	}
	return o
}

func (p Params) openSpacing(iface *ddr.Interface) drc.OpenSpacing {
	o := drc.OpenSpacing{Clearance: p.OpenClearanceMM, Margin: p.OpenMarginMM}
	if o.Clearance <= 0 {
		return drc.OpenSpacing{}
	}
	o.Coupled = map[string]string{}
	if iface != nil {
		for net, sig := range iface.Signals {
			if sig.Pair != "" {
				o.Coupled[net] = sig.Pair
			}
		}
	}
	return o
}

// boardInfo is what the board is, independent of any interface on it.
func boardInfo(b *board.Board, proj *board.Project, filename string) BoardInfo {
	var tracks, vias int
	for _, t := range b.Tracks {
		if t.Kind == board.KindVia {
			vias++
		} else {
			tracks++
		}
	}
	info := BoardInfo{
		Filename: filename, CopperLayers: b.CopperLayers, StackupMM: b.Stackup.Thickness,
		Footprints: len(b.Footprints), Pads: len(b.Pads), Tracks: tracks, Vias: vias,
		NetClasses: proj.ClassNames(), HasCustomDRU: proj.HasCustomRules,
		ViaLengthUsed: b.UseHeightForLength, HasProjectFile: len(proj.Classes) > 0,
	}
	if r := customRules(b, proj); r != nil {
		applied, skipped := r.Understood()
		info.CustomRules = &CustomRulesInfo{Applied: applied, Skipped: skipped}
		for _, rule := range r.Skipped() {
			info.CustomRules.SkippedRules = append(info.CustomRules.SkippedRules,
				SkippedRule{Name: rule.Name, Why: rule.Why})
		}
	}
	return info
}

func analyse(b *board.Board, proj *board.Project, filename string, p Params, sv Surveyor) (*Analysis, *ddr.Plan, error) {
	p = p.withDefaults()
	if err := p.Validate(); err != nil {
		return nil, nil, err
	}
	// Package lengths before anything is measured: every DDR limit is on the
	// path from die to die, and a board-only length matches the wrong thing.
	pkgs := pkglen.Apply(b)
	engine := netlen.New(b)

	prefix, scoped := ddr.Scope(b, p.NetPrefix)

	// A board with no DDR on it is not a board this has nothing to say about.
	// Refusing it was the last thing left over from when DDR was the only
	// interface the tool knew: everything else it now recognises -- USB, PCIe,
	// Ethernet, SD, and any pair of complements -- is measured on any board at
	// all. So the DDR half is skipped and the rest is reported.
	if prefix == "" && scoped == nil {
		// Empty rather than absent: a client that maps over these should get
		// nothing to map over, not a null it has to know about.
		// The same clearance and the same allowed areas the DDR half measures
		// under, or the room reported here would be room the apply refuses.
		spacing := withPairs(p.openSpacing(nil), b)
		areas, err := areasFor(b, p.MeanderAreas)
		if err != nil {
			return nil, nil, err
		}
		if s, ok := sv.(openSpacer); ok {
			s.SetOpenSpacing(spacing)
		}
		if ar, ok := sv.(areaSetter); ok {
			ar.SetAreas(areas)
		}
		a := &Analysis{Params: p, Groups: []GroupInfo{}, Candidates: []MemberInfo{},
			spacing: spacing, areas: areas}
		a.PackageLengths = pkgs
		a.Board = boardInfo(b, proj, filename)
		a.Interfaces = detectInterfaces(b, engine, p.Interfaces, p.MaxIntraPairFixMM, p.GroupToleranceMM)
		measureInterfaces(b, sv, a.Interfaces)
		nets := interfaceNets(a.Interfaces)
		if len(nets) == 0 {
			nets = b.Nets()
		}
		a.Rules = designRules(b, proj, p, nets, nil)
		a.Notes = append(a.Notes,
			"No DDR memory found on this board. The interfaces below are still measured.")
		return a, nil, nil
	}

	iface, err := ddr.Classify(b, ddr.Options{NetPrefix: prefix, Nets: scoped, Controller: p.Controller})
	if err != nil {
		return nil, nil, err
	}
	// Overrides go on before anything is measured; the engine reads pads as
	// it measures, so nothing measured so far is stale.
	if p.PackagePart != "" {
		chosen := pkglen.UsePart(b, iface.Controller, p.PackagePart)
		pkgs = slices.DeleteFunc(pkgs, func(a pkglen.Applied) bool { return a.Ref == iface.Controller })
		if chosen.Pads+chosen.FromBoard > 0 {
			pkgs = append(pkgs, chosen)
		}
	}
	pads := pkglen.Override(b, iface.Controller, ifaceNets(iface), p.PackageLengthsMM)
	plan, err := ddr.BuildPlan(iface, engine, p.rules())
	if err != nil {
		return nil, nil, err
	}

	// The open-field clearance has to be in force before anything measures
	// room, and it needs the pair map, which only exists once the interface is
	// classified -- so it is applied here rather than where the surveyor was
	// built. A surveyor that cannot take one (a test double) simply does not.
	if sp, ok := sv.(openSpacer); ok {
		sp.SetOpenSpacing(withPairs(p.openSpacing(iface), b))
	}
	// The allowed areas have to be in force before the room is measured, or
	// the report promises room the apply will then refuse to use.
	areas, err := areasFor(b, p.MeanderAreas)
	if err != nil {
		return nil, nil, err
	}
	if ar, ok := sv.(areaSetter); ok {
		ar.SetAreas(areas)
	}
	if sv != nil {
		plan.MeasureHeadroom(sv)
	}

	a := &Analysis{Params: p, Skipped: plan.Skipped, spacing: withPairs(p.openSpacing(iface), b), areas: areas}
	a.PackageLengths = pkgs
	a.PackagePads = matchedPads(pads, plan)
	// No note for missing package lengths: the page warns about it with a
	// way to fix it, and the report prints its own section.

	a.Board = boardInfo(b, proj, filename)
	a.Interface = InterfaceInfo{
		NetPrefix: prefix, Controller: iface.Controller, Devices: iface.Devices,
		WidthBits: iface.Width, Lanes: iface.Lanes, NetsFound: len(iface.Signals),
		Unclassified: iface.Unclassified, Notes: iface.Notes,
	}
	if fp := b.Footprint(iface.Controller); fp != nil {
		a.Interface.ControllerValue = fp.Value
	}
	a.Routing = routingInfo(engine, iface)
	a.Routing.Chain = chainInfo(plan.Chain)
	for _, q := range ddr.MissingHops(b, iface, plan) {
		a.Routing.Missing = append(a.Routing.Missing, MissingConnection{
			Net: q.Net, Label: label(q.Net), From: q.From, To: q.To,
			Hop: hopOf(plan.Chain, q.From, q.To),
		})
	}
	a.Interfaces = detectInterfaces(b, engine, p.Interfaces, p.MaxIntraPairFixMM, p.GroupToleranceMM)
	measureInterfaces(b, sv, a.Interfaces)

	totals := map[string]float64{}
	change := map[string]float64{}
	for _, g := range plan.Groups {
		for _, m := range g.Members {
			if m.Routed && !m.InTolerance && !m.Reference {
				change[m.Net] += m.Need - m.Excess
			}
		}
	}
	for _, g := range plan.Groups {
		gi := GroupInfo{
			Name: g.Name, Kind: g.Kind.String(), Lane: g.Lane, Leg: g.Leg,
			Reference: label(g.Reference), ReferenceLengthMM: g.ReferenceLength,
			TargetMM: g.Target, TargetFromReferenceMM: g.TargetFromReference,
			ToleranceMM: g.ToleranceMM, SpreadMM: g.SpreadBefore(),
			TotalNeedMM: g.TotalNeed(), OutOfTolerance: g.OutOfTolerance(),
			Notes: g.Notes,
		}
		for _, m := range g.Members {
			gi.Members = append(gi.Members, MemberInfo{
				Net: m.Net, Label: label(m.Net), Role: m.Role.String(), Routed: m.Routed,
				LengthMM: m.Length, DelayPS: m.Delay, DeviationMM: m.Deviation,
				NeedMM: m.Need, NeedPS: m.NeedDelay, InTolerance: m.InTolerance,
				From: m.From, To: m.To, pathTracks: m.PathTracks,
				HeadroomMM: m.Headroom, NeedsReroute: m.NeedsReroute(),
				ExcessMM: m.Excess, Reference: m.Reference,
				NetTotalMM: netTotal(totals, engine, m.Net, m.Routed && g.Leg != ""),
			})
		}
		for i := range gi.Members {
			if t := gi.Members[i].NetTotalMM; t > 0 {
				gi.Members[i].NetAimMM = t + change[gi.Members[i].Net]
			}
		}
		for _, r := range g.ReferenceMembers {
			gi.ReferenceMembers = append(gi.ReferenceMembers, ReferenceHalf{
				Net: r.Net, Label: label(r.Net), LengthMM: r.Length,
			})
		}
		sort.Slice(gi.Members, func(i, j int) bool {
			if gi.Members[i].Routed != gi.Members[j].Routed {
				return gi.Members[j].Routed
			}
			return gi.Members[i].LengthMM < gi.Members[j].LengthMM
		})
		a.Groups = append(a.Groups, gi)
	}

	for _, c := range plan.Checks() {
		a.Checks = append(a.Checks, CheckInfo{
			Kind: c.Kind, Name: c.Name, ValueMM: c.ValueMM, LimitMM: c.LimitMM, OK: c.OK, Detail: c.Detail,
		})
	}

	for _, f := range plan.LayerFindings(b) {
		lf := LayerFinding{Rule: f.Rule, Expect: f.Expect}
		for _, n := range f.Nets {
			lf.Nets = append(lf.Nets, LayerFindingNet{Net: n.Net, Label: label(n.Net), ByLayer: n.ByLayer, Share: n.Share})
		}
		a.Layers = append(a.Layers, lf)
	}

	// For information only: ST sets no limit on the skew inside a DDR pair.
	for pair, skew := range plan.IntraPairSkew() {
		a.PairSkew = append(a.PairSkew, PairSkew{Pair: label(pair), SkewMM: skew, OK: true})
	}
	sort.Slice(a.PairSkew, func(i, j int) bool { return a.PairSkew[i].Pair < a.PairSkew[j].Pair })

	{
		ns := make([]string, 0, len(iface.Signals))
		for n := range iface.Signals {
			ns = append(ns, n)
		}
		sort.Strings(ns)
		a.Rules = designRules(b, proj, p, ns, iface)
	}

	a.Candidates = candidates(plan)
	sp, canSize := sv.(spacer)
	var nets []string
	for i, c := range a.Candidates {
		a.TotalNeedMM += c.NeedMM
		a.GettableMM += math.Min(c.NeedMM, c.HeadroomMM)
		if c.NeedsReroute {
			a.RerouteCount++
		}
		// What that length would take up, so a requirement in millimetres can
		// be read as a decision about the board.
		if canSize {
			s := sp.SpaceFor(c.Net, c.NeedMM)
			a.Candidates[i].RunNeededMM = s.RunMM
			a.Candidates[i].SpaceNeededMM2 = s.AreaMM2
			a.RunNeededMM += s.RunMM
			a.SpaceNeededMM2 += s.AreaMM2
			for j, leg := range a.Candidates[i].Legs {
				ls := sp.SpaceFor(c.Net, leg.NeedMM)
				a.Candidates[i].Legs[j].RunNeededMM = ls.RunMM
				a.Candidates[i].Legs[j].SpaceNeededMM2 = ls.AreaMM2
			}
		}
		nets = append(nets, c.Net)
	}
	if sv != nil && len(nets) > 0 {
		a.BusSpareMM, _ = sv.BundleSpare(nets)
	}
	return a, plan, nil
}

// candidates is every net that needs length, with what it needs altogether,
// worst first.
//
// One entry per net, but the requirement is summed over every leg it is short
// on rather than taken from its worst. A fly-by address line is measured over
// each span of the chain separately and has to match the clock over each of
// them, so a net short on U3->U4 and short again on U4->U5 needs both fixed,
// in different copper. Reporting only the worse of the two understated what
// the board needs and -- because the apply worked from the same list -- left
// the other leg exactly as short as it was.
func candidates(p *ddr.Plan) []MemberInfo {
	best := map[string]*MemberInfo{}
	worst := map[string]float64{}
	var order []string
	for _, g := range p.Groups {
		for _, m := range g.Members {
			// Short of the target, or too long for it: both are the member's
			// business. A member too long has nothing to add, and is listed so
			// the reader is told it needs routing shorter rather than not told.
			if !m.Routed || m.InTolerance || (m.Need <= 1e-6 && m.Excess <= 1e-6) {
				continue
			}
			leg := CandidateLeg{
				Group: g.Name, Leg: g.Leg, LengthMM: m.Length, NeedMM: m.Need,
				ExcessMM: m.Excess, TargetMM: g.Target,
				HeadroomMM: m.Headroom, NeedsReroute: m.NeedsReroute(),
				pathTracks: m.PathTracks,
			}
			cur, ok := best[m.Net]
			if !ok {
				order = append(order, m.Net)
				best[m.Net] = &MemberInfo{
					Net: m.Net, Label: label(m.Net), Role: m.Role.String(), Routed: true,
					LengthMM: m.Length, DelayPS: m.Delay, DeviationMM: m.Deviation,
					NeedPS: m.NeedDelay, InTolerance: m.InTolerance,
					From: m.From, To: m.To, pathTracks: m.PathTracks,
					ExcessMM: m.Excess,
					Legs:     []CandidateLeg{leg},
				}
				cur = best[m.Net]
			} else {
				cur.Legs = append(cur.Legs, leg)
				// The face the net shows is its worst leg: that is the one a
				// reader is being warned about, and the one whose length and
				// deviation answer "how bad is this".
				if m.Need > worst[m.Net] {
					cur.LengthMM, cur.DelayPS, cur.DeviationMM = m.Length, m.Delay, m.Deviation
					cur.NeedPS = m.NeedDelay
					cur.From, cur.To, cur.pathTracks = m.From, m.To, m.PathTracks
				}
			}
			if m.Need > worst[m.Net] {
				worst[m.Net] = m.Need
			}
			cur.NeedMM += m.Need
			// Room on one leg is no use to another, so what can be had is
			// each leg's room capped at that leg's requirement.
			cur.HeadroomMM += math.Min(m.Headroom, m.Need)
			cur.NeedsReroute = cur.NeedsReroute || m.NeedsReroute()
		}
	}
	out := make([]MemberInfo, 0, len(best))
	for _, n := range order {
		out = append(out, *best[n])
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].NeedMM != out[j].NeedMM {
			return out[i].NeedMM > out[j].NeedMM
		}
		return out[i].Net < out[j].Net
	})
	return out
}

// hopOf names the span a missing connection belongs to, by matching the pads
// against the chain's own hops. The pad ids carry the component reference, so
// "U4.K3 -> U5.K3" is the U4 -> U5 hop.
func hopOf(c *ddr.Chain, from, to string) string {
	if c == nil {
		return ""
	}
	ref := func(pad string) string {
		if i := strings.IndexByte(pad, '.'); i > 0 {
			return pad[:i]
		}
		return pad
	}
	f, t := ref(from), ref(to)
	for _, h := range c.Hops {
		if h.From == f && (h.To == t || h.To == "termination") {
			return h.From + " -> " + h.To
		}
	}
	return f + " -> " + t
}

// chainInfo carries the fly-by topology to the client. The order comes after
// the plan has checked it against the routed copper, so it is the board's order
// and not the designators'.
func chainInfo(c *ddr.Chain) *ChainInfo {
	if c == nil || len(c.Hops) == 0 {
		return nil
	}
	out := &ChainInfo{Order: c.Order, OrderFrom: c.OrderFrom, Note: c.Note, Complete: true}
	for _, h := range c.Hops {
		out.Hops = append(out.Hops, HopInfo{
			From: h.From, To: h.To, Nets: h.Nets, Of: h.Of, Routed: h.Routed(),
		})
		if !h.Routed() {
			out.Complete = false
		}
	}
	return out
}

func routingInfo(e *netlen.Engine, iface *ddr.Interface) RoutingInfo {
	role := map[string]bool{iface.Controller: true}
	for _, d := range iface.Devices {
		role[d] = true
	}
	generalise := func(ref string) string {
		if role[ref] {
			return ref
		}
		return "termination"
	}

	var info RoutingInfo
	byShape := map[string][]string{}
	shapes := map[string][]string{}
	nets := make([]string, 0, len(iface.Signals))
	for n := range iface.Signals {
		nets = append(nets, n)
	}
	sort.Strings(nets)
	for _, n := range nets {
		m := e.Measure(n)
		if m.Complete {
			info.Complete++
			continue
		}
		info.Incomplete++
		var parts []string
		for _, isle := range m.Islands {
			seen := map[string]bool{}
			var rs []string
			for _, id := range isle {
				r := generalise(id[:strings.IndexByte(id+".", '.')])
				if !seen[r] {
					seen[r] = true
					rs = append(rs, r)
				}
			}
			sort.Strings(rs)
			parts = append(parts, strings.Join(rs, "+"))
		}
		sort.Strings(parts)
		key := strings.Join(parts, " | ")
		byShape[key] = append(byShape[key], label(n))
		shapes[key] = parts
	}
	keys := make([]string, 0, len(byShape))
	for k := range byShape {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(byShape[keys[i]]) != len(byShape[keys[j]]) {
			return len(byShape[keys[i]]) > len(byShape[keys[j]])
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys {
		info.Gaps = append(info.Gaps, RoutingGap{Islands: shapes[k], Nets: byShape[k]})
	}
	return info
}

// Where every figure this tool works to comes from.
//
// KiCad tells you why a track is the width it is -- this class, that rule --
// and a tool that quietly applies its own numbers on top of the board's owes
// the same. There are three sources and they are not interchangeable: the
// board's own setup and net classes, the custom rules in the .kicad_dru, and
// the settings of this tool, which are the only ones the user can change here.
type DesignRules struct {
	// Board is what the .kicad_pro sets for the whole board.
	MinClearanceMM  float64 `json:"min_clearance_mm"`
	MinTrackWidthMM float64 `json:"min_track_width_mm"`
	EdgeClearanceMM float64 `json:"edge_clearance_mm"`

	// Classes are the net classes, as KiCad's Board Setup lists them.
	Classes []NetClassInfo `json:"classes,omitempty"`

	// Custom are the rules from the .kicad_dru, including the ones this does
	// not implement: a rule left alone is not a rule quietly ignored, and
	// which ones those are decides whether a "no room here" is real.
	Custom []CustomRuleInfo `json:"custom,omitempty"`

	// Effective is what the nets being matched actually work to, and why.
	Effective []EffectiveRule `json:"effective,omitempty"`
}

// NetClassInfo is one net class, with the nets of this interface in it.
type NetClassInfo struct {
	Name            string  `json:"name"`
	ClearanceMM     float64 `json:"clearance_mm"`
	TrackWidthMM    float64 `json:"track_width_mm"`
	DiffPairWidthMM float64 `json:"diff_pair_width_mm,omitempty"`
	DiffPairGapMM   float64 `json:"diff_pair_gap_mm,omitempty"`
	ViaDiameterMM   float64 `json:"via_diameter_mm,omitempty"`
	ViaDrillMM      float64 `json:"via_drill_mm,omitempty"`

	// Nets is how many of the nets being matched fall in this class.
	Nets int `json:"nets,omitempty"`
}

// CustomRuleInfo is one rule from the .kicad_dru, as written.
type CustomRuleInfo struct {
	Name        string  `json:"name"`
	Condition   string  `json:"condition,omitempty"`
	ClearanceMM float64 `json:"clearance_mm,omitempty"`
	Applied     bool    `json:"applied"`
	Why         string  `json:"why,omitempty"`
}

// EffectiveRule is one number this tool works to, and where it came from.
type EffectiveRule struct {
	// What it is ("track width"), the value, and Source the thing that set it
	// ("net class DDR", "your setting", "rule bga_fanout_tight").
	What    string  `json:"what"`
	ValueMM float64 `json:"value_mm"`
	Source  string  `json:"source"`

	// Note says when it applies, for a figure that does not apply everywhere.
	Note string `json:"note,omitempty"`
}

// netTotal is the net's KiCad length when the member is measured to one device
// of a fly-by chain, and zero otherwise. It is board copper only, as KiCad
// shows it unless the footprint sets die lengths, so it can be shorter than the
// member's length, which includes the package. A point-to-point net is left
// alone: KiCad's total there differs from its length only by pad stubs. Totals
// are cached per net: a fly-by net sits in one group per device.
func netTotal(cache map[string]float64, e *netlen.Engine, net string, leg bool) float64 {
	if !leg {
		return 0
	}
	t, ok := cache[net]
	if !ok {
		t = e.Measure(net).KiCadLength
		cache[net] = t
	}
	return t
}

// ifaceNets is every net of the DDR interface.
func ifaceNets(iface *ddr.Interface) []string {
	out := make([]string, 0, len(iface.Signals))
	for n := range iface.Signals {
		out = append(out, n)
	}
	return out
}

// matchedPads keeps the pads on nets some group matches. A line that is not
// length matched, such as RESETN, needs no package length, and ST's sheet
// gives it none.
func matchedPads(pads []pkglen.PadLength, plan *ddr.Plan) []pkglen.PadLength {
	inGroup := map[string]bool{}
	for _, g := range plan.Groups {
		for _, m := range g.Members {
			inGroup[m.Net] = true
		}
	}
	out := pads[:0:0]
	for _, p := range pads {
		if inGroup[p.Net] {
			out = append(out, p)
		}
	}
	return out
}
