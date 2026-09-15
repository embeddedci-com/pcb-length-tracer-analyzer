package board

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/embeddedci-com/pcb-autorouter/geom"
	"github.com/embeddedci-com/pcb-autorouter/sexpr"
)

// Custom design rules, as far as they can be trusted.
//
// A .kicad_dru is a small language, and most of it is about things this tool
// has no opinion on. What matters here is the one kind of rule that changes
// where copper may go: a clearance constraint with a condition. The demo board
// has five of them, and three say the same important thing -- inside a BGA's
// courtyard, 0.1 mm is allowed rather than the 0.2 mm the net class asks for.
//
// Until now those were not read, and the tool said so and worked to the
// stricter figure. That is the right default and it cost nothing while the tool
// only lengthened existing traces: being stricter than the board means
// declining to put a meander somewhere it would have fitted. It stopped being
// free when the router arrived, because a BGA ball cannot be escaped at 0.2 mm
// -- the channel between two balls is not wide enough -- so every one of the
// demo board's 26 second-hop connections was refused as having no way through.
//
// Reading a rule that *relaxes* a clearance is a different risk from ignoring
// one. Get it wrong and the tool produces copper KiCad will reject, which is
// the one thing it must not do. So three things hold it down:
//
//   - Only conditions it fully understands are used. Anything else leaves the
//     rule unapplied and named in the report, not guessed at.
//   - A relaxation applies only when the condition holds for *both* pieces of
//     copper being measured, where KiCad asks only about one. That is stricter
//     than KiCad and can only decline where KiCad would allow.
//   - "Inside a courtyard" means the item is entirely inside it, where KiCad
//     says intersects. Again stricter, again in the safe direction.
//
// And the flow still ends by asking KiCad, which is the only authority on its
// own rule language.

// Rule is one custom design rule.
type Rule struct {
	Name string

	// Condition is the rule's condition as written, for the report.
	Condition string

	// ClearanceMin is the clearance the rule sets, in millimetres. Zero when
	// the rule constrains something else.
	ClearanceMin float64

	// Understood is false when the condition uses something this does not
	// implement, in which case the rule is not applied at all.
	Understood bool

	// Why says what stopped it being understood.
	Why string

	match predicate
}

// Item is a piece of copper a condition is evaluated against.
type Item struct {
	// Type is "Track", "Via", "Pad" or "Zone", as the rule language spells it.
	Type string

	// Net is the net name, Owner the footprint reference for a pad.
	Net, Owner string

	// Box is the item's extent, for the geometric predicates.
	Box geom.Rect
}

// Rules are a board's custom rules, with what they need to evaluate.
type Rules struct {
	List []Rule

	// courtyards is each footprint's courtyard, by reference.
	courtyards map[string][]geom.Poly
}

// LoadRules reads the .kicad_dru beside a board, if there is one.
func LoadRules(boardPath string, b *Board) (*Rules, error) {
	dir := filepath.Dir(boardPath)
	base := strings.TrimSuffix(filepath.Base(boardPath), filepath.Ext(boardPath))
	raw, err := os.ReadFile(filepath.Join(dir, base+".kicad_dru"))
	if err != nil {
		if os.IsNotExist(err) {
			return &Rules{}, nil
		}
		return nil, err
	}
	return ParseRules(raw, b)
}

// ParseRules reads the bytes of a .kicad_dru.
func ParseRules(raw []byte, b *Board) (*Rules, error) {
	doc, err := sexpr.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("rules: %w", err)
	}
	r := &Rules{courtyards: map[string][]geom.Poly{}}
	if b != nil {
		for _, f := range b.Footprints {
			if len(f.Courtyard) > 0 {
				r.courtyards[f.Ref] = f.Courtyard
			}
		}
	}
	for _, root := range doc.Roots {
		if root.Name() != "rule" {
			continue
		}
		rule := Rule{Name: root.ArgString(0)}
		if c := root.Child("condition"); c != nil {
			rule.Condition = c.ArgString(0)
		}
		for _, con := range root.Children("constraint") {
			if con.ArgString(0) != "clearance" {
				continue
			}
			for _, kind := range []string{"min", "opt"} {
				if m := con.Child(kind); m != nil {
					rule.ClearanceMin = parseLength(m.ArgString(0))
					break
				}
			}
		}
		switch {
		case rule.ClearanceMin <= 0:
			rule.Why = "it does not set a clearance, which is the only constraint this reads"
		default:
			p, err := compile(rule.Condition, r)
			if err != nil {
				rule.Why = err.Error()
			} else {
				rule.match, rule.Understood = p, true
			}
		}
		r.List = append(r.List, rule)
	}
	return r, nil
}

