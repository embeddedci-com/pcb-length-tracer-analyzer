package server

import (
	"bytes"
	"math"
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
	"github.com/embeddedci-com/pcb-autorouter/route"
)

// Making copper is a different kind of change from lengthening it, and these
// check the part of that which is not the search: what the request reports,
// what it leaves behind, and what it refuses to touch when it achieves
// nothing. The search itself is the router's own business and has its own
// tests -- driving it here would make these take ten seconds to say nothing
// about the handler.

// withRouter stands in for the search. Restores the real one afterwards, and
// these tests are deliberately not parallel because of it.
func withRouter(t *testing.T, f func(*board.Board, *board.Project, []route.Request, route.Options) ([]*route.Result, error)) {
	t.Helper()
	was := runRouter
	runRouter = f
	t.Cleanup(func() { runRouter = was })
}

// connected answers as though every request was made, and lays a stub of
// copper for each so the board really does change.
func connected(b *board.Board, _ *board.Project, reqs []route.Request, _ route.Options) ([]*route.Result, error) {
	out := make([]*route.Result, 0, len(reqs))
	for i, q := range reqs {
		y := float64(i) * 0.5
		b.AddTrack(&board.Track{
			Kind: board.KindSegment, Net: q.Net, Layer: "F.Cu", Width: 0.1,
			Start: geom.Pt{X: 200, Y: 200 + y}, End: geom.Pt{X: 205, Y: 200 + y},
		})
		out = append(out, &route.Result{
			Net: q.Net, From: q.From, To: q.To, Routed: true, LengthMM: 5, Vias: 1, Attempts: 2,
		})
	}
	return out, nil
}

func refused(_ *board.Board, _ *board.Project, reqs []route.Request, _ route.Options) ([]*route.Result, error) {
	out := make([]*route.Result, 0, len(reqs))
	for _, q := range reqs {
		out = append(out, &route.Result{
			Net: q.Net, From: q.From, To: q.To, Reason: "no way through", Attempts: 5,
		})
	}
	return out, nil
}

func TestRoutingReportsEveryHopItWasAskedFor(t *testing.T) {
	h := newHarness(t)
	id := decode[SessionResponse](t, h.upload(true, true)).Session.ID
	withRouter(t, connected)

	rec := h.postJSON("/api/pcb-trace-length-analyzer/sessions/"+id+"/route", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, truncate(rec.Body.String()))
	}
	got := decode[RouteResponse](t, rec)
	if got.Requested == 0 {
		t.Fatal("the demo board is missing a whole leg of its chain; nothing was requested")
	}
	if got.Connected != got.Requested {
		t.Errorf("connected %d of %d, and this router said yes to all of them", got.Connected, got.Requested)
	}
	if len(got.Hops) != got.Requested {
		t.Errorf("%d hops reported for %d requests: every one has to be accounted for",
			len(got.Hops), got.Requested)
	}
	// Named the way the rest of the tool names nets, or the list cannot be
	// read beside the group tables.
	for _, hop := range got.Hops {
		if hop.Label == "" || hop.From == "" || hop.To == "" {
			t.Errorf("a hop arrived as %+v", hop)
			break
		}
	}
	if !got.Changed {
		t.Error("copper was laid and the response says nothing changed")
	}
}

