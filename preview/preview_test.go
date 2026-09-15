package preview

import (
	"encoding/json"
	"math"
	"sort"
	"strings"
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/geom"
)

const demoPath = "../demo-pcb/ai-vision.kicad_pcb"

func demo(t *testing.T) *board.Board {
	t.Helper()
	b, err := board.Load(demoPath)
	if err != nil {
		t.Skipf("demo board missing: %v", err)
	}
	return b
}

// ---- what a shape's triangles must add up to ----
//
// Area is the test that matters for a tessellator. Every shape here has a
// closed-form area, and a fan of triangles over its outline must come to that
// figure: too little means a piece is missing, too much means the outline
// self-intersects. The faceting of the round parts makes the triangles come out
// slightly small -- a chord cuts the corner off an arc -- which is why the
// tolerance is one-sided and scaled by the flatness the outline was built to.

func triArea(verts []float32) float64 {
	var total float64
	for i := 0; i+5 < len(verts); i += 6 {
		ax, ay := float64(verts[i]), float64(verts[i+1])
		bx, by := float64(verts[i+2]), float64(verts[i+3])
		cx, cy := float64(verts[i+4]), float64(verts[i+5])
		total += math.Abs((bx-ax)*(cy-ay)-(by-ay)*(cx-ax)) / 2
	}
	return total
}

func TestShapeAreaMatchesItsClosedForm(t *testing.T) {
	const w = 0.09
	r := w / 2
	for _, tc := range []struct {
		name  string
		shape geom.RoundPoly
		want  float64
	}{
		{"a via", geom.Disc(geom.Pt{X: 10, Y: 10}, 0.3), math.Pi * 0.09},
		{"a track", geom.Capsule(geom.Pt{X: 1, Y: 1}, geom.Pt{X: 6, Y: 1}, w), 5*w + math.Pi*r*r},
		{"a diagonal track", geom.Capsule(geom.Pt{X: 0, Y: 0}, geom.Pt{X: 3, Y: 4}, w), 5*w + math.Pi*r*r},
		{"a rectangular pad", geom.RectShape(geom.Pt{X: 5, Y: 5}, 1.2, 0.8, 0, 0), 1.2 * 0.8},
		// A corner radius takes a bite out of each corner: the outer size is
		// still w by h, so the area is w*h less what four quarter-squares lose
		// to four quarter-circles.
		{"a rounded rectangular pad", geom.RectShape(geom.Pt{X: 5, Y: 5}, 1.2, 0.8, 0, 0.15),
			1.2*0.8 - (4-math.Pi)*0.15*0.15},
		{"a rotated pad", geom.RectShape(geom.Pt{X: 5, Y: 5}, 1.2, 0.8, 30, 0.15),
			1.2*0.8 - (4-math.Pi)*0.15*0.15},
		{"an oval pad", geom.OvalShape(geom.Pt{X: 5, Y: 5}, 1.2, 0.6, 0),
			0.6*0.6 + math.Pi*0.09},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := triArea(tessellate(nil, tc.shape))
			// A chord cuts a sliver off every arc, so the tessellation is
			// always a little small and never large.
			//
			// How small is set by the step angle rather than by the sag: a
			// polygon inscribed in a circle loses about t^2/6 of the area for a
			// step of t radians, which for the flatness this package draws to
			// is a couple of percent on a small via and less on anything
			// bigger. Two and a half percent is therefore the honest bound; it
			// still catches a missing cap, which costs a third of a track's
			// end or a quarter of a pad's corner.
			if got > tc.want*(1+1e-6) {
				t.Errorf("area %.6f, more than the shape's %.6f", got, tc.want)
			}
			if got < tc.want*0.975 {
				t.Errorf("area %.6f, want %.6f (%.1f%% missing)", got, tc.want, 100*(1-got/tc.want))
			}
		})
	}
}

func TestAnEmptyOrDegenerateShapeDrawsNothing(t *testing.T) {
	for name, s := range map[string]geom.RoundPoly{
		"no points":      {},
		"a zero disc":    geom.Disc(geom.Pt{X: 1, Y: 1}, 0),
		"a zero capsule": geom.Capsule(geom.Pt{X: 1, Y: 1}, geom.Pt{X: 1, Y: 1}, 0),
	} {
		if got := tessellate(nil, s); len(got) != 0 {
			t.Errorf("%s produced %d vertices", name, len(got)/2)
		}
	}
}

// ---- ear clipping ----

