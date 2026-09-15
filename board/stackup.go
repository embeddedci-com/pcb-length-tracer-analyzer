package board

import (
	"fmt"
	"math"
	"strings"

	"github.com/embeddedci-com/pcb-autorouter/sexpr"
)

// SpeedOfLight in millimetres per picosecond. Handy because every delay in this
// tool is quoted in ps and every length in mm.
const SpeedOfLight = 0.299792458 // mm/ps

// CopperLayer is one copper layer with its position in the stack. ZTop and
// ZBot are measured from the top of the topmost copper layer, in millimetres.
type CopperLayer struct {
	Name      string
	Thickness float64
	ZTop      float64
	ZBot      float64

	// Index is the layer's position counting from the top copper layer, so
	// F.Cu is 0 and B.Cu is the highest.
	Index int

	// ErAbove and ErBelow are the relative permittivities of the dielectric
	// immediately either side of this layer; zero means air (i.e. the layer is
	// on the outside of the board).
	ErAbove, ErBelow float64

	// HAbove and HBelow are those dielectrics' thicknesses.
	HAbove, HBelow float64
}

// Outer reports whether the layer is on the surface of the board, and is
// therefore microstrip rather than stripline.
func (l CopperLayer) Outer() bool { return l.ErAbove == 0 || l.ErBelow == 0 }

// Stackup is the board's layer stack, read from the setup/stackup block.
type Stackup struct {
	Copper []CopperLayer
	byName map[string]int

	// Thickness is the overall copper-to-copper height, i.e. the distance a
	// full-depth via travels.
	Thickness float64
}

// Layer returns the named copper layer, or false if the board has no such
// layer.
func (s *Stackup) Layer(name string) (CopperLayer, bool) {
	if s == nil {
		return CopperLayer{}, false
	}
	i, ok := s.byName[name]
	if !ok {
		return CopperLayer{}, false
	}
	return s.Copper[i], true
}

// ViaLength is the vertical distance a via spans between two copper layers.
//
// KiCad counts this toward net length when the board's
// use_height_for_length_calcs setting is on, measuring from the top face of the
// upper layer to the bottom face of the lower one. On the demo board that makes
// a full F.Cu-to-B.Cu via exactly 1.594 mm, which is what pcbnew reports.
func (s *Stackup) ViaLength(from, to string) float64 {
	a, okA := s.Layer(from)
	b, okB := s.Layer(to)
	if !okA || !okB {
		return 0
	}
	top := math.Min(a.ZTop, b.ZTop)
	bot := math.Max(a.ZBot, b.ZBot)
	return bot - top
}

// EffectiveEr estimates the effective relative permittivity seen by a track of
// the given width on the named layer.
//
// An inner layer is treated as symmetric stripline, so the effective
// permittivity is just the dielectric's. An outer layer is microstrip, where
// part of the field returns through air; that is estimated with the standard
// Hammerstad approximation.
//
// This is an estimate, not a field solve. It is good enough for its one job:
// length matching is a comparison between nets in the same group, so a common
// error cancels, and what actually has to be right is the roughly 30 per cent
// difference between a microstrip millimetre and a stripline millimetre. A net
// that changes layers mid-run — which every fly-by net on this board does — is
// mismatched by that much if it is matched geometrically.
func (s *Stackup) EffectiveEr(layer string, width float64) float64 {
	l, ok := s.Layer(layer)
	if !ok {
		return 4.3
	}
	if !l.Outer() {
		// Stripline between two dielectrics: weight by thickness, since the
		// field is shared between them.
		ha, hb := l.HAbove, l.HBelow
		if ha+hb <= 0 {
			return math.Max(l.ErAbove, l.ErBelow)
		}
		return (l.ErAbove*ha + l.ErBelow*hb) / (ha + hb)
	}
	er, h := l.ErBelow, l.HBelow
	if er == 0 {
		er, h = l.ErAbove, l.HAbove
	}
	if er == 0 {
		return 1
	}
	if h <= 0 || width <= 0 {
		return (er + 1) / 2
	}
	return (er+1)/2 + (er-1)/2/math.Sqrt(1+10*h/width)
}

