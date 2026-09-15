package server

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/board"
)

// The two endpoints the KiCad plugin leans on: one net's standing by the name
// pcbnew gives it, and an apply expressed as edits to the open board.

func netsURL(id string, q url.Values) string {
	u := "/api/pcb-trace-length-analyzer/sessions/" + id + "/nets"
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

func TestNetsAttentionListsEveryOutOfToleranceNetWorstFirst(t *testing.T) {
	h := newHarness(t)
	first := decode[SessionResponse](t, h.upload(true, true))
	rec := h.do("GET", netsURL(first.Session.ID, url.Values{"attention": {"1"}}), nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, truncate(rec.Body.String()))
	}
	got := decode[NetsResponse](t, rec)
	if len(got.Nets) == 0 {
		t.Fatal("the demo board has out-of-tolerance nets, the list is empty")
	}
	listed := map[string]bool{}
	for i, s := range got.Nets {
		if s.InTolerance || !s.Routed || s.Reference {
			t.Errorf("%s (%s) listed as needing attention: %+v", s.Label, s.Group, s)
		}
		if s.NeedMM <= 0 && s.ExcessMM <= 0 {
			t.Errorf("%s needs attention but has nothing to add or remove", s.Label)
		}
		if i > 0 {
			prev := got.Nets[i-1]
			if prev.NeedMM+prev.ExcessMM < s.NeedMM+s.ExcessMM-1e-9 {
				t.Errorf("not worst first at %d: %s %.3f before %s %.3f",
					i, prev.Label, prev.NeedMM+prev.ExcessMM, s.Label, s.NeedMM+s.ExcessMM)
			}
		}
		listed[s.Net] = true
	}
	// Everything the report asks to lengthen is on the list: the two must not
	// disagree about what is wrong with the board.
	for _, c := range first.Analysis.Candidates {
		if c.NeedMM > 0 && !listed[c.Net] {
			t.Errorf("candidate %s (needs %.3f mm) missing from the attention list", c.Label, c.NeedMM)
		}
	}
	for _, i := range first.Analysis.Interfaces {
		if i.Planner != "" {
			continue
		}
		for _, c := range i.Candidates {
			if !listed[c.Net] {
				t.Errorf("%s candidate %s missing from the attention list", i.Name, c.Label)
			}
		}
	}
}

func TestNetsLookupByFullNameLabelAndUnknown(t *testing.T) {
	h := newHarness(t)
	first := decode[SessionResponse](t, h.upload(true, true))
	if len(first.Analysis.Groups) == 0 {
		t.Fatal("demo board has no DDR groups")
	}
	m := first.Analysis.Groups[0].Members[0]

	byName := decode[NetsResponse](t, h.do("GET", netsURL(first.Session.ID, url.Values{"name": {m.Net}}), nil, ""))
	if len(byName.Nets) == 0 || byName.Nets[0].Net != m.Net || byName.Nets[0].Interface != "DDR" {
		t.Fatalf("lookup of %s: %+v", m.Net, byName)
	}
	row := byName.Nets[0]
	if d := row.LengthMM - m.LengthMM; d > 1e-9 || d < -1e-9 {
		t.Errorf("length %.4f, the report says %.4f", row.LengthMM, m.LengthMM)
	}
	if row.TargetMM != first.Analysis.Groups[0].TargetMM {
		t.Errorf("target %.4f, the group says %.4f", row.TargetMM, first.Analysis.Groups[0].TargetMM)
	}

	byLabel := decode[NetsResponse](t, h.do("GET", netsURL(first.Session.ID, url.Values{"name": {m.Label}}), nil, ""))
	if len(byLabel.Nets) == 0 || byLabel.Nets[0].Net != m.Net {
		t.Errorf("lookup by label %q found %+v", m.Label, byLabel)
	}

	other := decode[NetsResponse](t, h.do("GET",
		netsURL(first.Session.ID, url.Values{"name": {"GND", "NO_SUCH_NET_ANYWHERE"}}), nil, ""))
	if len(other.Nets) != 0 || len(other.Unmatched) != 2 {
		t.Fatalf("GND and a made-up name: %+v", other)
	}
	if !other.Unmatched[0].Exists || other.Unmatched[1].Exists {
		t.Errorf("existence wrong: %+v", other.Unmatched)
	}
}

// A fly-by net is matched once per leg, and a lookup has to say so rather than
// pick one.
func TestNetsLookupReturnsEveryLegOfAFlyByNet(t *testing.T) {
	h := newHarness(t)
	first := decode[SessionResponse](t, h.upload(true, true))
	legs := map[string]int{}
	for _, g := range first.Analysis.Groups {
		for _, m := range g.Members {
			legs[m.Net]++
		}
	}
	var net string
	for n, c := range legs {
		if c > 1 {
			net = n
			break
		}
	}
	if net == "" {
		t.Skip("no net on the demo board is matched over more than one leg")
	}
	got := decode[NetsResponse](t, h.do("GET", netsURL(first.Session.ID, url.Values{"name": {net}}), nil, ""))
	if len(got.Nets) != legs[net] {
		t.Errorf("%s is in %d groups, the lookup returned %d rows", net, legs[net], len(got.Nets))
	}
}

