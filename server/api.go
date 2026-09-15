package server

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/ddr"
	"github.com/embeddedci-com/pcb-autorouter/pkglen"
	"github.com/embeddedci-com/pcb-autorouter/tune"
)

// SessionResponse is what the upload and read endpoints return.
type SessionResponse struct {
	Session  *Session  `json:"session"`
	Analysis *Analysis `json:"analysis"`
}

// handleUpload takes a board, reads it, and returns the report.
//
// The report comes back from the upload itself rather than from a second
// request. Reading a board and measuring its whole DDR interface takes about
// 150 ms, so there is nothing to wait for, and a user who has just uploaded a
// board wants to see what is in it.
func (s *Service) handleUpload(w http.ResponseWriter, r *http.Request) {
	u, ok := s.user(w, r)
	if !ok {
		return
	}
	max := s.deps.maxUpload()
	r.Body = http.MaxBytesReader(w, r.Body, max+1<<20)
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		// MaxBytesReader trips during parsing for a body well over the limit,
		// which is a size problem and not a malformed request.
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) || strings.Contains(err.Error(), "request body too large") {
			s.fail(w, r, http.StatusRequestEntityTooLarge,
				fmt.Errorf("%w: the limit is %d bytes", ErrTooLarge, max))
			return
		}
		s.fail(w, r, http.StatusBadRequest, fmt.Errorf("could not read the upload: %w", err))
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	file, header, err := r.FormFile("board")
	if err != nil {
		s.fail(w, r, http.StatusBadRequest, errors.New("attach the .kicad_pcb file as the 'board' field"))
		return
	}
	defer file.Close()

	raw, err := io.ReadAll(io.LimitReader(file, max+1))
	if err != nil {
		s.fail(w, r, http.StatusBadRequest, fmt.Errorf("could not read the upload: %w", err))
		return
	}
	if int64(len(raw)) > max {
		s.fail(w, r, http.StatusRequestEntityTooLarge,
			fmt.Errorf("%w: %d bytes, the limit is %d", ErrTooLarge, len(raw), max))
		return
	}
	name := safeFilename(header.Filename)
	if ext := strings.ToLower(filepath.Ext(name)); ext != ".kicad_pcb" {
		s.fail(w, r, http.StatusBadRequest,
			fmt.Errorf("%w: expected a .kicad_pcb file, got %q", ErrBadBoard, name))
		return
	}

	// Refuse before storing, not after: a file that will not parse is not
	// worth keeping, and the user needs to know now.
	b, proj, hasProject, err := loadUploaded(raw, name, r)
	if err != nil {
		s.fail(w, r, http.StatusBadRequest, err)
		return
	}

	// One user may not fill storage with abandoned boards.
	if existing, err := s.deps.Store.ListByUser(r.Context(), u.UserID, 0); err == nil {
		if n := s.deps.maxSessions(); len(existing) >= n {
			// Drop the oldest rather than refusing: the user is working, and
			// the thing they want least is to be told to tidy up first.
			for i := len(existing) - 1; i >= n-1 && i >= 0; i-- {
				_ = s.deps.Store.Delete(r.Context(), existing[i].ID)
			}
		}
	}

	params := DefaultParams()
	params.NetPrefix = r.FormValue("net_prefix")
	params.Controller = r.FormValue("controller")

	analysis, _, err := analyse(b, proj, name, params, nil)
	if err != nil {
		s.fail(w, r, http.StatusBadRequest, err)
		return
	}

	now := s.deps.now()
	sess := &Session{
		ID: newID(), UserID: u.UserID, OrganizationID: u.OrganizationID,
		Filename: name, BoardBytes: len(raw),
		CreatedAt: now, ExpiresAt: now.Add(s.deps.sessionTTL()),
		Params: analysis.Params, Project: proj, HasProjectFile: hasProject,
	}
	if err := s.deps.Store.Put(r.Context(), sess, raw); err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}
	s.deps.log().Info("board uploaded",
		"session", sess.ID, "user", u.UserID, "file", name, "bytes", len(raw),
		"nets", analysis.Interface.NetsFound, "candidates", len(analysis.Candidates))
	writeJSON(w, http.StatusCreated, SessionResponse{Session: sess, Analysis: analysis})
}

