package server

import (
	"math"
	"sort"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/netlen"
	"github.com/embeddedci-com/pcb-autorouter/proto"
)

// What else is on the board.
//
// The tool began as a DDR matcher, and DDR is the hardest case rather than the
// only one: Ethernet, USB, PCIe, MIPI and SD all need the same measurement over
// simpler shapes. So the board is read for every interface it has, and the user
// picks which ones to work on rather than being given one answer about one bus.
//
// Detection is by net name, which is a reading and not a proof -- nothing in the
// geometry says a pair is PCIe rather than SATA. Every interface therefore
// carries the evidence it was recognised by, and the picker shows it, so
// disagreeing with the tool is a matter of unticking a box.

// DetectedInterface is one interface found on the board.
type DetectedInterface struct {
	// ID identifies it for selection. Stable for a given board.
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`

	// Nets and Routed count what belongs to it and what has copper.
	Nets   int `json:"nets"`
	Routed int `json:"routed"`

	// Pairs is how many differential pairs it has.
	Pairs int `json:"pairs"`

	// Evidence is how it was recognised, in the board's own net names.
	Evidence string `json:"evidence"`

	// Planner names the analyser that handles it in detail, where one exists.
	Planner string `json:"planner,omitempty"`

	// IntraPairMM is the limit its pairs are held to, zero when it has none.
	IntraPairMM float64 `json:"intra_pair_mm,omitempty"`

	// Summary says in a sentence what is wrong with it, or that nothing is.
	Summary string `json:"summary"`

	// Actionable is true when a length matcher could improve something here.
	Actionable bool `json:"actionable"`

	// Unroutable is how many of its nets have no complete route. Nothing can
	// be matched over copper that does not exist, so this comes first.
	Unroutable int `json:"unroutable"`

	// UnroutedNets names them. A count tells a reader there is work; the names
	// tell them which work, which is the difference between a status line and
	// something they can act on.
	UnroutedNets []string `json:"unrouted_nets,omitempty"`

	// Candidates are the nets of this interface that need length, in the same
	// shape the DDR plan produces, so one briefing can cover a whole board
	// rather than only the half of it this tool plans in detail.
	Candidates []MemberInfo `json:"candidates,omitempty"`

	// TotalNeedMM is what those add up to.
	TotalNeedMM float64 `json:"total_need_mm,omitempty"`

	// Missing is every pad-to-pad connection its nets have not got: the work
	// list for routing it, in the same shape the fly-by chain's gaps take, so
	// the router can be asked to make them the same way.
	Missing []MissingConnection `json:"missing,omitempty"`

	PairSkew []PairSkewInfo  `json:"pair_skew,omitempty"`
	Groups   []GroupSkewInfo `json:"groups,omitempty"`

	// Geometry is the width and pair spacing, measured off the board where it
	// is routed and supplied by the user where it is not.
	Geometry GeometryInfo `json:"geometry"`

	// Impedance is what that geometry comes out at, against what the family
	// usually asks for.
	Impedance ImpedanceInfo `json:"impedance"`

	// Assigned is true when the user said what this interface is, rather than
	// the tool reading it from the names.
	Assigned bool `json:"assigned,omitempty"`

	// netRows is every matched net's standing, in tolerance or not, for the
	// one-net lookup. Not sent with the report: Candidates already carries the
	// nets that need something, and the rest would double its size.
	netRows []NetStatus
}

// GeometryInfo is an interface's physical shape.
type GeometryInfo struct {
	WidthMM float64 `json:"width_mm"`
	GapMM   float64 `json:"gap_mm"`
	Layer   string  `json:"layer,omitempty"`

	// Measured names what came off the board rather than from the user, so the
	// form can offer those for confirmation and ask for the rest.
	Measured []string `json:"measured,omitempty"`

	// NeedsWidth and NeedsGap are true when the board cannot supply the
	// figure and the interface needs it.
	NeedsWidth bool `json:"needs_width"`
	NeedsGap   bool `json:"needs_gap"`

	// Widths lists every width the interface is drawn at, longest first. A bus
	// drawn at two widths is worth seeing rather than averaging away.
	Widths []WidthUseInfo `json:"widths,omitempty"`

	Note string `json:"note,omitempty"`
}

// WidthUseInfo is one width and how much copper is at it.
type WidthUseInfo struct {
	WidthMM  float64 `json:"width_mm"`
	LengthMM float64 `json:"length_mm"`
}

// ImpedanceInfo is the computed impedance against the controller's target, or
// the family's usual one where the controller's guide gives none.
//
// It is an estimate from closed-form models and the stackup in the board file.
// A board that has to hold its impedance to a few percent needs the
// fabricator's stackup and a field solver; this catches the commoner and
// blunter problem of a width nowhere near what the interface wants.
type ImpedanceInfo struct {
	SingleEndedOhms float64 `json:"single_ended_ohms,omitempty"`
	DiffOhms        float64 `json:"diff_ohms,omitempty"`

	TargetSingleEndedOhms float64 `json:"target_single_ended_ohms,omitempty"`
	TargetDiffOhms        float64 `json:"target_diff_ohms,omitempty"`

	// The top of a target range where the guide gives one ("80 to 90").
	TargetSingleEndedMaxOhms float64 `json:"target_single_ended_max_ohms,omitempty"`
	TargetDiffMaxOhms        float64 `json:"target_diff_max_ohms,omitempty"`

	// Chip is the controller recognised on this interface, and ChipRef the
	// footprint. ChipConnected is false when it has no pad on these nets and
	// was taken as the only recognised chip on the board. TargetSource is its
	// guide; empty means the guide gives no figure for this protocol and the
	// targets are the family's usual ones.
	Chip          string `json:"chip,omitempty"`
	ChipRef       string `json:"chip_ref,omitempty"`
	ChipConnected bool   `json:"chip_connected,omitempty"`
	TargetSource  string `json:"target_source,omitempty"`

	// Computed is false when there was no width to work from, in which case
	// every other field here is meaningless and must not be shown. A zero
	// Microstrip on an interface nobody has routed is not a claim that it is
	// stripline.
	Computed bool `json:"computed"`

	// Microstrip is true on an outer layer. The two cases differ by far more
	// than the models' own error.
	Microstrip bool `json:"microstrip"`

	// InRange is false when the geometry is outside where the closed form is
	// trustworthy. The figure is still given, to be read as an indication.
	InRange bool `json:"in_range"`

	Layer string `json:"layer,omitempty"`

	// LayerAssumed is true when nothing is routed, so the layer the figures
	// were computed for is a guess. Microstrip and stripline differ by far
	// more than the models' own error, so this matters.
	LayerAssumed bool `json:"layer_assumed,omitempty"`

	Note       string `json:"note,omitempty"`
	TargetNote string `json:"target_note,omitempty"`
}

// FamilyInfo describes one kind of interface the tool knows, for the picker
// that lets a user say what their nets actually are.
type FamilyInfo struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Summary string `json:"summary"`

	// TX, RX and Common are the signal names a board usually gives each, so a
	// user can recognise their own nets in the list.
	TX     []string `json:"tx,omitempty"`
	RX     []string `json:"rx,omitempty"`
	Common []string `json:"common,omitempty"`

	SingleEndedOhms float64 `json:"single_ended_ohms,omitempty"`
	DiffOhms        float64 `json:"diff_ohms,omitempty"`
	OhmsNote        string  `json:"ohms_note,omitempty"`

	IntraPairMM  float64 `json:"intra_pair_mm,omitempty"`
	GroupMM      float64 `json:"group_mm,omitempty"`
	Differential bool    `json:"differential,omitempty"`
}

// KnownFamilies is the catalogue, for the client.
func KnownFamilies() []FamilyInfo {
	out := make([]FamilyInfo, 0, len(proto.Families))
	for _, f := range proto.Families {
		out = append(out, FamilyInfo{
			Kind: string(f.Kind), Label: f.Label, Summary: f.Summary,
			TX: f.TX, RX: f.RX, Common: f.Common,
			SingleEndedOhms: f.SingleEndedOhms, DiffOhms: f.DiffOhms, OhmsNote: f.OhmsNote,
			IntraPairMM: f.IntraPairMM, GroupMM: f.GroupMM, Differential: f.Differential,
		})
	}
	return out
}

// InterfaceOverride is the user saying what an interface actually is.
//
// Detection is a reading of the net names and the user is the authority on
// their own board, so every part of that reading can be replaced: what family
// it belongs to, which net is its reference, and the two numbers the board
// cannot supply when nothing is routed yet.
type InterfaceOverride struct {
	// ID names the detected interface this applies to.
	ID string `json:"id"`

	// Kind reassigns the family. Empty keeps what was detected.
	Kind string `json:"kind,omitempty"`

	// Reference names the net everything else is matched to, for an interface
	// whose structure is not in its names.
	Reference string `json:"reference,omitempty"`

	// WidthMM and GapMM are the figures the board could not supply.
	WidthMM float64 `json:"width_mm,omitempty"`
	GapMM   float64 `json:"gap_mm,omitempty"`

	// Ignore drops the interface from the report entirely: the tool read
	// something that is not there.
	Ignore bool `json:"ignore,omitempty"`
}

// PairSkewInfo is one differential pair measured.
type PairSkewInfo struct {
	Name        string  `json:"name"`
	P           string  `json:"p"`
	N           string  `json:"n"`
	SkewMM      float64 `json:"skew_mm"`
	LimitMM     float64 `json:"limit_mm"`
	Routed      bool    `json:"routed"`
	InTolerance bool    `json:"in_tolerance"`
}

// GroupSkewInfo is one group measured against its reference.
type GroupSkewInfo struct {
	Name        string  `json:"name"`
	Reference   string  `json:"reference"`
	ReferenceMM float64 `json:"reference_mm"`
	SpreadMM    float64 `json:"spread_mm"`
	LimitMM     float64 `json:"limit_mm"`
	OutOfTol    int     `json:"out_of_tolerance"`
	Unroutable  int     `json:"unroutable"`

	// TargetMM is the length every member is to be brought to, and NeedMM what
	// the group needs altogether to get there. The target is the reference
	// unless a member is already longer than it: a meander can only lengthen a
	// track, so the longest member sets the bar for everyone.
	TargetMM float64 `json:"target_mm,omitempty"`
	NeedMM   float64 `json:"need_mm,omitempty"`

	// Members is how many nets it has.
	Members int `json:"members,omitempty"`

	// Rows is every member measured, in tolerance or not, the way a DDR group
	// lists its own. Without it a protocol that is not DDR could say how many
	// nets were out but never which, and there was nothing to select on the
	// board.
	Rows []MemberInfo `json:"rows,omitempty"`

	// Why says what the group is and where its limit comes from, because the
	// limit is a starting point and the vendor's guide governs.
	Why string `json:"why,omitempty"`
}

// detectInterfaces reads the board for everything it carries, applies whatever
// the user has said about it, and measures each.
func detectInterfaces(b *board.Board, e *netlen.Engine, overrides []InterfaceOverride, maxPairFix float64, groupTol map[string]float64) []DetectedInterface {
	by := map[string]InterfaceOverride{}
	for _, o := range overrides {
		by[o.ID] = o
	}
	found := proto.Detect(b)
	out := make([]DetectedInterface, 0, len(found))
	for _, i := range found {
		o, assigned := by[i.ID()]
		if o.Ignore {
			continue
		}
		if assigned && o.Kind != "" && proto.Kind(o.Kind) != i.Kind {
			i = proto.Reassign(i.Nets, proto.Kind(o.Kind), "")
			i.Routed = countRouted(b, i.Nets)
		}
		if assigned && o.Reference != "" {
			limit := proto.Tolerance{}
			if f, ok := proto.FamilyOf(i.Kind); ok {
				limit = proto.Tolerance{MM: f.GroupMM}
			}
			i.WithReference(o.Reference, limit, "")
		}
		// A tolerance the user set for one of this interface's groups replaces
		// the family's, before anything is judged against it.
		for gi := range i.Groups {
			if mm, ok := groupTol[i.Name+" / "+i.Groups[gi].Name]; ok && mm > 0 {
				i.Groups[gi].Tolerance = proto.Tolerance{MM: mm}
			}
		}
		a := i.Assess(e)
		d := DetectedInterface{
			ID: i.Name, Kind: string(i.Kind), Name: i.Name,
			Nets: i.Total, Routed: i.Routed, Pairs: len(i.Pairs),
			Evidence: i.Evidence, Planner: i.Planner,
			IntraPairMM: i.IntraPair.MM,
			Summary:     a.Summary, Actionable: a.Actionable(),
			Unroutable: a.Unroutable,
		}
		for _, p := range a.Pairs {
			d.PairSkew = append(d.PairSkew, PairSkewInfo{
				Name: proto.Leaf(p.Base), P: p.P, N: p.N,
				SkewMM: p.SkewMM, LimitMM: p.LimitMM,
				Routed: p.Routed, InTolerance: p.InTolerance,
			})
		}
		for _, net := range i.Nets {
			m := e.Measure(net)
			if m == nil || !m.Complete || !m.Longest.Found {
				d.UnroutedNets = append(d.UnroutedNets, label(net))
			}
			if m != nil && len(m.Islands) > 1 {
				for _, q := range islandJoins(b, net, m.Islands) {
					d.Missing = append(d.Missing, MissingConnection{
						Net: net, Label: label(net), From: q[0], To: q[1], Hop: i.Name,
					})
				}
			}
		}
		sort.Strings(d.UnroutedNets)
		// The copper a net's length is measured over, which is also where
		// length may be added: the longest pad-to-pad route. Not all of the
		// net's copper -- a stub or a stranded island is on no timing path, and
		// lengthening it would add copper without changing anything it was
		// added for. The DDR planner makes the same distinction per leg.
		pathOf := func(net string) []string {
			if m := e.Measure(net); m != nil && m.Longest.Found {
				return m.Longest.Tracks
			}
			return nil
		}
		grouped := map[string]bool{}
		for gi, g := range a.Groups {
			why := ""
			if gi < len(i.Groups) {
				why = i.Groups[gi].Why
			}
			gi := GroupSkewInfo{
				Name: g.Name, Reference: label(g.Reference), ReferenceMM: g.ReferenceMM,
				SpreadMM: g.SpreadMM, LimitMM: g.LimitMM, Members: len(g.Members),
				OutOfTol: g.OutOfTol, Unroutable: g.Unroutable, Why: why,
			}
			// The target is the reference: the clock, or the mean of the
			// clock pair when the reference is half of one -- the same rule the
			// DDR planner uses. It is not raised to a member that is longer;
			// that member is too long, and says so.
			gi.TargetMM = g.ReferenceMM
			refNets := map[string]bool{g.Reference: true}
			for _, pr := range i.Pairs {
				var other string
				switch g.Reference {
				case pr.P:
					other = pr.N
				case pr.N:
					other = pr.P
				}
				if other == "" {
					continue
				}
				if m := e.Measure(other); m != nil && m.Longest.Found && g.ReferenceMM > 0 {
					gi.TargetMM = (g.ReferenceMM + m.Longest.Length) / 2
					gi.Reference = label(g.Reference) + " / " + label(other)
					refNets[other] = true
				}
			}
			for _, m := range g.Members {
				grouped[m.Net] = true
				d.netRows = append(d.netRows, judge(NetStatus{
					Net: m.Net, Label: label(m.Net), Interface: i.Name, Group: g.Name,
					Routed: m.Routed, LengthMM: m.LengthMM, Parts: joinedParts(e, m.Net), TargetMM: gi.TargetMM,
					ToleranceMM: g.LimitMM, Reference: refNets[m.Net],
				}))

				// The same row a DDR group shows, for every member rather than
				// only the ones asking for length.
				dev := m.LengthMM - gi.TargetMM
				row := MemberInfo{
					Net: m.Net, Label: label(m.Net), Routed: m.Routed,
					LengthMM: m.LengthMM, Parts: joinedParts(e, m.Net),
					DeviationMM: dev,
					InTolerance: !m.Routed || g.LimitMM <= 0 || math.Abs(dev) <= g.LimitMM,
					Through:     m.Through,
				}
				if refNets[m.Net] {
					row.Role = "reference"
					row.InTolerance = true
					row.DeviationMM = m.LengthMM - gi.TargetMM
				}
				if m.Routed && !row.InTolerance {
					if dev < 0 {
						row.NeedMM = -dev
					} else {
						row.ExcessMM = dev - g.LimitMM
					}
				}
				gi.Rows = append(gi.Rows, row)
			}
			sort.Slice(gi.Rows, func(x, y int) bool { return gi.Rows[x].LengthMM < gi.Rows[y].LengthMM })
			for _, m := range g.Members {
				if !m.Routed || refNets[m.Net] {
					continue
				}
				dev := m.LengthMM - gi.TargetMM
				need := -dev
				excess := dev - g.LimitMM
				if g.LimitMM > 0 && math.Abs(dev) <= g.LimitMM {
					continue
				}
				if need <= 1e-6 && excess <= 1e-6 {
					continue
				}
				if need < 0 {
					need = 0
				}
				if excess < 0 {
					excess = 0
				}
				gi.NeedMM += need
				reroute := excess > 0 || need > 0.25*m.LengthMM
				c := MemberInfo{
					Net: m.Net, Label: label(m.Net), Routed: true,
					LengthMM: m.LengthMM, Parts: joinedParts(e, m.Net), DeviationMM: dev, NeedMM: need,
					NeedsReroute: reroute, pathTracks: pathOf(m.Net),
					Legs: []CandidateLeg{{
						Group: g.Name, LengthMM: m.LengthMM, Parts: joinedParts(e, m.Net), NeedMM: need, ExcessMM: excess,
						TargetMM: gi.TargetMM, NeedsReroute: reroute, pathTracks: pathOf(m.Net),
					}},
				}
				d.Candidates = append(d.Candidates, c)
				d.TotalNeedMM += need
			}
			d.Groups = append(d.Groups, gi)
		}
		// A pair whose halves belong to no group -- USB's D+/D-, a PCIe lane --
		// is matched only to itself: the shorter half is brought up to the
		// longer. Inside a group the group target does that already, the way
		// the DDR planner handles a strobe, so it is not asked for twice.
		for _, p := range a.Pairs {
			// A group may hold only one half of a pair: MIPI matches each data
			// lane's P line to the clock. The other half is still a net somebody
			// clicks, and it is matched to its partner, so it gets that row --
			// otherwise the lookup calls it "not in any length-matched group".
			if grouped[p.P] != grouped[p.N] {
				half, hmm, other, omm := p.N, p.NMM, p.P, p.PMM
				if grouped[p.N] {
					half, hmm, other, omm = p.P, p.PMM, p.N, p.NMM
				}
				d.netRows = append(d.netRows, judge(NetStatus{
					Net: half, Label: label(half), Interface: i.Name,
					Group: "pair " + proto.Leaf(p.Base) + " (to " + label(other) + ")", Pair: true,
					Routed: p.Routed, LengthMM: hmm, Parts: joinedParts(e, half), TargetMM: omm,
					ToleranceMM: p.LimitMM,
				}))
				continue
			}
			if grouped[p.P] || grouped[p.N] {
				continue
			}
			for _, half := range []struct {
				net    string
				length float64
			}{{p.P, p.PMM}, {p.N, p.NMM}} {
				d.netRows = append(d.netRows, judge(NetStatus{
					Net: half.net, Label: label(half.net), Interface: i.Name,
					Group: "pair " + proto.Leaf(p.Base), Pair: true,
					Routed: p.Routed, LengthMM: half.length, Parts: joinedParts(e, half.net), TargetMM: math.Max(p.PMM, p.NMM),
					ToleranceMM: p.LimitMM,
				}))
			}
			if !p.Routed || p.InTolerance {
				continue
			}
			short, length := p.P, p.PMM
			if p.NMM < p.PMM {
				short, length = p.N, p.NMM
			}
			need := p.SkewMM
			// Past the most that may be padded into one half, the pair wants
			// routing differently: the same cap, and the same verdict, as a
			// DDR strobe pair gets.
			reroute := need > 0.25*length || (maxPairFix > 0 && need > maxPairFix)
			group := "pair " + proto.Leaf(p.Base)
			d.Candidates = append(d.Candidates, MemberInfo{
				Net: short, Label: label(short), Routed: true,
				LengthMM: length, Parts: joinedParts(e, short), DeviationMM: -need, NeedMM: need,
				NeedsReroute: reroute, pathTracks: pathOf(short),
				Legs: []CandidateLeg{{
					Group: group, LengthMM: length, Parts: joinedParts(e, short), NeedMM: need,
					NeedsReroute: reroute, pathTracks: pathOf(short),
				}},
			})
			long := map[bool]string{true: p.N, false: p.P}[short == p.P]
			d.Groups = append(d.Groups, GroupSkewInfo{
				Name: group, Reference: label(long),
				ReferenceMM: length + need, SpreadMM: need, LimitMM: p.LimitMM,
				OutOfTol: 1, Members: 2, TargetMM: length + need, NeedMM: need,
				Why: "the two halves of a differential pair are matched to each other; the shorter is brought up to the longer",
				// Both halves, the way any other group lists its members. A
				// pair group used to carry only its counts, and the page, with
				// no rows to show, said the pair was not routed while printing
				// its length a line above.
				Rows: []MemberInfo{
					{
						Net: short, Label: label(short), Routed: true, LengthMM: length,
						Parts: joinedParts(e, short), DeviationMM: -need, NeedMM: need,
						NeedsReroute: reroute, Through: e.Joined(short).Through,
					},
					{
						Net: long, Label: label(long), Role: "reference", Routed: true, LengthMM: length + need,
						Parts: joinedParts(e, long), InTolerance: true, Through: e.Joined(long).Through,
					},
				},
			})
			d.TotalNeedMM += need
		}
		sort.SliceStable(d.Candidates, func(x, y int) bool {
			return d.Candidates[x].NeedMM > d.Candidates[y].NeedMM
		})
		g := i.MeasureGeometry(b)
		if o.WidthMM > 0 {
			g.WidthMM = o.WidthMM
			g.Measured = without(g.Measured, "width")
		}
		if o.GapMM > 0 {
			g.GapMM = o.GapMM
			g.Measured = without(g.Measured, "pair spacing")
		}
		d.Geometry = GeometryInfo{
			WidthMM: g.WidthMM, GapMM: g.GapMM, Layer: g.Layer,
			Measured:   g.Measured,
			NeedsWidth: g.WidthMM <= 0,
			NeedsGap:   len(i.Pairs) > 0 && g.GapMM <= 0,
			Note:       g.Note,
		}
		for _, w := range g.Widths {
			d.Geometry.Widths = append(d.Geometry.Widths, WidthUseInfo{WidthMM: w.WidthMM, LengthMM: w.LengthMM})
		}
		z := i.CheckImpedance(b, g)
		d.Impedance = ImpedanceInfo{
			SingleEndedOhms: round1(z.SingleEnded.Ohms), DiffOhms: round1(z.Differential.Ohms),
			TargetSingleEndedOhms: z.TargetSingleEnded, TargetDiffOhms: z.TargetDifferential,
			TargetSingleEndedMaxOhms: z.TargetSingleEndedMax, TargetDiffMaxOhms: z.TargetDifferentialMax,
			TargetSource: z.TargetSource,
			Computed:     z.SingleEnded.Ohms > 0,
			Microstrip:   z.SingleEnded.Microstrip, InRange: z.SingleEnded.InRange,
			Layer: z.Layer, Note: z.SingleEnded.Note, TargetNote: z.TargetNote,
		}
		if z.Chip != nil {
			d.Impedance.Chip, d.Impedance.ChipRef, d.Impedance.ChipConnected = z.Chip.Chip.Name, z.Chip.Ref, z.Chip.Connected
		}
		d.Assigned = assigned && o.Kind != ""
		d.Evidence = i.Evidence
		d.Kind, d.Name = string(i.Kind), i.Name
		out = append(out, d)
	}
	return out
}

func countRouted(b *board.Board, nets []string) int {
	n := 0
	for _, net := range nets {
		if len(b.TracksOfNet(net)) > 0 {
			n++
		}
	}
	return n
}

func without(ss []string, drop string) []string {
	out := ss[:0]
	for _, s := range ss {
		if s != drop {
			out = append(out, s)
		}
	}
	return out
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// islandJoins is the fewest pad-to-pad connections that join a net's islands
// into one, each between the closest pair of pads on either side.
//
// Built the way a person would route it: start from one island, repeatedly
// join the nearest island not yet reached. That is a minimum spanning tree over
// island distance, which never asks for a connection a shorter one would have
// made unnecessary, and never joins two islands through a third.
func islandJoins(b *board.Board, net string, islands [][]string) [][2]string {
	pos := map[string]geomPt{}
	for _, p := range b.PadsOfNet(net) {
		pos[p.ID()] = geomPt{p.Centre.X, p.Centre.Y}
	}
	in := map[int]bool{0: true}
	var out [][2]string
	for len(in) < len(islands) {
		best, bi := -1.0, -1
		var from, to string
		for i := range islands {
			if !in[i] {
				continue
			}
			for j := range islands {
				if in[j] {
					continue
				}
				for _, a := range islands[i] {
					pa, ok := pos[a]
					if !ok {
						continue
					}
					for _, c := range islands[j] {
						pc, ok := pos[c]
						if !ok {
							continue
						}
						d := math.Hypot(pa.x-pc.x, pa.y-pc.y)
						if best < 0 || d < best {
							best, bi, from, to = d, j, a, c
						}
					}
				}
			}
		}
		if bi < 0 {
			// Pads with no position: nothing more can be said about them.
			break
		}
		in[bi] = true
		out = append(out, [2]string{from, to})
	}
	return out
}

type geomPt struct{ x, y float64 }