func TestChangesBeforeApplyIs404(t *testing.T) {
	h := newHarness(t)
	first := decode[SessionResponse](t, h.upload(true, true))
	rec := h.do("GET", "/api/pcb-trace-length-analyzer/sessions/"+first.Session.ID+"/changes", nil, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
}

// The edits, made to the uploaded board, have to produce exactly the board the
// tuner wrote -- item for item, to the nanometre. That is the whole promise the
// plugin makes when it applies them as one commit instead of reloading a file.
func TestChangesReproduceTheTunedBoard(t *testing.T) {
	h := newHarness(t)
	first := decode[SessionResponse](t, h.upload(true, true))
	id := first.Session.ID
	var nets []string
	for _, c := range first.Analysis.Candidates {
		if c.NeedMM > 0 && !c.NeedsReroute && len(nets) < 6 {
			nets = append(nets, c.Net)
		}
	}
	applied := decode[ApplyResponse](t, h.postJSON("/api/pcb-trace-length-analyzer/sessions/"+id+"/apply", ApplyRequest{Nets: nets}))
	if !applied.Changed {
		t.Fatal("apply changed nothing, so there are no edits to test")
	}

	rec := h.do("GET", "/api/pcb-trace-length-analyzer/sessions/"+id+"/changes", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, truncate(rec.Body.String()))
	}
	ch := decode[ChangesResponse](t, rec)
	if len(ch.Remove) == 0 || len(ch.Add) == 0 {
		t.Fatalf("expected removals and additions, got %d and %d", len(ch.Remove), len(ch.Add))
	}
	if ch.Message == "" {
		t.Error("no undo message")
	}

	raw, err := os.ReadFile(filepath.Join(demoDir, "ai-vision.kicad_pcb"))
	if err != nil {
		t.Skip(err)
	}
	before, err := board.Parse(raw, "before")
	if err != nil {
		t.Fatal(err)
	}
	result, err := h.store.Result(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	after, err := board.Parse(result, "after")
	if err != nil {
		t.Fatal(err)
	}

	// Every removal names a track that is on the board, with the geometry it
	// has there -- the plugin's staleness check depends on both.
	byUUID := map[string]ChangeItem{}
	for _, tr := range before.Tracks {
		byUUID[tr.UUID] = changeItem(tr)
	}
	changedNets := map[string]bool{}
	for _, n := range ch.Nets {
		changedNets[n] = true
	}
	removed := map[string]bool{}
	for _, r := range ch.Remove {
		o, ok := byUUID[r.UUID]
		if !ok {
			t.Fatalf("removal of %s, which is not on the board", r.UUID)
		}
		if !sameItem(o, r) {
			t.Errorf("removal %s carries geometry %+v, the board has %+v", r.UUID, r, o)
		}
		if !changedNets[r.Net] {
			t.Errorf("removal on %s, which is not in the nets list", r.Net)
		}
		removed[r.UUID] = true
	}
	for _, a := range ch.Add {
		if a.UUID != "" {
			t.Errorf("an addition carries a uuid (%s); pcbnew assigns those", a.UUID)
		}
		if a.Kind != "via" && (a.WidthNM <= 0 || a.Layer == "") {
			t.Errorf("addition without width or layer: %+v", a)
		}
	}

	// before - remove + add == after, as a multiset of geometry.
	key := func(c ChangeItem) string {
		c.UUID = ""
		mid := ""
		if c.Mid != nil {
			mid = fmt.Sprint(*c.Mid)
		}
		c.Mid = nil
		return fmt.Sprintf("%+v%s", c, mid)
	}
	var rebuilt, want []string
	for _, tr := range before.Tracks {
		if !removed[tr.UUID] {
			rebuilt = append(rebuilt, key(changeItem(tr)))
		}
	}
	for _, a := range ch.Add {
		rebuilt = append(rebuilt, key(a))
	}
	for _, tr := range after.Tracks {
		want = append(want, key(changeItem(tr)))
	}
	sort.Strings(rebuilt)
	sort.Strings(want)
	if len(rebuilt) != len(want) {
		t.Fatalf("rebuilt board has %d items, the tuned one %d", len(rebuilt), len(want))
	}
	for i := range want {
		if rebuilt[i] != want[i] {
			t.Fatalf("item %d differs:\n rebuilt %s\n    want %s", i, rebuilt[i], want[i])
		}
	}

	// And the edits touch only the nets that were asked for.
	asked := map[string]bool{}
	for _, n := range nets {
		asked[n] = true
	}
	for _, n := range ch.Nets {
		if !asked[n] {
			t.Errorf("edits touch %s, which was not asked for", n)
		}
	}
}
