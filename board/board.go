// Package board loads a KiCad PCB into a model the length engine and the
// clearance checker can work on, and writes it back out.
//
// The model is a view over the parsed S-expression tree, not a copy of it:
// every track, via and pad keeps a pointer to the node it came from. Editing a
// track therefore edits the file, and everything the model does not understand
// still round-trips untouched. That is what makes it safe to point this tool at
// somebody's real 2.4 MB design.
package board

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/embeddedci-com/pcb-autorouter/geom"
	"github.com/embeddedci-com/pcb-autorouter/sexpr"
)

// TrackKind distinguishes the three kinds of routed copper.
type TrackKind int

const (
	// KindSegment is a straight track.
	KindSegment TrackKind = iota
	// KindArc is a curved track, stored as start/mid/end.
	KindArc
	// KindVia is a plated hole joining two or more layers.
	KindVia
)

func (k TrackKind) String() string {
	switch k {
	case KindSegment:
		return "segment"
	case KindArc:
		return "arc"
	case KindVia:
		return "via"
	}
	return "?"
}

// Track is one piece of routed copper: a segment, an arc or a via.
type Track struct {
	Kind TrackKind
	Net  string

	// Start and End are the endpoints. For a via both hold the via centre.
	Start, End geom.Pt

	// Mid is the arc's midpoint, unused for segments and vias.
	Mid geom.Pt

	// Layer is the copper layer, for segments and arcs.
	Layer string

	// LayerTop and LayerBottom are the layers a via spans.
	LayerTop, LayerBottom string

	// Width is the track width; Size and Drill are the via's pad and hole
	// diameters.
	Width, Size, Drill float64

	UUID string

	// Node is the S-expression this track was read from, and Index its
	// position among the board root's items. Edits go through these.
	Node  *sexpr.Node
	Index int
}

// Length is the track's geometric length: along the curve for an arc, and the
// vertical span through the stack for a via.
func (t *Track) Length(s *Stackup) float64 {
	switch t.Kind {
	case KindSegment:
		return t.Start.Dist(t.End)
	case KindArc:
		return geom.Arc{Start: t.Start, Mid: t.Mid, End: t.End}.Len()
	case KindVia:
		return s.ViaLength(t.LayerTop, t.LayerBottom)
	}
	return 0
}

// Delay is the track's propagation delay in picoseconds.
func (t *Track) Delay(s *Stackup) float64 {
	if t.Kind == KindVia {
		return t.Length(s) * s.ViaDelayPerMM()
	}
	return t.Length(s) * s.DelayPerMM(t.Layer, t.Width)
}

// Shape returns the track's copper footprint on its layer, for clearance
// checks. Arcs are flattened at the given tolerance.
func (t *Track) Shape(tol float64) []geom.RoundPoly {
	switch t.Kind {
	case KindSegment:
		return []geom.RoundPoly{geom.Capsule(t.Start, t.End, t.Width)}
	case KindArc:
		pts := geom.Arc{Start: t.Start, Mid: t.Mid, End: t.End}.Flatten(tol)
		out := make([]geom.RoundPoly, 0, len(pts)-1)
		for i := 0; i+1 < len(pts); i++ {
			out = append(out, geom.Capsule(pts[i], pts[i+1], t.Width))
		}
		return out
	case KindVia:
		return []geom.RoundPoly{geom.Disc(t.Start, t.Size/2)}
	}
	return nil
}

// Box is the track's bounding box, not counting its width.
func (t *Track) Box() geom.Rect {
	if t.Kind == KindArc {
		return geom.RectFromPts(geom.Arc{Start: t.Start, Mid: t.Mid, End: t.End}.Flatten(0.01)...)
	}
	return geom.RectFromPts(t.Start, t.End)
}

// Layers returns every copper layer the track occupies.
func (t *Track) Layers(s *Stackup) []string {
	if t.Kind != KindVia {
		return []string{t.Layer}
	}
	top, okT := s.Layer(t.LayerTop)
	bot, okB := s.Layer(t.LayerBottom)
	if !okT || !okB {
		return []string{t.LayerTop, t.LayerBottom}
	}
	lo, hi := top.Index, bot.Index
	if lo > hi {
		lo, hi = hi, lo
	}
	var out []string
	for _, l := range s.Copper {
		if l.Index >= lo && l.Index <= hi {
			out = append(out, l.Name)
		}
	}
	return out
}

