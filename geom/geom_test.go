package geom

import (
	"math"
	"testing"
)

func close2(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestArcLenSemicircle(t *testing.T) {
	// Unit semicircle from (1,0) through (0,1) to (-1,0): length pi.
	a := Arc{Pt{1, 0}, Pt{0, 1}, Pt{-1, 0}}
	if !close2(a.Len(), math.Pi, 1e-9) {
		t.Errorf("Len = %v, want pi", a.Len())
	}
	c, r, ok := a.Circle()
	if !ok || !close2(r, 1, 1e-9) || !c.Near(Pt{0, 0}) {
		t.Errorf("Circle = %v %v %v", c, r, ok)
	}
}

func TestArcLenMinorAndMajor(t *testing.T) {
	// Quarter arc: length pi/2.
	minor := Arc{Pt{1, 0}, Pt{math.Sqrt2 / 2, math.Sqrt2 / 2}, Pt{0, 1}}
	if !close2(minor.Len(), math.Pi/2, 1e-9) {
		t.Errorf("minor Len = %v, want pi/2", minor.Len())
	}
	// Same endpoints, mid on the far side: three-quarter arc, length 3pi/2.
	// A start-to-end angle difference alone cannot tell these apart, which is
	// why Sweep walks through the midpoint.
	major := Arc{Pt{1, 0}, Pt{-math.Sqrt2 / 2, -math.Sqrt2 / 2}, Pt{0, 1}}
	if !close2(major.Len(), 3*math.Pi/2, 1e-9) {
		t.Errorf("major Len = %v, want 3pi/2", major.Len())
	}
}

func TestArcDegenerateBecomesChord(t *testing.T) {
	a := Arc{Pt{0, 0}, Pt{1, 0}, Pt{2, 0}}
	if !a.Degenerate() {
		t.Error("collinear arc not reported degenerate")
	}
	if !close2(a.Len(), 2, 1e-12) {
		t.Errorf("Len = %v, want 2", a.Len())
	}
}

func TestArcFlattenRespectsTolerance(t *testing.T) {
	a := Arc{Pt{1, 0}, Pt{0, 1}, Pt{-1, 0}}
	for _, tol := range []float64{0.1, 0.01, 0.005, 0.001} {
		pts := a.Flatten(tol)
		if len(pts) < 2 {
			t.Fatalf("tol %v: %d points", tol, len(pts))
		}
		if !pts[0].Near(a.Start) || !pts[len(pts)-1].Near(a.End) {
			t.Errorf("tol %v: endpoints not pinned", tol)
		}
		// Every chord midpoint must sit within tol of the true circle.
		for i := 0; i+1 < len(pts); i++ {
			m := (Seg{pts[i], pts[i+1]}).Mid()
			if err := math.Abs(m.Len() - 1); err > tol*1.05 {
				t.Errorf("tol %v: sagitta %v exceeds tolerance", tol, err)
			}
		}
		// And the polyline must under-measure the true arc by only a little.
		var l float64
		for i := 0; i+1 < len(pts); i++ {
			l += pts[i].Dist(pts[i+1])
		}
		if l > math.Pi+1e-9 || l < math.Pi*(1-0.05) {
			t.Errorf("tol %v: polyline len %v vs pi", tol, l)
		}
	}
}

func TestSegDistToSeg(t *testing.T) {
	cases := []struct {
		a, b Seg
		want float64
	}{
		{Seg{Pt{0, 0}, Pt{10, 0}}, Seg{Pt{0, 2}, Pt{10, 2}}, 2},           // parallel
		{Seg{Pt{0, 0}, Pt{10, 0}}, Seg{Pt{5, -5}, Pt{5, 5}}, 0},           // crossing
		{Seg{Pt{0, 0}, Pt{1, 0}}, Seg{Pt{3, 0}, Pt{4, 0}}, 2},             // collinear apart
		{Seg{Pt{0, 0}, Pt{0, 1}}, Seg{Pt{3, 4}, Pt{3, 5}}, math.Sqrt(18)}, // nearest ends are (0,1) and (3,4)
		{Seg{Pt{0, 0}, Pt{0, 1}}, Seg{Pt{3, 5}, Pt{4, 5}}, 5},             // 3-4-5 endpoint pair
		{Seg{Pt{0, 0}, Pt{10, 0}}, Seg{Pt{5, 0}, Pt{5, 3}}, 0},            // T touching
	}
	for i, c := range cases {
		if got := c.a.DistToSeg(c.b); !close2(got, c.want, 1e-9) {
			t.Errorf("case %d: got %v, want %v", i, got, c.want)
		}
		if got := c.b.DistToSeg(c.a); !close2(got, c.want, 1e-9) {
			t.Errorf("case %d reversed: got %v, want %v", i, got, c.want)
		}
	}
}

func TestSegDegenerate(t *testing.T) {
	// A zero-length track is legal in a .kicad_pcb and must not divide by zero.
	s := Seg{Pt{1, 1}, Pt{1, 1}}
	if got := s.DistToPoint(Pt{4, 5}); !close2(got, 5, 1e-9) {
		t.Errorf("got %v, want 5", got)
	}
}

func TestSnapToNanometreGrid(t *testing.T) {
	p := Pt{1.00000049, -2.00000051}.Snap()
	if p.X != 1.0 || p.Y != -2.000001 {
		t.Errorf("Snap = %v", p)
	}
}

func TestPolyContainsAndDist(t *testing.T) {
	sq := Poly{{0, 0}, {10, 0}, {10, 10}, {0, 10}}
	if !sq.Contains(Pt{5, 5}) {
		t.Error("inside point not contained")
	}
	if sq.Contains(Pt{15, 5}) {
		t.Error("outside point contained")
	}
	if got := sq.DistToPoint(Pt{13, 5}); !close2(got, 3, 1e-9) {
		t.Errorf("DistToPoint outside = %v, want 3", got)
	}
	if got := sq.DistToPoint(Pt{5, 5}); got > 0 {
		t.Errorf("DistToPoint inside = %v, want negative", got)
	}
	if got := sq.DistToSeg(Seg{Pt{13, 5}, Pt{14, 5}}); !close2(got, 3, 1e-9) {
		t.Errorf("DistToSeg = %v, want 3", got)
	}
	if got := sq.DistToSeg(Seg{Pt{5, 5}, Pt{20, 5}}); got != 0 {
		t.Errorf("crossing DistToSeg = %v, want 0", got)
	}
}

func TestRect(t *testing.T) {
	r := RectFromPts(Pt{1, 2}, Pt{5, 7})
	if r.MinX != 1 || r.MaxY != 7 {
		t.Errorf("r = %+v", r)
	}
	if !r.Overlaps(RectFromPts(Pt{5, 7}, Pt{9, 9})) {
		t.Error("touching boxes should overlap")
	}
	if r.Overlaps(RectFromPts(Pt{6, 8}, Pt{9, 9})) {
		t.Error("disjoint boxes should not overlap")
	}
	if !r.Grow(1).Contains(Pt{0.5, 1.5}) {
		t.Error("Grow did not expand")
	}
	if !RectFromPts().Empty() {
		t.Error("empty rect not reported empty")
	}
}

func TestRotate(t *testing.T) {
	p := Pt{1, 0}.Rotate(math.Pi / 2)
	if !close2(p.X, 0, 1e-12) || !close2(p.Y, 1, 1e-12) {
		t.Errorf("Rotate = %v", p)
	}
}

func TestRoundPolyShapes(t *testing.T) {
	// A disc: surface distance is centre distance minus radius.
	d := Disc(Pt{0, 0}, 2)
	if got := d.DistToPoint(Pt{5, 0}); !close2(got, 3, 1e-9) {
		t.Errorf("disc DistToPoint = %v, want 3", got)
	}
	if got := d.DistToPoint(Pt{1, 0}); !close2(got, -1, 1e-9) {
		t.Errorf("disc inside = %v, want -1", got)
	}

	// A 0.09 mm track: half-width 0.045 either side.
	c := Capsule(Pt{0, 0}, Pt{10, 0}, 0.09)
	if got := c.DistToPoint(Pt{5, 1}); !close2(got, 1-0.045, 1e-9) {
		t.Errorf("capsule = %v", got)
	}

	// Two parallel tracks 0.2 mm apart centre to centre, each 0.09 wide: the
	// copper-to-copper gap is 0.2 - 0.09 = 0.11.
	c2 := Capsule(Pt{0, 0.2}, Pt{10, 0.2}, 0.09)
	if got := c.Dist(c2); !close2(got, 0.11, 1e-9) {
		t.Errorf("track gap = %v, want 0.11", got)
	}
}

func TestRectShapeOuterSize(t *testing.T) {
	// The outer dimensions must come out as asked whatever the corner radius.
	for _, r := range []float64{0, 0.1, 0.25} {
		s := RectShape(Pt{0, 0}, 2, 1, 0, r)
		b := s.Box()
		if !close2(b.MaxX-b.MinX, 2, 1e-9) || !close2(b.MaxY-b.MinY, 1, 1e-9) {
			t.Errorf("r=%v box = %+v, want 2x1", r, b)
		}
		if !s.Contains(Pt{0.99, 0}) {
			t.Errorf("r=%v: point just inside the right edge not contained", r)
		}
		if s.Contains(Pt{1.01, 0}) {
			t.Errorf("r=%v: point just outside the right edge contained", r)
		}
	}
	// Rounding the full half-height collapses one axis into a capsule.
	s := RectShape(Pt{0, 0}, 2, 1, 0, 0.5)
	if len(s.Pts) != 2 {
		t.Errorf("fully rounded rect has %d points, want a 2-point capsule", len(s.Pts))
	}
}

func TestOvalShape(t *testing.T) {
	s := OvalShape(Pt{0, 0}, 2, 1, 0)
	b := s.Box()
	if !close2(b.MaxX-b.MinX, 2, 1e-9) || !close2(b.MaxY-b.MinY, 1, 1e-9) {
		t.Errorf("box = %+v, want 2x1", b)
	}
	// Rotating a quarter turn swaps the extents.
	b = OvalShape(Pt{0, 0}, 2, 1, math.Pi/2).Box()
	if !close2(b.MaxX-b.MinX, 1, 1e-9) || !close2(b.MaxY-b.MinY, 2, 1e-9) {
		t.Errorf("rotated box = %+v, want 1x2", b)
	}
}

func TestRoundPolyPolygonDistanceIsSymmetric(t *testing.T) {
	a := RectShape(Pt{0, 0}, 2, 2, 0, 0)
	b := RectShape(Pt{5, 0}, 2, 2, 0, 0)
	d1, d2 := a.Dist(b), b.Dist(a)
	if !close2(d1, 3, 1e-9) || !close2(d2, 3, 1e-9) {
		t.Errorf("Dist = %v / %v, want 3", d1, d2)
	}
}
