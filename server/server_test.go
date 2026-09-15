package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/geom"
	"github.com/embeddedci-com/pcb-autorouter/preview"
)

const demoDir = "../demo-pcb"

type harness struct {
	t     *testing.T
	svc   *Service
	mux   *http.ServeMux
	store *MemoryStore
	now   time.Time
	user  UserIdentity
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		t:    t,
		now:  time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC),
		user: UserIdentity{UserID: "u1", OrganizationID: "o1", Login: "ward"},
	}
	h.store = NewMemoryStore(func() time.Time { return h.now })
	h.svc = New(Deps{
		Store: h.store,
		Now:   func() time.Time { return h.now },
	})
	h.mux = http.NewServeMux()
	// Stand in for the host's authentication, the way embeddedci-server will.
	h.svc.Mount(h.mux, "/api", func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			next(w, r.WithContext(WithUser(r.Context(), h.user)))
		}
	})
	return h
}

func (h *harness) do(method, path string, body io.Reader, contentType string) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	return rec
}

func (h *harness) postJSON(path string, v any) *httptest.ResponseRecorder {
	h.t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		h.t.Fatal(err)
	}
	return h.do("POST", path, bytes.NewReader(raw), "application/json")
}

// upload posts the demo board, optionally with its project and rules files.
func (h *harness) upload(withProject, withRules bool) *httptest.ResponseRecorder {
	h.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	add := func(field, name, path string) {
		data, err := os.ReadFile(path)
		if err != nil {
			h.t.Skipf("fixture missing: %v", err)
		}
		part, err := mw.CreateFormFile(field, name)
		if err != nil {
			h.t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			h.t.Fatal(err)
		}
	}
	add("board", "ai-vision.kicad_pcb", filepath.Join(demoDir, "ai-vision.kicad_pcb"))
	if withProject {
		add("project", "ai-vision.kicad_pro", filepath.Join(demoDir, "ai-vision.kicad_pro"))
	}
	if withRules {
		add("rules", "ai-vision.kicad_dru", filepath.Join(demoDir, "ai-vision.kicad_dru"))
	}
	if err := mw.Close(); err != nil {
		h.t.Fatal(err)
	}
	return h.do("POST", "/api/pcb-trace-length-analyzer/sessions", &buf, mw.FormDataContentType())
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decoding %d response: %v\nbody: %s", rec.Code, err, truncate(rec.Body.String()))
	}
	return v
}

func truncate(s string) string {
	if len(s) > 600 {
		return s[:600] + "..."
	}
	return s
}

// TestUploadReturnsTheReport checks the first thing a user does: the report
// comes back from the upload itself, because there is nothing to wait for.
func TestUploadReturnsTheReport(t *testing.T) {
	h := newHarness(t)
	rec := h.upload(true, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, truncate(rec.Body.String()))
	}
	got := decode[SessionResponse](t, rec)
	if got.Session == nil || got.Session.ID == "" {
		t.Fatal("no session returned")
	}
	if got.Session.UserID != "u1" {
		t.Errorf("session filed under %q", got.Session.UserID)
	}
	a := got.Analysis
	if a == nil {
		t.Fatal("no analysis returned")
	}
	if a.Interface.Controller != "U3" {
		t.Errorf("controller = %q, want U3", a.Interface.Controller)
	}
	if a.Interface.WidthBits != 32 || a.Interface.Lanes != 4 {
		t.Errorf("interface = x%d in %d lanes", a.Interface.WidthBits, a.Interface.Lanes)
	}
	if a.Interface.NetPrefix != "/ddr4/" {
		t.Errorf("guessed prefix %q, want /ddr4/", a.Interface.NetPrefix)
	}
	if a.Routing.Complete != 44 || a.Routing.Incomplete != 27 {
		t.Errorf("routing = %d complete / %d incomplete, want 44/27",
			a.Routing.Complete, a.Routing.Incomplete)
	}
	// The 25 address nets with the same gap must arrive as one finding.
	if len(a.Routing.Gaps) == 0 || len(a.Routing.Gaps[0].Nets) != 25 {
		t.Errorf("routing gaps not grouped: %+v", a.Routing.Gaps)
	}
	if len(a.Groups) != 5 {
		t.Errorf("%d groups, want 4 byte lanes plus the routed fly-by leg", len(a.Groups))
	}
	if len(a.Candidates) == 0 || a.TotalNeedMM <= 0 {
		t.Errorf("no candidates: %d, %.3f mm", len(a.Candidates), a.TotalNeedMM)
	}
	// Worst first, because that is the one the user has to decide about.
	for i := 1; i < len(a.Candidates); i++ {
		if a.Candidates[i].NeedMM > a.Candidates[i-1].NeedMM {
			t.Errorf("candidates not ordered worst first at %d", i)
			break
		}
	}
	if !a.Board.HasProjectFile || !a.Board.HasCustomDRU {
		t.Errorf("board info lost the uploaded project/rules: %+v", a.Board)
	}
	if got.Session.BoardBytes < 1_000_000 {
		t.Errorf("board bytes = %d", got.Session.BoardBytes)
	}
	// Responses carrying somebody's board must not be cached anywhere.
	if cc := rec.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}
}

// TestNetClassesSurviveTheSession is the correctness point behind keeping the
// project file. Without its net classes every clearance falls back to the
// board minimum, and the meanders applied on a later request would be checked
// against looser rules than the user was shown.
func TestNetClassesSurviveTheSession(t *testing.T) {
	h := newHarness(t)
	withProject := decode[SessionResponse](t, h.upload(true, false))
	if n := len(withProject.Session.Project.Classes); n < 10 {
		t.Fatalf("session kept %d net classes, want the board's full set", n)
	}
	// DDR asks 0.2 mm; the board minimum is 0.1. Losing the classes would
	// halve the clearance the tuner works to.
	if got := withProject.Session.Project.ClearanceOf("/ddr4/DDR_DQ0"); got != 0.2 {
		t.Errorf("DQ0 clearance = %v, want 0.2", got)
	}
	if !withProject.Analysis.Board.HasProjectFile {
		t.Error("the report should say the project file was supplied")
	}

	// And a board uploaded without one is reported as such rather than
	// pretending.
	h2 := newHarness(t)
	bare := decode[SessionResponse](t, h2.upload(false, false))
	if bare.Analysis.Board.HasProjectFile {
		t.Error("a board with no project file must not claim to have one")
	}
	if len(bare.Analysis.Board.NetClasses) != 0 {
		t.Errorf("net classes appeared from nowhere: %v", bare.Analysis.Board.NetClasses)
	}
	if got := bare.Session.Project.ClearanceOf("/ddr4/DDR_DQ0"); got != bare.Session.Project.MinClearance {
		t.Errorf("without classes the clearance should be the board floor, got %v", got)
	}
}

