package server

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/geom"
	"github.com/embeddedci-com/pcb-autorouter/netlen"
)

// What the KiCad plugin asks that the web page does not.
//
// The page works on a copy of the board and hands a file back. The plugin works
// on the board open in pcbnew, and needs two things in a different shape: one
// net's standing, looked up by the name KiCad gave it when the user clicked it,
// and the result of an apply as a list of edits -- tracks to delete by uuid and
// tracks to add -- so it can make them as one undoable commit instead of
// replacing the file under the editor.
//
// Both are plain endpoints on the same service, so the site has them too.

// NetStatus is one net's length against what it is matched to.
type NetStatus struct {
	Net   string `json:"net"`
	Label string `json:"label"`

	// Interface and Group say what it is matched against: "DDR" and "byte lane
	// 3", or "ETH1" and "transmit". A fly-by net appears once per leg.
	Interface string `json:"interface"`
	Group     string `json:"group"`
	Leg       string `json:"leg,omitempty"`

	// Pair is true when the row is a differential pair matched only to its
	// other half, so the target is that half's length.
	Pair bool `json:"pair,omitempty"`

	// Reference is true for the net the group is matched to. It is reported
	// but not tuned.
	Reference bool `json:"reference,omitempty"`

	// Asked is set when the net looked up is not this row's net but continues
	// it through a series part: the far side of an AC-coupling cap or a
	// damping resistor. Through names the parts between the two.
	Asked   string   `json:"asked,omitempty"`
	Through []string `json:"through,omitempty"`

	Routed   bool    `json:"routed"`
	LengthMM float64 `json:"length_mm"`
	// Parts is LengthMM taken apart: track, vias, pad entry, package.
	Parts       *LengthParts `json:"parts,omitempty"`
	TargetMM    float64      `json:"target_mm"`
	ToleranceMM float64      `json:"tolerance_mm"`

	// DeviationMM is signed: negative is short.
	DeviationMM float64 `json:"deviation_mm"`
	InTolerance bool    `json:"in_tolerance"`

	// NeedMM is what to add to come inside the band's centre; ExcessMM how
	// much too long it is past the band. At most one is non-zero.
	NeedMM   float64 `json:"need_mm"`
	ExcessMM float64 `json:"excess_mm,omitempty"`
}

// UnmatchedNet is a net asked about that no group measures.
type UnmatchedNet struct {
	Net   string `json:"net"`
	Label string `json:"label"`
	// Exists is false when the board has no net of that name at all.
	Exists bool `json:"exists"`
	// LengthMM is KiCad's own figure for the net: every piece of copper on it,
	// the number the net inspector shows.
	LengthMM float64 `json:"length_mm"`
	Complete bool    `json:"complete"`
}

// NetsResponse answers a lookup.
type NetsResponse struct {
	Nets      []NetStatus    `json:"nets"`
	Unmatched []UnmatchedNet `json:"unmatched,omitempty"`
}

// judge fills in the verdict from length, target and tolerance.
func judge(s NetStatus) NetStatus {
	if !s.Routed {
		return s
	}
	s.DeviationMM = s.LengthMM - s.TargetMM
	s.InTolerance = s.ToleranceMM <= 0 || math.Abs(s.DeviationMM) <= s.ToleranceMM+1e-9
	if !s.InTolerance && !s.Reference {
		s.NeedMM = math.Max(0, -s.DeviationMM)
		s.ExcessMM = math.Max(0, s.DeviationMM-s.ToleranceMM)
	}
	return s
}

// netRows is every matched net on the board, DDR and every other interface.
func netRows(a *Analysis) []NetStatus {
	var out []NetStatus
	for _, g := range a.Groups {
		for _, m := range g.Members {
			s := NetStatus{
				Net: m.Net, Label: m.Label, Interface: "DDR", Group: g.Name, Leg: g.Leg,
				Reference: m.Reference, Routed: m.Routed, LengthMM: m.LengthMM, Parts: m.Parts,
				TargetMM: g.TargetMM, ToleranceMM: g.ToleranceMM,
				DeviationMM: m.DeviationMM, InTolerance: m.InTolerance,
				NeedMM: m.NeedMM, ExcessMM: m.ExcessMM,
			}
			// The DDR planner has already judged it, against a reference that
			// may be a pair's mean; its verdict stands rather than being
			// recomputed here from the rounded figures.
			if !s.Routed {
				s.InTolerance = false
			}
			out = append(out, s)
		}
	}
	for _, i := range a.Interfaces {
		if i.Planner != "" {
			continue // DDR, already covered from its own plan
		}
		out = append(out, i.netRows...)
	}
	return out
}

