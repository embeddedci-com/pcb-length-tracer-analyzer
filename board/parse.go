package board

import (
	"math"
	"strings"

	"github.com/embeddedci-com/pcb-autorouter/geom"
	"github.com/embeddedci-com/pcb-autorouter/sexpr"
)

func pt(n *sexpr.Node) geom.Pt {
	x, _ := n.ArgFloat(0)
	y, _ := n.ArgFloat(1)
	return geom.Pt{X: x, Y: y}
}

func parseTrack(n *sexpr.Node, idx int) *Track {
	t := &Track{Net: n.ChildString("net"), UUID: n.ChildString("uuid"), Node: n, Index: idx}
	switch n.Name() {
	case "segment", "arc":
		t.Start = pt(n.Child("start"))
		t.End = pt(n.Child("end"))
		t.Layer = n.ChildString("layer")
		t.Width, _ = n.ChildFloat("width")
		if n.Name() == "arc" {
			t.Kind = KindArc
			t.Mid = pt(n.Child("mid"))
		} else {
			t.Kind = KindSegment
		}
	case "via":
		t.Kind = KindVia
		t.Start = pt(n.Child("at"))
		t.End = t.Start
		t.Size, _ = n.ChildFloat("size")
		t.Drill, _ = n.ChildFloat("drill")
		if l := n.Child("layers"); l != nil {
			t.LayerTop = l.ArgString(0)
			t.LayerBottom = l.ArgString(1)
		}
	default:
		return nil
	}
	return t
}

// buildTrackNode renders a track as a fresh S-expression, in the same child
// order pcbnew writes so that a diff against a pcbnew-saved file stays legible.
func buildTrackNode(t *Track) *sexpr.Node {
	switch t.Kind {
	case KindArc:
		n := sexpr.Sym("arc",
			sexpr.Sym("start", sexpr.Float(t.Start.X), sexpr.Float(t.Start.Y)),
			sexpr.Sym("mid", sexpr.Float(t.Mid.X), sexpr.Float(t.Mid.Y)),
			sexpr.Sym("end", sexpr.Float(t.End.X), sexpr.Float(t.End.Y)),
			sexpr.Sym("width", sexpr.Float(t.Width)),
			sexpr.Sym("layer", sexpr.String(t.Layer)),
			sexpr.Sym("net", sexpr.String(t.Net)),
		)
		if t.UUID != "" {
			n.Append(sexpr.Sym("uuid", sexpr.String(t.UUID)))
		}
		return n
	case KindVia:
		n := sexpr.Sym("via",
			sexpr.Sym("at", sexpr.Float(t.Start.X), sexpr.Float(t.Start.Y)),
			sexpr.Sym("size", sexpr.Float(t.Size)),
			sexpr.Sym("drill", sexpr.Float(t.Drill)),
			sexpr.Sym("layers", sexpr.String(t.LayerTop), sexpr.String(t.LayerBottom)),
			sexpr.Sym("net", sexpr.String(t.Net)),
		)
		if t.UUID != "" {
			n.Append(sexpr.Sym("uuid", sexpr.String(t.UUID)))
		}
		return n
	default:
		n := sexpr.Sym("segment",
			sexpr.Sym("start", sexpr.Float(t.Start.X), sexpr.Float(t.Start.Y)),
			sexpr.Sym("end", sexpr.Float(t.End.X), sexpr.Float(t.End.Y)),
			sexpr.Sym("width", sexpr.Float(t.Width)),
			sexpr.Sym("layer", sexpr.String(t.Layer)),
			sexpr.Sym("net", sexpr.String(t.Net)),
		)
		if t.UUID != "" {
			n.Append(sexpr.Sym("uuid", sexpr.String(t.UUID)))
		}
		return n
	}
}