// loadUploaded parses the board, and picks up the project and rules files if
// the form carried them.
//
// The project file is what carries the net classes, and without it every
// clearance resolves to the board minimum -- stricter than a real design, not
// looser, but also not what the designer set. The upload form asks for it and
// the report states whether it arrived, because the answer changes what the
// clearance check means.
func loadUploaded(raw []byte, name string, r *http.Request) (*board.Board, *board.Project, bool, error) {
	b, err := board.Parse(raw, name)
	if err != nil {
		return nil, nil, false, fmt.Errorf("%w: %v", ErrBadBoard, err)
	}
	proj := board.Defaults()
	hasProject := false
	if f, _, err := r.FormFile("project"); err == nil {
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, 8<<20))
		if err == nil && len(bytes.TrimSpace(data)) > 0 {
			p, perr := board.ParseProject(data, name)
			if perr != nil {
				return nil, nil, false, fmt.Errorf("the .kicad_pro file could not be read: %w", perr)
			}
			proj = p
			hasProject = true
		}
	}
	if f, _, err := r.FormFile("rules"); err == nil {
		defer f.Close()
		if data, err := io.ReadAll(io.LimitReader(f, 4<<20)); err == nil && len(bytes.TrimSpace(data)) > 0 {
			// Parsed here only to refuse a file that is not a .kicad_dru at
			// the point the user can still do something about it. What the
			// later requests use is re-parsed against the board they hold,
			// because a rule about a courtyard means nothing without one.
			if _, err := board.ParseRules(data, b); err != nil {
				return nil, nil, false, fmt.Errorf("the .kicad_dru file could not be read: %w", err)
			}
			proj.HasCustomRules = true
			proj.CustomRules = string(data)
		}
	}
	b.UseHeightForLength = proj.UseHeightForLength
	return b, proj, hasProject, nil
}

// reload rebuilds the board and project for a stored session.
func (s *Service) reload(r *http.Request, sess *Session) (*board.Board, *board.Project, error) {
	raw, err := s.deps.Store.Board(r.Context(), sess.ID)
	if err != nil {
		return nil, nil, err
	}
	b, err := board.Parse(raw, sess.Filename)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrBadBoard, err)
	}
	proj := sess.Project
	if proj == nil {
		proj = board.Defaults()
	}
	b.UseHeightForLength = proj.UseHeightForLength
	return b, proj, nil
}

// customRules are the board's own .kicad_dru, re-read against this copy of the
// board.
//
// They can only tighten what the tuner draws to, never loosen it -- a meander
// has no business being crammed into a BGA fanout because a rule there allows
// it -- but they are what the router works to, and a board whose rules relax
// clearance inside a courtyard cannot escape a ball without them.
//
// A file that no longer parses is not worth failing a request over: it was
// parsed once at upload, and the strictest reading of the net classes is the
// safe fallback.
func customRules(b *board.Board, proj *board.Project) *board.Rules {
	if proj == nil || proj.CustomRules == "" {
		return nil
	}
	r, err := board.ParseRules([]byte(proj.CustomRules), b)
	if err != nil {
		return nil
	}
	return r
}

func (s *Service) handleGet(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, SessionResponse{Session: sess, Analysis: analysis})
}

