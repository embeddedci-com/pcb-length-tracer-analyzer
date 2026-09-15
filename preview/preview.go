// Package preview renders a board into the two files a viewer needs: a JSON
// description of the board, and a binary buffer of triangles.
//
// The format is the EMI Analyzer's `board.json` plus `geometry.bin`, and it is
// matched deliberately rather than invented. That tool already has a WebGL
// viewer for exactly this -- layers, nets, pan and zoom, highlight a net --
// which was written against boards produced by its own Python ingest. Emitting
// the same contract from Go is what lets this tool reuse it as it stands.
//
// The contract, in short:
//
//   - Coordinates are millimetres in board space: origin at the bottom-left of
//     the board extent, Y up. KiCad's page coordinates are Y down, so the
//     conversion happens here, once, and nothing downstream has to know.
//   - geometry.bin is one flat little-endian float32 array of x,y pairs, three
//     vertices to a triangle.
//   - Every triangle belongs to exactly one (layer, net) group, and groups
//     index the buffer by vertex offset and count. That is what lets the viewer
//     hide a layer or highlight a net by changing a draw range instead of
//     re-uploading the buffer.
package preview

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/geom"
	"github.com/embeddedci-com/pcb-autorouter/sexpr"
)

// FormatVersion is the board.json schema version, matching the viewer's.
const FormatVersion = 1

// planeCoverageMin is how much of the board a net's pours must cover before the
// layer is called that net's plane.
const planeCoverageMin = 0.30

// Zone fills arrive at the resolution KiCad's filler worked at. Above this many
// points a ring is thinned, to this tolerance.
const (
	zoneSimplifyAbove     = 400
	zoneSimplifyTolerance = 0.02
)

// CoordinateSystem records what the numbers in the document mean.
type CoordinateSystem struct {
	X      string `json:"x"`
	Y      string `json:"y"`
	Z      string `json:"z"`
	Origin string `json:"origin"`
	Note   string `json:"note,omitempty"`
}

// StackupEntry is one physical layer of the board, copper or not.
type StackupEntry struct {
	Name        string   `json:"name"`
	Role        string   `json:"role"` // copper, dielectric, other
	Type        string   `json:"type"`
	ThicknessMM float64  `json:"thickness_mm"`
	Material    string   `json:"material"`
	EpsilonR    *float64 `json:"epsilon_r"`
	LossTangent *float64 `json:"loss_tangent"`
	FromFile    bool     `json:"from_file"`
	ZBottomMM   float64  `json:"z_bottom_mm"`
	ZTopMM      float64  `json:"z_top_mm"`
}

// Layer is one copper layer.
type Layer struct {
	PlaneNet      string  `json:"plane_net,omitempty"`
	PlaneCoverage float64 `json:"plane_coverage,omitempty"`
	Name          string  `json:"name"`
	Kind          string  `json:"kind"`
	Index         int     `json:"index"`
	ZMM           float64 `json:"z_mm"`
}

// Net is one net's tally.
type Net struct {
	Index  int    `json:"index"`
	Name   string `json:"name"`
	Tracks int    `json:"tracks"`
	Vias   int    `json:"vias"`
	Pads   int    `json:"pads"`
	// LengthMM is all the copper on the net, which is not the length a signal
	// travels. The plan tables are where routed lengths are reported; this is
	// here because the viewer's net list shows it.
	LengthMM float64  `json:"length_mm"`
	Layers   []string `json:"layers"`
}

// Via is one via, for the viewer's pick-nearest.
type Via struct {
	X       float64  `json:"x"`
	Y       float64  `json:"y"`
	SizeMM  float64  `json:"size_mm"`
	DrillMM float64  `json:"drill_mm"`
	Net     string   `json:"net"`
	Layers  []string `json:"layers"`
	Kind    string   `json:"kind"`
}

// Pad is one pad.
type Pad struct {
	Ref     string   `json:"ref"`
	Number  string   `json:"number"`
	Net     string   `json:"net"`
	X       float64  `json:"x"`
	Y       float64  `json:"y"`
	Type    string   `json:"type"`
	DrillMM float64  `json:"drill_mm"`
	Layers  []string `json:"layers"`
}