func parseFootprint(n *sexpr.Node) *Footprint {
	fp := &Footprint{Library: n.ArgString(0), Node: n}
	if at := n.Child("at"); at != nil {
		fp.At = pt(at)
		fp.Rot, _ = at.ArgFloat(2)
	}
	for _, p := range n.Children("property") {
		switch p.ArgString(0) {
		case "Reference":
			fp.Ref = p.ArgString(1)
		case "Value":
			fp.Value = p.ArgString(1)
		}
	}
	// KiCad's footprint transform: a footprint rotation of r degrees maps a
	// local point (x,y) to (x cos r + y sin r, -x sin r + y cos r) before the
	// placement offset. The sign convention follows from Y pointing down.
	rad := fp.Rot * math.Pi / 180
	cs, sn := math.Cos(rad), math.Sin(rad)
	toBoard := func(p geom.Pt) geom.Pt {
		return geom.Pt{
			X: fp.At.X + p.X*cs + p.Y*sn,
			Y: fp.At.Y - p.X*sn + p.Y*cs,
		}
	}

	for _, pn := range n.Children("pad") {
		fp.Pads = append(fp.Pads, parsePad(pn, fp, toBoard))
	}
	var lines []geom.Pt
	// The courtyard is whatever the footprint draws on a courtyard layer, and
	// a library part draws it however its author felt like. On the demo board
	// alone there are 317 rectangles, 860 lines, 89 circles and 6 polygons on
	// those layers -- so reading only polygons, as this did, found a courtyard
	// on almost nothing. That mattered once the custom design rules were read,
	// because theirs are written in terms of what a courtyard contains.
	for _, gn := range n.Items {
		if gn.Kind != sexpr.KindList {
			continue
		}
		if l := gn.ChildString("layer"); l != "F.CrtYd" && l != "B.CrtYd" {
			continue
		}
		switch gn.Name() {
		case "fp_poly":
			fp.Courtyard = append(fp.Courtyard, polyOf(gn, toBoard))
		case "fp_rect":
			a, b := pt(gn.Child("start")), pt(gn.Child("end"))
			fp.Courtyard = append(fp.Courtyard, geom.Poly{
				toBoard(a), toBoard(geom.Pt{X: b.X, Y: a.Y}),
				toBoard(b), toBoard(geom.Pt{X: a.X, Y: b.Y}),
			})
		case "fp_line":
			// One line is not an area. Lines are collected and the extent of
			// the whole set is taken below, which is what a courtyard drawn as
			// an outline of segments amounts to.
			lines = append(lines, toBoard(pt(gn.Child("start"))), toBoard(pt(gn.Child("end"))))
		case "fp_circle":
			c, e := toBoard(pt(gn.Child("center"))), toBoard(pt(gn.Child("end")))
			r := c.Dist(e)
			lines = append(lines,
				geom.Pt{X: c.X - r, Y: c.Y - r}, geom.Pt{X: c.X + r, Y: c.Y + r})
		}
	}
	// A courtyard drawn as loose lines or circles becomes its bounding box.
	// Not the true outline, and deliberately so: it is used to ask whether a
	// piece of copper is inside the part's footprint, and a box is the
	// conservative answer to that -- it can only ever say yes where the true
	// outline would say no.
	if len(lines) >= 2 {
		box := geom.Rect{MinX: lines[0].X, MinY: lines[0].Y, MaxX: lines[0].X, MaxY: lines[0].Y}
		for _, p := range lines[1:] {
			box = box.Include(p)
		}
		fp.Courtyard = append(fp.Courtyard, geom.Poly{
			{X: box.MinX, Y: box.MinY}, {X: box.MaxX, Y: box.MinY},
			{X: box.MaxX, Y: box.MaxY}, {X: box.MinX, Y: box.MaxY},
		})
	}
	return fp
}

func polyOf(n *sexpr.Node, xf func(geom.Pt) geom.Pt) geom.Poly {
	var out geom.Poly
	for _, xy := range n.Child("pts").Children("xy") {
		out = append(out, xf(pt(xy)))
	}
	return out
}

