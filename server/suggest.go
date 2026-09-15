package server

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/embeddedci-com/pcb-autorouter/ddr"

	"github.com/embeddedci-com/pcb-autorouter/preview"
	"github.com/embeddedci-com/pcb-autorouter/tune"
)

// Proposing the areas instead of leaving them to be invented.
//
// "Draw where copper may go" is a worse question than it looks on a blank
// board: the answer depends on where the nets that need length run and where
// there happens to be space beside them, and both are things this has already
// measured and the user would otherwise have to hunt for. Starting from nothing
// means drawing a rectangle, being told it holds 0.000 mm, and trying again.
//
// So the same room profile that answers "is this area enough" also answers
// "where would you have put it", and the user adjusts rather than guesses.
// Nothing here is a recommendation about the board -- this still has no idea
// whether a region is under a connector or over a split in a plane. It says
// where the length could come from.

// SuggestedArea is a region worth drawing, in the viewer's coordinates.
type SuggestedArea struct {
	Area

	// GainMM is the length the stretches inside it could supply, and Nets is
	// how many nets contribute to that.
	GainMM float64 `json:"gain_mm"`
	Nets   int     `json:"nets"`
}

// SuggestResponse is what the tool would draw.
type SuggestResponse struct {
	Areas []SuggestedArea `json:"areas"`

	// TotalNeedMM is what the candidates need altogether, so the proposal can
	// be read against it.
	TotalNeedMM float64 `json:"total_need_mm"`

	// Note says what these are and are not.
	Note string `json:"note"`
}

// suggestMargin is how close two stretches must be to count as one place on the
// board. Generous on purpose: an area the user will adjust anyway is better a
// little too large than delivered in four pieces.
const suggestMargin = 2.0

// maxSuggestions keeps the proposal to something a person can look at. Beyond a
// handful of regions the answer stops being "here is where to start" and
// becomes another list to work through.
const maxSuggestions = 6

func (s *Service) handleSuggestAreas(w http.ResponseWriter, r *http.Request) {
	_, sess, ok := s.session(w, r)
	if !ok {
		return
	}
	release, ok := s.job(w, r, heavyJob)
	if !ok {
		return
	}
	defer release()
	b, proj, err := s.reload(r, sess)
	if err != nil {
		s.fail(w, r, http.StatusNotFound, err)
		return
	}

	// Measured with no areas in force: the question is what the board offers,
	// not what is allowed under the areas drawn so far.
	surveyor := tune.NewTuner(b, proj, sess.Params.style())
	surveyor.SetRules(customRules(b, proj))
	analysis, plan, err := analyse(b, proj, sess.Filename, sess.Params, nil)
	if err != nil {
		s.fail(w, r, http.StatusBadRequest, err)
		return
	}
	surveyor.SetOpenSpacing(analysis.spacing)

	// Every net that needs length, on every interface: the room beside an
	// Ethernet track is found the same way as the room beside a DQ bit, and a
	// proposal that ignored it would send somebody drawing areas for half the
	// board.
	var spots []tune.Spot
	add := func(c MemberInfo, tracks []string) {
		on := map[string]bool{}
		for _, id := range tracks {
			on[id] = true
		}
		spots = append(spots, surveyor.Spots(c.Net, on)...)
	}
	need := analysis.TotalNeedMM
	for _, c := range analysis.Candidates {
		add(c, trackIDs(plan, c.Net))
	}
	for _, i := range analysis.Interfaces {
		if i.Planner != "" {
			continue
		}
		for _, c := range i.Candidates {
			for _, l := range c.Legs {
				add(c, l.pathTracks)
			}
		}
		need += i.TotalNeedMM
	}
	if need <= 1e-9 {
		writeJSON(w, http.StatusOK, SuggestResponse{
			Note: "nothing on this board needs length under these parameters, so there is " +
				"nowhere to propose an area for",
		})
		return
	}

	tf, ok := preview.ExtentOf(b)
	if !ok {
		s.fail(w, r, http.StatusBadRequest, errors.New("this board has no extent to place an area on"))
		return
	}
	out := SuggestResponse{
		TotalNeedMM: need,
		Note: "these are the stretches where a net that needs length has room beside it, " +
			"clustered. They say where the length could come from, not whether the region is " +
			"a good place for copper -- this cannot see a connector, a plane split, or the " +
			"part of the board you were keeping clear.",
	}
	// Below a fraction of what is needed, a region is noise: the demo board
	// proposes one worth 0.158 mm and 0.8 mm across, which nobody is going to
	// draw or adjust. Proportional rather than absolute, because a board that
	// needs 3 mm in total has a different idea of small.
	floor := need / 100
	dropped := 0
	for _, sg := range tune.Suggest(spots, suggestMargin, 0) {
		if sg.GainMM < floor {
			dropped++
			continue
		}
		if len(out.Areas) >= maxSuggestions {
			dropped++
			continue
		}
		// Back into the coordinates the user draws in, so a proposal and a
		// hand-drawn area are the same kind of thing.
		r := tf.RectToBoard(sg.Area)
		out.Areas = append(out.Areas, SuggestedArea{
			Area:   Area{MinX: r.MinX, MinY: r.MinY, MaxX: r.MaxX, MaxY: r.MaxY},
			GainMM: sg.GainMM, Nets: sg.Nets,
		})
	}
	if dropped > 0 {
		out.Note += fmt.Sprintf(" %d smaller region(s) were left out as too little to be worth drawing.", dropped)
	}
	writeJSON(w, http.StatusOK, out)
}

// trackIDs is the copper a net's measured route runs over, which is where its
// length may be added and nowhere else.
func trackIDs(plan *ddr.Plan, net string) []string {
	if plan == nil {
		return nil
	}
	for _, g := range plan.Groups {
		for _, m := range g.Members {
			if m.Net == net {
				return m.PathTracks
			}
		}
	}
	return nil
}