func TestEarClipTriangulatesAConcavePolygon(t *testing.T) {
	// An L: 3 x 3 with a 2 x 2 bite out of the top right, so area 5.
	ring := []geom.Pt{{X: 0, Y: 0}, {X: 3, Y: 0}, {X: 3, Y: 1}, {X: 1, Y: 1}, {X: 1, Y: 3}, {X: 0, Y: 3}}
	got, ok := earClip(nil, ring)
	if !ok {
		t.Fatal("an L-shape was not triangulated")
	}
	if n := len(got) / 6; n != 4 {
		t.Errorf("%d triangles for a 6-gon, want 4", n)
	}
	if a := triArea(got); math.Abs(a-5) > 1e-9 {
		t.Errorf("area %.6f, want 5", a)
	}
}

func TestEarClipHandlesWhatKiCadActuallyWrites(t *testing.T) {
	// Repeated points, a closing point, and three collinear vertices: all of
	// these turn up in a zone fill, and none of them is an error.
	ring := []geom.Pt{
		{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 2, Y: 0}, {X: 2, Y: 0},
		{X: 2, Y: 2}, {X: 0, Y: 2}, {X: 0, Y: 0},
	}
	got, ok := earClip(nil, ring)
	if !ok {
		t.Fatal("a ring with duplicate and collinear points was refused")
	}
	if a := triArea(got); math.Abs(a-4) > 1e-9 {
		t.Errorf("area %.6f, want 4", a)
	}
}

func TestEarClipGivesUpRatherThanLoopingOnASelfCrossingRing(t *testing.T) {
	// A bow tie is not a simple polygon. What matters is that it terminates
	// and says it did not finish.
	ring := []geom.Pt{{X: 0, Y: 0}, {X: 2, Y: 2}, {X: 2, Y: 0}, {X: 0, Y: 2}}
	if _, ok := earClip(nil, ring); ok {
		t.Error("a self-crossing ring reported success")
	}
}

func TestSimplifyKeepsTheShape(t *testing.T) {
	// A straight run of many points plus one real corner.
	var ring []geom.Pt
	for i := 0; i <= 100; i++ {
		ring = append(ring, geom.Pt{X: float64(i) * 0.1, Y: 0})
	}
	ring = append(ring, geom.Pt{X: 10, Y: 5}, geom.Pt{X: 0, Y: 5})
	got := simplify(ring, 0.02)
	if len(got) > 5 {
		t.Errorf("%d points kept of %d, want the corners only", len(got), len(ring))
	}
	if got[0] != ring[0] || got[len(got)-1] != ring[len(ring)-1] {
		t.Error("simplify moved an end point")
	}
}

// ---- the document ----

func TestTheDemoBoardExportsACoherentDocument(t *testing.T) {
	b := demo(t)
	doc, bin, err := Build(b, Options{IncludeZones: true})
	if err != nil {
		t.Fatal(err)
	}

	// The index and the buffer have to agree, or the viewer refuses the board.
	if doc.Geometry.ByteLength != len(bin) {
		t.Errorf("byte_length %d, buffer %d", doc.Geometry.ByteLength, len(bin))
	}
	if want := doc.Geometry.VertexCount * 8; len(bin) != want {
		t.Errorf("buffer %d bytes for %d vertices, want %d", len(bin), doc.Geometry.VertexCount, want)
	}
	if doc.Geometry.VertexCount%3 != 0 {
		t.Errorf("%d vertices is not a whole number of triangles", doc.Geometry.VertexCount)
	}

	// Groups must tile the buffer exactly: contiguous, in order, no gaps and no
	// overlap. A gap is copper that is in the file and drawn by nobody.
	at := 0
	for i, g := range doc.Geometry.Groups {
		if g.Offset != at {
			t.Fatalf("group %d (%s/%s) starts at %d, want %d", i, g.Layer, g.Net, g.Offset, at)
		}
		if g.Count <= 0 || g.Count%3 != 0 {
			t.Errorf("group %d (%s/%s) holds %d vertices", i, g.Layer, g.Net, g.Count)
		}
		at += g.Count
	}
	if at != doc.Geometry.VertexCount {
		t.Errorf("groups cover %d vertices of %d", at, doc.Geometry.VertexCount)
	}
	if !sort.SliceIsSorted(doc.Geometry.Groups, func(i, j int) bool {
		a, b := doc.Geometry.Groups[i], doc.Geometry.Groups[j]
		if a.Layer != b.Layer {
			return a.Layer < b.Layer
		}
		return a.Net < b.Net
	}) {
		t.Error("groups are not in a stable order")
	}

	// Every group names a layer and a net the document declares, or the
	// viewer's layer rail and net list cannot show it.
	layers := map[string]bool{}
	for _, l := range doc.Layers {
		layers[l.Name] = true
	}
	nets := map[string]bool{}
	for _, n := range doc.Nets {
		nets[n.Name] = true
	}
	for _, g := range doc.Geometry.Groups {
		if !layers[g.Layer] {
			t.Errorf("group on layer %q, which is not in the document", g.Layer)
		}
		if !nets[g.Net] {
			t.Errorf("group on net %q, which is not in the document", g.Net)
		}
	}

	if doc.Board.WidthMM <= 0 || doc.Board.HeightMM <= 0 {
		t.Errorf("board is %.3f x %.3f mm", doc.Board.WidthMM, doc.Board.HeightMM)
	}
	if len(doc.Layers) != 6 {
		t.Errorf("%d copper layers, want 6", len(doc.Layers))
	}
	if doc.KiCad.Version != 20260206 {
		t.Errorf("kicad version %d", doc.KiCad.Version)
	}

	// And it is JSON the viewer's type declaration describes.
	raw, err := doc.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"format_version", "units", "coordinate_system", "board", "layers",
		"stackup", "nets", "vias", "pads", "geometry", "kicad", "warnings",
	} {
		if _, ok := back[key]; !ok {
			t.Errorf("board.json has no %q", key)
		}
	}
	t.Logf("%d vertices (%d triangles), %d groups, %.1f kB of geometry, %.1f kB of json",
		doc.Geometry.VertexCount, doc.Geometry.VertexCount/3, len(doc.Geometry.Groups),
		float64(len(bin))/1024, float64(len(raw))/1024)
}