func (s *Service) handleList(w http.ResponseWriter, r *http.Request) {
	u, ok := s.user(w, r)
	if !ok {
		return
	}
	rows, err := s.deps.Store.ListByUser(r.Context(), u.UserID, 50)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}
	if rows == nil {
		rows = []*Session{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": rows})
}

func (s *Service) handleDelete(w http.ResponseWriter, r *http.Request) {
	_, sess, ok := s.session(w, r)
	if !ok {
		return
	}
	if err := s.deps.Store.Delete(r.Context(), sess.ID); err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleDefaults(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.user(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"params":              DefaultParams(),
		"max_upload_bytes":    s.deps.maxUpload(),
		"session_ttl_seconds": int(s.deps.sessionTTL().Seconds()),
		// The families the tool can describe, so the client can offer them
		// when the user says what an interface actually is. Each carries the
		// signal names a board usually gives it, which is how somebody
		// recognises their own nets in the list.
		"families": KnownFamilies(),
		// The parts with a package length table, for choosing one by hand.
		"package_parts": pkglen.Parts(),
	})
}

// PlanRequest changes the parameters and re-reports.
type PlanRequest struct {
	Params Params `json:"params"`
}

// handlePlan re-analyses the stored board with new parameters.
//
// The parameters are saved, so the apply that follows uses exactly what the
// user was looking at when they decided.
func (s *Service) handlePlan(w http.ResponseWriter, r *http.Request) {
	_, sess, ok := s.session(w, r)
	if !ok {
		return
	}
	var req PlanRequest
	if err := decodeJSON(r, &req); err != nil {
		s.fail(w, r, http.StatusBadRequest, err)
		return
	}
	b, proj, err := s.reload(r, sess)
	if err != nil {
		s.fail(w, r, http.StatusNotFound, err)
		return
	}
	analysis, _, err := analyse(b, proj, sess.Filename, req.Params, nil)
	if err != nil {
		s.fail(w, r, http.StatusBadRequest, err)
		return
	}
	sess.Params = analysis.Params
	if err := s.deps.Store.Update(r.Context(), sess); err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, SessionResponse{Session: sess, Analysis: analysis})
}

// ApplyRequest is the confirmation step: which nets to change.
type ApplyRequest struct {
	// Params, if given, replace the session's. Sending them with the apply
	// means the client cannot accidentally confirm one plan and apply another.
	Params *Params `json:"params,omitempty"`

	// Nets to lengthen. Empty means every candidate the plan found, which is
	// the "apply all" button.
	Nets []string `json:"nets,omitempty"`

	// Groups to lengthen, by name, as an alternative to naming nets.
	Groups []string `json:"groups,omitempty"`
}

// NetResult is what happened to one net.
type NetResult struct {
	Net   string `json:"net"`
	Label string `json:"label"`

	// Leg names the span this row is about, as "U3->U4", when the net was
	// short on more than one and so appears more than once.
	Leg string `json:"leg,omitempty"`

	RequestedMM float64  `json:"requested_mm"`
	AddedMM     float64  `json:"added_mm"`
	ShortfallMM float64  `json:"shortfall_mm"`
	Meanders    int      `json:"meanders"`
	Notes       []string `json:"notes,omitempty"`
}

// ApplyResponse is the result page.
type ApplyResponse struct {
	Session *Session    `json:"session"`
	Results []NetResult `json:"results"`

	// After is the board re-measured from the edited copper, not the tuner's
	// own bookkeeping, so the numbers shown are the board's.
	After *Analysis `json:"after"`

	AddedMM     float64 `json:"added_mm"`
	ShortfallMM float64 `json:"shortfall_mm"`
	NetsMet     int     `json:"nets_met"`
	NetsShort   int     `json:"nets_short"`

	// Changed is false when nothing could be fitted anywhere, in which case
	// there is no board to download.
	//
	// The alternative -- offering the untouched upload back as a result -- is
	// the sort of thing that gets a board fabricated in the belief it was
	// corrected. It is a perfectly valid file, which is exactly the problem.
	Changed bool `json:"changed"`

	// VerifyCommand is what to run to have KiCad check the result. This tool's
	// own checks are not the last word and it does not interpret custom design
	// rules, so the result page says so and hands over the command.
	VerifyCommand string `json:"verify_command"`
}