// Pad is one pad of one footprint, in board coordinates.
type Pad struct {
	// Ref is the parent footprint's reference, Name the pad's own name, so a
	// pad is identified as "U3.K18".
	Ref, Name string
	Net       string

	// Function is the schematic pin function, e.g. "DDR_DQ0_W22". Useful for
	// classifying DDR signals when net names are unhelpful.
	Function string

	// Type is smd, thru_hole, np_thru_hole or connect.
	Type string

	Centre geom.Pt
	Shape  geom.RoundPoly
	Layers []string

	// DieLength is the on-package routing length KiCad adds to the net, zero
	// when the footprint does not declare it.
	DieLength float64
	// DieSource says where DieLength came from: "footprint" when the board
	// file declares it, otherwise whatever filled it in, empty when unset.
	DieSource string

	// Clearance is a pad-local clearance override, zero when unset.
	Clearance float64

	Node *sexpr.Node
}

// ID is the pad's board-unique name, as KiCad's DRC reports it.
func (p *Pad) ID() string { return p.Ref + "." + p.Name }

// OnLayer reports whether the pad has copper on the named layer.
func (p *Pad) OnLayer(l string) bool {
	for _, x := range p.Layers {
		if x == l || x == "*.Cu" {
			return true
		}
	}
	return false
}

// Footprint is a placed component.
type Footprint struct {
	Ref       string
	Value     string
	Library   string
	At        geom.Pt
	Rot       float64 // degrees, as stored
	Pads      []*Pad
	Courtyard []geom.Poly
	Node      *sexpr.Node
}

// Zone is a copper pour. Only its filled polygons matter here, since that is
// what a track has to keep clear of.
type Zone struct {
	Net    string
	Layers []string
	Filled map[string][]geom.Poly // layer -> filled polygons
	Node   *sexpr.Node
}

// Board is a loaded .kicad_pcb.
type Board struct {
	Path string
	Doc  *sexpr.Doc
	Root *sexpr.Node

	Stackup *Stackup

	// CopperLayers lists the copper layer names top to bottom.
	CopperLayers []string

	Tracks     []*Track
	Footprints []*Footprint
	Pads       []*Pad
	Zones      []*Zone

	// Outline is the board edge, as polylines on Edge.Cuts.
	Outline []geom.Seg

	// UseHeightForLength mirrors the board setting of the same name: when set,
	// via barrels count toward net length, which is how KiCad reports it.
	UseHeightForLength bool

	// MaxError is the board's arc approximation tolerance.
	MaxError float64

	padsByNet   map[string][]*Pad
	tracksByNet map[string][]*Track
}

// Load reads a .kicad_pcb.
func Load(path string) (*Board, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(src, path)
}

// Parse builds a board from the bytes of a .kicad_pcb.
func Parse(src []byte, path string) (*Board, error) {
	doc, err := sexpr.Parse(src)
	if err != nil {
		return nil, err
	}
	root := doc.Root()
	if root.Name() != "kicad_pcb" {
		return nil, fmt.Errorf("board: %s is not a .kicad_pcb (root is %q)", path, root.Name())
	}
	b := &Board{
		Path:        path,
		Doc:         doc,
		Root:        root,
		MaxError:    0.005,
		padsByNet:   map[string][]*Pad{},
		tracksByNet: map[string][]*Track{},
	}

	setup := root.Child("setup")
	b.Stackup = parseStackup(setup)
	for _, l := range b.Stackup.Copper {
		b.CopperLayers = append(b.CopperLayers, l.Name)
	}
	// A board with no stackup block still has a (layers ...) list; fall back to
	// that so such files are usable, just without via lengths.
	if len(b.CopperLayers) == 0 {
		for _, ln := range root.Child("layers").Args() {
			if ln.Kind == sexpr.KindList && strings.HasSuffix(ln.ArgString(0), ".Cu") {
				b.CopperLayers = append(b.CopperLayers, ln.ArgString(0))
			}
		}
	}

	// The setting lives in the project file, not the board, so default it on:
	// that is KiCad's own default and matches what pcbnew reports.
	b.UseHeightForLength = true

	for i, it := range root.Items {
		if it.Kind != sexpr.KindList {
			continue
		}
		switch it.Name() {
		case "segment", "arc", "via":
			if t := parseTrack(it, i); t != nil {
				b.Tracks = append(b.Tracks, t)
				b.tracksByNet[t.Net] = append(b.tracksByNet[t.Net], t)
			}
		case "footprint":
			fp := parseFootprint(it)
			b.Footprints = append(b.Footprints, fp)
			for _, p := range fp.Pads {
				b.Pads = append(b.Pads, p)
				if p.Net != "" {
					b.padsByNet[p.Net] = append(b.padsByNet[p.Net], p)
				}
			}
		case "zone":
			if z := parseZone(it); z != nil {
				b.Zones = append(b.Zones, z)
			}
		case "gr_line", "gr_arc", "gr_rect", "gr_circle", "gr_poly":
			b.Outline = append(b.Outline, parseGraphic(it)...)
		}
	}
	return b, nil
}

