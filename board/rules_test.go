package board

import (
	"strings"
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/geom"
)

const demoBoard = "../demo-pcb/ai-vision.kicad_pcb"

func demoWithRules(t *testing.T) (*Board, *Rules) {
	t.Helper()
	b, err := Load(demoBoard)
	if err != nil {
		t.Skipf("demo board missing: %v", err)
	}
	r, err := LoadRules(demoBoard, b)
	if err != nil {
		t.Fatal(err)
	}
	return b, r
}

// The courtyard is what the rules are written in terms of, and it was read from
// polygons alone -- which on this board found nothing at all, because its
// library parts draw courtyards as rectangles, lines and circles.
func TestCourtyardsAreReadWhateverTheyAreDrawnWith(t *testing.T) {
	b, _ := demoWithRules(t)
	for _, ref := range []string{"U3", "U4", "U5"} {
		f := b.Footprint(ref)
		if f == nil {
			t.Fatalf("no %s", ref)
		}
		if len(f.Courtyard) == 0 {
			t.Errorf("%s has no courtyard", ref)
			continue
		}
		// It has to be at least as big as the part's own balls, or it is some
		// other graphic that happened to be on the layer.
		var cbox, pbox Rect2
		for _, poly := range f.Courtyard {
			for _, p := range poly {
				cbox = cbox.include(p.X, p.Y)
			}
		}
		for _, p := range f.Pads {
			pbox = pbox.include(p.Centre.X, p.Centre.Y)
		}
		if cbox.w() < pbox.w() || cbox.h() < pbox.h() {
			t.Errorf("%s courtyard is %.1f x %.1f mm around a ball field of %.1f x %.1f",
				ref, cbox.w(), cbox.h(), pbox.w(), pbox.h())
		}
	}
}

// A tiny box helper, so the test says what it means without borrowing geom's.
type Rect2 struct {
	minX, minY, maxX, maxY float64
	set                    bool
}

func (r Rect2) include(x, y float64) Rect2 {
	if !r.set {
		return Rect2{x, y, x, y, true}
	}
	if x < r.minX {
		r.minX = x
	}
	if y < r.minY {
		r.minY = y
	}
	if x > r.maxX {
		r.maxX = x
	}
	if y > r.maxY {
		r.maxY = y
	}
	return r
}
func (r Rect2) w() float64 { return r.maxX - r.minX }
func (r Rect2) h() float64 { return r.maxY - r.minY }

func TestTheDemoBoardsRulesAreReadAndTheRestAreNamed(t *testing.T) {
	_, r := demoWithRules(t)
	if len(r.List) != 5 {
		t.Fatalf("%d rules, want the board's 5", len(r.List))
	}
	applied, skipped := r.Understood()
	if applied != 4 || skipped != 1 {
		t.Errorf("%d applied and %d skipped, want 4 and 1", applied, skipped)
	}
	// The one left alone is named and says why, rather than being applied on a
	// guess at what insideArea means.
	for _, rule := range r.Skipped() {
		if !strings.Contains(rule.Why, "insideArea") {
			t.Errorf("%s was skipped for %q", rule.Name, rule.Why)
		}
	}
	for _, rule := range r.List {
		if rule.Understood && rule.ClearanceMin <= 0 {
			t.Errorf("%s is applied with a clearance of %v", rule.Name, rule.ClearanceMin)
		}
	}
}

// The rule that matters: inside a BGA courtyard the board allows 0.1 mm, and
// without reading it the router cannot escape a ball.
func TestACourtyardRuleRelaxesTheClearanceInsideItAndNowhereElse(t *testing.T) {
	b, r := demoWithRules(t)
	u4 := b.Footprint("U4")
	if u4 == nil || len(u4.Pads) == 0 {
		t.Skip("no U4")
	}
	// Two pieces of track well inside U4's courtyard.
	inside := u4.Pads[0].Centre
	a := Item{Type: "Track", Net: "/ddr4/DDR_A0", Box: boxAt(inside.X, inside.Y, 0.05)}
	bItem := Item{Type: "Track", Net: "/ddr4/DDR_A1", Box: boxAt(inside.X+0.3, inside.Y, 0.05)}
	got, ok := r.ClearanceFor(a, bItem)
	if !ok {
		t.Fatal("no rule applied inside a BGA courtyard, so a ball cannot be escaped")
	}
	if got > 0.1001 {
		t.Errorf("clearance inside the courtyard is %.4f mm, want the rule's 0.1", got)
	}

	// The same two, far away from any part, get the outside-the-BGA rule.
	far := Item{Type: "Track", Net: "/ddr4/DDR_A0", Box: boxAt(5, 5, 0.05)}
	far2 := Item{Type: "Track", Net: "/ddr4/DDR_A1", Box: boxAt(5.5, 5, 0.05)}
	got, ok = r.ClearanceFor(far, far2)
	if !ok {
		t.Fatal("no rule applied away from the parts, where the board sets 0.2")
	}
	if got < 0.1999 {
		t.Errorf("clearance away from the parts is %.4f mm, want 0.2", got)
	}
}

// A relaxation applies only when both pieces of copper are inside. KiCad asks
// about one; being stricter can only decline where KiCad would allow, which is
// the safe direction for a rule that loosens things.
func TestARelaxationNeedsBothPiecesInside(t *testing.T) {
	b, r := demoWithRules(t)
	u4 := b.Footprint("U4")
	if u4 == nil || len(u4.Pads) == 0 {
		t.Skip("no U4")
	}
	inside := u4.Pads[0].Centre
	in := Item{Type: "Track", Net: "/ddr4/DDR_A0", Box: boxAt(inside.X, inside.Y, 0.05)}
	out := Item{Type: "Track", Net: "/ddr4/DDR_A1", Box: boxAt(5, 5, 0.05)}
	got, ok := r.ClearanceFor(in, out)
	if ok && got < 0.1999 {
		t.Errorf("one piece inside and one outside got the relaxed %.4f mm", got)
	}
}

func boxAt(x, y, half float64) geom.Rect {
	return geom.Rect{MinX: x - half, MinY: y - half, MaxX: x + half, MaxY: y + half}
}

// A condition this does not implement leaves its rule alone. Half-understanding
// a rule that loosens a clearance produces copper KiCad rejects, which is the
// one thing the tool must not do.
func TestAnUnreadableConditionLeavesTheRuleAlone(t *testing.T) {
	for _, cond := range []string{
		"A.insideArea('somewhere')",
		"A.Layer == 'F.Cu'",
		"B.SomethingElse('x')",
		"A.NetClass == 'DDR' && A.unknownThing()",
		"",
		"A.intersectsCourtyard('U3'",
	} {
		src := []byte(`(version 1)
(rule "r" (condition "` + cond + `") (constraint clearance (min 0.1mm)))`)
		r, err := ParseRules(src, nil)
		if err != nil {
			t.Fatalf("%q: %v", cond, err)
		}
		if len(r.List) != 1 {
			t.Fatalf("%q: %d rules", cond, len(r.List))
		}
		if r.List[0].Understood {
			t.Errorf("%q was applied", cond)
		}
		if r.List[0].Why == "" {
			t.Errorf("%q was skipped without saying why", cond)
		}
	}
}

func TestLengthsAreReadInEveryUnitKiCadWrites(t *testing.T) {
	for in, want := range map[string]float64{
		"0.1mm": 0.1, "0.2 mm": 0.2, "4mil": 0.1016, "0.005in": 0.127, "3": 3,
	} {
		if got := parseLength(in); got < want-1e-9 || got > want+1e-9 {
			t.Errorf("%q = %v, want %v", in, got, want)
		}
	}
}