// GeometryGroup is one run of vertices, all of one layer and one net. Offset
// and count are in vertices, not bytes and not triangles.
type GeometryGroup struct {
	Layer  string `json:"layer"`
	Net    string `json:"net"`
	Offset int    `json:"offset"`
	Count  int    `json:"count"`
}

// GeometryIndex describes geometry.bin.
type GeometryIndex struct {
	File        string          `json:"file"`
	DType       string          `json:"dtype"`
	Components  int             `json:"components"`
	Primitive   string          `json:"primitive"`
	VertexCount int             `json:"vertex_count"`
	ByteLength  int             `json:"byte_length"`
	Groups      []GeometryGroup `json:"groups"`
}

// Extent is the board's size and edge.
type Extent struct {
	WidthMM     float64        `json:"width_mm"`
	HeightMM    float64        `json:"height_mm"`
	ThicknessMM float64        `json:"thickness_mm"`
	Outline     [][][2]float64 `json:"outline"`
}

// KiCadInfo is what wrote the board.
type KiCadInfo struct {
	Version   int    `json:"version"`
	Generator string `json:"generator"`
}

// Doc is board.json.
type Doc struct {
	FormatVersion    int              `json:"format_version"`
	Source           map[string]any   `json:"source"`
	Units            string           `json:"units"`
	CoordinateSystem CoordinateSystem `json:"coordinate_system"`
	Board            Extent           `json:"board"`
	Layers           []Layer          `json:"layers"`
	Stackup          []StackupEntry   `json:"stackup"`
	Nets             []Net            `json:"nets"`
	Vias             []Via            `json:"vias"`
	Pads             []Pad            `json:"pads"`
	Geometry         GeometryIndex    `json:"geometry"`
	KiCad            KiCadInfo        `json:"kicad"`
	Warnings         []string         `json:"warnings"`
}

// JSON renders the document compactly, as the viewer fetches it.
func (d *Doc) JSON() ([]byte, error) { return json.Marshal(d) }

// Options tune an export.
type Options struct {
	// Source is free-form provenance the host wants carried through, e.g. the
	// uploaded filename and whether this is the board before or after tuning.
	Source map[string]any

	// IncludeZones draws the copper pours. They are the largest thing on a
	// board by far and they sit under the traces; a caller interested in the
	// routing can leave them out.
	IncludeZones bool
}

// Transform converts between KiCad's page coordinates and the board space the
// viewer works in.
//
// Exported because the conversion has to happen in both directions and in more
// than one place: geometry goes out to the browser, and anything the user draws
// on it -- an area they will allow copper to be added in -- comes back. Two
// implementations of a Y flip is one too many, and the one that is wrong is
// silent: it mirrors the board, which looks plausible until somebody compares
// it with KiCad.
type Transform struct {
	minX, minY, maxX, maxY float64
}

// ToBoard converts a KiCad page point to board space.
func (t Transform) ToBoard(p geom.Pt) geom.Pt {
	return geom.Pt{X: p.X - t.minX, Y: t.maxY - p.Y}
}

// ToKiCad converts a board-space point back to KiCad's page coordinates.
func (t Transform) ToKiCad(p geom.Pt) geom.Pt {
	return geom.Pt{X: p.X + t.minX, Y: t.maxY - p.Y}
}

// RectToKiCad converts a board-space rectangle. The Y flip swaps which edge is
// the minimum, which is the part that is easy to get wrong and produces an
// empty rectangle rather than a wrong one -- so it is done here, once.
func (t Transform) RectToKiCad(r geom.Rect) geom.Rect {
	a := t.ToKiCad(geom.Pt{X: r.MinX, Y: r.MinY})
	b := t.ToKiCad(geom.Pt{X: r.MaxX, Y: r.MaxY})
	return geom.Rect{
		MinX: math.Min(a.X, b.X), MinY: math.Min(a.Y, b.Y),
		MaxX: math.Max(a.X, b.X), MaxY: math.Max(a.Y, b.Y),
	}
}