// lookupNets picks rows by name. A name matches a net exactly, or failing that
// by the label the report shows -- "DQ23" for "/ddr4/DDR_DQ23" -- when exactly
// one net carries it.
func lookupNets(rows []NetStatus, b *board.Board, e *netlen.Engine, names []string, attention bool) NetsResponse {
	resp := NetsResponse{Nets: []NetStatus{}}
	keep := func(s NetStatus) bool { return !attention || (s.Routed && !s.InTolerance && !s.Reference) }
	if len(names) == 0 {
		for _, s := range rows {
			if keep(s) {
				resp.Nets = append(resp.Nets, s)
			}
		}
		sortRows(resp.Nets)
		return resp
	}

	boardNets := map[string]bool{}
	byLabel := map[string][]string{}
	for _, n := range b.Nets() {
		boardNets[n] = true
		byLabel[label(n)] = append(byLabel[label(n)], n)
	}
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		net := name
		if !boardNets[net] {
			if cands := byLabel[label(name)]; len(cands) == 1 {
				net = cands[0]
			}
		}
		found := false
		for _, s := range rows {
			if s.Net == net {
				found = true
				if keep(s) {
					resp.Nets = append(resp.Nets, s)
				}
			}
		}
		if found {
			continue
		}
		// The far side of a series part. A PCIe lane is two nets joined by
		// its AC-coupling caps, and the one clicked is as likely to be the
		// connector side as the side the interface is named on. The row is the
		// signal's, measured across both.
		if boardNets[net] {
			if hits := continuing(rows, e, net); len(hits) > 0 {
				for _, s := range hits {
					if keep(s) {
						resp.Nets = append(resp.Nets, s)
					}
				}
				continue
			}
		}
		u := UnmatchedNet{Net: net, Label: label(net), Exists: boardNets[net]}
		if u.Exists {
			if m := e.Measure(net); m != nil {
				u.LengthMM, u.Complete = m.KiCadLength, m.Complete
			}
		}
		resp.Unmatched = append(resp.Unmatched, u)
	}
	sortRows(resp.Nets)
	return resp
}

// continuing returns the rows whose signal runs on through series parts onto
// net, each marked with what was asked and the parts in between.
func continuing(rows []NetStatus, e *netlen.Engine, net string) []NetStatus {
	var out []NetStatus
	joined := map[string]*netlen.Joined{}
	for _, s := range rows {
		j, ok := joined[s.Net]
		if !ok {
			j = e.Joined(s.Net)
			joined[s.Net] = j
		}
		for k, seg := range j.Segments {
			if k == 0 || seg != net {
				continue
			}
			// Segments[k] was reached across Through[k-1]; on a chain the
			// parts before it are the ones in between.
			s.Asked = net
			s.Through = append([]string(nil), j.Through[:k]...)
			out = append(out, s)
		}
	}
	return out
}

// sortRows puts the worst first: what is out of tolerance by the most, then
// by name, so the "needs attention" list reads as a work order.
func sortRows(rows []NetStatus) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.InTolerance != b.InTolerance {
			return !a.InTolerance
		}
		da, db := a.NeedMM+a.ExcessMM, b.NeedMM+b.ExcessMM
		if math.Abs(da-db) > 1e-9 {
			return da > db
		}
		if a.Net != b.Net {
			return a.Net < b.Net
		}
		return a.Leg < b.Leg
	})
}

// handleNets looks up nets by name, or lists what needs attention.
//
//	GET .../nets?name=/ddr4/DDR_DQ23&name=CLK_P   those nets
//	GET .../nets?attention=1                      every net out of tolerance
//	GET .../nets                                  every matched net
//
// Names go in the query rather than the path because KiCad's hierarchical net
// names are full of slashes.
func (s *Service) handleNets(w http.ResponseWriter, r *http.Request) {
	_, sess, ok := s.session(w, r)
	if !ok {
		return
	}
	b, proj, err := s.reload(r, sess)
	if err != nil {
		s.fail(w, r, http.StatusNotFound, err)
		return
	}
	analysis, _, err := analyse(b, proj, sess.Filename, sess.Params, nil)
	if err != nil {
		s.fail(w, r, http.StatusBadRequest, err)
		return
	}
	q := r.URL.Query()
	attention := q.Get("attention") == "1" || q.Get("attention") == "true"
	writeJSON(w, http.StatusOK, lookupNets(netRows(analysis), b, netlen.New(b), q["name"], attention))
}

// ChangeItem is one piece of copper, in KiCad's internal units.
//
// Nanometres as integers because that is what pcbnew stores and what its API
// takes: a millimetre float rounded on the far side could land a meander's end
// a nanometre off the track it has to join.
type ChangeItem struct {
	// UUID is set on items to remove. Items to add have none: pcbnew assigns
	// one when it creates them.
	UUID  string `json:"uuid,omitempty"`
	Kind  string `json:"kind"` // segment, arc or via
	Net   string `json:"net"`
	Layer string `json:"layer,omitempty"`

	Start [2]int64  `json:"start_nm"`
	End   [2]int64  `json:"end_nm"`
	Mid   *[2]int64 `json:"mid_nm,omitempty"`

	WidthNM int64 `json:"width_nm,omitempty"`

	// Vias only.
	SizeNM      int64  `json:"size_nm,omitempty"`
	DrillNM     int64  `json:"drill_nm,omitempty"`
	LayerTop    string `json:"layer_top,omitempty"`
	LayerBottom string `json:"layer_bottom,omitempty"`
}

