package board

import (
	"fmt"
	"strings"
)

// The condition language, as much of it as is safe to read.
//
// KiCad's rule conditions are a general expression language. This implements
// the part the demo board uses and refuses the rest, because a condition half
// understood is worse than one not read at all: it would relax a clearance
// somewhere the board never said to, and the copper that came out would be
// rejected by the tool the rule was written for.
//
// So the parser is deliberately narrow. It knows `&&`, `||`, `!`, parentheses,
// a handful of named predicates, and equality against a string. Anything else
// -- a comparison this does not implement, a function it has not heard of --
// fails the whole condition, and the rule is reported as not applied rather
// than applied wrongly.

type predicate func(Item, *Rules) bool

// compile turns a condition into a predicate, or explains why it cannot.
func compile(cond string, r *Rules) (predicate, error) {
	if strings.TrimSpace(cond) == "" {
		return nil, fmt.Errorf("it has no condition")
	}
	p := &condParser{src: cond}
	fn, err := p.expr()
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if p.at < len(p.src) {
		return nil, fmt.Errorf("this does not understand %q in the condition", p.rest())
	}
	return fn, nil
}

type condParser struct {
	src string
	at  int
}

func (p *condParser) rest() string {
	s := strings.TrimSpace(p.src[p.at:])
	if len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}

func (p *condParser) skipSpace() {
	for p.at < len(p.src) && (p.src[p.at] == ' ' || p.src[p.at] == '\t' || p.src[p.at] == '\n') {
		p.at++
	}
}

func (p *condParser) accept(tok string) bool {
	p.skipSpace()
	if strings.HasPrefix(p.src[p.at:], tok) {
		p.at += len(tok)
		return true
	}
	return false
}

// expr is a sequence of `and` joined by ||.
func (p *condParser) expr() (predicate, error) {
	left, err := p.and()
	if err != nil {
		return nil, err
	}
	for p.accept("||") {
		right, err := p.and()
		if err != nil {
			return nil, err
		}
		l, r := left, right
		left = func(i Item, rs *Rules) bool { return l(i, rs) || r(i, rs) }
	}
	return left, nil
}

// and is a sequence of `unary` joined by &&.
func (p *condParser) and() (predicate, error) {
	left, err := p.unary()
	if err != nil {
		return nil, err
	}
	for p.accept("&&") {
		right, err := p.unary()
		if err != nil {
			return nil, err
		}
		l, r := left, right
		left = func(i Item, rs *Rules) bool { return l(i, rs) && r(i, rs) }
	}
	return left, nil
}

func (p *condParser) unary() (predicate, error) {
	if p.accept("!") {
		inner, err := p.unary()
		if err != nil {
			return nil, err
		}
		return func(i Item, rs *Rules) bool { return !inner(i, rs) }, nil
	}
	if p.accept("(") {
		inner, err := p.expr()
		if err != nil {
			return nil, err
		}
		if !p.accept(")") {
			return nil, fmt.Errorf("a bracket is not closed in the condition")
		}
		return inner, nil
	}
	return p.term()
}

// term is a predicate on A or B, optionally compared to a string.
func (p *condParser) term() (predicate, error) {
	p.skipSpace()
	start := p.at
	for p.at < len(p.src) && (isWord(p.src[p.at]) || p.src[p.at] == '.') {
		p.at++
	}
	word := p.src[start:p.at]
	if word == "" {
		return nil, fmt.Errorf("this does not understand %q in the condition", p.rest())
	}

	// Only A is evaluated. B is the other item in a clearance check, and this
	// applies a rule only when it holds for both items anyway, so a condition
	// written about B would be answered about the wrong one.
	subject, field, ok := strings.Cut(word, ".")
	if !ok {
		return nil, fmt.Errorf("this does not understand %q in the condition", word)
	}
	if subject != "A" && subject != "B" {
		return nil, fmt.Errorf("this only reads conditions about A and B, not %q", subject)
	}

	// A call: name('argument').
	if p.accept("(") {
		arg, err := p.stringLit()
		if err != nil {
			return nil, err
		}
		if !p.accept(")") {
			return nil, fmt.Errorf("a bracket is not closed after %s", field)
		}
		switch field {
		case "intersectsCourtyard", "enclosedByArea":
			return func(i Item, rs *Rules) bool { return rs.insideCourtyard(arg, i.Box) }, nil
		case "memberOfFootprint", "memberOf":
			return func(i Item, _ *Rules) bool { return i.Owner == arg }, nil
		}
		return nil, fmt.Errorf("this does not implement %s(), so the rule is left alone", field)
	}

	// A comparison: field == 'value' or != 'value'.
	eq := true
	switch {
	case p.accept("=="):
	case p.accept("!="):
		eq = false
	default:
		return nil, fmt.Errorf("this does not understand %q in the condition", word+p.rest())
	}
	want, err := p.stringLit()
	if err != nil {
		return nil, err
	}
	var get func(Item) string
	switch field {
	case "Type":
		get = func(i Item) string { return i.Type }
	case "NetName":
		get = func(i Item) string { return i.Net }
	default:
		return nil, fmt.Errorf("this does not read A.%s, so the rule is left alone", field)
	}
	return func(i Item, _ *Rules) bool { return matchPattern(want, get(i)) == eq }, nil
}

func (p *condParser) stringLit() (string, error) {
	p.skipSpace()
	if p.at >= len(p.src) {
		return "", fmt.Errorf("the condition ends where a value was expected")
	}
	q := p.src[p.at]
	if q != '\'' && q != '"' {
		return "", fmt.Errorf("this expects a quoted value, not %q", p.rest())
	}
	p.at++
	start := p.at
	for p.at < len(p.src) && p.src[p.at] != q {
		p.at++
	}
	if p.at >= len(p.src) {
		return "", fmt.Errorf("a quoted value is not closed in the condition")
	}
	out := p.src[start:p.at]
	p.at++
	return out, nil
}

func isWord(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