// RectToBoard converts a KiCad rectangle into board space. The Y flip swaps
// which edge is the minimum, so it is done here rather than at each call.
func (t Transform) RectToBoard(r geom.Rect) geom.Rect {
	a := t.ToBoard(geom.Pt{X: r.MinX, Y: r.MinY})
	b := t.ToBoard(geom.Pt{X: r.MaxX, Y: r.MaxY})
	return geom.Rect{
		MinX: math.Min(a.X, b.X), MinY: math.Min(a.Y, b.Y),
		MaxX: math.Max(a.X, b.X), MaxY: math.Max(a.Y, b.Y),
	}
}

// WidthMM and HeightMM are the board's extent.
func (t Transform) WidthMM() float64  { return t.maxX - t.minX }
func (t Transform) HeightMM() float64 { return t.maxY - t.minY }

// ExtentOf is the transform for a board: the same extent the preview is drawn
// in, so an area drawn on the preview converts back to where the user meant.
func ExtentOf(b *board.Board) (Transform, bool) { return extent(b) }

func (t Transform) pt(p geom.Pt) geom.Pt { return t.ToBoard(p) }

func (t Transform) width() float64  { return t.WidthMM() }
func (t Transform) height() float64 { return t.HeightMM() }

// Build produces board.json and geometry.bin for a board.
func Build(b *board.Board, opt Options) (*Doc, []byte, error) {
	tf, ok := extent(b)
	if !ok {
		return nil, nil, fmt.Errorf("preview: the board has no geometry to measure")
	}

	copper := map[string]bool{}
	for _, l := range b.CopperLayers {
		copper[l] = true
	}

	var warnings []string
	groups := map[[2]string]*group{}
	at := func(layer, net string) *group {
		k := [2]string{layer, net}
		g := groups[k]
		if g == nil {
			g = &group{layer: layer, net: net}
			groups[k] = g
		}
		return g
	}

	type tally struct {
		tracks, vias, pads int
		length             float64
		layers             map[string]bool
	}
	stats := map[string]*tally{}
	count := func(net string) *tally {
		t := stats[net]
		if t == nil {
			t = &tally{layers: map[string]bool{}}
			stats[net] = t
		}
		return t
	}

	var vias []Via
	for _, t := range b.Tracks {
		st := count(t.Net)
		if t.Kind == board.KindVia {
			st.vias++
			on := viaLayers(b, t)
			ring := circle(tf.pt(t.Start), t.Size/2)
			for _, l := range on {
				g := at(l, t.Net)
				g.verts = fan(g.verts, ring)
				st.layers[l] = true
			}
			vias = append(vias, Via{
				X: round4(tf.pt(t.Start).X), Y: round4(tf.pt(t.Start).Y),
				SizeMM: t.Size, DrillMM: t.Drill, Net: t.Net,
				Layers: on, Kind: viaKind(b, on),
			})
			continue
		}
		if !copper[t.Layer] {
			continue
		}
		st.tracks++
		st.length += t.Length(b.Stackup)
		st.layers[t.Layer] = true
		g := at(t.Layer, t.Net)
		for _, sh := range t.Shape(b.MaxError) {
			g.verts = tessellate(g.verts, moved(tf, sh))
		}
	}

	var pads []Pad
	offBoard := map[string]bool{}
	for _, p := range b.Pads {
		st := count(p.Net)
		st.pads++
		on := padLayers(b, p, copper)
		c := tf.pt(p.Centre)
		if c.X < -0.5 || c.Y < -0.5 || c.X > tf.width()+0.5 || c.Y > tf.height()+0.5 {
			offBoard[p.Ref] = true
		}
		for _, l := range on {
			g := at(l, p.Net)
			g.verts = tessellate(g.verts, moved(tf, p.Shape))
			st.layers[l] = true
		}
		pads = append(pads, Pad{
			Ref: p.Ref, Number: p.Name, Net: p.Net,
			X: round4(c.X), Y: round4(c.Y), Type: p.Type,
			DrillMM: 0, Layers: on,
		})
	}

	// A board still being laid out has parts parked outside the edge cut,
	// waiting to be placed -- 557 pads on the demo board. Their copper is real
	// and is drawn where it is, but the view opens on the board itself, so it
	// sits off-screen. Say so, or a missing decoupling capacitor looks like a
	// hole in the preview.
	if len(offBoard) > 0 {
		refs := make([]string, 0, len(offBoard))
		for r := range offBoard {
			refs = append(refs, r)
		}
		sort.Strings(refs)
		warnings = append(warnings, fmt.Sprintf(
			"%d part(s) sit outside the board edge and are drawn there: %s",
			len(refs), strings.Join(clip(refs, 8), ", ")))
	}

	// Pour area per layer and net, which is what decides whether a layer is a
	// plane -- a fact about the board, where the layer's type field is only a
	// label somebody chose.
	planeArea := map[[2]string]float64{}
	if opt.IncludeZones {
		for _, z := range b.Zones {
			for layer, polys := range z.Filled {
				if !copper[layer] {
					continue
				}
				st := count(z.Net)
				st.layers[layer] = true
				g := at(layer, z.Net)
				for _, poly := range polys {
					ring := []geom.Pt(poly)
					planeArea[[2]string{layer, z.Net}] += math.Abs(signedArea(ring))
					if len(ring) > zoneSimplifyAbove {
						ring = simplify(ring, zoneSimplifyTolerance)
					}
					moved := make([]geom.Pt, len(ring))
					for i, p := range ring {
						moved[i] = tf.pt(p)
					}
					var ok bool
					g.verts, ok = earClip(g.verts, moved)
					if !ok {
						warnings = append(warnings, fmt.Sprintf(
							"a copper pour on %s (%s) could not be fully drawn; the preview shows part of it",
							layer, netLabel(z.Net)))
					}
				}
			}
		}
	} else if len(b.Zones) > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"%d copper pour(s) are not drawn in this preview", len(b.Zones)))
	}

	// One buffer, groups in a stable order.
	list := make([]*group, 0, len(groups))
	for _, g := range groups {
		if len(g.verts) > 0 {
			list = append(list, g)
		}
	}
	sortGroups(list)

	index := make([]GeometryGroup, 0, len(list))
	total := 0
	for _, g := range list {
		n := len(g.verts) / 2
		index = append(index, GeometryGroup{Layer: g.layer, Net: g.net, Offset: total, Count: n})
		total += n
	}
	buf := make([]byte, 0, total*8)
	for _, g := range list {
		for _, v := range g.verts {
			buf = binary.LittleEndian.AppendUint32(buf, math.Float32bits(v))
		}
	}

	nets := make([]Net, 0, len(stats))
	names := make([]string, 0, len(stats))
	for n := range stats {
		names = append(names, n)
	}
	sort.Strings(names)
	for i, n := range names {
		st := stats[n]
		layers := make([]string, 0, len(st.layers))
		for l := range st.layers {
			layers = append(layers, l)
		}
		sort.Strings(layers)
		nets = append(nets, Net{
			Index: i, Name: n, Tracks: st.tracks, Vias: st.vias, Pads: st.pads,
			LengthMM: round4(st.length), Layers: layers,
		})
	}

	doc := &Doc{
		FormatVersion: FormatVersion,
		Source:        opt.Source,
		Units:         "mm",
		CoordinateSystem: CoordinateSystem{
			X: "right", Y: "up", Z: "up from the bottom copper",
			Origin: "bottom-left corner of the board extent",
			Note:   "KiCad's Y-down page coordinates are converted here, once.",
		},
		Board: Extent{
			WidthMM: round4(tf.width()), HeightMM: round4(tf.height()),
			ThicknessMM: round4(stackThickness(b)),
			Outline:     outlineRings(b, tf),
		},
		Layers:  layers(b, tf, planeArea),
		Stackup: stackup(b),
		Nets:    nets,
		Vias:    vias,
		Pads:    pads,
		Geometry: GeometryIndex{
			File: "geometry.bin", DType: "float32", Components: 2,
			Primitive: "triangles", VertexCount: total, ByteLength: len(buf),
			Groups: index,
		},
		KiCad:    kicadInfo(b),
		Warnings: warnings,
	}
	if doc.Source == nil {
		doc.Source = map[string]any{}
	}
	if doc.Warnings == nil {
		doc.Warnings = []string{}
	}
	return doc, buf, nil
}