func (s *Service) handleApply(w http.ResponseWriter, r *http.Request) {
	_, sess, ok := s.session(w, r)
	if !ok {
		return
	}
	release, ok := s.job(w, r, heavyJob)
	if !ok {
		return
	}
	defer release()
	var req ApplyRequest
	if err := decodeJSON(r, &req); err != nil {
		s.fail(w, r, http.StatusBadRequest, err)
		return
	}
	params := sess.Params
	if req.Params != nil {
		params = *req.Params
	}
	b, proj, err := s.reload(r, sess)
	if err != nil {
		s.fail(w, r, http.StatusNotFound, err)
		return
	}
	analysis, plan, err := analyse(b, proj, sess.Filename, params, nil)
	if err != nil {
		s.fail(w, r, http.StatusBadRequest, err)
		return
	}

	targets, err := selectTargets(plan, analysis, req)
	if err != nil {
		s.fail(w, r, http.StatusBadRequest, err)
		return
	}
	if len(targets) == 0 {
		s.fail(w, r, http.StatusBadRequest,
			errors.New("nothing to change: none of the selected nets is out of tolerance"))
		return
	}

	tuner := tune.NewTuner(b, proj, analysis.Params.style())
	tuner.SetRules(customRules(b, proj))
	// The same clearance and the same allowed areas the room was measured
	// under, or the meanders would be drawn to rules the report never promised.
	tuner.SetOpenSpacing(analysis.spacing)
	tuner.SetAreas(analysis.areas)
	out := ApplyResponse{VerifyCommand: "kicad-cli pcb drc --format json --output drc.json " + resultName(sess.Filename)}
	for _, t := range targets {
		res, err := tuner.Tune(t.net, t.need, t.tracks)
		if err != nil {
			s.fail(w, r, http.StatusInternalServerError, fmt.Errorf("tuning %s: %w", t.net, err))
			return
		}
		out.Results = append(out.Results, NetResult{
			Net: res.Net, Label: label(res.Net), Leg: t.leg, RequestedMM: res.Requested,
			AddedMM: res.Added, ShortfallMM: res.Shortfall, Meanders: res.Meanders,
			Notes: res.Notes,
		})
		out.AddedMM += res.Added
		out.ShortfallMM += res.Shortfall
		if res.Shortfall <= 1e-4 {
			out.NetsMet++
		} else {
			out.NetsShort++
		}
	}
	sort.Slice(out.Results, func(i, j int) bool {
		if out.Results[i].ShortfallMM != out.Results[j].ShortfallMM {
			return out.Results[i].ShortfallMM > out.Results[j].ShortfallMM
		}
		return out.Results[i].Net < out.Results[j].Net
	})

	// Re-measure the edited board rather than reporting what the tuner
	// believed it did.
	after, _, err := analyse(b, proj, sess.Filename, analysis.Params, nil)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}
	out.After = after

	out.Changed = out.AddedMM > 1e-6
	// The nets, each once: a net short on two legs is two targets and one net.
	nets := make([]string, 0, len(targets))
	seen := make(map[string]bool, len(targets))
	for _, t := range targets {
		if seen[t.net] {
			continue
		}
		seen[t.net] = true
		nets = append(nets, t.net)
	}
	sess.Params = analysis.Params
	if out.Changed {
		tuned := b.Bytes()
		if err := s.deps.Store.PutResult(r.Context(), sess.ID, tuned); err != nil {
			s.fail(w, r, http.StatusInternalServerError, err)
			return
		}
		sess.Applied = &ApplyRecord{
			At: s.deps.now(), Nets: nets,
			AddedMM: out.AddedMM, ShortfallMM: out.ShortfallMM,
			NetsMet: out.NetsMet, NetsShort: out.NetsShort,
			ResultFilename: resultName(sess.Filename), ResultBytes: len(tuned),
		}
	}
	if err := s.deps.Store.Update(r.Context(), sess); err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}
	out.Session = sess
	s.deps.log().Info("board tuned",
		"session", sess.ID, "nets", len(targets),
		"added_mm", out.AddedMM, "shortfall_mm", out.ShortfallMM)
	writeJSON(w, http.StatusOK, out)
}