// DelayPerMM returns the propagation delay in picoseconds per millimetre for a
// track of the given width on the named layer.
func (s *Stackup) DelayPerMM(layer string, width float64) float64 {
	return math.Sqrt(s.EffectiveEr(layer, width)) / SpeedOfLight
}

// ViaDelayPerMM is the delay per millimetre of via barrel. A via runs through
// the bulk dielectric with no air return path, so it sees the full permittivity
// rather than a microstrip's reduced effective value.
func (s *Stackup) ViaDelayPerMM() float64 {
	er := 0.0
	n := 0
	for _, l := range s.Copper {
		for _, e := range []float64{l.ErAbove, l.ErBelow} {
			if e > 0 {
				er += e
				n++
			}
		}
	}
	if n == 0 {
		return math.Sqrt(4.3) / SpeedOfLight
	}
	return math.Sqrt(er/float64(n)) / SpeedOfLight
}

// parseStackup reads the setup/stackup block.
//
// The block lists physical layers from top to bottom, interleaving copper with
// dielectric, so a single pass accumulates z positions and attaches each copper
// layer to the dielectric either side of it.
func parseStackup(setup *sexpr.Node) *Stackup {
	s := &Stackup{byName: map[string]int{}}
	su := setup.Child("stackup")
	if su == nil {
		return s
	}

	type entry struct {
		name      string
		typ       string
		thickness float64
		er        float64
		copper    bool
	}
	var seq []entry
	for _, ln := range su.Children("layer") {
		name := ln.ArgString(0)
		typ := ln.ChildString("type")
		th, _ := ln.ChildFloat("thickness")
		er, _ := ln.ChildFloat("epsilon_r")
		isCu := strings.EqualFold(typ, "copper")
		// Solder mask, paste and silkscreen sit outside the copper stack and
		// do not affect via length or propagation, so drop them.
		if !isCu && er == 0 && !strings.Contains(strings.ToLower(typ), "dielectric") &&
			!strings.Contains(strings.ToLower(typ), "prepreg") && !strings.Contains(strings.ToLower(typ), "core") {
			continue
		}
		seq = append(seq, entry{name: name, typ: typ, thickness: th, er: er, copper: isCu})
	}

	// Trim to the copper span: z = 0 is the top of the first copper layer.
	first, last := -1, -1
	for i, e := range seq {
		if e.copper {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		return s
	}
	seq = seq[first : last+1]

	z := 0.0
	for i, e := range seq {
		if !e.copper {
			z += e.thickness
			continue
		}
		l := CopperLayer{
			Name:      e.name,
			Thickness: e.thickness,
			ZTop:      z,
			ZBot:      z + e.thickness,
			Index:     len(s.Copper),
		}
		if i > 0 {
			l.ErAbove, l.HAbove = seq[i-1].er, seq[i-1].thickness
		}
		if i+1 < len(seq) {
			l.ErBelow, l.HBelow = seq[i+1].er, seq[i+1].thickness
		}
		s.byName[e.name] = len(s.Copper)
		s.Copper = append(s.Copper, l)
		z = l.ZBot
	}
	if n := len(s.Copper); n > 0 {
		s.Thickness = s.Copper[n-1].ZBot - s.Copper[0].ZTop
	}
	return s
}

// Impedance is a track's characteristic impedance in ohms, and whether the
// model was used inside the range it is good for.
//
// The models are the standard IPC-2141 closed forms: Hammerstad's microstrip
// and the symmetric-stripline expression. They are approximations, and they are
// here for one purpose -- telling a user whether the width they have drawn is
// anywhere near the impedance their interface asks for. A board that has to
// hold 90 ohms to a few percent needs the fabricator's own stackup and a field
// solver, and the report says so rather than implying this replaces it.
type Impedance struct {
	// Ohms is the computed impedance, zero when the stackup does not carry
	// enough to compute one.
	Ohms float64

	// Microstrip is true for an outer layer, where part of the field returns
	// through air. The two cases differ by far more than the models' own
	// error.
	Microstrip bool

	// InRange is false when the geometry is outside where the closed form is
	// trustworthy, in which case Ohms is still returned but should be read as
	// an indication rather than a figure.
	InRange bool

	// Note says what was assumed or what is out of range.
	Note string
}

// TrackImpedance estimates the single-ended impedance of a track.
//
// width and thickness are in millimetres; thickness is the copper weight, which
// matters more than it looks on a thin track -- at 35 micrometres on a 0.09 mm
// track it is a third of the width.
func (s *Stackup) TrackImpedance(layer string, width, thickness float64) Impedance {
	l, ok := s.Layer(layer)
	if !ok || width <= 0 {
		return Impedance{Note: "no stackup for this layer"}
	}
	if thickness <= 0 {
		thickness = l.Thickness
	}
	if l.Outer() {
		er, h := l.ErBelow, l.HBelow
		if er == 0 {
			er, h = l.ErAbove, l.HAbove
		}
		if er <= 0 || h <= 0 {
			return Impedance{Microstrip: true, Note: "the stackup does not say what is under this layer"}
		}
		// Hammerstad, as IPC-2141 states it.
		z := 87 / math.Sqrt(er+1.41) * math.Log(5.98*h/(0.8*width+thickness))
		imp := Impedance{Ohms: z, Microstrip: true, InRange: true}
		if r := width / h; r < 0.1 || r > 2.0 {
			imp.InRange = false
			imp.Note = fmt.Sprintf("width/height is %.2f, outside the 0.1 to 2.0 the model is good for", r)
		}
		return imp
	}
	// Stripline: the plate separation is what the field crosses.
	b := l.HAbove + l.HBelow + thickness
	er := s.EffectiveEr(layer, width)
	if b <= 0 || er <= 0 {
		return Impedance{Note: "the stackup does not say what surrounds this layer"}
	}
	z := 60 / math.Sqrt(er) * math.Log(4*b/(0.67*math.Pi*(0.8*width+thickness)))
	imp := Impedance{Ohms: z, InRange: true}
	if r := width / b; r > 0.35 {
		imp.InRange = false
		imp.Note = fmt.Sprintf("width/separation is %.2f, above the 0.35 the model is good for", r)
	}
	return imp
}

// DiffImpedance estimates the differential impedance of an edge-coupled pair.
//
// gap is the copper-to-copper space between the two halves, in millimetres. The
// coupling terms are the IPC-2141 ones: two tracks far apart are simply twice a
// single track, and the exponential is how much the coupling pulls that down as
// they close.
func (s *Stackup) DiffImpedance(layer string, width, gap, thickness float64) Impedance {
	single := s.TrackImpedance(layer, width, thickness)
	if single.Ohms <= 0 {
		return single
	}
	l, _ := s.Layer(layer)
	if gap <= 0 {
		single.Note = "no spacing given, so this is twice the single-ended figure and ignores the coupling"
		single.Ohms *= 2
		single.InRange = false
		return single
	}
	if single.Microstrip {
		h := l.HBelow
		if h == 0 {
			h = l.HAbove
		}
		if h <= 0 {
			return Impedance{Microstrip: true, Note: "the stackup does not say what is under this layer"}
		}
		single.Ohms = 2 * single.Ohms * (1 - 0.48*math.Exp(-0.96*gap/h))
		return single
	}
	b := l.HAbove + l.HBelow + thickness
	if b <= 0 {
		return Impedance{Note: "the stackup does not say what surrounds this layer"}
	}
	single.Ohms = 2 * single.Ohms * (1 - 0.347*math.Exp(-2.9*gap/b))
	return single
}