func moved(tf Transform, s geom.RoundPoly) geom.RoundPoly {
	out := geom.RoundPoly{Pts: make([]geom.Pt, len(s.Pts)), R: s.R}
	for i, p := range s.Pts {
		out.Pts[i] = tf.pt(p)
	}
	return out
}

// extent is the board's bounds: the edge outline where there is one, otherwise
// whatever copper the board has.
func extent(b *board.Board) (Transform, bool) {
	t := Transform{minX: math.Inf(1), minY: math.Inf(1), maxX: math.Inf(-1), maxY: math.Inf(-1)}
	seen := false
	add := func(p geom.Pt, r float64) {
		seen = true
		t.minX = math.Min(t.minX, p.X-r)
		t.minY = math.Min(t.minY, p.Y-r)
		t.maxX = math.Max(t.maxX, p.X+r)
		t.maxY = math.Max(t.maxY, p.Y+r)
	}
	for _, s := range b.Outline {
		add(s.A, 0)
		add(s.B, 0)
	}
	if seen {
		return t, true
	}
	for _, tr := range b.Tracks {
		for _, sh := range tr.Shape(b.MaxError) {
			for _, p := range sh.Pts {
				add(p, sh.R)
			}
		}
	}
	for _, p := range b.Pads {
		for _, q := range p.Shape.Pts {
			add(q, p.Shape.R)
		}
	}
	for _, z := range b.Zones {
		for _, polys := range z.Filled {
			for _, poly := range polys {
				for _, p := range poly {
					add(p, 0)
				}
			}
		}
	}
	return t, seen
}

