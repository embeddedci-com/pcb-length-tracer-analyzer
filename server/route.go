package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/ddr"
	"github.com/embeddedci-com/pcb-autorouter/netlen"
	"github.com/embeddedci-com/pcb-autorouter/pkglen"
	"github.com/embeddedci-com/pcb-autorouter/route"
)

// Making the copper that is missing, when the user asks for it.
//
// Everything else here edits copper that exists. This writes new copper, which
// is a different kind of change: lengthening a trace cannot alter what a board
// does electrically, and adding one can. So it is a request of its own, it is
// never part of an apply, and the front end asks before calling it.
//
// It is also, honestly, not very good yet. On the demo board it makes 2 of the
// 52 connections the fly-by chain is missing; the other 50 fail for the same
// reason, which is that the corridor between the two devices is full of copper
// belonging to other nets and this router only ever rips up its own work.
// Moving somebody else's routing is their decision, not the tool's. That is
// not a bug to be fixed by trying harder: it is what a board looks like when
// the space for a bus was never left.
//
// So the response says exactly what happened per hop, including why each
// failure failed, and the front end offers this second -- after "route it in
// KiCad", which is the answer that works.

// RouteHopResult is what happened to one connection.
type RouteHopResult struct {
	Net   string `json:"net"`
	Label string `json:"label"`

	// From and To are the pads, as "U4.K3".
	From string `json:"from"`
	To   string `json:"to"`

	Routed   bool    `json:"routed"`
	LengthMM float64 `json:"length_mm,omitempty"`
	Vias     int     `json:"vias,omitempty"`

	// Attempts is how many paths were tried. More than one means the grid
	// proposed something the real clearance check then rejected.
	Attempts int `json:"attempts,omitempty"`

	// Reason says why it did not route.
	Reason string `json:"reason,omitempty"`
}

// RouteResponse is what the router made of the missing hops.
type RouteResponse struct {
	Requested int `json:"requested"`
	Connected int `json:"connected"`

	AddedMM float64 `json:"added_mm"`
	Vias    int     `json:"vias"`

	Hops []RouteHopResult `json:"hops,omitempty"`

	// Changed is true when copper was written, which is also when the board in
	// this session was replaced by the routed one.
	Changed bool `json:"changed"`

	// After is the board re-measured, so the chain and the groups say what
	// they say now rather than what they said before the copper existed.
	After *Analysis `json:"after,omitempty"`

	Session *Session `json:"session,omitempty"`

	// Notes are what the caller has to know about this result.
	Notes []string `json:"notes,omitempty"`
}

// RouteRequest limits a run to some nets. Empty means every missing
// connection on the board.
type RouteRequest struct {
	Nets []string `json:"nets,omitempty"`
}

// maxRouteRequests bounds one run of the router.
const maxRouteRequests = 64