// ChangesResponse is an apply as edits.
type ChangesResponse struct {
	// Remove carries each item's geometry as well as its uuid, so the plugin
	// can refuse when the track in the editor is no longer the track that was
	// analysed -- moved, re-widthed, or on another layer -- and not only when
	// it has been deleted.
	Remove []ChangeItem `json:"remove"`
	Add    []ChangeItem `json:"add"`

	// Nets is every net the edits touch.
	Nets []string `json:"nets"`

	// Message is a one-line description, for the undo history.
	Message string `json:"message"`
}

// ErrNotApplied is returned when there is no result to express as edits.
var ErrNotApplied = errors.New("nothing has been applied to this board yet")

// handleChanges expresses the last apply as edits to the uploaded board.
func (s *Service) handleChanges(w http.ResponseWriter, r *http.Request) {
	_, sess, ok := s.session(w, r)
	if !ok {
		return
	}
	// The router replaces the session's board with the routed one, so the
	// board this would diff against is no longer the one the user has open.
	if sess.Routed != nil {
		s.fail(w, r, http.StatusConflict, errors.New(
			"this board has been routed here, so its edits cannot be expressed against the board in KiCad; "+
				"download the result instead"))
		return
	}
	before, err := s.deps.Store.Board(r.Context(), sess.ID)
	if err != nil {
		s.fail(w, r, http.StatusNotFound, err)
		return
	}
	after, err := s.deps.Store.Result(r.Context(), sess.ID)
	if err != nil {
		s.fail(w, r, http.StatusNotFound, ErrNotApplied)
		return
	}
	b0, err := board.Parse(before, sess.Filename)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}
	b1, err := board.Parse(after, sess.Filename)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}
	resp := diffTracks(b0, b1)
	if sess.Applied != nil {
		resp.Message = fmt.Sprintf("Length-match %d net(s), +%.3f mm", len(sess.Applied.Nets), sess.Applied.AddedMM)
	}
	writeJSON(w, http.StatusOK, resp)
}

// diffTracks lists the copper that differs between two versions of a board.
//
// An item is unchanged when the same uuid carries the same geometry on both
// sides. The tuner never edits a track in place -- folding a meander in
// replaces the whole track under new uuids -- but a changed item under an old
// uuid is still reported as a removal and an addition rather than trusted.
func diffTracks(before, after *board.Board) ChangesResponse {
	resp := ChangesResponse{Remove: []ChangeItem{}, Add: []ChangeItem{}, Nets: []string{}}
	old := make(map[string]ChangeItem, len(before.Tracks))
	for _, t := range before.Tracks {
		old[t.UUID] = changeItem(t)
	}
	kept := map[string]bool{}
	nets := map[string]bool{}
	for _, t := range after.Tracks {
		c := changeItem(t)
		if o, ok := old[t.UUID]; ok && t.UUID != "" && sameItem(o, c) {
			kept[t.UUID] = true
			continue
		}
		c.UUID = ""
		resp.Add = append(resp.Add, c)
		nets[c.Net] = true
	}
	for _, t := range before.Tracks {
		if kept[t.UUID] {
			continue
		}
		resp.Remove = append(resp.Remove, old[t.UUID])
		nets[t.Net] = true
	}
	for n := range nets {
		resp.Nets = append(resp.Nets, n)
	}
	sort.Strings(resp.Nets)
	return resp
}

func nm(v float64) int64 { return int64(math.Round(v * 1e6)) }

func pt(p geom.Pt) [2]int64 { return [2]int64{nm(p.X), nm(p.Y)} }

func changeItem(t *board.Track) ChangeItem {
	c := ChangeItem{UUID: t.UUID, Kind: t.Kind.String(), Net: t.Net, Start: pt(t.Start), End: pt(t.End)}
	switch t.Kind {
	case board.KindVia:
		c.SizeNM, c.DrillNM = nm(t.Size), nm(t.Drill)
		c.LayerTop, c.LayerBottom = t.LayerTop, t.LayerBottom
	case board.KindArc:
		m := pt(t.Mid)
		c.Mid = &m
		fallthrough
	default:
		c.Layer, c.WidthNM = t.Layer, nm(t.Width)
	}
	return c
}

func sameItem(a, b ChangeItem) bool {
	if a.Kind != b.Kind || a.Net != b.Net || a.Layer != b.Layer || a.Start != b.Start || a.End != b.End ||
		a.WidthNM != b.WidthNM || a.SizeNM != b.SizeNM || a.DrillNM != b.DrillNM ||
		a.LayerTop != b.LayerTop || a.LayerBottom != b.LayerBottom {
		return false
	}
	if (a.Mid == nil) != (b.Mid == nil) {
		return false
	}
	return a.Mid == nil || *a.Mid == *b.Mid
}