// TestPlanAppliesTheClockOffset checks the parameter the brief names
// explicitly: the percentage the address and command group runs over the clock.
func TestPlanAppliesTheClockOffset(t *testing.T) {
	h := newHarness(t)
	first := decode[SessionResponse](t, h.upload(true, true))
	id := first.Session.ID

	find := func(a *Analysis) *GroupInfo {
		for i := range a.Groups {
			if strings.HasPrefix(a.Groups[i].Name, "address/command") {
				return &a.Groups[i]
			}
		}
		return nil
	}
	base := find(first.Analysis)
	if base == nil {
		t.Fatal("no address/command group")
	}
	if base.TargetFromReferenceMM != base.ReferenceLengthMM {
		t.Errorf("with no offset the target should equal the clock: %.4f vs %.4f",
			base.TargetFromReferenceMM, base.ReferenceLengthMM)
	}

	for _, pct := range []float64{2, 5} {
		p := first.Analysis.Params
		p.ClockOffsetPercent = pct
		rec := h.postJSON("/api/pcb-trace-length-analyzer/sessions/"+id+"/plan", PlanRequest{Params: p})
		if rec.Code != http.StatusOK {
			t.Fatalf("plan %v%%: status %d: %s", pct, rec.Code, truncate(rec.Body.String()))
		}
		got := decode[SessionResponse](t, rec)
		g := find(got.Analysis)
		want := g.ReferenceLengthMM * (1 + pct/100)
		if diff := g.TargetFromReferenceMM - want; diff > 1e-6 || diff < -1e-6 {
			t.Errorf("offset %v%%: target from reference %.4f, want %.4f", pct, g.TargetFromReferenceMM, want)
		}
		// The parameters must stick, so the apply that follows uses what the
		// user was looking at.
		if got.Session.Params.ClockOffsetPercent != pct {
			t.Errorf("offset %v%% not saved on the session", pct)
		}
	}
}

func TestPlanRejectsImplausibleParameters(t *testing.T) {
	h := newHarness(t)
	id := decode[SessionResponse](t, h.upload(true, true)).Session.ID
	for name, mutate := range map[string]func(*Params){
		"absurd clock offset":   func(p *Params) { p.ClockOffsetPercent = 200 },
		"negative past zero":    func(p *Params) { p.ClockOffsetPercent = -150 },
		"meander legs touching": func(p *Params) { p.MeanderGapW = 0.5 },
		"amplitude inverted":    func(p *Params) { p.MinAmplitudeMM = 2; p.MaxAmplitudeMM = 1 },
		"negative open spacing": func(p *Params) { p.OpenClearanceMM = -0.1 },
		"absurd open spacing":   func(p *Params) { p.OpenClearanceMM = 20 },
	} {
		p := DefaultParams()
		mutate(&p)
		rec := h.postJSON("/api/pcb-trace-length-analyzer/sessions/"+id+"/plan", PlanRequest{Params: p})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400: %s", name, rec.Code, truncate(rec.Body.String()))
		}
	}
}

// TestApplyTunesAndRemeasures checks the confirm-then-apply step, and that the
// numbers reported afterwards come from the edited board rather than from the
// tuner's own bookkeeping.
func TestApplyTunesAndRemeasures(t *testing.T) {
	h := newHarness(t)
	first := decode[SessionResponse](t, h.upload(true, true))
	id := first.Session.ID

	// Pick a handful of the smaller corrections: a meander has somewhere to go
	// for those, and which nets they are depends on the targets, not on this test.
	var nets []string
	for _, c := range first.Analysis.Candidates {
		if c.NeedMM > 0 && !c.NeedsReroute && len(nets) < 6 {
			nets = append(nets, c.Net)
		}
	}
	if len(nets) < 2 {
		t.Fatalf("expected several tunable candidates, got %v", nets)
	}

	rec := h.postJSON("/api/pcb-trace-length-analyzer/sessions/"+id+"/apply", ApplyRequest{Nets: nets})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, truncate(rec.Body.String()))
	}
	out := decode[ApplyResponse](t, rec)
	if len(out.Results) != len(nets) {
		t.Errorf("%d results for %d nets", len(out.Results), len(nets))
	}
	if out.AddedMM <= 0 {
		t.Errorf("nothing was added: %+v", out.Results)
	}
	if out.After == nil {
		t.Fatal("no re-measurement returned")
	}
	if out.VerifyCommand == "" || !strings.Contains(out.VerifyCommand, "kicad-cli") {
		t.Errorf("the result should hand over KiCad's own check, got %q", out.VerifyCommand)
	}

	// Every net that the tuner says it lengthened must measure longer in the
	// re-analysis, by the amount claimed.
	before := map[string]float64{}
	for _, g := range first.Analysis.Groups {
		for _, m := range g.Members {
			before[m.Net] = m.LengthMM
		}
	}
	after := map[string]float64{}
	for _, g := range out.After.Groups {
		for _, m := range g.Members {
			after[m.Net] = m.LengthMM
		}
	}
	checked := 0
	for _, r := range out.Results {
		if r.AddedMM <= 1e-6 {
			continue
		}
		checked++
		grew := after[r.Net] - before[r.Net]
		if diff := grew - r.AddedMM; diff > 1e-3 || diff < -1e-3 {
			t.Errorf("%s: reported %.4f mm added, the board grew %.4f mm", r.Label, r.AddedMM, grew)
		}
	}
	if checked == 0 {
		t.Error("no net actually grew")
	}

	// The session records the apply, so a reload lands on the result.
	if out.Session.Applied == nil || out.Session.Applied.ResultBytes == 0 {
		t.Errorf("apply not recorded on the session: %+v", out.Session.Applied)
	}
}

// TestDownloadReturnsAParseableBoard checks the last step: what comes back is
// a board KiCad could open, and it is not the one that went in.
func TestDownloadReturnsAParseableBoard(t *testing.T) {
	h := newHarness(t)
	first := decode[SessionResponse](t, h.upload(true, true))
	id := first.Session.ID

	// Nothing applied yet.
	if rec := h.do("GET", "/api/pcb-trace-length-analyzer/sessions/"+id+"/download", nil, ""); rec.Code != http.StatusNotFound {
		t.Errorf("download before apply: status %d, want 404", rec.Code)
	}

	// Two nets that are short of their target, whichever those are: which
	// ones are depends on the lengths, package included.
	var nets []string
	for _, c := range first.Analysis.Candidates {
		if c.NeedMM > 0 && len(nets) < 2 {
			nets = append(nets, c.Net)
		}
	}
	if rec := h.postJSON("/api/pcb-trace-length-analyzer/sessions/"+id+"/apply", ApplyRequest{Nets: nets}); rec.Code != http.StatusOK {
		t.Fatalf("apply: %d %s", rec.Code, truncate(rec.Body.String()))
	}

	rec := h.do("GET", "/api/pcb-trace-length-analyzer/sessions/"+id+"/download", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("download: status %d", rec.Code)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, ".tuned.kicad_pcb") {
		t.Errorf("Content-Disposition = %q", cd)
	}
	tuned := rec.Body.Bytes()
	original, err := os.ReadFile(filepath.Join(demoDir, "ai-vision.kicad_pcb"))
	if err != nil {
		t.Skip(err)
	}
	if bytes.Equal(tuned, original) {
		t.Error("the download is byte-identical to the upload; nothing was applied")
	}
	if !bytes.HasPrefix(tuned, []byte("(kicad_pcb")) {
		t.Error("the download is not a KiCad board")
	}
	// And it must still load.
	h2 := newHarness(t)
	_ = h2
	if err := reparse(tuned); err != nil {
		t.Errorf("the tuned board does not parse: %v", err)
	}
}

// TestApplyRefusesNetsThatAreNotOutOfTolerance checks that a stale selection is
// refused rather than quietly doing nothing, which would look like success.
func TestApplyRefusesNetsThatAreNotOutOfTolerance(t *testing.T) {
	h := newHarness(t)
	first := decode[SessionResponse](t, h.upload(true, true))
	id := first.Session.ID

	// A net that is in tolerance against its reference.
	var inTolerance string
	for _, g := range first.Analysis.Groups {
		for _, m := range g.Members {
			if m.Routed && m.InTolerance {
				inTolerance = m.Net
			}
		}
	}
	if inTolerance == "" {
		t.Fatal("no in-tolerance net to test with")
	}
	rec := h.postJSON("/api/pcb-trace-length-analyzer/sessions/"+id+"/apply", ApplyRequest{Nets: []string{inTolerance}})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400: %s", rec.Code, truncate(rec.Body.String()))
	}
	if !strings.Contains(rec.Body.String(), "not out of tolerance") {
		t.Errorf("unhelpful message: %s", truncate(rec.Body.String()))
	}
	// And an unknown group name is an error, not an empty run.
	rec = h.postJSON("/api/pcb-trace-length-analyzer/sessions/"+id+"/apply", ApplyRequest{Groups: []string{"byte lane 9"}})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown group: status %d, want 400", rec.Code)
	}
}

