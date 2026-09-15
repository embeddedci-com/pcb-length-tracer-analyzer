// Package sexpr parses and writes the S-expression dialect KiCad uses for its
// .kicad_pcb, .kicad_dru and footprint files.
//
// The whole point of this package is byte-exact round-tripping. A .kicad_pcb is
// a human-edited file the user opens in pcbnew every day; a tool that rewrites
// 2.4 MB of it to change four tracks would produce an unreviewable diff and
// would silently drop any construct the parser did not model. So every node
// remembers the exact source text it came from, including the whitespace
// between its children, and Write emits that text verbatim unless the node was
// actually modified. Only modified (or newly built) nodes get reformatted, in
// KiCad's own style.
package sexpr

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// Kind distinguishes the two node shapes.
type Kind int

const (
	// KindAtom is a bare token: a symbol, a number, or a quoted string.
	KindAtom Kind = iota
	// KindList is a parenthesised list of items.
	KindList
)

// Node is one S-expression node.
//
// An atom holds its raw source token in Raw — quotes and escapes exactly as
// they appeared. A list holds its items, plus the inter-item whitespace needed
// to reproduce the original layout.
type Node struct {
	Kind Kind

	// Raw is the verbatim source token, for atoms.
	Raw string

	// Items are the list's children, for lists.
	Items []*Node

	// pre[i] is the whitespace (and any comments) that preceded Items[i] in the
	// source. len(pre) == len(Items). post is the whitespace before the closing
	// paren.
	pre  []string
	post string

	// dirty marks a node whose source text no longer describes it, either
	// because it was edited or because it was built from scratch. A dirty node
	// is reformatted on write, and dirtiness propagates to ancestors when they
	// are written.
	dirty bool

	// depth is the nesting level, used to indent reformatted output the way
	// KiCad does.
	depth int
}

// Atom builds an unquoted atom (a symbol or a pre-formatted number).
func Atom(raw string) *Node { return &Node{Kind: KindAtom, Raw: raw, dirty: true} }

// String builds a quoted string atom, escaping as KiCad does.
func String(s string) *Node { return &Node{Kind: KindAtom, Raw: quote(s), dirty: true} }

// Float builds a numeric atom using KiCad's millimetre formatting.
func Float(v float64) *Node { return &Node{Kind: KindAtom, Raw: FormatFloat(v), dirty: true} }

// List builds a list from the given items.
func List(items ...*Node) *Node {
	n := &Node{Kind: KindList, Items: items, dirty: true}
	n.pre = make([]string, len(items))
	return n
}

// Sym builds the common `(name rest...)` form.
func Sym(name string, rest ...*Node) *Node {
	return List(append([]*Node{Atom(name)}, rest...)...)
}