// Save writes the board back to disk. Unmodified nodes are reproduced byte for
// byte, so the diff shows only the tracks that actually changed.
func (b *Board) Save(path string) error {
	return os.WriteFile(path, b.Doc.Bytes(), 0o644)
}

// Bytes serialises the board.
func (b *Board) Bytes() []byte { return b.Doc.Bytes() }

// PadsOfNet returns the pads on a net, in a stable order.
func (b *Board) PadsOfNet(net string) []*Pad { return b.padsByNet[net] }

// TracksOfNet returns the copper on a net.
func (b *Board) TracksOfNet(net string) []*Track { return b.tracksByNet[net] }

// Nets lists every net that has a pad or copper, sorted.
func (b *Board) Nets() []string {
	seen := map[string]bool{}
	for n := range b.padsByNet {
		seen[n] = true
	}
	for n := range b.tracksByNet {
		seen[n] = true
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		if n != "" {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// Footprint returns the footprint with the given reference, or nil.
func (b *Board) Footprint(ref string) *Footprint {
	for _, f := range b.Footprints {
		if f.Ref == ref {
			return f
		}
	}
	return nil
}

// AddTrack appends a new piece of copper to the board and returns it.
func (b *Board) AddTrack(t *Track) *Track {
	t.Node = buildTrackNode(t)
	t.Node.SetDepth(1)
	b.Root.Append(t.Node)
	t.Index = len(b.Root.Items) - 1
	b.Tracks = append(b.Tracks, t)
	b.tracksByNet[t.Net] = append(b.tracksByNet[t.Net], t)
	return t
}

// ReplaceTrack swaps one track for a run of new ones, in place, so the new
// copper lands where the old did in the file rather than at the end.
//
// It reports whether the swap happened. A caller holding a track that has
// already been replaced gets false rather than a silent no-op: that mistake is
// easy to make -- two edits derived from one earlier scan of the board -- and
// it leaves the caller believing it changed something it did not.
func (b *Board) ReplaceTrack(old *Track, with []*Track) bool {
	i := b.Root.IndexOf(old.Node)
	if i < 0 {
		return false
	}
	nodes := make([]*sexpr.Node, 0, len(with))
	for _, t := range with {
		t.Node = buildTrackNode(t)
		t.Node.SetDepth(1)
		nodes = append(nodes, t.Node)
	}
	b.Root.Replace(i, nodes...)
	b.reindex()

	// Swap the model over too, keeping both the flat list and the per-net index
	// consistent with the tree.
	b.Tracks = replaceInSlice(b.Tracks, old, with)
	b.tracksByNet[old.Net] = replaceInSlice(b.tracksByNet[old.Net], old, with)
	return true
}

// RemoveTracks deletes copper from the board.
func (b *Board) RemoveTracks(drop []*Track) int {
	gone := map[*sexpr.Node]bool{}
	for _, t := range drop {
		gone[t.Node] = true
	}
	n := b.Root.RemoveFunc(func(x *sexpr.Node) bool { return gone[x] })
	if n == 0 {
		return 0
	}
	b.reindex()
	keep := b.Tracks[:0]
	for _, t := range b.Tracks {
		if !gone[t.Node] {
			keep = append(keep, t)
		}
	}
	b.Tracks = keep
	for net, ts := range b.tracksByNet {
		k := ts[:0]
		for _, t := range ts {
			if !gone[t.Node] {
				k = append(k, t)
			}
		}
		b.tracksByNet[net] = k
	}
	return n
}

func (b *Board) reindex() {
	pos := make(map[*sexpr.Node]int, len(b.Root.Items))
	for i, it := range b.Root.Items {
		pos[it] = i
	}
	for _, t := range b.Tracks {
		if i, ok := pos[t.Node]; ok {
			t.Index = i
		}
	}
}

func replaceInSlice(in []*Track, old *Track, with []*Track) []*Track {
	out := make([]*Track, 0, len(in)+len(with))
	for _, t := range in {
		if t == old {
			out = append(out, with...)
			continue
		}
		out = append(out, t)
	}
	return out
}