// TestApplyByGroup checks the other way of choosing: a whole group by name.
func TestApplyByGroup(t *testing.T) {
	h := newHarness(t)
	up := decode[SessionResponse](t, h.upload(true, true))
	id := up.Session.ID
	// The first byte lane with something to add, against its strobe.
	lane := ""
	for _, g := range up.Analysis.Groups {
		if g.Kind != "byte-lane" {
			continue
		}
		for _, m := range g.Members {
			if m.NeedMM > 0 && !m.InTolerance {
				lane = g.Name
			}
		}
		if lane != "" {
			break
		}
	}
	if lane == "" {
		t.Fatal("no byte lane has anything to add")
	}
	rec := h.postJSON("/api/pcb-trace-length-analyzer/sessions/"+id+"/apply", ApplyRequest{Groups: []string{lane}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, truncate(rec.Body.String()))
	}
	out := decode[ApplyResponse](t, rec)
	if len(out.Results) == 0 {
		t.Fatal("no results")
	}
	for _, r := range out.Results {
		if !strings.HasPrefix(r.Label, "DQ") && !strings.HasPrefix(r.Label, "DQS") && !strings.HasPrefix(r.Label, "DQM") {
			t.Errorf("%s is not in %s", r.Label, lane)
		}
	}
}

// TestSessionsAreNotSharedBetweenUsers is the access check. A session id is not
// an access token, and another user's session must read as missing rather than
// as forbidden, so ids cannot be probed.
func TestSessionsAreNotSharedBetweenUsers(t *testing.T) {
	h := newHarness(t)
	id := decode[SessionResponse](t, h.upload(true, false)).Session.ID

	h.user = UserIdentity{UserID: "u2", OrganizationID: "o1"}
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/pcb-trace-length-analyzer/sessions/" + id, nil},
		{"DELETE", "/api/pcb-trace-length-analyzer/sessions/" + id, nil},
		{"POST", "/api/pcb-trace-length-analyzer/sessions/" + id + "/plan", PlanRequest{}},
		{"POST", "/api/pcb-trace-length-analyzer/sessions/" + id + "/apply", ApplyRequest{}},
		{"GET", "/api/pcb-trace-length-analyzer/sessions/" + id + "/download", nil},
	} {
		var rec *httptest.ResponseRecorder
		if c.body != nil {
			rec = h.postJSON(c.path, c.body)
		} else {
			rec = h.do(c.method, c.path, nil, "")
		}
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s as another user: status %d, want 404", c.method, c.path, rec.Code)
		}
	}
	// And it is not in their listing either.
	rec := h.do("GET", "/api/pcb-trace-length-analyzer/sessions", nil, "")
	if strings.Contains(rec.Body.String(), id) {
		t.Error("another user's session appeared in the listing")
	}
}

// TestAnonymousRequestsAreRefused checks that a session is never filed under
// nobody. A session holds somebody's board, so without an identity there is
// nothing to file it under and nothing to check on the way back out.
//
// "Anonymous" and "nobody" are not the same thing. A host that lets people try
// the tool signed out gives each browser an id of its own and vouches for it;
// that is served, and kept apart from every other visitor.
func TestAnonymousRequestsAreRefused(t *testing.T) {
	visitor := newHarness(t)
	visitor.user = UserIdentity{UserID: "visitor:a", OrganizationID: "visitor:a", Anonymous: true}
	if rec := visitor.upload(true, false); rec.Code != http.StatusCreated {
		t.Fatalf("a vouched-for visitor: upload status %d, want 201", rec.Code)
	}
	visitor.user = UserIdentity{UserID: "visitor:b", OrganizationID: "visitor:b", Anonymous: true}
	rec := visitor.do("GET", "/api/pcb-trace-length-analyzer/sessions", nil, "")
	if strings.Contains(rec.Body.String(), "ai-vision") {
		t.Error("one visitor can see another visitor's board")
	}

	for name, u := range map[string]UserIdentity{
		"anonymous with no id": {Anonymous: true},
		"no user id":           {OrganizationID: "o1"},
	} {
		h := newHarness(t)
		h.user = u
		if rec := h.upload(true, false); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: upload status %d, want 401", name, rec.Code)
		}
		if rec := h.do("GET", "/api/pcb-trace-length-analyzer/sessions", nil, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: list status %d, want 401", name, rec.Code)
		}
	}
	// A host that never calls WithUser at all must also be refused, not
	// treated as some default user.
	h := newHarness(t)
	mux := http.NewServeMux()
	h.svc.Mount(mux, "/api", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/pcb-trace-length-analyzer/sessions", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no auth middleware at all: status %d, want 401", rec.Code)
	}
}

func TestExpiredSessionIsGone(t *testing.T) {
	h := newHarness(t)
	id := decode[SessionResponse](t, h.upload(true, false)).Session.ID
	if rec := h.do("GET", "/api/pcb-trace-length-analyzer/sessions/"+id, nil, ""); rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	h.now = h.now.Add(DefaultSessionTTL + time.Second)
	if rec := h.do("GET", "/api/pcb-trace-length-analyzer/sessions/"+id, nil, ""); rec.Code != http.StatusNotFound {
		t.Errorf("after the TTL: status %d, want 404", rec.Code)
	}
	// And the bytes are released rather than held for the life of the process.
	if _, err := h.store.ListByUser(context.Background(), "u1", 0); err != nil {
		t.Fatal(err)
	}
	if n := h.store.Len(); n != 0 {
		t.Errorf("%d sessions still held after expiry", n)
	}
}

func TestUploadRejectsBadInput(t *testing.T) {
	h := newHarness(t)

	// Not a multipart body at all.
	if rec := h.do("POST", "/api/pcb-trace-length-analyzer/sessions", strings.NewReader("{}"), "application/json"); rec.Code != http.StatusBadRequest {
		t.Errorf("json body: status %d, want 400", rec.Code)
	}

	post := func(field, name, content string) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		part, _ := mw.CreateFormFile(field, name)
		_, _ = part.Write([]byte(content))
		_ = mw.Close()
		return h.do("POST", "/api/pcb-trace-length-analyzer/sessions", &buf, mw.FormDataContentType())
	}
	for name, c := range map[string]struct {
		field, filename, content string
		wantBody                 string
	}{
		"wrong field name": {"file", "x.kicad_pcb", "(kicad_pcb)", "board"},
		"wrong extension":  {"board", "x.kicad_sch", "(kicad_sch)", "kicad_pcb"},
		"not a board":      {"board", "x.kicad_pcb", "hello, world", "KiCad board"},
		"a schematic":      {"board", "x.kicad_pcb", "(kicad_sch (version 1))", "KiCad board"},
		"empty":            {"board", "x.kicad_pcb", "", "KiCad board"},
	} {
		rec := post(c.field, c.filename, c.content)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, rec.Code)
			continue
		}
		if !strings.Contains(rec.Body.String(), c.wantBody) {
			t.Errorf("%s: message %q does not mention %q", name, truncate(rec.Body.String()), c.wantBody)
		}
	}
}