func parsePad(n *sexpr.Node, fp *Footprint, toBoard func(geom.Pt) geom.Pt) *Pad {
	p := &Pad{
		Ref:      fp.Ref,
		Name:     n.ArgString(0),
		Type:     n.ArgString(1),
		Net:      n.ChildString("net"),
		Function: n.ChildString("pinfunction"),
		Node:     n,
	}
	p.DieLength, _ = n.ChildFloat("die_length")
	if p.DieLength > 0 {
		p.DieSource = "footprint"
	}
	p.Clearance, _ = n.ChildFloat("clearance")

	shape := n.ArgString(2)
	local := geom.Pt{}
	padRot := 0.0
	if at := n.Child("at"); at != nil {
		local = pt(at)
		// The pad's stored angle is already absolute: pcbnew folds the parent
		// footprint's rotation into it when the footprint is placed. So it is
		// used as-is for the shape's orientation, while only the position goes
		// through the footprint transform.
		padRot, _ = at.ArgFloat(2)
	}
	p.Centre = toBoard(local)

	var w, h float64
	if s := n.Child("size"); s != nil {
		w, _ = s.ArgFloat(0)
		h, _ = s.ArgFloat(1)
	}
	rot := padRot * math.Pi / 180

	switch shape {
	case "circle":
		p.Shape = geom.Disc(p.Centre, math.Max(w, h)/2)
	case "oval":
		p.Shape = geom.OvalShape(p.Centre, w, h, rot)
	case "roundrect":
		ratio, ok := n.ChildFloat("roundrect_rratio")
		if !ok {
			ratio = 0.25
		}
		p.Shape = geom.RectShape(p.Centre, w, h, rot, ratio*math.Min(w, h))
	case "trapezoid":
		d := geom.Pt{}
		if rd := n.Child("rect_delta"); rd != nil {
			d = pt(rd)
		}
		p.Shape = geom.TrapShape(p.Centre, w, h, rot, d)
	case "custom":
		p.Shape = customPadShape(n, p.Centre, w, h, rot)
	default: // "rect", and anything a future KiCad adds
		p.Shape = geom.RectShape(p.Centre, w, h, rot, 0)
	}

	if l := n.Child("layers"); l != nil {
		for _, a := range l.Args() {
			name := a.Value()
			if strings.HasSuffix(name, ".Cu") || name == "*.Cu" {
				p.Layers = append(p.Layers, name)
			}
		}
	}
	// A through hole conducts on every copper layer whatever its layer list
	// says, which matters for tracks that leave from an inner layer.
	if p.Type == "thru_hole" || p.Type == "np_thru_hole" {
		p.Layers = []string{"*.Cu"}
	}
	return p
}

// customPadShape approximates a custom pad by the convex hull of its anchor
// rectangle and its primitive outlines. Exactness is not needed: the shape is
// only used to decide whether a track endpoint lands on the pad and to keep
// meanders away from it, and a slightly generous outline errs toward caution.
func customPadShape(n *sexpr.Node, centre geom.Pt, w, h, rot float64) geom.RoundPoly {
	pts := []geom.Pt{}
	anchor := geom.RectShape(centre, w, h, rot, 0)
	pts = append(pts, anchor.Pts...)
	if prims := n.Child("primitives"); prims != nil {
		for _, g := range prims.Items {
			if g.Kind != sexpr.KindList {
				continue
			}
			switch g.Name() {
			case "gs_poly", "gr_poly":
				for _, xy := range g.Child("pts").Children("xy") {
					pts = append(pts, pt(xy).Rotate(rot).Add(centre))
				}
			case "gs_line", "gr_line":
				pts = append(pts, pt(g.Child("start")).Rotate(rot).Add(centre))
				pts = append(pts, pt(g.Child("end")).Rotate(rot).Add(centre))
			case "gs_circle", "gr_circle":
				c := pt(g.Child("center")).Rotate(rot).Add(centre)
				e := pt(g.Child("end")).Rotate(rot).Add(centre)
				r := c.Dist(e)
				pts = append(pts, geom.Pt{X: c.X - r, Y: c.Y - r}, geom.Pt{X: c.X + r, Y: c.Y + r})
			}
		}
	}
	if len(pts) < 3 {
		return anchor
	}
	return geom.RoundPoly{Pts: hull(pts)}
}