// FormatFloat renders a millimetre value the way KiCad's writer does: at most
// six decimal places, with trailing zeros and a trailing point removed.
//
// KiCad stores coordinates as integer nanometres, so six decimals is exactly
// lossless for anything the program itself could have written, and anything we
// compute is rounded onto the same 1 nm grid. Keeping to that grid is what lets
// an untouched value survive a load/save cycle unchanged.
func FormatFloat(v float64) string {
	// Round onto the 1 nm grid first so that, e.g., 0.09000000000000001 does
	// not come out as "0.09" by luck of the formatter but by construction.
	nm := int64(0)
	if v >= 0 {
		nm = int64(v*1e6 + 0.5)
	} else {
		nm = -int64(-v*1e6 + 0.5)
	}
	if nm == 0 {
		return "0"
	}
	s := strconv.FormatFloat(float64(nm)/1e6, 'f', 6, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimSuffix(s, ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// Unquote returns an atom's value with surrounding quotes and escapes removed.
// An unquoted atom is returned as-is.
func Unquote(raw string) string {
	if len(raw) < 2 || raw[0] != '"' {
		return raw
	}
	inner := raw[1 : len(raw)-1]
	if !strings.Contains(inner, `\`) {
		return inner
	}
	var b strings.Builder
	for i := 0; i < len(inner); i++ {
		if inner[i] != '\\' || i+1 >= len(inner) {
			b.WriteByte(inner[i])
			continue
		}
		i++
		switch inner[i] {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		default:
			b.WriteByte(inner[i])
		}
	}
	return b.String()
}

// ---- accessors ----

// Name returns the list's head symbol, or "" if the node is not a list whose
// first item is an atom.
func (n *Node) Name() string {
	if n == nil || n.Kind != KindList || len(n.Items) == 0 || n.Items[0].Kind != KindAtom {
		return ""
	}
	return Unquote(n.Items[0].Raw)
}

// Value returns an atom's unquoted text.
func (n *Node) Value() string {
	if n == nil || n.Kind != KindAtom {
		return ""
	}
	return Unquote(n.Raw)
}

// Arg returns the i'th item after the head symbol, or nil.
func (n *Node) Arg(i int) *Node {
	if n == nil || n.Kind != KindList || len(n.Items) <= i+1 {
		return nil
	}
	return n.Items[i+1]
}

// Args returns every item after the head symbol.
func (n *Node) Args() []*Node {
	if n == nil || n.Kind != KindList || len(n.Items) == 0 {
		return nil
	}
	return n.Items[1:]
}

// ArgString returns the i'th argument as an unquoted string.
func (n *Node) ArgString(i int) string { return n.Arg(i).Value() }

// ArgFloat returns the i'th argument parsed as a number. The second result is
// false if the argument is missing or unparseable.
func (n *Node) ArgFloat(i int) (float64, bool) {
	a := n.Arg(i)
	if a == nil || a.Kind != KindAtom {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(Unquote(a.Raw)), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// Child returns the first direct child list with the given head symbol, or nil.
func (n *Node) Child(name string) *Node {
	if n == nil {
		return nil
	}
	for _, it := range n.Items {
		if it.Kind == KindList && it.Name() == name {
			return it
		}
	}
	return nil
}

// Children returns every direct child list with the given head symbol.
func (n *Node) Children(name string) []*Node {
	if n == nil {
		return nil
	}
	var out []*Node
	for _, it := range n.Items {
		if it.Kind == KindList && it.Name() == name {
			out = append(out, it)
		}
	}
	return out
}

// ChildString returns the first argument of the named child as a string.
func (n *Node) ChildString(name string) string { return n.Child(name).ArgString(0) }

// ChildFloat returns the first argument of the named child as a number.
func (n *Node) ChildFloat(name string) (float64, bool) { return n.Child(name).ArgFloat(0) }

// ---- mutation ----

// Dirty reports whether the node will be reformatted on write.
func (n *Node) Dirty() bool { return n != nil && n.dirty }

// Touch marks the node as modified, so that Write reformats it instead of
// echoing its original source text.
func (n *Node) Touch() {
	if n != nil {
		n.dirty = true
	}
}

// SetFloat replaces the i'th argument with a number, marking the node dirty.
func (n *Node) SetFloat(i int, v float64) {
	if a := n.Arg(i); a != nil {
		a.Raw = FormatFloat(v)
		a.dirty = true
		n.dirty = true
	}
}

// Append adds items to the end of a list, marking it dirty.
func (n *Node) Append(items ...*Node) {
	if n == nil || n.Kind != KindList {
		return
	}
	n.Items = append(n.Items, items...)
	for len(n.pre) < len(n.Items) {
		n.pre = append(n.pre, "")
	}
	n.dirty = true
}

// Replace swaps the child at raw item index i for the given nodes. Used to
// splice a tuned meander in where a single straight segment used to be.
//
// The index counts every item including the list's head symbol, so in
// `(kicad_pcb (segment ...) ...)` the first segment is at index 1. Use IndexOf
// to find it rather than computing an offset.
func (n *Node) Replace(i int, with ...*Node) {
	if n == nil || n.Kind != KindList || i < 0 || i >= len(n.Items) {
		return
	}
	items := make([]*Node, 0, len(n.Items)+len(with)-1)
	items = append(items, n.Items[:i]...)
	items = append(items, with...)
	items = append(items, n.Items[i+1:]...)
	n.Items = items
	n.pre = make([]string, len(items))
	n.dirty = true
}

// IndexOf returns the raw item index of the given child, or -1.
func (n *Node) IndexOf(child *Node) int {
	if n == nil {
		return -1
	}
	for i, it := range n.Items {
		if it == child {
			return i
		}
	}
	return -1
}

// RemoveFunc deletes every direct child for which drop returns true and
// reports how many were removed.
func (n *Node) RemoveFunc(drop func(*Node) bool) int {
	if n == nil || n.Kind != KindList {
		return 0
	}
	kept := n.Items[:0]
	removed := 0
	for _, it := range n.Items {
		if drop(it) {
			removed++
			continue
		}
		kept = append(kept, it)
	}
	if removed > 0 {
		n.Items = kept
		n.pre = make([]string, len(n.Items))
		n.dirty = true
	}
	return removed
}

// ---- writing ----

// Write serialises the node, keeping the diff against the original file as
// small as possible. There are three cases, in order of increasing disturbance:
//
//   - nothing in the subtree changed: echo the original bytes;
//   - the node itself is unchanged but something below it is: keep this node's
//     own separators and recurse, so only the changed descendant is touched;
//   - the node itself changed (items added, removed or spliced): regenerate its
//     separators in KiCad's style, still recursing so unchanged descendants
//     keep their original text.
func (n *Node) Write(b *bytes.Buffer) {
	if n == nil {
		return
	}
	if n.Kind == KindAtom {
		b.WriteString(n.Raw)
		return
	}
	switch {
	case !n.subtreeDirty():
		n.writeVerbatim(b)
	case !n.dirty:
		n.writePreservingSeparators(b)
	default:
		n.writeFormatted(b, n.depth)
	}
}

// writePreservingSeparators keeps this list's original inter-item whitespace
// and recurses into the children, so a one-track edit deep inside a 2 MB file
// reformats only that track.
func (n *Node) writePreservingSeparators(b *bytes.Buffer) {
	b.WriteByte('(')
	for i, it := range n.Items {
		if i < len(n.pre) {
			b.WriteString(n.pre[i])
		}
		it.Write(b)
	}
	b.WriteString(n.post)
	b.WriteByte(')')
}

func (n *Node) subtreeDirty() bool {
	if n.dirty {
		return true
	}
	for _, it := range n.Items {
		if it.subtreeDirty() {
			return true
		}
	}
	return false
}

func (n *Node) writeVerbatim(b *bytes.Buffer) {
	b.WriteByte('(')
	for i, it := range n.Items {
		if i < len(n.pre) {
			b.WriteString(n.pre[i])
		}
		if it.Kind == KindAtom {
			b.WriteString(it.Raw)
		} else {
			it.writeVerbatim(b)
		}
	}
	b.WriteString(n.post)
	b.WriteByte(')')
}

// writeFormatted reproduces KiCad's layout rules: a list of bare atoms stays on
// one line with single spaces; a list containing any sub-list puts every item on
// its own line, indented one tab deeper, with the closing paren on a line of
// its own at the parent's indent.
func (n *Node) writeFormatted(b *bytes.Buffer, depth int) {
	hasList := false
	for _, it := range n.Items {
		if it.Kind == KindList {
			hasList = true
			break
		}
	}
	b.WriteByte('(')
	if !hasList {
		for i, it := range n.Items {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(it.Raw)
		}
		b.WriteByte(')')
		return
	}
	indent := strings.Repeat("\t", depth+1)
	for i, it := range n.Items {
		// A leading head symbol stays on the opening line, as KiCad writes it.
		if i == 0 && it.Kind == KindAtom {
			b.WriteString(it.Raw)
			continue
		}
		b.WriteByte('\n')
		b.WriteString(indent)
		if it.Kind == KindAtom {
			b.WriteString(it.Raw)
			continue
		}
		it.depth = depth + 1
		it.Write(b)
	}
	b.WriteByte('\n')
	b.WriteString(strings.Repeat("\t", depth))
	b.WriteByte(')')
}

// Bytes serialises the node to a byte slice.
func (n *Node) Bytes() []byte {
	var b bytes.Buffer
	n.Write(&b)
	return b.Bytes()
}

// SetDepth records the nesting level used to indent reformatted output. Parse
// sets this automatically; callers only need it for hand-built subtrees.
func (n *Node) SetDepth(d int) {
	if n == nil {
		return
	}
	n.depth = d
	for _, it := range n.Items {
		it.SetDepth(d + 1)
	}
}

// ---- parsing ----

type parser struct {
	src string
	pos int
}

// Parse reads a KiCad S-expression file.
//
// A .kicad_pcb has a single root node, but a .kicad_dru is a sequence of
// top-level forms — `(version 1)` followed by one `(rule ...)` per rule — so the
// document holds a list of roots and the whitespace between them.
func Parse(src []byte) (*Doc, error) {
	p := &parser{src: string(src)}
	d := &Doc{}
	for {
		ws := p.skipSpace()
		if p.pos >= len(p.src) {
			d.trail = ws
			break
		}
		if p.src[p.pos] != '(' {
			return nil, fmt.Errorf("sexpr: expected '(' at offset %d, got %q", p.pos, p.src[p.pos])
		}
		root, err := p.parseList(0)
		if err != nil {
			return nil, err
		}
		d.Roots = append(d.Roots, root)
		d.lead = append(d.lead, ws)
	}
	if len(d.Roots) == 0 {
		return nil, fmt.Errorf("sexpr: no top-level expression")
	}
	return d, nil
}

// Doc is a parsed file: its top-level nodes plus the whitespace around them.
type Doc struct {
	Roots []*Node

	// lead[i] is the whitespace before Roots[i]; trail is what follows the last
	// one. Keeping both is what makes the round trip byte-exact.
	lead  []string
	trail string
}

// Root returns the first top-level node, which for a .kicad_pcb is the whole
// board.
func (d *Doc) Root() *Node {
	if d == nil || len(d.Roots) == 0 {
		return nil
	}
	return d.Roots[0]
}

// Bytes serialises the document.
func (d *Doc) Bytes() []byte {
	var b bytes.Buffer
	for i, r := range d.Roots {
		if i < len(d.lead) {
			b.WriteString(d.lead[i])
		}
		r.Write(&b)
	}
	b.WriteString(d.trail)
	return b.Bytes()
}

func (p *parser) skipSpace() string {
	start := p.pos
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			p.pos++
			continue
		}
		break
	}
	return p.src[start:p.pos]
}

func (p *parser) parseList(depth int) (*Node, error) {
	p.pos++ // consume '('
	n := &Node{Kind: KindList, depth: depth}
	for {
		ws := p.skipSpace()
		if p.pos >= len(p.src) {
			return nil, fmt.Errorf("sexpr: unterminated list at offset %d", p.pos)
		}
		if p.src[p.pos] == ')' {
			p.pos++
			n.post = ws
			return n, nil
		}
		var item *Node
		var err error
		if p.src[p.pos] == '(' {
			item, err = p.parseList(depth + 1)
			if err != nil {
				return nil, err
			}
		} else {
			item, err = p.parseAtom(depth + 1)
			if err != nil {
				return nil, err
			}
		}
		n.Items = append(n.Items, item)
		n.pre = append(n.pre, ws)
	}
}

func (p *parser) parseAtom(depth int) (*Node, error) {
	start := p.pos
	if p.src[p.pos] == '"' {
		p.pos++
		for p.pos < len(p.src) {
			c := p.src[p.pos]
			if c == '\\' {
				p.pos += 2
				continue
			}
			p.pos++
			if c == '"' {
				return &Node{Kind: KindAtom, Raw: p.src[start:p.pos], depth: depth}, nil
			}
		}
		return nil, fmt.Errorf("sexpr: unterminated string at offset %d", start)
	}
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '(' || c == ')' {
			break
		}
		p.pos++
	}
	if p.pos == start {
		return nil, fmt.Errorf("sexpr: empty atom at offset %d", start)
	}
	return &Node{Kind: KindAtom, Raw: p.src[start:p.pos], depth: depth}, nil
}