func (s *Service) handleRoute(w http.ResponseWriter, r *http.Request) {
	_, sess, ok := s.session(w, r)
	if !ok {
		return
	}
	release, ok := s.job(w, r, routeJob)
	if !ok {
		return
	}
	defer release()
	b, proj, err := s.reload(r, sess)
	if err != nil {
		s.fail(w, r, http.StatusNotFound, err)
		return
	}
	var req RouteRequest
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req); err != nil {
			s.fail(w, r, http.StatusBadRequest, err)
			return
		}
	}
	only := map[string]bool{}
	for _, n := range req.Nets {
		only[n] = true
	}
	keep := func(net string) bool { return len(only) == 0 || only[net] }

	// Every connection the board is missing, on every interface: the fly-by
	// chain's hops where there is DDR, and the island-to-island joins of every
	// other interface's nets. A USB pair with no copper is the same request
	// to the router as a missing address hop -- pad to pad -- and the same
	// honesty about whether it could be made.
	var reqs []route.Request
	if iface, plan, err := classifyDDR(b, sess.Params); err == nil {
		for _, q := range ddr.MissingHops(b, iface, plan) {
			if keep(q.Net) {
				reqs = append(reqs, q)
			}
		}
	}
	analysis, _, err := analyse(b, proj, sess.Filename, sess.Params, nil)
	if err != nil {
		s.fail(w, r, http.StatusBadRequest, err)
		return
	}
	for _, i := range analysis.Interfaces {
		if i.Planner != "" {
			continue
		}
		for _, m := range i.Missing {
			if keep(m.Net) {
				reqs = append(reqs, route.Request{Net: m.Net, From: m.From, To: m.To})
			}
		}
	}

	out := RouteResponse{Requested: len(reqs)}
	if len(reqs) == 0 {
		out.Notes = append(out.Notes,
			"every net asked about is already joined end to end, so there is nothing here for "+
				"the router to connect")
		writeJSON(w, http.StatusOK, out)
		return
	}
	// A search over the whole board for a hundred nets is minutes, not
	// seconds, and a request that long is one the browser gives up on. So a
	// batch at a time, said out loud, and the rest on the next press.
	if len(reqs) > maxRouteRequests {
		out.Notes = append(out.Notes, fmt.Sprintf(
			"%d connections are missing; this run tries the first %d, and running it again "+
				"carries on with the rest", len(reqs), maxRouteRequests))
		reqs = reqs[:maxRouteRequests]
		out.Requested = len(reqs)
	}
	opt := route.DefaultOptions()
	// The board's own rules, which is what makes the difference between "no
	// way out of this BGA" and the ball being escapable at all.
	opt.Rules = customRules(b, proj)
	if sess.Project != nil && sess.Project.HasCustomRules && opt.Rules == nil {
		out.Notes = append(out.Notes,
			"this board has custom design rules (.kicad_dru) that could not be read, so the "+
				"router worked to the net classes at their strictest everywhere: a hop it could "+
				"not make may be one your own rules would have allowed")
	}

	// Stop the search when the caller goes away or it runs too long: the
	// router holds its grid and search state until it returns.
	ctx, cancel := context.WithTimeout(r.Context(), s.deps.routeTimeout())
	defer cancel()
	opt.Stop = ctx.Done()

	results, err := runRouter(b, proj, reqs, opt)
	switch {
	case errors.Is(err, route.ErrStopped) && r.Context().Err() != nil:
		return // nobody is waiting for the answer
	case errors.Is(err, route.ErrStopped):
		out.Notes = append(out.Notes, fmt.Sprintf(
			"the router stopped after %s; what it connected so far is kept, and running it again "+
				"carries on with the rest", s.deps.routeTimeout()))
	case err != nil:
		s.fail(w, r, http.StatusBadRequest, err)
		return
	}
	for _, res := range results {
		out.Hops = append(out.Hops, RouteHopResult{
			Net: res.Net, Label: label(res.Net), From: res.From, To: res.To,
			Routed: res.Routed, LengthMM: res.LengthMM, Vias: res.Vias,
			Attempts: res.Attempts, Reason: res.Reason,
		})
		if res.Routed {
			out.Connected++
			out.AddedMM += res.LengthMM
			out.Vias += res.Vias
		}
	}
	sort.Slice(out.Hops, func(i, j int) bool {
		if out.Hops[i].Routed != out.Hops[j].Routed {
			return out.Hops[i].Routed
		}
		return out.Hops[i].Net < out.Hops[j].Net
	})

	out.Changed = out.Connected > 0
	if !out.Changed {
		out.Notes = append(out.Notes,
			"nothing was written. A hop the router cannot find a way through is a placement or a "+
				"layer decision rather than a routing one: the corridor between those pads is full "+
				"of copper belonging to other nets, and this router only ever rips up its own work")
		out.Session = sess
		writeJSON(w, http.StatusOK, out)
		return
	}

	// The routed board becomes the board this session works on. It has to:
	// every length on those nets is different now, and a plan measured against
	// the old copper would be matching a topology the board no longer has.
	// The user's own file is untouched -- this is a copy taken at upload --
	// and the routed board is offered as a download at the same time.
	routed := b.Bytes()
	if err := s.deps.Store.Put(r.Context(), sess, routed); err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}
	if err := s.deps.Store.PutResult(r.Context(), sess.ID, routed); err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}
	// Any earlier tuning was done to copper that has since changed, so the
	// record of it would describe a board nobody has any more.
	sess.Applied = nil
	sess.BoardBytes = len(routed)
	sess.Routed = &RouteRecord{
		At: s.deps.now(), Requested: out.Requested, Connected: out.Connected,
		AddedMM: out.AddedMM, Vias: out.Vias, ResultBytes: len(routed),
	}
	// Named after the record exists, because what the download is called
	// depends on what it is: a board that has only been routed is not a tuned
	// one.
	sess.Routed.ResultFilename = resultNameFor(sess)
	if err := s.deps.Store.Update(r.Context(), sess); err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}

	after, _, err := analyse(b, proj, sess.Filename, sess.Params, nil)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}
	out.After = after
	out.Session = sess
	out.Notes = append(out.Notes,
		"the board in this session is now the routed one, so everything below is measured over "+
			"the new copper. Download it and check it in KiCad before you trust it: this router "+
			"proposes a path on a grid and accepts it against the real clearances, which is not "+
			"the same as a human deciding where a bus should run")
	if out.Connected < out.Requested {
		out.Notes = append(out.Notes, fmt.Sprintf(
			"%d of %d hops were not made, so the chain is still incomplete and what the groups "+
				"below report is the matching of the hops that do exist",
			out.Requested-out.Connected, out.Requested))
	}
	s.deps.log().Info("board routed",
		"session", sess.ID, "requested", out.Requested, "connected", out.Connected,
		"added_mm", out.AddedMM)
	writeJSON(w, http.StatusOK, out)
}

// classifyDDR reads the DDR interface and plans it, which is the DDR half of
// analyse without the report around it.
func classifyDDR(b *board.Board, p Params) (*ddr.Interface, *ddr.Plan, error) {
	p = p.withDefaults()
	if err := p.Validate(); err != nil {
		return nil, nil, err
	}
	pkglen.Apply(b)
	prefix, scoped := ddr.Scope(b, p.NetPrefix)
	if prefix == "" && scoped == nil {
		return nil, nil, errors.New("there is no DDR on this board, and the fly-by chain is the " +
			"only thing this router is asked to make")
	}
	iface, err := ddr.Classify(b, ddr.Options{NetPrefix: prefix, Nets: scoped, Controller: p.Controller})
	if err != nil {
		return nil, nil, err
	}
	if p.PackagePart != "" {
		pkglen.UsePart(b, iface.Controller, p.PackagePart)
	}
	pkglen.Override(b, iface.Controller, ifaceNets(iface), p.PackageLengthsMM)
	plan, err := ddr.BuildPlan(iface, netlen.New(b), p.rules())
	if err != nil {
		return nil, nil, err
	}
	return iface, plan, nil
}

// runRouter is a seam for tests, which have no need to search a real board.
var runRouter = func(b *board.Board, proj *board.Project, reqs []route.Request, opt route.Options) ([]*route.Result, error) {
	r, err := route.New(b, proj, reqs, opt)
	if err != nil {
		return nil, err
	}
	return r.Route(reqs)
}