func outlineRings(b *board.Board, tf Transform) [][][2]float64 {
	out := make([][][2]float64, 0, len(b.Outline))
	for _, s := range b.Outline {
		a, c := tf.pt(s.A), tf.pt(s.B)
		out = append(out, [][2]float64{
			{round4(a.X), round4(a.Y)},
			{round4(c.X), round4(c.Y)},
		})
	}
	return out
}

// viaLayers is the copper layers a via actually reaches, in stack order.
func viaLayers(b *board.Board, v *board.Track) []string {
	on := map[string]bool{}
	for _, l := range v.Layers(b.Stackup) {
		on[l] = true
	}
	out := make([]string, 0, len(on))
	for _, l := range b.CopperLayers {
		if on[l] {
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		return b.CopperLayers
	}
	return out
}

func viaKind(b *board.Board, on []string) string {
	if len(b.CopperLayers) >= 2 && len(on) >= 2 &&
		on[0] == b.CopperLayers[0] && on[len(on)-1] == b.CopperLayers[len(b.CopperLayers)-1] {
		return "through"
	}
	return "blind"
}

// padLayers resolves a pad's layer list against the board's copper, expanding
// KiCad's "*.Cu" wildcard.
func padLayers(b *board.Board, p *board.Pad, copper map[string]bool) []string {
	for _, l := range p.Layers {
		if l == "*.Cu" {
			return b.CopperLayers
		}
	}
	var out []string
	for _, l := range b.CopperLayers {
		for _, q := range p.Layers {
			if q == l {
				out = append(out, l)
				break
			}
		}
	}
	return out
}

func stackThickness(b *board.Board) float64 {
	if b.Stackup != nil && b.Stackup.Thickness > 0 {
		return b.Stackup.Thickness
	}
	return 1.6
}

// layers describes the copper layers, including which net's pours dominate each.
func layers(b *board.Board, tf Transform, planeArea map[[2]string]float64) []Layer {
	kinds := layerKinds(b)
	area := tf.width() * tf.height()
	total := stackThickness(b)
	out := make([]Layer, 0, len(b.CopperLayers))
	for i, name := range b.CopperLayers {
		l := Layer{Name: name, Kind: kinds[name], Index: i}
		if l.Kind == "" {
			l.Kind = "signal"
		}
		if cl, ok := b.Stackup.Layer(name); ok {
			l.ZMM = round4(total - (cl.ZTop+cl.ZBot)/2)
		}
		if area > 0 {
			bestNet, best := "", 0.0
			for k, a := range planeArea {
				if k[0] == name && k[1] != "" && a > best {
					bestNet, best = k[1], a
				}
			}
			if cov := best / area; bestNet != "" && cov >= planeCoverageMin {
				l.PlaneNet = bestNet
				l.PlaneCoverage = math.Round(math.Min(cov, 1)*1000) / 1000
			}
		}
		out = append(out, l)
	}
	return out
}

// layerKinds reads the layer type words out of the board's own layer table --
// "signal", "power", "mixed" -- which the rest of this tool has no use for and
// so does not keep.
func layerKinds(b *board.Board) map[string]string {
	out := map[string]string{}
	for _, ln := range b.Root.Child("layers").Args() {
		if ln.Kind != sexpr.KindList {
			continue
		}
		name := ln.ArgString(0)
		if !strings.HasSuffix(name, ".Cu") {
			continue
		}
		out[name] = ln.ArgString(1)
	}
	return out
}

// stackup is the physical stack: the copper layers, and the dielectric between
// each pair of them.
//
// The board model keeps the permittivity and thickness either side of each
// copper layer rather than a list of substrate entries, because that is what a
// propagation delay needs. Rebuilding the list from it is exact for the gaps
// between copper layers, which is all a viewer shows.
func stackup(b *board.Board) []StackupEntry {
	if b.Stackup == nil {
		return nil
	}
	total := stackThickness(b)
	z := func(fromTop float64) float64 { return round4(total - fromTop) }
	out := make([]StackupEntry, 0, 2*len(b.Stackup.Copper))
	for i, cl := range b.Stackup.Copper {
		out = append(out, StackupEntry{
			Name: cl.Name, Role: "copper", Type: "copper",
			ThicknessMM: round4(cl.Thickness), Material: "copper",
			FromFile: true, ZBottomMM: z(cl.ZBot), ZTopMM: z(cl.ZTop),
		})
		if i+1 >= len(b.Stackup.Copper) {
			continue
		}
		next := b.Stackup.Copper[i+1]
		if h := next.ZTop - cl.ZBot; h > 1e-9 {
			er, lt := cl.ErBelow, 0.0
			e := StackupEntry{
				Name: fmt.Sprintf("dielectric %d", i+1), Role: "dielectric",
				Type: "core", ThicknessMM: round4(h), Material: "FR4",
				FromFile: true, ZBottomMM: z(next.ZTop), ZTopMM: z(cl.ZBot),
			}
			if er > 0 {
				e.EpsilonR = &er
				e.LossTangent = &lt
			}
			out = append(out, e)
		}
	}
	return out
}

func kicadInfo(b *board.Board) KiCadInfo {
	info := KiCadInfo{}
	if n := b.Root.Child("version"); n != nil {
		if v, ok := n.ArgFloat(0); ok {
			info.Version = int(v)
		}
	}
	if n := b.Root.Child("generator"); n != nil {
		info.Generator = n.ArgString(0)
	}
	return info
}

// clip shortens a list for a message, saying how many it left out.
func clip(in []string, n int) []string {
	if len(in) <= n {
		return in
	}
	return append(in[:n:n], fmt.Sprintf("and %d more", len(in)-n))
}

func netLabel(net string) string {
	if net == "" {
		return "no net"
	}
	return net
}

func round4(v float64) float64 { return math.Round(v*1e4) / 1e4 }