func TestUploadSizeLimit(t *testing.T) {
	h := newHarness(t)
	h.svc = New(Deps{Store: h.store, Now: func() time.Time { return h.now }, MaxUploadBytes: 1024})
	h.mux = http.NewServeMux()
	h.svc.Mount(h.mux, "/api", func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			next(w, r.WithContext(WithUser(r.Context(), h.user)))
		}
	})
	rec := h.upload(false, false)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status %d, want 413: %s", rec.Code, truncate(rec.Body.String()))
	}
}

// TestOldSessionsAreDroppedRatherThanRefused checks the quota behaviour: a user
// who keeps uploading loses their oldest board, not their next one.
func TestOldSessionsAreDroppedRatherThanRefused(t *testing.T) {
	h := newHarness(t)
	h.svc = New(Deps{Store: h.store, Now: func() time.Time { return h.now }, MaxSessionsPerUser: 2})
	h.mux = http.NewServeMux()
	h.svc.Mount(h.mux, "/api", func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			next(w, r.WithContext(WithUser(r.Context(), h.user)))
		}
	})
	var ids []string
	for i := range 4 {
		h.now = h.now.Add(time.Duration(i) * time.Minute)
		rec := h.upload(false, false)
		if rec.Code != http.StatusCreated {
			t.Fatalf("upload %d: status %d", i, rec.Code)
		}
		ids = append(ids, decode[SessionResponse](t, rec).Session.ID)
	}
	rows := decode[struct {
		Sessions []*Session `json:"sessions"`
	}](t, h.do("GET", "/api/pcb-trace-length-analyzer/sessions", nil, ""))
	if len(rows.Sessions) > 2 {
		t.Errorf("%d sessions kept, limit was 2", len(rows.Sessions))
	}
	// The newest survived.
	if rec := h.do("GET", "/api/pcb-trace-length-analyzer/sessions/"+ids[len(ids)-1], nil, ""); rec.Code != http.StatusOK {
		t.Errorf("the newest session was dropped: status %d", rec.Code)
	}
}

func TestDeleteRemovesEverything(t *testing.T) {
	h := newHarness(t)
	id := decode[SessionResponse](t, h.upload(true, false)).Session.ID
	if rec := h.do("DELETE", "/api/pcb-trace-length-analyzer/sessions/"+id, nil, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: status %d", rec.Code)
	}
	if rec := h.do("GET", "/api/pcb-trace-length-analyzer/sessions/"+id, nil, ""); rec.Code != http.StatusNotFound {
		t.Errorf("still readable after delete: status %d", rec.Code)
	}
	if n := h.store.Len(); n != 0 {
		t.Errorf("%d rows left in the store", n)
	}
}

func TestDefaultsEndpoint(t *testing.T) {
	h := newHarness(t)
	rec := h.do("GET", "/api/pcb-trace-length-analyzer/defaults", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	got := decode[struct {
		Params         Params `json:"params"`
		MaxUploadBytes int64  `json:"max_upload_bytes"`
		TTLSeconds     int    `json:"session_ttl_seconds"`
	}](t, rec)
	if got.Params.DataToStrobeMM <= 0 || got.Params.MeanderGapW <= 0 {
		t.Errorf("defaults look empty: %+v", got.Params)
	}
	if got.MaxUploadBytes != DefaultMaxUploadBytes || got.TTLSeconds <= 0 {
		t.Errorf("limits not reported: %+v", got)
	}
}

func TestNewWithoutStorePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("New with no Store should panic rather than fail on the first request")
		}
	}()
	New(Deps{})
}

func TestSafeFilename(t *testing.T) {
	for in, want := range map[string]string{
		"board.kicad_pcb":         "board.kicad_pcb",
		"../../etc/passwd":        "passwd",
		`..\..\windows\system32`:  "system32",
		"my board (v2).kicad_pcb": "my_board__v2_.kicad_pcb",
		"":                        "board.kicad_pcb",
		"...":                     "board.kicad_pcb",
		"a/b/c/deep.kicad_pcb":    "deep.kicad_pcb",
		"semìçolon.kicad_pcb":     "sem__olon.kicad_pcb",
	} {
		if got := safeFilename(in); got != want {
			t.Errorf("safeFilename(%q) = %q, want %q", in, got, want)
		}
	}
	if got := safeFilename(strings.Repeat("a", 300) + ".kicad_pcb"); len(got) > 120 {
		t.Errorf("long name not truncated: %d chars", len(got))
	}
}

func TestResultName(t *testing.T) {
	for in, want := range map[string]string{
		"ai-vision.kicad_pcb": "ai-vision.tuned.kicad_pcb",
		"board.kicad_pcb":     "board.tuned.kicad_pcb",
		".kicad_pcb":          "board.tuned.kicad_pcb",
	} {
		if got := resultName(in); got != want {
			t.Errorf("resultName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUnknownSessionIs404(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{
		"/api/pcb-trace-length-analyzer/sessions/nope",
		"/api/pcb-trace-length-analyzer/sessions/nope/download",
	} {
		if rec := h.do("GET", path, nil, ""); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", path, rec.Code)
		}
	}
}

func TestPlanRejectsUnknownJSONFields(t *testing.T) {
	h := newHarness(t)
	id := decode[SessionResponse](t, h.upload(true, false)).Session.ID
	body := strings.NewReader(`{"params":{},"typo":1}`)
	rec := h.do("POST", "/api/pcb-trace-length-analyzer/sessions/"+id+"/plan", body, "application/json")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400 for an unknown field", rec.Code)
	}
}

func TestMemoryStoreIsNotFoundForMissingRows(t *testing.T) {
	ms := NewMemoryStore(nil)
	ctx := context.Background()
	for name, err := range map[string]error{
		"get":    mustErr(func() error { _, e := ms.Get(ctx, "x"); return e }),
		"board":  mustErr(func() error { _, e := ms.Board(ctx, "x"); return e }),
		"result": mustErr(func() error { _, e := ms.Result(ctx, "x"); return e }),
		"update": ms.Update(ctx, &Session{ID: "x"}),
		"put":    ms.PutResult(ctx, "x", nil),
	} {
		if err != ErrNotFound {
			t.Errorf("%s returned %v, want ErrNotFound", name, err)
		}
	}
	// Deleting something that is not there is not an error.
	if err := ms.Delete(ctx, "x"); err != nil {
		t.Errorf("delete of a missing row: %v", err)
	}
}

func mustErr(f func() error) error { return f() }

func reparse(data []byte) error {
	_, err := parseBoard(data)
	return err
}

func BenchmarkUploadAndAnalyse(bench *testing.B) {
	data, err := os.ReadFile(filepath.Join(demoDir, "ai-vision.kicad_pcb"))
	if err != nil {
		bench.Skip(err)
	}
	proj, err := os.ReadFile(filepath.Join(demoDir, "ai-vision.kicad_pro"))
	if err != nil {
		bench.Skip(err)
	}
	for bench.Loop() {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		p1, _ := mw.CreateFormFile("board", "b.kicad_pcb")
		_, _ = p1.Write(data)
		p2, _ := mw.CreateFormFile("project", "b.kicad_pro")
		_, _ = p2.Write(proj)
		_ = mw.Close()

		store := NewMemoryStore(nil)
		svc := New(Deps{Store: store})
		mux := http.NewServeMux()
		svc.Mount(mux, "/api", func(next http.HandlerFunc) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				next(w, r.WithContext(WithUser(r.Context(), UserIdentity{UserID: "u"})))
			}
		})
		req := httptest.NewRequest("POST", "/api/pcb-trace-length-analyzer/sessions", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			bench.Fatalf("status %d: %s", rec.Code, truncate(rec.Body.String()))
		}
	}
	_ = fmt.Sprint()
}