// Every routed net's copper has to land inside the board, or the view opens on
// a board with traces off the edge of the world and no way to tell why.
//
// Pads are a different matter, and the demo board is why: 557 of them belong to
// parts parked outside the edge cut, waiting to be placed. That is the board's
// state, not a transform error -- every one of its 3120 tracks is inside the
// outline -- so the test is about the routing, and the document has to declare
// the strays rather than quietly drawing them off-screen.
func TestEveryTraceLandsInsideTheBoard(t *testing.T) {
	b := demo(t)
	doc, bin, err := Build(b, Options{IncludeZones: true})
	if err != nil {
		t.Fatal(err)
	}
	// A hair of slack: copper may legitimately overhang the edge cut.
	const slack = 1.0
	tf, _ := extent(b)
	inside := func(p geom.Pt) bool {
		q := tf.pt(p)
		return q.X >= -slack && q.Y >= -slack &&
			q.X <= doc.Board.WidthMM+slack && q.Y <= doc.Board.HeightMM+slack
	}
	stray := 0
	for _, tr := range b.Tracks {
		for _, sh := range tr.Shape(b.MaxError) {
			for _, p := range sh.Pts {
				if !inside(p) {
					stray++
					if stray <= 5 {
						t.Errorf("%s copper on %s at (%.3f, %.3f) falls outside the board",
							netLabel(tr.Net), tr.Layer, tf.pt(p).X, tf.pt(p).Y)
					}
				}
			}
		}
	}
	if stray > 0 {
		t.Fatalf("%d pieces of routed copper fall outside the board extent", stray)
	}
	// The buffer is no bigger than the document claims, and the document's own
	// extent is what the viewer frames.
	if len(bin) != doc.Geometry.VertexCount*8 {
		t.Errorf("buffer %d bytes for %d vertices", len(bin), doc.Geometry.VertexCount)
	}
	var said bool
	for _, w := range doc.Warnings {
		if strings.Contains(w, "outside the board edge") {
			said = true
			t.Logf("%s", w)
		}
	}
	if !said {
		t.Error("parts outside the board edge were drawn without a word about it")
	}
}

// The Y flip is the one transform in this package, and getting it backwards
// mirrors the board -- which looks plausible until you compare it with KiCad.
func TestTheBoardIsNotMirrored(t *testing.T) {
	b := demo(t)
	doc, _, err := Build(b, Options{})
	if err != nil {
		t.Fatal(err)
	}
	// U3 is the controller, at a known place on the demo board. Find its
	// centre in KiCad space and in board space, and check the Y flip.
	fp := b.Footprint("U3")
	if fp == nil {
		t.Skip("no U3 on this board")
	}
	tf, _ := extent(b)
	want := tf.pt(fp.At)
	var got *Pad
	for i := range doc.Pads {
		if doc.Pads[i].Ref == "U3" {
			got = &doc.Pads[i]
			break
		}
	}
	if got == nil {
		t.Fatal("no U3 pad in the document")
	}
	// The pad is somewhere in the part's own footprint, so within its extent
	// of the origin rather than at it.
	if math.Abs(got.X-want.X) > 25 || math.Abs(got.Y-want.Y) > 25 {
		t.Errorf("U3 pad at (%.2f, %.2f), footprint origin maps to (%.2f, %.2f)",
			got.X, got.Y, want.X, want.Y)
	}
	// And the flip itself: KiCad Y grows downward, board Y upward.
	if tf.pt(geom.Pt{X: 0, Y: tf.minY}).Y <= tf.pt(geom.Pt{X: 0, Y: tf.maxY}).Y {
		t.Error("board Y does not grow upward")
	}
}