// parseLength reads "0.1mm" or "4mil".
func parseLength(s string) float64 {
	s = strings.TrimSpace(s)
	mult := 1.0
	switch {
	case strings.HasSuffix(s, "mm"):
		s = strings.TrimSuffix(s, "mm")
	case strings.HasSuffix(s, "mil"):
		s, mult = strings.TrimSuffix(s, "mil"), 0.0254
	case strings.HasSuffix(s, "in"):
		s, mult = strings.TrimSuffix(s, "in"), 25.4
	}
	var v float64
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%g", &v); err != nil {
		return 0
	}
	return v * mult
}

// ClearanceFor returns the clearance the custom rules set between two pieces of
// copper, and whether any rule applied.
//
// Later rules win, which is how KiCad resolves them. A rule applies only when
// its condition holds for both items: the rule language asks about one, and
// being stricter than that is the safe direction for a relaxation.
func (r *Rules) ClearanceFor(a, b Item) (float64, bool) {
	if r == nil {
		return 0, false
	}
	out, found := 0.0, false
	for i := range r.List {
		rule := &r.List[i]
		if !rule.Understood || rule.match == nil {
			continue
		}
		if rule.match(a, r) && rule.match(b, r) {
			out, found = rule.ClearanceMin, true
		}
	}
	return out, found
}

// TightestClearance is the smallest clearance any applied rule permits, zero
// when none does.
//
// It is what a gridded router has to size its grid by. The rules can allow
// 0.1 mm inside a BGA courtyard where the net class asks 0.2, and a grid
// stepped at the class figure has no cell in the escape channel between two
// balls -- so the router reports no way out of a part the board escapes
// perfectly well.
func (r *Rules) TightestClearance() float64 {
	if r == nil {
		return 0
	}
	out := 0.0
	for _, rule := range r.List {
		if !rule.Understood || rule.ClearanceMin <= 0 {
			continue
		}
		if out == 0 || rule.ClearanceMin < out {
			out = rule.ClearanceMin
		}
	}
	return out
}

// Understood counts the rules that are applied and the rules that are not.
func (r *Rules) Understood() (applied, skipped int) {
	if r == nil {
		return 0, 0
	}
	for _, rule := range r.List {
		if rule.Understood {
			applied++
			continue
		}
		skipped++
	}
	return applied, skipped
}

// Skipped lists the rules that are not applied, with why.
func (r *Rules) Skipped() []Rule {
	if r == nil {
		return nil
	}
	var out []Rule
	for _, rule := range r.List {
		if !rule.Understood {
			out = append(out, rule)
		}
	}
	return out
}

// insideCourtyard reports whether an item lies wholly within a footprint's
// courtyard.
//
// Wholly, where KiCad's intersectsCourtyard asks only that they touch. The
// difference matters only for copper straddling the boundary, and there the
// stricter reading declines a relaxation KiCad would allow -- which costs a
// little routing freedom at the edge of a part and cannot produce copper KiCad
// rejects.
func (r *Rules) insideCourtyard(ref string, box geom.Rect) bool {
	for _, poly := range r.courtyards[ref] {
		if polyContainsBox(poly, box) {
			return true
		}
	}
	return false
}

func polyContainsBox(poly geom.Poly, box geom.Rect) bool {
	corners := []geom.Pt{
		{X: box.MinX, Y: box.MinY}, {X: box.MaxX, Y: box.MinY},
		{X: box.MaxX, Y: box.MaxY}, {X: box.MinX, Y: box.MaxY},
	}
	for _, c := range corners {
		if !poly.Contains(c) {
			return false
		}
	}
	return true
}