// hull is a monotone-chain convex hull.
func hull(pts []geom.Pt) []geom.Pt {
	if len(pts) < 3 {
		return pts
	}
	ps := make([]geom.Pt, len(pts))
	copy(ps, pts)
	less := func(a, b geom.Pt) bool {
		if a.X != b.X {
			return a.X < b.X
		}
		return a.Y < b.Y
	}
	// Insertion sort is fine: custom pads have a handful of points.
	for i := 1; i < len(ps); i++ {
		for j := i; j > 0 && less(ps[j], ps[j-1]); j-- {
			ps[j], ps[j-1] = ps[j-1], ps[j]
		}
	}
	build := func(in []geom.Pt) []geom.Pt {
		var out []geom.Pt
		for _, p := range in {
			for len(out) >= 2 {
				if out[len(out)-1].Sub(out[len(out)-2]).Cross(p.Sub(out[len(out)-2])) > 0 {
					break
				}
				out = out[:len(out)-1]
			}
			out = append(out, p)
		}
		return out
	}
	lower := build(ps)
	rev := make([]geom.Pt, len(ps))
	for i := range ps {
		rev[i] = ps[len(ps)-1-i]
	}
	upper := build(rev)
	return append(lower[:len(lower)-1], upper[:len(upper)-1]...)
}

func parseZone(n *sexpr.Node) *Zone {
	z := &Zone{Net: n.ChildString("net"), Filled: map[string][]geom.Poly{}, Node: n}
	if l := n.Child("layer"); l != nil {
		z.Layers = append(z.Layers, l.ArgString(0))
	}
	for _, l := range n.Children("layers") {
		for _, a := range l.Args() {
			z.Layers = append(z.Layers, a.Value())
		}
	}
	// filled_polygon blocks are what the pour actually covers after filling;
	// the zone outline is irrelevant for clearance.
	for _, fp := range n.Children("filled_polygon") {
		layer := fp.ChildString("layer")
		for _, pts := range fp.Children("pts") {
			var poly geom.Poly
			for _, xy := range pts.Children("xy") {
				poly = append(poly, pt(xy))
			}
			if len(poly) >= 3 {
				z.Filled[layer] = append(z.Filled[layer], poly)
			}
		}
	}
	return z
}

// parseGraphic extracts Edge.Cuts geometry as segments, so the checker can keep
// copper away from the board edge. Arcs and circles are flattened.
func parseGraphic(n *sexpr.Node) []geom.Seg {
	if n.ChildString("layer") != "Edge.Cuts" {
		return nil
	}
	switch n.Name() {
	case "gr_line":
		return []geom.Seg{{A: pt(n.Child("start")), B: pt(n.Child("end"))}}
	case "gr_arc":
		a := geom.Arc{Start: pt(n.Child("start")), Mid: pt(n.Child("mid")), End: pt(n.Child("end"))}
		return segsOf(a.Flatten(0.01))
	case "gr_rect":
		s, e := pt(n.Child("start")), pt(n.Child("end"))
		return segsOf([]geom.Pt{{X: s.X, Y: s.Y}, {X: e.X, Y: s.Y}, {X: e.X, Y: e.Y}, {X: s.X, Y: e.Y}, {X: s.X, Y: s.Y}})
	case "gr_circle":
		c, e := pt(n.Child("center")), pt(n.Child("end"))
		r := c.Dist(e)
		var pts []geom.Pt
		const n0 = 64
		for i := 0; i <= n0; i++ {
			a := 2 * math.Pi * float64(i) / n0
			pts = append(pts, geom.Pt{X: c.X + r*math.Cos(a), Y: c.Y + r*math.Sin(a)})
		}
		return segsOf(pts)
	case "gr_poly":
		var pts []geom.Pt
		for _, xy := range n.Child("pts").Children("xy") {
			pts = append(pts, pt(xy))
		}
		if len(pts) >= 2 {
			pts = append(pts, pts[0])
		}
		return segsOf(pts)
	}
	return nil
}

func segsOf(pts []geom.Pt) []geom.Seg {
	var out []geom.Seg
	for i := 0; i+1 < len(pts); i++ {
		out = append(out, geom.Seg{A: pts[i], B: pts[i+1]})
	}
	return out
}