// TestApplyThatChangesNothingOffersNoDownload is a safety property rather than
// a nicety.
//
// On a densely routed board a whole selection can turn out to be unfittable. If
// the untouched upload were handed back as a result it would be a perfectly
// valid board file -- which is exactly the danger, because it would be
// indistinguishable from a corrected one. So nothing is stored, the download
// stays a 404, and the response says so.
func TestApplyThatChangesNothingOffersNoDownload(t *testing.T) {
	h := newHarness(t)
	first := decode[SessionResponse](t, h.upload(true, true))
	id := first.Session.ID

	// These sit in a bus routed at minimum clearance, with nowhere beside them
	// for a meander to go.
	var nets []string
	for _, c := range first.Analysis.Candidates {
		switch c.Label {
		case "DQM1", "DQ28", "DQ29", "DQM3", "DQ27":
			nets = append(nets, c.Net)
		}
	}
	if len(nets) == 0 {
		t.Skip("no unfittable candidates on this board")
	}
	rec := h.postJSON("/api/pcb-trace-length-analyzer/sessions/"+id+"/apply", ApplyRequest{Nets: nets})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, truncate(rec.Body.String()))
	}
	out := decode[ApplyResponse](t, rec)
	if out.AddedMM > 1e-6 {
		t.Skipf("%.4f mm was fitted after all, so there is nothing to check", out.AddedMM)
	}
	if out.Changed {
		t.Error("Changed should be false when nothing was added")
	}
	if out.ShortfallMM <= 0 || out.NetsShort == 0 {
		t.Errorf("the shortfall should be reported: %+v", out)
	}
	if out.Session.Applied != nil {
		t.Error("an apply that changed nothing must not be recorded as one")
	}
	if rec := h.do("GET", "/api/pcb-trace-length-analyzer/sessions/"+id+"/download", nil, ""); rec.Code != http.StatusNotFound {
		t.Errorf("download after an empty apply: status %d, want 404", rec.Code)
	}
}

// TestHeadroomIsASeparateRequest checks both halves of that decision: the
// report comes back fast without it, and asking for it explicitly gives the
// numbers the user needs to tell a shortfall of space from a reroute.
func TestHeadroomIsASeparateRequest(t *testing.T) {
	h := newHarness(t)
	first := decode[SessionResponse](t, h.upload(true, true))
	id := first.Session.ID

	// The upload does not pay for it.
	for _, c := range first.Analysis.Candidates {
		if c.HeadroomMM != 0 {
			t.Errorf("%s arrived with headroom %.3f; the upload should not measure it", c.Label, c.HeadroomMM)
			break
		}
	}
	if first.Analysis.GettableMM != 0 || first.Analysis.BusSpareMM != 0 {
		t.Errorf("upload reported gettable %.3f and bus spare %.3f without measuring",
			first.Analysis.GettableMM, first.Analysis.BusSpareMM)
	}

	rec := h.do("GET", "/api/pcb-trace-length-analyzer/sessions/"+id+"/headroom", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, truncate(rec.Body.String()))
	}
	got := decode[HeadroomResponse](t, rec)
	if len(got.Candidates) != len(first.Analysis.Candidates) {
		t.Errorf("%d candidates, the report had %d", len(got.Candidates), len(first.Analysis.Candidates))
	}
	if got.TotalNeedMM <= 0 {
		t.Fatalf("nothing needed: %+v", got)
	}
	// Room cannot be lent between nets, so what is gettable is well below the
	// total needed on this board.
	if got.GettableMM <= 0 || got.GettableMM >= got.TotalNeedMM {
		t.Errorf("gettable %.3f of %.3f needed; expected some but not all", got.GettableMM, got.TotalNeedMM)
	}
	if got.RerouteCount == 0 {
		t.Error("expected some nets to be flagged as needing a reroute on this board")
	}
	if got.BusSpareMM <= 0 {
		t.Error("expected the buses to have some spare room")
	}

	// Both kinds of shortfall must be distinguishable per net.
	var withRoom, reroute int
	for _, c := range got.Candidates {
		if c.NeedsReroute {
			reroute++
			continue
		}
		if c.HeadroomMM > 0 {
			withRoom++
		}
	}
	if withRoom == 0 || reroute == 0 {
		t.Errorf("%d nets with room and %d needing a reroute; expected both", withRoom, reroute)
	}
	t.Logf("needed %.1f mm, gettable %.1f mm, %d reroutes, bus spare %.2f mm",
		got.TotalNeedMM, got.GettableMM, got.RerouteCount, got.BusSpareMM)
}

func TestHeadroomNeedsTheSession(t *testing.T) {
	h := newHarness(t)
	if rec := h.do("GET", "/api/pcb-trace-length-analyzer/sessions/nope/headroom", nil, ""); rec.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404", rec.Code)
	}
	id := decode[SessionResponse](t, h.upload(true, false)).Session.ID
	h.user = UserIdentity{UserID: "someone-else"}
	if rec := h.do("GET", "/api/pcb-trace-length-analyzer/sessions/"+id+"/headroom", nil, ""); rec.Code != http.StatusNotFound {
		t.Errorf("another user: status %d, want 404", rec.Code)
	}
}

// The open-field clearance is a rule about where copper may go, so what proves
// it arrived is that the room the board reports having changes with it. The
// demo board's DDR class already asks for 0.2 mm, so the default is a no-op
// there and the test has to ask for something stricter to see anything -- which
// is itself worth asserting, because a parameter that silently did nothing
// would look exactly like one that was never wired up.
func TestOpenFieldClearanceChangesTheRoomTheBoardHas(t *testing.T) {
	h := newHarness(t)
	id := decode[SessionResponse](t, h.upload(true, true)).Session.ID

	gettable := func(p Params) float64 {
		rec := h.postJSON("/api/pcb-trace-length-analyzer/sessions/"+id+"/plan", PlanRequest{Params: p})
		if rec.Code != http.StatusOK {
			t.Fatalf("plan: status %d: %s", rec.Code, truncate(rec.Body.String()))
		}
		if got := decode[SessionResponse](t, rec).Session.Params.OpenClearanceMM; got != p.OpenClearanceMM {
			t.Errorf("open clearance %.3f not saved on the session (got %.3f)", p.OpenClearanceMM, got)
		}
		rec = h.do("GET", "/api/pcb-trace-length-analyzer/sessions/"+id+"/headroom", nil, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("headroom: status %d: %s", rec.Code, truncate(rec.Body.String()))
		}
		return decode[HeadroomResponse](t, rec).GettableMM
	}

	board := DefaultParams()
	board.OpenClearanceMM = 0 // the board's own rules, everywhere
	loose := gettable(board)

	strict := DefaultParams()
	strict.OpenClearanceMM = 0.35
	tight := gettable(strict)

	if loose <= 0 {
		t.Fatalf("the board offers %.3f mm of room under its own rules; the fixture is wrong", loose)
	}
	if tight >= loose {
		t.Errorf("holding to 0.35 mm in the open left %.3f mm of room, no less than the board's own rules' %.3f mm",
			tight, loose)
	}

	// And zero really is the board's own rules rather than a missing value the
	// defaults fill in: the default is 0.2 mm, so if zero were being replaced
	// this would come back as the default's answer.
	deflt := DefaultParams()
	if got := gettable(deflt); got != loose {
		t.Logf("the default 0.2 mm differs from the board's own rules here: %.3f vs %.3f", got, loose)
	}
	if again := gettable(board); again != loose {
		t.Errorf("zero did not survive a round trip: %.3f then %.3f", loose, again)
	}
}