func TestARoutedBoardBecomesTheBoardTheSessionWorksOn(t *testing.T) {
	h := newHarness(t)
	id := decode[SessionResponse](t, h.upload(true, true)).Session.ID
	before, err := h.store.Board(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	withRouter(t, connected)

	got := decode[RouteResponse](t, h.postJSON("/api/pcb-trace-length-analyzer/sessions/"+id+"/route", nil))

	after, err := h.store.Board(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) <= len(before) {
		// Everything measured from here on is measured over the new copper;
		// leaving the old board in place would mean matching a topology the
		// board no longer has.
		t.Errorf("the session still holds %d bytes of board; the routed one is %d", len(after), len(before))
	}
	if got.Session == nil || got.Session.Routed == nil {
		t.Fatal("the session does not record that it was routed")
	}
	if got.Session.Routed.Connected != got.Connected {
		t.Errorf("record says %d connected, response says %d", got.Session.Routed.Connected, got.Connected)
	}
	// A board that has only been routed is not a tuned board.
	if name := got.Session.Routed.ResultFilename; name != "ai-vision.routed.kicad_pcb" {
		t.Errorf("offered as %q", name)
	}
	if got.After == nil {
		t.Fatal("no re-measurement came back, so the page would still show the old board")
	}

	// And it is downloadable, because the whole point is to check it in KiCad.
	rec := h.do("GET", "/api/pcb-trace-length-analyzer/sessions/"+id+"/download", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("download: status %d", rec.Code)
	}
	if _, err := parseBoard(rec.Body.Bytes()); err != nil {
		t.Errorf("what came back does not parse as a board: %v", err)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="ai-vision.routed.kicad_pcb"` {
		t.Errorf("offered as %s", got)
	}
}

func TestRoutingNothingLeavesTheBoardAlone(t *testing.T) {
	h := newHarness(t)
	id := decode[SessionResponse](t, h.upload(true, true)).Session.ID
	before, err := h.store.Board(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	withRouter(t, refused)

	got := decode[RouteResponse](t, h.postJSON("/api/pcb-trace-length-analyzer/sessions/"+id+"/route", nil))
	if got.Changed || got.Connected != 0 {
		t.Fatalf("nothing was routed and the response claims %d connections", got.Connected)
	}
	after, err := h.store.Board(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Error("a failed run rewrote the board anyway")
	}
	if len(got.Notes) == 0 {
		t.Error("52 refusals and not a word about why")
	}
	// Each refusal carries its own reason, because "it did not work" is not
	// something anybody can act on.
	for _, hop := range got.Hops {
		if hop.Reason == "" {
			t.Errorf("%s failed with no reason given", hop.Label)
			break
		}
	}
	// Nothing to download: there is no result.
	if rec := h.do("GET", "/api/pcb-trace-length-analyzer/sessions/"+id+"/download", nil, ""); rec.Code != http.StatusNotFound {
		t.Errorf("download after a failed route: status %d", rec.Code)
	}
}

func TestRoutingNeedsTheSession(t *testing.T) {
	h := newHarness(t)
	if rec := h.postJSON("/api/pcb-trace-length-analyzer/sessions/nope/route", nil); rec.Code != http.StatusNotFound {
		t.Errorf("status %d for a session that does not exist", rec.Code)
	}
}

// The board's own .kicad_dru, read rather than noted.
//
// It used to be taken, recorded as existing, and thrown away, so every
// clearance was checked against the net classes at their strictest. That is
// safe and it is also wrong in a way that costs the user: the demo board's
// rules relax clearance to 0.1 mm inside the BGA courtyards, which is the
// difference between a ball that can be escaped and one that cannot.
func TestTheUploadedRulesAreRead(t *testing.T) {
	h := newHarness(t)
	with := decode[SessionResponse](t, h.upload(true, true)).Analysis
	if with.Board.CustomRules == nil {
		t.Fatal("the .kicad_dru was uploaded and the report says nothing about it")
	}
	if with.Board.CustomRules.Applied == 0 {
		t.Error("no rule from the .kicad_dru was applied")
	}
	// And a rule it cannot read is named rather than silently dropped: the
	// demo board has one that uses insideArea().
	if with.Board.CustomRules.Skipped > 0 && len(with.Board.CustomRules.SkippedRules) == 0 {
		t.Error("a rule was skipped and not named")
	}
	for _, r := range with.Board.CustomRules.SkippedRules {
		if r.Name == "" || r.Why == "" {
			t.Errorf("a skipped rule arrived as %+v", r)
		}
	}

	// Without the file, nothing is claimed about rules at all.
	h2 := newHarness(t)
	without := decode[SessionResponse](t, h2.upload(true, false)).Analysis
	if without.Board.CustomRules != nil {
		t.Error("no .kicad_dru was uploaded and the report describes some anyway")
	}
}

func TestAnUnreadableRulesFileIsRefusedAtUpload(t *testing.T) {
	h := newHarness(t)
	rec := h.uploadBadRules(t, "not a kicad_dru at all (((")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d for a .kicad_dru that is not one: %s", rec.Code, truncate(rec.Body.String()))
	}
	if !strings.Contains(rec.Body.String(), "kicad_dru") {
		t.Errorf("the refusal does not say which file was wrong: %s", truncate(rec.Body.String()))
	}
}

// uploadBadRules posts the demo board with a rules file that is not one.
func (h *harness) uploadBadRules(t *testing.T, rules string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	data, err := os.ReadFile(filepath.Join(demoDir, "ai-vision.kicad_pcb"))
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	part, err := mw.CreateFormFile("board", "ai-vision.kicad_pcb")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	part, err = mw.CreateFormFile("rules", "ai-vision.kicad_dru")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(rules)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return h.do("POST", "/api/pcb-trace-length-analyzer/sessions", &buf, mw.FormDataContentType())
}

// skewedUSB is a board with nothing on it but a USB pair whose D- half is
// shorter than D+ by a jog: no DDR, no groups, one pair out of tolerance.
func skewedUSB() []byte {
	return []byte(`(kicad_pcb
	(version 20260206)
	(generator "pcb-trace-length-analyzer-test")
	(paper "A4")
	(layers (0 "F.Cu" signal) (2 "B.Cu" signal) (25 "Edge.Cuts" user))
	(setup (stackup
		(layer "F.Cu" (type "copper") (thickness 0.035))
		(layer "dielectric 1" (type "core") (thickness 1.51) (material "FR4") (epsilon_r 4.5))
		(layer "B.Cu" (type "copper") (thickness 0.035))
	))
	(gr_rect (start 0 0) (end 40 30) (stroke (width 0.05) (type default)) (fill none) (layer "Edge.Cuts") (uuid "ffffffff-4444-4000-8000-000000000001"))
	(footprint "t:u" (layer "F.Cu") (uuid "aaaaaaaa-4444-4000-8000-000000000002") (at 10 15) (attr smd)
		(property "Reference" "U1" (at 0 0) (layer "F.Cu") (uuid "aaaaaaaa-4444-4000-8000-000000000003"))
		(pad "1" smd circle (at 0 0) (size 0.3 0.3) (layers "F.Cu") (net "/USB_D+") (uuid "aaaaaaaa-4444-4000-8000-000000000004"))
		(pad "2" smd circle (at 0 1) (size 0.3 0.3) (layers "F.Cu") (net "/USB_D-") (uuid "aaaaaaaa-4444-4000-8000-000000000005"))
	)
	(footprint "t:j" (layer "F.Cu") (uuid "aaaaaaaa-4444-4000-8000-000000000006") (at 30 15) (attr smd)
		(property "Reference" "J1" (at 0 0) (layer "F.Cu") (uuid "aaaaaaaa-4444-4000-8000-000000000007"))
		(pad "1" smd circle (at 0 0) (size 0.3 0.3) (layers "F.Cu") (net "/USB_D+") (uuid "aaaaaaaa-4444-4000-8000-000000000008"))
		(pad "2" smd circle (at 0 1) (size 0.3 0.3) (layers "F.Cu") (net "/USB_D-") (uuid "aaaaaaaa-4444-4000-8000-000000000009"))
	)
	(segment (start 10 15) (end 12 15) (width 0.2) (layer "F.Cu") (net "/USB_D+") (uuid "bbbbbbbb-4444-4000-8000-000000000010"))
	(segment (start 12 15) (end 13 14) (width 0.2) (layer "F.Cu") (net "/USB_D+") (uuid "bbbbbbbb-4444-4000-8000-000000000011"))
	(segment (start 13 14) (end 27 14) (width 0.2) (layer "F.Cu") (net "/USB_D+") (uuid "bbbbbbbb-4444-4000-8000-000000000012"))
	(segment (start 27 14) (end 28 15) (width 0.2) (layer "F.Cu") (net "/USB_D+") (uuid "bbbbbbbb-4444-4000-8000-000000000013"))
	(segment (start 28 15) (end 30 15) (width 0.2) (layer "F.Cu") (net "/USB_D+") (uuid "bbbbbbbb-4444-4000-8000-000000000014"))
	(segment (start 10 16) (end 30 16) (width 0.2) (layer "F.Cu") (net "/USB_D-") (uuid "bbbbbbbb-4444-4000-8000-000000000015"))
)
`)
}

// The parity this is here for: a board with no DDR on it is analysed, briefed,
// measured and tuned exactly the way a DDR one is. It used to stop at
// "measured" -- the apply refused any board without a DDR plan.
func TestANonDDRPairIsTunedLikeADDRNet(t *testing.T) {
	h := newHarness(t)
	up := decode[SessionResponse](t, h.uploadBytes("usb.kicad_pcb", skewedUSB()))
	id := up.Session.ID

	var usb *DetectedInterface
	for i := range up.Analysis.Interfaces {
		if up.Analysis.Interfaces[i].Kind == "usb2" {
			usb = &up.Analysis.Interfaces[i]
		}
	}
	if usb == nil {
		t.Fatalf("no USB interface found in %+v", up.Analysis.Interfaces)
	}
	if len(usb.Candidates) != 1 || usb.Candidates[0].Net != "/USB_D-" {
		t.Fatalf("candidates %+v, want the shorter half D- alone", usb.Candidates)
	}
	c := usb.Candidates[0]
	if c.NeedMM < 0.5 || c.NeedMM > 1 {
		t.Errorf("D- needs %.3f mm; the jog on D+ is about 0.83 mm", c.NeedMM)
	}
	// The same per-leg shape a DDR candidate has.
	if len(c.Legs) != 1 || !strings.HasPrefix(c.Legs[0].Group, "pair ") {
		t.Errorf("legs %+v, want one leg naming the pair", c.Legs)
	}

	// The room is measured the way it is for DDR.
	hr := decode[HeadroomResponse](t, h.do("GET", "/api/pcb-trace-length-analyzer/sessions/"+id+"/headroom", nil, ""))
	var measured MemberInfo
	for _, i := range hr.Interfaces {
		for _, m := range i.Candidates {
			if m.Net == "/USB_D-" {
				measured = m
			}
		}
	}
	if measured.HeadroomMM <= 0 {
		t.Errorf("no room measured beside D-, which has the whole board below it")
	}

	// And the apply does it.
	rec := h.postJSON("/api/pcb-trace-length-analyzer/sessions/"+id+"/apply", ApplyRequest{Nets: []string{"/USB_D-"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("apply: status %d: %s", rec.Code, truncate(rec.Body.String()))
	}
	res := decode[ApplyResponse](t, rec)
	if res.AddedMM < c.NeedMM-0.05 {
		t.Errorf("added %.3f mm of the %.3f needed", res.AddedMM, c.NeedMM)
	}
	for _, i := range res.After.Interfaces {
		for _, p := range i.PairSkew {
			if p.Routed && !p.InTolerance {
				t.Errorf("after tuning, %s is still %.3f mm apart (limit %.3f)", p.Name, p.SkewMM, p.LimitMM)
			}
		}
	}
}

// Routing is offered for every interface, not only for the fly-by chain.
func TestTheRouterIsAskedForNonDDRConnections(t *testing.T) {
	h := newHarness(t)
	id := decode[SessionResponse](t, h.upload(true, true)).Session.ID
	var asked []route.Request
	withRouter(t, func(b *board.Board, p *board.Project, reqs []route.Request, o route.Options) ([]*route.Result, error) {
		asked = reqs
		return refused(b, p, reqs, o)
	})
	rec := h.postJSON("/api/pcb-trace-length-analyzer/sessions/"+id+"/route", RouteRequest{
		Nets: []string{"/usb-c/USB_D+", "/usb-c/USB_D-"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, truncate(rec.Body.String()))
	}
	if len(asked) == 0 {
		t.Fatal("two unrouted USB nets and the router was asked for nothing")
	}
	for _, q := range asked {
		if q.Net != "/usb-c/USB_D+" && q.Net != "/usb-c/USB_D-" {
			t.Errorf("asked for %s, which was not in the request", q.Net)
		}
		if q.From == "" || q.To == "" || q.From == q.To {
			t.Errorf("request %+v is not a pad-to-pad join", q)
		}
	}
}

// A public tool has no per-user bound worth the name: every browser that never
// signed in is its own user. The store's byte cap is what keeps the process
// inside its machine, and it gives way from the oldest end.
func TestTheMemoryStoreDropsTheOldestBoardsToStayUnderItsCap(t *testing.T) {
	now := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	m := NewMemoryStore(func() time.Time { return now })
	m.SetMaxBytes(250)
	put := func(id string, at time.Duration) {
		s := &Session{ID: id, UserID: "u", CreatedAt: now.Add(at), ExpiresAt: now.Add(time.Hour)}
		if err := m.Put(t.Context(), s, make([]byte, 100)); err != nil {
			t.Fatal(err)
		}
	}
	put("old", 0)
	put("mid", time.Minute)
	put("new", 2*time.Minute) // 300 bytes would be over: "old" has to go

	if _, err := m.Board(t.Context(), "old"); err == nil {
		t.Error("the oldest board is still held past the cap")
	}
	for _, id := range []string{"mid", "new"} {
		if _, err := m.Board(t.Context(), id); err != nil {
			t.Errorf("%s was dropped, but only one board had to go", id)
		}
	}
	// A result counts too.
	if err := m.PutResult(t.Context(), "new", make([]byte, 60)); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Board(t.Context(), "mid"); err == nil {
		t.Error("a result took the total past the cap and nothing gave way")
	}
	if _, err := m.Result(t.Context(), "new"); err != nil {
		t.Error("the session being written to was evicted to make room for itself")
	}
}

// A pair out of tolerance is shown as a group of its own, and that group has to
// list both halves like any other. It used to carry only its counts, and the
// page, finding no rows, said the pair was not routed right under its length.
func TestAPairGroupListsBothHalves(t *testing.T) {
	h := newHarness(t)
	up := decode[SessionResponse](t, h.uploadBytes("usb.kicad_pcb", skewedUSB()))

	var usb *DetectedInterface
	for i := range up.Analysis.Interfaces {
		if up.Analysis.Interfaces[i].Kind == "usb2" {
			usb = &up.Analysis.Interfaces[i]
		}
	}
	if usb == nil || len(usb.Groups) != 1 {
		t.Fatalf("want one pair group, got %+v", usb)
	}
	g := usb.Groups[0]
	if len(g.Rows) != 2 {
		t.Fatalf("pair group has %d rows, want both halves: %+v", len(g.Rows), g.Rows)
	}
	var ref, out *MemberInfo
	for i := range g.Rows {
		r := &g.Rows[i]
		if !r.Routed {
			t.Errorf("%s is routed, and the row says it is not", r.Net)
		}
		if r.Role == "reference" {
			ref = r
		} else {
			out = r
		}
	}
	if ref == nil || ref.Net != "/USB_D+" || !ref.InTolerance {
		t.Errorf("reference row %+v, want the longer half D+", ref)
	}
	if out == nil || out.Net != "/USB_D-" || out.InTolerance || out.NeedMM <= 0 {
		t.Errorf("out row %+v, want the shorter half D- asking for length", out)
	}
	// The row's figures are the group's.
	if ref != nil && math.Abs(ref.LengthMM-g.ReferenceMM) > 1e-9 {
		t.Errorf("reference row %.3f mm, group says %.3f", ref.LengthMM, g.ReferenceMM)
	}
}