// HeadroomResponse says how much of what is needed the board can actually hold.
type HeadroomResponse struct {
	// Candidates carries the same nets the analysis reported, with their room
	// and reroute verdict filled in.
	Candidates []MemberInfo `json:"candidates"`

	TotalNeedMM float64 `json:"total_need_mm"`

	// GettableMM is each net's room capped at what it needs, summed. Room
	// beside one net cannot be lent to another, so this is not the sum of the
	// room figures and is usually much less.
	GettableMM float64 `json:"gettable_mm"`

	// RerouteCount is how many of them need routing differently instead.
	RerouteCount int `json:"reroute_count"`

	// BusSpareMM bounds what re-spacing the buses could add.
	BusSpareMM float64 `json:"bus_spare_mm"`

	// Interfaces carries every other interface with its room measured, the
	// same measurement the DDR candidates above got.
	Interfaces []DetectedInterface `json:"interfaces,omitempty"`

	// RunNeededMM and SpaceNeededMM2 are what the whole requirement would take
	// up if it were all meandered: the straight track to fold it into, and the
	// board area that would occupy.
	RunNeededMM    float64 `json:"run_needed_mm,omitempty"`
	SpaceNeededMM2 float64 `json:"space_needed_mm2,omitempty"`
}

// handleHeadroom measures how much room there is beside each route.
//
// This is a request of its own because it is slow in a way the rest is not:
// where the report measures lengths, this probes the design rules along every
// candidate track, and on a large board that is seconds rather than
// milliseconds. Doing it on upload would make every board feel slow to answer
// a question the user has not asked yet; they ask it when they are deciding
// what to change, and that is when it runs.
func (s *Service) handleHeadroom(w http.ResponseWriter, r *http.Request) {
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
	analysis, _, err := analyse(b, proj, sess.Filename, sess.Params, surveyor(b, proj, sess.Params))
	if err != nil {
		s.fail(w, r, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, HeadroomResponse{
		Candidates:     analysis.Candidates,
		TotalNeedMM:    analysis.TotalNeedMM,
		GettableMM:     analysis.GettableMM,
		RerouteCount:   analysis.RerouteCount,
		BusSpareMM:     analysis.BusSpareMM,
		RunNeededMM:    analysis.RunNeededMM,
		SpaceNeededMM2: analysis.SpaceNeededMM2,
		Interfaces:     analysis.Interfaces,
	})
}

func (s *Service) handleDownload(w http.ResponseWriter, r *http.Request) {
	_, sess, ok := s.session(w, r)
	if !ok {
		return
	}
	data, err := s.deps.Store.Result(r.Context(), sess.ID)
	if err != nil {
		s.fail(w, r, http.StatusNotFound, errors.New("nothing has been applied to this board yet"))
		return
	}
	name := resultNameFor(sess)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// parseBoard is exposed for tests that need to confirm a downloaded board
// still loads.
func parseBoard(data []byte) (*board.Board, error) { return board.Parse(data, "download") }

// resultNameFor names the download for whatever the result actually is. A
// board that has only been routed is not a tuned board, and offering it as one
// would be the tool describing its own output wrongly.
func resultNameFor(sess *Session) string {
	if sess.Applied == nil && sess.Routed != nil {
		base := strings.TrimSuffix(sess.Filename, filepath.Ext(sess.Filename))
		if base == "" {
			base = "board"
		}
		return base + ".routed.kicad_pcb"
	}
	return resultName(sess.Filename)
}

func resultName(filename string) string {
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	if base == "" {
		base = "board"
	}
	return base + ".tuned.kicad_pcb"
}

type target struct {
	net  string
	need float64

	// leg names the span this length belongs to, as "U3->U4", for nets that
	// are short on more than one.
	leg string

	// tracks restricts the meander to the copper the measured route runs over.
	tracks map[string]bool
}

// selectTargets works out which nets to change from the request.
//
// Naming nets that are already in tolerance is refused rather than ignored: it
// means the client is working from a plan that no longer matches the
// parameters, and quietly doing nothing would look like success.
func selectTargets(plan *ddr.Plan, a *Analysis, req ApplyRequest) ([]target, error) {
	// Every net that needs length, whichever interface it is on. The DDR plan
	// contributes its own, per leg of the chain; every other interface the
	// detector found contributes its group members and its unmatched pairs.
	// There is no second-class list: a net picked from the Ethernet group is
	// tuned exactly the way a DQ bit is.
	byNet := map[string]MemberInfo{}
	for _, c := range a.Candidates {
		byNet[c.Net] = c
	}
	for _, i := range a.Interfaces {
		if i.Planner != "" {
			continue
		}
		for _, c := range i.Candidates {
			if _, taken := byNet[c.Net]; !taken {
				byNet[c.Net] = c
			}
		}
	}

	wanted := map[string]bool{}
	for _, n := range req.Nets {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		c, ok := byNet[n]
		if !ok {
			return nil, fmt.Errorf("%s is not out of tolerance under these parameters", label(n))
		}
		if c.NeedMM <= 1e-6 {
			return nil, fmt.Errorf("%s is longer than its reference, so there is nothing to add: "+
				"it needs routing shorter, or the reference longer", label(n))
		}
		wanted[n] = true
	}
	for _, g := range req.Groups {
		found := false
		if plan != nil {
			for _, gi := range plan.Groups {
				if !strings.EqualFold(gi.Name, strings.TrimSpace(g)) {
					continue
				}
				found = true
				for _, m := range gi.Members {
					if _, ok := byNet[m.Net]; ok {
						wanted[m.Net] = true
					}
				}
			}
		}
		// An interface's group is named after the interface as well, since
		// "transmit" alone could be an Ethernet's or anything else's.
		for _, i := range a.Interfaces {
			for _, c := range i.Candidates {
				for _, l := range c.Legs {
					if strings.EqualFold(i.Name+" / "+l.Group, strings.TrimSpace(g)) {
						found = true
						wanted[c.Net] = true
					}
				}
			}
		}
		if !found {
			return nil, fmt.Errorf("no group called %q", g)
		}
	}
	if len(wanted) == 0 && len(req.Nets) == 0 && len(req.Groups) == 0 {
		for n := range byNet {
			wanted[n] = true
		}
	}

	// One target per leg the net is short on, not one per net.
	//
	// Picking a net means fixing it, and a fly-by address line short on both
	// U3->U4 and U4->U5 is only fixed when both are: each leg is matched
	// against the clock over that same span, and the length for each has to go
	// into that leg's own copper. Tuning the worse leg and calling the net done
	// left the other exactly as short as it was.
	out := make([]target, 0, len(wanted))
	for n := range wanted {
		c := byNet[n]
		if len(c.Legs) == 0 {
			on := make(map[string]bool, len(c.pathTracks))
			for _, u := range c.pathTracks {
				on[u] = true
			}
			out = append(out, target{net: n, need: c.NeedMM, tracks: on})
			continue
		}
		for _, leg := range c.Legs {
			// A leg that is too long for its reference has nothing to add:
			// a meander cannot shorten a track.
			if leg.NeedMM <= 1e-6 {
				continue
			}
			on := make(map[string]bool, len(leg.pathTracks))
			for _, u := range leg.pathTracks {
				on[u] = true
			}
			out = append(out, target{net: n, need: leg.NeedMM, leg: leg.Leg, tracks: on})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].need != out[j].need {
			return out[i].need > out[j].need
		}
		if out[i].net != out[j].net {
			return out[i].net < out[j].net
		}
		return out[i].leg < out[j].leg
	})
	return out, nil
}