// The preview is what makes the tool's work visible: a meander is obvious on
// screen and invisible in a diff. So the test is that both boards can be drawn,
// that the result differs from the input, and that the index and the buffer
// agree -- the viewer refuses a board whose geometry.bin is not the length
// board.json claims, and it is right to.
func TestPreviewServesBothBoards(t *testing.T) {
	h := newHarness(t)
	id := decode[SessionResponse](t, h.upload(true, true)).Session.ID

	get := func(path string) *httptest.ResponseRecorder {
		return h.do("GET", "/api/pcb-trace-length-analyzer/sessions/"+id+path, nil, "")
	}

	// Before anything is applied there is a board but no result.
	if rec := get("/preview/board.json"); rec.Code != http.StatusOK {
		t.Fatalf("board.json: status %d: %s", rec.Code, truncate(rec.Body.String()))
	}
	if rec := get("/preview/board.json?of=result"); rec.Code != http.StatusNotFound {
		t.Errorf("result preview before any apply: status %d, want 404", rec.Code)
	}
	if rec := get("/preview/board.json?of=nonsense"); rec.Code != http.StatusBadRequest {
		t.Errorf("of=nonsense: status %d, want 400", rec.Code)
	}

	check := func(of string) *preview.Doc {
		t.Helper()
		q := ""
		if of != "" {
			q = "?of=" + of
		}
		rec := get("/preview/board.json" + q)
		if rec.Code != http.StatusOK {
			t.Fatalf("board.json%s: status %d: %s", q, rec.Code, truncate(rec.Body.String()))
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("board.json Content-Type %q", ct)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "private, no-store" {
			t.Errorf("board.json Cache-Control %q: somebody's board must not be cached", cc)
		}
		doc := decode[preview.Doc](t, rec)

		bin := get("/preview/geometry.bin" + q)
		if bin.Code != http.StatusOK {
			t.Fatalf("geometry.bin%s: status %d", q, bin.Code)
		}
		if got := bin.Body.Len(); got != doc.Geometry.ByteLength {
			t.Errorf("geometry.bin is %d bytes, board.json says %d", got, doc.Geometry.ByteLength)
		}
		if doc.Geometry.VertexCount*8 != doc.Geometry.ByteLength {
			t.Errorf("%d vertices in %d bytes", doc.Geometry.VertexCount, doc.Geometry.ByteLength)
		}
		if len(doc.Layers) != 6 || doc.Board.WidthMM <= 0 {
			t.Errorf("board is %.1f x %.1f mm on %d layers", doc.Board.WidthMM, doc.Board.HeightMM, len(doc.Layers))
		}
		if doc.Source["of"] != either(of, "board") {
			t.Errorf("source says of=%v", doc.Source["of"])
		}
		return &doc
	}

	before := check("")

	// Tune one lane, then the result is drawable too -- and bigger, because
	// meandering adds copper.
	rec := h.postJSON("/api/pcb-trace-length-analyzer/sessions/"+id+"/apply", ApplyRequest{Groups: []string{"byte lane 3"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("apply: status %d: %s", rec.Code, truncate(rec.Body.String()))
	}
	if !decode[ApplyResponse](t, rec).Changed {
		t.Fatal("apply changed nothing, so there is nothing to see")
	}
	after := check("result")

	if after.Geometry.VertexCount <= before.Geometry.VertexCount {
		t.Errorf("the tuned board draws %d vertices, the original %d: meandering adds copper",
			after.Geometry.VertexCount, before.Geometry.VertexCount)
	}
	// The nets that were tuned carry more copper than they did.
	lengths := func(d *preview.Doc) map[string]float64 {
		out := map[string]float64{}
		for _, n := range d.Nets {
			out[n.Name] = n.LengthMM
		}
		return out
	}
	was, now := lengths(before), lengths(after)
	grew := 0
	for net, l := range now {
		if l > was[net]+1e-6 {
			grew++
		}
		if l < was[net]-1e-6 {
			t.Errorf("%s lost copper: %.4f -> %.4f mm", net, was[net], l)
		}
	}
	if grew == 0 {
		t.Error("no net carries more copper after tuning")
	}
	t.Logf("%d nets grew; %d -> %d vertices", grew, before.Geometry.VertexCount, after.Geometry.VertexCount)
}

// The pours are most of a board's copper by area and they sit under the traces
// this tool changes, so they are opt-in -- and a preview without them says so,
// because a missing plane otherwise reads as a hole in the board.
func TestPreviewPoursAreOptIn(t *testing.T) {
	h := newHarness(t)
	id := decode[SessionResponse](t, h.upload(true, true)).Session.ID
	one := func(q string) preview.Doc {
		rec := h.do("GET", "/api/pcb-trace-length-analyzer/sessions/"+id+"/preview/board.json"+q, nil, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
		return decode[preview.Doc](t, rec)
	}
	without, with := one(""), one("?zones=1")
	if with.Geometry.VertexCount <= without.Geometry.VertexCount {
		t.Errorf("zones=1 drew no more: %d vs %d", with.Geometry.VertexCount, without.Geometry.VertexCount)
	}
	var said bool
	for _, w := range without.Warnings {
		if strings.Contains(w, "pour") {
			said = true
		}
	}
	if !said {
		t.Errorf("a preview with no pours did not say so: %v", without.Warnings)
	}
}

func TestPreviewNeedsTheSession(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/preview/board.json", "/preview/geometry.bin"} {
		if rec := h.do("GET", "/api/pcb-trace-length-analyzer/sessions/nope"+path, nil, ""); rec.Code != http.StatusNotFound {
			t.Errorf("%s on an unknown session: status %d, want 404", path, rec.Code)
		}
	}
}

// tinyBoard is a board with no DDR on it at all: a USB pair and a couple of
// unrelated nets. Built here rather than taken from a fixture because the point
// is the absence of something, and a board that happens to lack DDR today
// might not tomorrow.
func tinyBoard() []byte {
	return []byte(`(kicad_pcb
	(version 20260206)
	(generator "pcb-trace-length-analyzer-test")
	(paper "A4")
	(layers (0 "F.Cu" signal) (2 "B.Cu" signal) (1 "F.Mask" user) (5 "F.SilkS" user "F.Silkscreen") (25 "Edge.Cuts" user))
	(setup (stackup
		(layer "F.Cu" (type "copper") (thickness 0.035))
		(layer "dielectric 1" (type "core") (thickness 1.51) (material "FR4") (epsilon_r 4.5))
		(layer "B.Cu" (type "copper") (thickness 0.035))
	))
	(gr_rect (start 0 0) (end 40 30) (stroke (width 0.05) (type default)) (fill none) (layer "Edge.Cuts") (uuid "ffffffff-3333-4000-8000-000000000001"))
	(footprint "t:u" (layer "F.Cu") (uuid "aaaaaaaa-3333-4000-8000-000000000002") (at 10 15) (attr smd)
		(property "Reference" "U1" (at 0 0) (layer "F.SilkS") (uuid "aaaaaaaa-3333-4000-8000-000000000003") (effects (font (size 1 1) (thickness 0.15))))
		(pad "1" smd circle (at 0 0) (size 0.3 0.3) (layers "F.Cu") (net "/USB_D+") (uuid "aaaaaaaa-3333-4000-8000-000000000004"))
		(pad "2" smd circle (at 0 1) (size 0.3 0.3) (layers "F.Cu") (net "/USB_D-") (uuid "aaaaaaaa-3333-4000-8000-000000000005"))
	)
	(footprint "t:j" (layer "F.Cu") (uuid "aaaaaaaa-3333-4000-8000-000000000006") (at 30 15) (attr smd)
		(property "Reference" "J1" (at 0 0) (layer "F.SilkS") (uuid "aaaaaaaa-3333-4000-8000-000000000007") (effects (font (size 1 1) (thickness 0.15))))
		(pad "1" smd circle (at 0 0) (size 0.3 0.3) (layers "F.Cu") (net "/USB_D+") (uuid "aaaaaaaa-3333-4000-8000-000000000008"))
		(pad "2" smd circle (at 0 1) (size 0.3 0.3) (layers "F.Cu") (net "/USB_D-") (uuid "aaaaaaaa-3333-4000-8000-000000000009"))
	)
	(segment (start 10 15) (end 30 15) (width 0.2) (layer "F.Cu") (net "/USB_D+") (uuid "bbbbbbbb-3333-4000-8000-00000000000a"))
	(segment (start 10 16) (end 30 16) (width 0.2) (layer "F.Cu") (net "/USB_D-") (uuid "bbbbbbbb-3333-4000-8000-00000000000b"))
)
`)
}

// uploadBytes posts a board supplied inline.
func (h *harness) uploadBytes(name string, data []byte) *httptest.ResponseRecorder {
	h.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("board", name)
	if err != nil {
		h.t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		h.t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		h.t.Fatal(err)
	}
	return h.do("POST", "/api/pcb-trace-length-analyzer/sessions", &buf, mw.FormDataContentType())
}

// A board with no DDR on it is not a board this has nothing to say about.
// Refusing it was the last thing left over from when DDR was the only
// interface the tool knew, and it meant a user with an Ethernet board got a
// 400 and no report at all.
func TestABoardWithNoDDRIsStillAnalysed(t *testing.T) {
	h := newHarness(t)
	rec := h.uploadBytes("no-ddr.kicad_pcb", tinyBoard())
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d, want 201: %s", rec.Code, truncate(rec.Body.String()))
	}
	got := decode[SessionResponse](t, rec)
	a := got.Analysis
	if a == nil {
		t.Fatal("no analysis")
	}

	// The board itself is still described.
	if a.Board.Tracks != 2 || len(a.Board.CopperLayers) != 2 {
		t.Errorf("board = %d tracks on %d layers", a.Board.Tracks, len(a.Board.CopperLayers))
	}
	// And its USB pair is found and measured.
	var usb *DetectedInterface
	for i := range a.Interfaces {
		if a.Interfaces[i].Kind == "usb2" {
			usb = &a.Interfaces[i]
		}
	}
	if usb == nil {
		t.Fatalf("no USB found among %d interfaces", len(a.Interfaces))
	}
	if usb.Pairs != 1 {
		t.Errorf("%d pairs on a board with one", usb.Pairs)
	}
	if usb.Geometry.WidthMM <= 0 {
		t.Error("the pair is routed, so its width should have been measured")
	}

	// The DDR half is empty rather than absent: a client that maps over these
	// gets nothing to map over, not a null it has to know about.
	if a.Groups == nil || len(a.Groups) != 0 {
		t.Errorf("groups = %v, want an empty list", a.Groups)
	}
	if a.Candidates == nil || len(a.Candidates) != 0 {
		t.Errorf("candidates = %v, want an empty list", a.Candidates)
	}
	if len(a.Notes) == 0 || !strings.Contains(a.Notes[0], "No DDR") {
		t.Errorf("notes %v do not say there is no DDR", a.Notes)
	}

	// Applying to it is refused because nothing on it is out of tolerance --
	// not because it has no DDR, which is no longer a reason.
	id := got.Session.ID
	rec = h.postJSON("/api/pcb-trace-length-analyzer/sessions/"+id+"/apply", ApplyRequest{})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("apply status %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "none of the selected nets is out of tolerance") {
		t.Errorf("apply refusal %q", truncate(rec.Body.String()))
	}

	// And the board still draws.
	rec = h.do("GET", "/api/pcb-trace-length-analyzer/sessions/"+id+"/preview/board.json", nil, "")
	if rec.Code != http.StatusOK {
		t.Errorf("preview status %d", rec.Code)
	}
}

// Copper goes where the user said and nowhere else.
//
// This is the whole promise of the feature, and it is not the same as "every
// new track is inside the area": folding a meander into a track replaces the
// whole track, so its untouched ends are rewritten too and come back with new
// identifiers. What must be inside is copper that was not on the board before.
//
// The first version checked the centrelines and passed while 26 samples of new
// copper sat up to 0.05 mm beyond the edge -- a meander wanders sideways from
// the track it is folded into, and an area is a statement about where copper
// may be, not about where centrelines may be.
func TestNothingIsAddedOutsideTheAreasTheUserDrew(t *testing.T) {
	h := newHarness(t)
	id := decode[SessionResponse](t, h.upload(true, true)).Session.ID

	// A region over part of the board, in the viewer's coordinates.
	area := Area{MinX: 40, MinY: 30, MaxX: 70, MaxY: 60}
	p := DefaultParams()
	p.MeanderAreas = []Area{area}
	rec := h.postJSON("/api/pcb-trace-length-analyzer/sessions/"+id+"/plan", PlanRequest{Params: p})
	if rec.Code != http.StatusOK {
		t.Fatalf("plan: status %d: %s", rec.Code, truncate(rec.Body.String()))
	}

	rec = h.postJSON("/api/pcb-trace-length-analyzer/sessions/"+id+"/apply", ApplyRequest{})
	if rec.Code != http.StatusOK {
		t.Fatalf("apply: status %d: %s", rec.Code, truncate(rec.Body.String()))
	}
	res := decode[ApplyResponse](t, rec)
	if res.AddedMM <= 0 {
		t.Fatalf("nothing was added inside the area, so this proves nothing")
	}

	before, err := board.Load(filepath.Join(demoDir, "ai-vision.kicad_pcb"))
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	rec = h.do("GET", "/api/pcb-trace-length-analyzer/sessions/"+id+"/download", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("download: status %d", rec.Code)
	}
	after, err := parseBoard(rec.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	tf, ok := preview.ExtentOf(before)
	if !ok {
		t.Fatal("no extent")
	}
	allowed := tf.RectToKiCad(geom.Rect{
		MinX: area.MinX, MinY: area.MinY, MaxX: area.MaxX, MaxY: area.MaxY,
	})

	// The copper that was already there, per net, so re-created pieces are not
	// mistaken for new ones.
	orig := map[string][]geom.RoundPoly{}
	was := map[string]bool{}
	for _, tr := range before.Tracks {
		was[tr.UUID] = true
		if tr.Kind != board.KindVia {
			orig[tr.Net] = append(orig[tr.Net], tr.Shape(before.MaxError)...)
		}
	}
	onOriginal := func(net string, pt geom.Pt) bool {
		for _, sh := range orig[net] {
			if sh.DistToPoint(pt) <= 0.01 {
				return true
			}
		}
		return false
	}

	fresh, outside := 0, 0
	for _, tr := range after.Tracks {
		if was[tr.UUID] || tr.Kind == board.KindVia {
			continue
		}
		n := int(tr.Length(after.Stackup)/0.05) + 2
		for i := 0; i <= n; i++ {
			f := float64(i) / float64(n)
			pt := geom.Pt{
				X: tr.Start.X + (tr.End.X-tr.Start.X)*f,
				Y: tr.Start.Y + (tr.End.Y-tr.Start.Y)*f,
			}
			if onOriginal(tr.Net, pt) {
				continue
			}
			fresh++
			if !allowed.Contains(pt) {
				outside++
				if outside <= 3 {
					t.Errorf("%s: new copper at (%.3f, %.3f) is outside the area", tr.Net, pt.X, pt.Y)
				}
			}
		}
	}
	if fresh == 0 {
		t.Fatal("no new copper was found to check")
	}
	if outside != 0 {
		t.Fatalf("%d of %d samples of new copper fall outside the area", outside, fresh)
	}
	t.Logf("%.3f mm added, %d samples of new copper, all inside the area", res.AddedMM, fresh)
}

// An area over nothing is not an error, and it is not silently ignored either:
// the room comes out at nothing and the shortfall says so.
func TestAnAreaOverNoTrackLeavesNothingToDo(t *testing.T) {
	h := newHarness(t)
	id := decode[SessionResponse](t, h.upload(true, true)).Session.ID
	p := DefaultParams()
	p.MeanderAreas = []Area{{MinX: 0, MinY: 0, MaxX: 3, MaxY: 3}}
	rec := h.postJSON("/api/pcb-trace-length-analyzer/sessions/"+id+"/plan", PlanRequest{Params: p})
	if rec.Code != http.StatusOK {
		t.Fatalf("plan: status %d: %s", rec.Code, truncate(rec.Body.String()))
	}
	rec = h.do("GET", "/api/pcb-trace-length-analyzer/sessions/"+id+"/headroom", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("headroom: status %d", rec.Code)
	}
	if got := decode[HeadroomResponse](t, rec).GettableMM; got > 1e-9 {
		t.Errorf("%.4f mm is gettable in an area with no track under it", got)
	}
}

// A click is not an area, and taking one for a restriction would silently stop
// the tool doing anything at all.
func TestAnEmptyAreaIsRefused(t *testing.T) {
	h := newHarness(t)
	id := decode[SessionResponse](t, h.upload(true, true)).Session.ID
	p := DefaultParams()
	p.MeanderAreas = []Area{{MinX: 20, MinY: 20, MaxX: 20, MaxY: 30}}
	rec := h.postJSON("/api/pcb-trace-length-analyzer/sessions/"+id+"/plan", PlanRequest{Params: p})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400 for an area with no extent", rec.Code)
	}
}

// The spacing belongs to the area because that is where the user knows it.
// A global "keep 0.25 mm apart away from the components" needs the tool to
// work out what "away from the components" means and it guesses with a margin
// round every courtyard; "keep 0.25 mm apart in here" needs nothing guessed.
func TestAnAreaCarriesItsOwnSpacing(t *testing.T) {
	h := newHarness(t)
	id := decode[SessionResponse](t, h.upload(true, true)).Session.ID

	room := func(clearance float64) float64 {
		p := DefaultParams()
		p.MeanderAreas = []Area{{
			MinX: 40, MinY: 30, MaxX: 70, MaxY: 60, MinClearanceMM: clearance,
		}}
		rec := h.postJSON("/api/pcb-trace-length-analyzer/sessions/"+id+"/plan", PlanRequest{Params: p})
		if rec.Code != http.StatusOK {
			t.Fatalf("plan at %v: status %d: %s", clearance, rec.Code, truncate(rec.Body.String()))
		}
		rec = h.do("GET", "/api/pcb-trace-length-analyzer/sessions/"+id+"/headroom", nil, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("headroom: status %d", rec.Code)
		}
		return decode[HeadroomResponse](t, rec).GettableMM
	}

	loose := room(0)
	if loose <= 0 {
		t.Fatal("the area holds no room at the board's own spacing")
	}
	if tight := room(0.35); tight >= loose {
		t.Errorf("asking for 0.35 mm inside the area left %.3f mm, no less than the %.3f mm the board allows",
			tight, loose)
	}
	// And it only ever tightens: a figure below the board's own changes
	// nothing, because an area is somewhere to put copper, not a licence to
	// crowd it.
	if slack := room(0.05); slack < loose-1e-6 || slack > loose+1e-6 {
		t.Errorf("asking for 0.05 mm gave %.3f mm where the board's own gives %.3f", slack, loose)
	}
}

// Proposing the areas beats leaving them to be invented: a user starting from
// nothing draws a rectangle, is told it holds 0.000 mm, and tries again.
func TestSuggestedAreasAreWorthWhatTheyClaim(t *testing.T) {
	h := newHarness(t)
	id := decode[SessionResponse](t, h.upload(true, true)).Session.ID

	rec := h.do("GET", "/api/pcb-trace-length-analyzer/sessions/"+id+"/suggested-areas", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, truncate(rec.Body.String()))
	}
	got := decode[SuggestResponse](t, rec)
	if len(got.Areas) == 0 {
		t.Fatal("nothing proposed for a board with 235 mm to find")
	}
	if got.Note == "" {
		t.Error("proposed without saying what these are and are not")
	}
	// Ordered by what they are worth, so taking only the first takes the best.
	for i := 1; i < len(got.Areas); i++ {
		if got.Areas[i].GainMM > got.Areas[i-1].GainMM+1e-9 {
			t.Errorf("area %d is worth more than area %d", i, i-1)
		}
	}
	for i, a := range got.Areas {
		if a.Empty() {
			t.Errorf("area %d has no extent", i)
		}
		if a.GainMM <= 0 || a.Nets <= 0 {
			t.Errorf("area %d is worth %.3f mm over %d nets", i, a.GainMM, a.Nets)
		}
	}

	// And drawing what was proposed leaves the room it promised. Not the exact
	// figure -- the proposal measures each net's stretches and the plan caps
	// each net at what it needs -- but the same order, and nowhere near zero.
	p := DefaultParams()
	for _, a := range got.Areas {
		p.MeanderAreas = append(p.MeanderAreas, a.Area)
	}
	if rec := h.postJSON("/api/pcb-trace-length-analyzer/sessions/"+id+"/plan", PlanRequest{Params: p}); rec.Code != http.StatusOK {
		t.Fatalf("plan: status %d: %s", rec.Code, truncate(rec.Body.String()))
	}
	rec = h.do("GET", "/api/pcb-trace-length-analyzer/sessions/"+id+"/headroom", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("headroom: status %d", rec.Code)
	}
	inside := decode[HeadroomResponse](t, rec).GettableMM
	if inside <= 0 {
		t.Fatal("the proposed areas hold no room, though they were proposed for holding some")
	}

	// They should capture most of what the whole board offers -- that is what
	// makes them worth proposing rather than a starting point picked at random.
	p.MeanderAreas = nil
	if rec := h.postJSON("/api/pcb-trace-length-analyzer/sessions/"+id+"/plan", PlanRequest{Params: p}); rec.Code != http.StatusOK {
		t.Fatal("plan without areas failed")
	}
	rec = h.do("GET", "/api/pcb-trace-length-analyzer/sessions/"+id+"/headroom", nil, "")
	anywhere := decode[HeadroomResponse](t, rec).GettableMM
	if inside < anywhere*0.5 {
		t.Errorf("the proposal holds %.3f mm of the %.3f mm the whole board offers", inside, anywhere)
	}
	t.Logf("%d areas proposed, holding %.3f mm of the %.3f mm the board offers",
		len(got.Areas), inside, anywhere)
}

// Nothing to propose is an answer, not a failure. A board where nothing needs
// length has nowhere to propose an area, and saying so beats an empty list that
// looks like a bug.
func TestSuggestedAreasOnABoardWithNoDDR(t *testing.T) {
	h := newHarness(t)
	got := decode[SessionResponse](t, h.uploadBytes("no-ddr.kicad_pcb", tinyBoard()))
	rec := h.do("GET", "/api/pcb-trace-length-analyzer/sessions/"+got.Session.ID+"/suggested-areas", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, truncate(rec.Body.String()))
	}
	res := decode[SuggestResponse](t, rec)
	if len(res.Areas) != 0 {
		t.Errorf("%d areas proposed for a board with nothing to match", len(res.Areas))
	}
	if !strings.Contains(res.Note, "nothing on this board needs length") {
		t.Errorf("note %q does not say why there is nothing", res.Note)
	}
}

func TestSuggestedAreasNeedTheSession(t *testing.T) {
	h := newHarness(t)
	if rec := h.do("GET", "/api/pcb-trace-length-analyzer/sessions/nope/suggested-areas", nil, ""); rec.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404", rec.Code)
	}
}