func TestZonesCanBeLeftOut(t *testing.T) {
	b := demo(t)
	with, _, err := Build(b, Options{IncludeZones: true})
	if err != nil {
		t.Fatal(err)
	}
	without, _, err := Build(b, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if without.Geometry.VertexCount >= with.Geometry.VertexCount {
		t.Errorf("leaving the pours out did not make the buffer smaller: %d vs %d",
			without.Geometry.VertexCount, with.Geometry.VertexCount)
	}
	if len(without.Warnings) == 0 {
		t.Error("a preview with no pours in it should say so")
	}
}

func TestBuildRefusesABoardWithNoGeometry(t *testing.T) {
	if _, _, err := Build(&board.Board{}, Options{}); err == nil {
		t.Error("an empty board exported without complaint")
	}
}

// The Y flip has to work in both directions, and the round trip is the test
// that catches a mirrored board -- which looks plausible on screen and is only
// obvious beside KiCad.
func TestTheTransformRoundTrips(t *testing.T) {
	b := demo(t)
	tf, ok := ExtentOf(b)
	if !ok {
		t.Fatal("no extent")
	}
	for _, p := range []geom.Pt{
		{X: 30, Y: 40}, {X: 22.76, Y: 21.85}, {X: 122.76, Y: 121.85}, {X: 0, Y: 0},
	} {
		back := tf.ToKiCad(tf.ToBoard(p))
		if math.Abs(back.X-p.X) > 1e-9 || math.Abs(back.Y-p.Y) > 1e-9 {
			t.Errorf("(%.4f, %.4f) came back as (%.4f, %.4f)", p.X, p.Y, back.X, back.Y)
		}
	}
}

// A rectangle drawn on the preview has to come back as the same region of the
// board. The Y flip swaps which edge is the minimum, and getting that wrong
// gives an empty rectangle rather than a wrong one -- so it never matches
// anything and the feature silently does nothing.
func TestARectangleConvertsToTheSameRegion(t *testing.T) {
	b := demo(t)
	tf, _ := ExtentOf(b)

	// A box in board space, bottom-left quarter.
	drawn := geom.Rect{MinX: 5, MinY: 5, MaxX: 25, MaxY: 20}
	got := tf.RectToKiCad(drawn)
	if got.MinX > got.MaxX || got.MinY > got.MaxY {
		t.Fatalf("converted to an empty rectangle: %+v", got)
	}
	if w, h := got.MaxX-got.MinX, got.MaxY-got.MinY; math.Abs(w-20) > 1e-9 || math.Abs(h-15) > 1e-9 {
		t.Errorf("a 20 x 15 mm area became %.3f x %.3f", w, h)
	}
	// Every corner of the drawn box lands inside the converted one.
	for _, c := range []geom.Pt{
		{X: drawn.MinX, Y: drawn.MinY}, {X: drawn.MaxX, Y: drawn.MinY},
		{X: drawn.MaxX, Y: drawn.MaxY}, {X: drawn.MinX, Y: drawn.MaxY},
	} {
		k := tf.ToKiCad(c)
		if !got.Contains(k) {
			t.Errorf("corner (%.2f, %.2f) converts to (%.2f, %.2f), outside the converted area",
				c.X, c.Y, k.X, k.Y)
		}
	}
	// And the board's own copper agrees: a pad inside the drawn area in board
	// space is inside the converted area in KiCad space.
	inside := 0
	for _, p := range b.Pads {
		if drawn.Contains(tf.ToBoard(p.Centre)) {
			inside++
			if !got.Contains(p.Centre) {
				t.Fatalf("%s is in the drawn area but not in the converted one", p.ID())
			}
		}
	}
	if inside == 0 {
		t.Skip("no pads in that part of the board to check against")
	}
	t.Logf("%d pads fall in the drawn area, all of them in the converted one", inside)
}
