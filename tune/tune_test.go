package tune

import (
	"math"
	"sort"
	"strings"
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/drc"
	"github.com/embeddedci-com/pcb-autorouter/geom"
	"github.com/embeddedci-com/pcb-autorouter/netlen"
)

const demoPath = "../demo-pcb/ai-vision.kicad_pcb"

func fixture(t *testing.T) (*board.Board, *board.Project, *Tuner) {
	t.Helper()
	b, err := board.Load(demoPath)
	if err != nil {
		t.Skipf("demo board missing: %v", err)
	}
	p, err := board.LoadProject(demoPath)
	if err != nil {
		t.Fatal(err)
	}
	return b, p, NewTuner(b, p, DefaultStyle())
}

// TestTuneAddsTheLengthItReports is the core guarantee. After tuning, an
// independent re-measurement of the net -- by the length engine, from the
// rewritten board -- must show exactly the length the tuner said it added.
//
// This is the property the whole tool turns on. A tuner that draws a meander
// and then mis-reports how much length it bought is worse than useless: it
// would leave the board mismatched while claiming it was matched.
func TestTuneAddsTheLengthItReports(t *testing.T) {
	b, _, tu := fixture(t)
	e := netlen.New(b)

	// Nets chosen for having somewhere to put a meander on this very densely
	// routed board (see TestTheDemoBoardHasAlmostNoRoomToMeander), across a
	// range of requirements so that both the many-excursion and the
	// single-small-excursion paths are exercised.
	for _, c := range []struct {
		net  string
		need float64
	}{
		{"/ddr4/DDR_DQ3", 6.0},
		{"/ddr4/DDR_DQ19", 3.0},
		{"/ddr4/DDR_DQ31", 1.0},
		{"/ddr4/DDR_DQ11", 0.2},
	} {
		before := e.Measure(c.net)
		if !before.Longest.Found {
			t.Fatalf("%s: not routed", c.net)
		}
		res, err := tu.Tune(c.net, c.need, nil)
		if err != nil {
			t.Fatalf("%s: %v", c.net, err)
		}
		after := e.Measure(c.net)
		if !after.Longest.Found {
			t.Fatalf("%s: tuning broke the route", c.net)
		}
		grew := after.Longest.Length - before.Longest.Length
		if math.Abs(grew-res.Added) > 1e-4 {
			t.Errorf("%s: reported adding %.4f mm but the route grew by %.4f mm",
				c.net, res.Added, grew)
		}
		if res.Added <= 0 {
			t.Errorf("%s: nothing added; this net was picked for having room", c.net)
		}
		t.Logf("%-18s needed %.3f, added %.3f in %d meander(s), short by %.3f",
			c.net, c.need, res.Added, res.Meanders, res.Shortfall)
	}
}

// TestTuneNeverMovesEndpointsOrChangesTopology checks that tuning is a local
// edit. The net must keep its pads, its route must stay complete, its via count
// must not change, and it must stay on the same layers.
func TestTuneNeverMovesEndpointsOrChangesTopology(t *testing.T) {
	b, _, tu := fixture(t)
	e := netlen.New(b)
	const net = "/ddr4/DDR_DQ3"

	before := e.Measure(net)
	layersBefore := map[string]bool{}
	for _, tr := range b.TracksOfNet(net) {
		for _, l := range tr.Layers(b.Stackup) {
			layersBefore[l] = true
		}
	}
	res, err := tu.Tune(net, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added <= 0 {
		t.Fatal("nothing was added, so this proves nothing")
	}
	after := e.Measure(net)

	if len(after.Pads) != len(before.Pads) {
		t.Errorf("pads changed from %v to %v", before.Pads, after.Pads)
	}
	if !after.Complete {
		t.Errorf("the net is no longer fully connected: %v", after.Islands)
	}
	if after.Vias != before.Vias {
		t.Errorf("via count changed from %d to %d", before.Vias, after.Vias)
	}
	if after.Longest.From != before.Longest.From || after.Longest.To != before.Longest.To {
		t.Errorf("measured between %s..%s, was %s..%s",
			after.Longest.From, after.Longest.To, before.Longest.From, before.Longest.To)
	}
	layersAfter := map[string]bool{}
	for _, tr := range b.TracksOfNet(net) {
		for _, l := range tr.Layers(b.Stackup) {
			layersAfter[l] = true
		}
	}
	if len(layersAfter) != len(layersBefore) {
		t.Errorf("layers used changed from %v to %v", layersBefore, layersAfter)
	}
	// Everything written must still parse, and measure the same on reload.
	b2, err := board.Parse(b.Bytes(), "reparsed")
	if err != nil {
		t.Fatalf("the rewritten board does not parse: %v", err)
	}
	reloaded := netlen.New(b2).Measure(net)
	if math.Abs(reloaded.Longest.Length-after.Longest.Length) > 1e-6 {
		t.Errorf("reloaded length %.6f differs from in-memory %.6f",
			reloaded.Longest.Length, after.Longest.Length)
	}
}

// TestTuningNeverMakesClearanceWorse is the safety property.
//
// The right question on a board that already has 687 design rule violations is
// not whether the edited copper is clean -- replacing a track re-creates the
// parts of it that did not change, and if the original was too close to
// something then so is the copy. The question is whether anything got worse.
//
// So each replacement is compared against the track it replaced: it may clash
// with nothing new, and nothing more tightly. The meander itself, being copper
// where there was none, is held to the stricter standard of no clashes at all.
func TestTuningNeverMakesClearanceWorse(t *testing.T) {
	b, p, tu := fixture(t)
	nets := map[string]float64{
		"/ddr4/DDR_DQ3":   6.0,
		"/ddr4/DDR_DQ19":  3.0,
		"/ddr4/DDR_DQ31":  2.0,
		"/ddr4/DDR_A13":   2.0,
		"/ddr4/DDR_BG0":   9.0,
		"/ddr4/DDR_CLK_N": 1.0,
	}
	order := make([]string, 0, len(nets))
	for n := range nets {
		order = append(order, n)
	}
	sort.Strings(order)

	before := drc.New(b, p)
	// The worst clearance each net's copper leaves against every obstacle,
	// before anything is touched.
	baseline := map[string]map[string]float64{}
	for _, net := range order {
		baseline[net] = worstForNet(b, before, net)
	}

	added := 0
	for _, net := range order {
		res, err := tu.Tune(net, nets[net], nil)
		if err != nil {
			t.Fatal(err)
		}
		if res.Meanders > 0 {
			added++
		}
	}
	if added == 0 {
		t.Fatal("nothing was drawn, so this proves nothing")
	}

	after := drc.New(b, p)
	for _, net := range order {
		now := worstForNet(b, after, net)
		for key, actual := range now {
			was, existed := baseline[net][key]
			if !existed {
				t.Errorf("%s now clashes with %s, which it did not before (%.4f mm)", net, key, actual)
				continue
			}
			if actual < was-1e-9 {
				t.Errorf("%s clearance to %s fell from %.4f to %.4f mm", net, key, was, actual)
			}
		}
	}
	t.Logf("checked %d tuned nets against their own baselines", len(order))
}

// worstForNet returns the tightest clearance the net's copper leaves against
// each other net, with its own copper and pads exempted.
//
// The key is the other net's name and the kind of thing it is, deliberately not
// the item's uuid. Replacing a track re-creates the parts of it that did not
// change under a new uuid, so a uuid-keyed comparison reports every such stretch
// as a brand-new clash with the thing it was always that close to. What has to
// hold is a statement about copper, not about identifiers: no net's copper may
// end up closer to another net's copper than it already was.
func worstForNet(b *board.Board, chk *drc.Checker, net string) map[string]float64 {
	exempt := map[string]bool{}
	for _, o := range b.TracksOfNet(net) {
		if o.UUID != "" {
			exempt[o.UUID] = true
		}
	}
	for _, pd := range b.PadsOfNet(net) {
		exempt[pd.ID()] = true
	}
	out := map[string]float64{}
	for _, tr := range b.TracksOfNet(net) {
		for _, v := range chk.Check(drc.Candidate{
			Shapes: tr.Shape(b.MaxError), Layer: tr.Layer, Net: net, Exempt: exempt,
		}) {
			k := v.Kind + "|" + v.Net + "|" + v.Layer
			if cur, ok := out[k]; !ok || v.Actual < cur {
				out[k] = v.Actual
			}
		}
	}
	return out
}

// TestMeanderCopperItselfIsAlwaysClean checks the stricter half of the rule:
// copper drawn where there was none must have no clashes at all. Anything the
// replacement is blamed for has to be traceable to the original track.
func TestMeanderCopperItselfIsAlwaysClean(t *testing.T) {
	b, p, tu := fixture(t)
	const net = "/ddr4/DDR_BG0"

	// Where the original copper ran, so that inherited violations can be told
	// apart from newly created ones.
	var origPath []geom.RoundPoly
	was := map[*board.Track]bool{}
	for _, tr := range b.TracksOfNet(net) {
		was[tr] = true
		origPath = append(origPath, tr.Shape(b.MaxError)...)
	}
	res, err := tu.Tune(net, 9.0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Meanders == 0 {
		t.Skip("nothing drawn on this net")
	}

	chk := drc.New(b, p)
	exempt := map[string]bool{}
	for _, o := range b.TracksOfNet(net) {
		if o.UUID != "" {
			exempt[o.UUID] = true
		}
	}
	for _, pd := range b.PadsOfNet(net) {
		exempt[pd.ID()] = true
	}
	offOriginal, inherited := 0, 0
	for _, tr := range b.TracksOfNet(net) {
		if was[tr] {
			continue
		}
		vs := chk.Check(drc.Candidate{
			Shapes: tr.Shape(b.MaxError), Layer: tr.Layer, Net: net, Exempt: exempt,
		})
		if len(vs) == 0 {
			continue
		}
		// Does this track lie on the original copper's path?
		onOriginal := true
		for _, sh := range tr.Shape(b.MaxError) {
			nearest := math.Inf(1)
			for _, o := range origPath {
				if d := sh.Dist(o); d < nearest {
					nearest = d
				}
			}
			if nearest > 1e-6 {
				onOriginal = false
				break
			}
		}
		if onOriginal {
			inherited += len(vs)
			continue
		}
		offOriginal += len(vs)
		for _, v := range vs[:min(2, len(vs))] {
			t.Errorf("newly drawn copper off the original path clashes with %s [%s]: %.4f < %.4f",
				v.Kind, v.Net, v.Actual, v.Required)
		}
	}
	if offOriginal != 0 {
		t.Errorf("%d clash(es) on copper drawn where none was", offOriginal)
	}
	t.Logf("%d inherited clash(es) on re-created copper, %d on new copper", inherited, offOriginal)
}

// TestTuneDoesNotOvershoot checks that a small requirement produces a small
// meander. Overshooting is as bad as falling short: it pushes the net past its
// group and the error cannot be taken back out.
func TestTuneDoesNotOvershoot(t *testing.T) {
	b, _, tu := fixture(t)
	e := netlen.New(b)
	for _, net := range []string{"/ddr4/DDR_DQ3", "/ddr4/DDR_DQ11", "/ddr4/DDR_DQ19"} {
		for _, need := range []float64{0.05, 0.2, 0.5} {
			before := e.Measure(net).Longest.Length
			res, err := tu.Tune(net, need, nil)
			if err != nil {
				t.Fatal(err)
			}
			after := e.Measure(net).Longest.Length
			grew := after - before
			if grew > need+1e-4 {
				t.Errorf("%s: asked for %.3f mm, added %.3f mm", net, need, grew)
			}
			if res.Shortfall > 1e-9 && res.Added > 0 {
				t.Logf("%s: asked %.3f, got %.3f (short %.3f)", net, need, res.Added, res.Shortfall)
			}
		}
	}
}

// TestTuneReportsShortfallRatherThanFailing checks the behaviour asked for when
// a target cannot be reached: add what fits, say what did not, change nothing
// else.
func TestTuneReportsShortfallRatherThanFailing(t *testing.T) {
	b, _, tu := fixture(t)
	e := netlen.New(b)
	const net = "/ddr4/DDR_DQ3"
	before := e.Measure(net).Longest.Length

	// Far more than could possibly fit beside an existing DDR route.
	res, err := tu.Tune(net, 500, nil)
	if err != nil {
		t.Fatalf("a target that cannot be met must not be an error: %v", err)
	}
	if res.Shortfall <= 0 {
		t.Fatalf("expected a shortfall, got %+v", res)
	}
	if res.Added <= 0 {
		t.Error("expected best-effort progress, not nothing")
	}
	if len(res.Notes) == 0 {
		t.Error("expected a note explaining the shortfall")
	}
	after := e.Measure(net)
	if !after.Complete {
		t.Error("a partially met target left the net broken")
	}
	if math.Abs((after.Longest.Length-before)-res.Added) > 1e-4 {
		t.Errorf("reported %.4f mm added, route grew %.4f", res.Added, after.Longest.Length-before)
	}
	t.Logf("asked for 500 mm, fitted %.3f mm in %d meanders", res.Added, res.Meanders)
}

func TestTuneZeroAndNegativeAreNoOps(t *testing.T) {
	b, _, tu := fixture(t)
	src := string(b.Bytes())
	for _, need := range []float64{0, -1} {
		res, err := tu.Tune("/ddr4/DDR_DQ3", need, nil)
		if err != nil {
			t.Fatal(err)
		}
		if res.Added != 0 || res.Meanders != 0 {
			t.Errorf("need=%v changed the board: %+v", need, res)
		}
	}
	if string(b.Bytes()) != src {
		t.Error("the board was modified")
	}
}

// TestTuneUnknownNet checks that a net that is not on the board is reported as
// impossible rather than raising an error: the caller gets a shortfall it can
// show the user, and nothing is written.
func TestTuneUnknownNet(t *testing.T) {
	b, _, tu := fixture(t)
	src := string(b.Bytes())
	res, err := tu.Tune("/nope", 1, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Added != 0 || res.Shortfall != 1 || len(res.Notes) == 0 {
		t.Errorf("got %+v, want no progress and an explanation", res)
	}
	if string(b.Bytes()) != src {
		t.Error("the board was modified")
	}
}

// TestTuneWithNoWidthAvailableIsAnError covers the one case that really is a
// programming error rather than a board fact: no routed copper to copy a width
// from and no net class to fall back on.
func TestTuneWithNoWidthAvailableIsAnError(t *testing.T) {
	b, err := board.Load(demoPath)
	if err != nil {
		t.Skip(err)
	}
	// A project with no classes at all.
	empty, err := board.LoadProject(t.TempDir() + "/none.kicad_pcb")
	if err != nil {
		t.Fatal(err)
	}
	tu := NewTuner(b, empty, DefaultStyle())
	if _, err := tu.Tune("/nope", 1, nil); err == nil {
		t.Error("expected an error when no width can be determined")
	}
}

// TestTuneKeepsTheFileParseableAndOtherNetsUntouched checks the blast radius:
// tuning one net must leave every other net's copper exactly as it was.
func TestTuneKeepsTheFileParseableAndOtherNetsUntouched(t *testing.T) {
	b, _, tu := fixture(t)
	e := netlen.New(b)
	const tuned = "/ddr4/DDR_DQ3"

	before := map[string]float64{}
	for _, net := range b.Nets() {
		before[net] = e.Measure(net).TotalCopper
	}
	if _, err := tu.Tune(tuned, 5, nil); err != nil {
		t.Fatal(err)
	}
	b2, err := board.Parse(b.Bytes(), "reparsed")
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	e2 := netlen.New(b2)
	changed := 0
	for net, was := range before {
		now := e2.Measure(net).TotalCopper
		if math.Abs(now-was) < 1e-9 {
			continue
		}
		changed++
		if net != tuned {
			t.Errorf("%s changed from %.4f to %.4f mm but was not being tuned", net, was, now)
		}
	}
	if changed != 1 {
		t.Errorf("%d nets changed, want exactly 1", changed)
	}
}

func BenchmarkTuneOneNet(bench *testing.B) {
	for bench.Loop() {
		b, err := board.Load(demoPath)
		if err != nil {
			bench.Skip(err)
		}
		p, _ := board.LoadProject(demoPath)
		tu := NewTuner(b, p, DefaultStyle())
		if _, err := tu.Tune("/ddr4/DDR_DQ3", 6, nil); err != nil {
			bench.Fatal(err)
		}
	}
}

// TestTheDemoBoardIsRoutedAtMinimumClearance records the constraint that shapes
// what this tool can achieve, and how much of it profiling the room recovers.
//
// The demo board's byte lanes are routed at exactly the minimum DDR clearance:
// the nearest neighbour of one DQ24 segment sits 0.2000 mm away against a
// 0.2 mm rule. Asking for one amplitude that holds over a whole track therefore
// finds nothing at all on those tracks -- which is how an earlier version
// reported that DQ24 could not be lengthened by a single micrometre.
//
// The room is not uniform along a track, though. Profiling it in steps and
// taking the best stretches finds DQ24 its whole 7.609 mm, and raises what the
// out-of-tolerance nets can absorb across the board from 49 mm to 67 mm. What
// remains beyond that needs the bus itself re-spaced, or a reroute.
func TestTheDemoBoardIsRoutedAtMinimumClearance(t *testing.T) {
	b, _, tu := fixture(t)

	var probe *board.Track
	for _, tr := range b.TracksOfNet("/ddr4/DDR_DQ24") {
		if tr.Kind == board.KindSegment && tr.Length(b.Stackup) > 20 {
			probe = tr
		}
	}
	if probe == nil {
		t.Fatal("no long DQ24 segment")
	}
	nearest := math.Inf(1)
	probeShape := probe.Shape(b.MaxError)[0]
	for _, o := range b.Tracks {
		if o.Net == probe.Net || o.Kind == board.KindVia || o.Layer != probe.Layer {
			continue
		}
		for _, sh := range o.Shape(b.MaxError) {
			if d := probeShape.Dist(sh); d < nearest {
				nearest = d
			}
		}
	}
	if nearest > 0.25 {
		t.Errorf("nearest neighbouring copper is %.4f mm away; this test assumed a minimum-pitch bus", nearest)
	}

	// Whole-track probing finds nothing beside that segment.
	dir := probe.End.Sub(probe.Start).Norm()
	from := probe.Start.Add(dir.Mul(0.3))
	to := probe.End.Sub(dir.Mul(0.3))
	if amp, _ := tu.roomBeside(probe, probe.Net, from, to, 0.09); amp > 0 {
		t.Errorf("whole-track probe found %.3f mm beside a minimum-pitch segment", amp)
	}

	// Profiling finds enough stretches to cover the whole requirement.
	res, err := tu.Tune("/ddr4/DDR_DQ24", 7.609, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Shortfall > 1e-3 {
		t.Errorf("DQ24 fell %.4f mm short of 7.609; profiling should find it all", res.Shortfall)
	}
	t.Logf("nearest neighbour %.4f mm; DQ24 got %.3f mm across %d meander(s)",
		nearest, res.Added, res.Meanders)
}

// TestProfilingBeatsWholeTrackProbing measures the improvement across the whole
// interface rather than on one net, so a regression in the room profiling shows
// up as a number rather than as a mysteriously worse result.
func TestProfilingBeatsWholeTrackProbing(t *testing.T) {
	b, _, tu := fixture(t)

	// Every straight run on a DQ net, measured both ways.
	var whole, profiled float64
	tracks := 0
	for _, net := range []string{
		"/ddr4/DDR_DQ24", "/ddr4/DDR_DQ26", "/ddr4/DDR_DQ28", "/ddr4/DDR_DQ29",
		"/ddr4/DDR_DQ30", "/ddr4/DDR_DQM3", "/ddr4/DDR_DQ27",
	} {
		w := tu.trackWidth(net)
		for _, tr := range b.TracksOfNet(net) {
			if tr.Kind != board.KindSegment || tr.Width <= 0 {
				continue
			}
			keep := math.Max(tu.style.Gap*w, tu.style.KeepClearOfPads)
			if tr.Length(b.Stackup)-2*keep < tu.style.MinRunLength {
				continue
			}
			tracks++
			dir := tr.End.Sub(tr.Start).Norm()
			from := tr.Start.Add(dir.Mul(keep))
			to := tr.End.Sub(dir.Mul(keep))
			amp, _ := tu.roomBeside(tr, net, from, to, w)
			whole += tu.capacity(from.Dist(to), amp, w)
			for _, r := range tu.subRuns(tr, net, from, to, w) {
				profiled += tu.capacity(r.length, r.amp, w)
			}
		}
	}
	if tracks == 0 {
		t.Fatal("no tracks measured")
	}
	if profiled <= whole {
		t.Errorf("profiling found %.3f mm of capacity, whole-track probing %.3f: it should never find less",
			profiled, whole)
	}
	t.Logf("across %d tracks on byte lane 3: whole-track %.1f mm, profiled %.1f mm (%.1fx)",
		tracks, whole, profiled, profiled/math.Max(whole, 1e-9))
}

// TestTuningOnlyTouchesCopperOnTheRoute is the bug the restriction exists for.
//
// The clock pair on the demo board has copper in two islands: the routed leg
// from the controller to the near device, and a stranded piece at its
// termination resistor that no route reaches. Without the restriction the tuner
// folds a meander into the stranded island and reports the length as added --
// 2.4 mm of it on DDR_CLK_N -- while the leg being matched does not move at all.
func TestTuningOnlyTouchesCopperOnTheRoute(t *testing.T) {
	b, _, tu := fixture(t)
	e := netlen.New(b)
	const net = "/ddr4/DDR_CLK_N"

	m := e.Measure(net)
	if m.Complete {
		t.Skip("this net is fully routed, so there is nothing off-route to avoid")
	}
	var leg netlen.Path
	for _, p := range m.Paths {
		if !p.Found {
			continue
		}
		a, c := strings.Split(p.From, ".")[0], strings.Split(p.To, ".")[0]
		if (a == "U3" && c == "U4") || (a == "U4" && c == "U3") {
			leg = p
		}
	}
	if !leg.Found || len(leg.Tracks) == 0 {
		t.Fatal("the U3-U4 leg is not measurable")
	}
	onRoute := map[string]bool{}
	for _, u := range leg.Tracks {
		onRoute[u] = true
	}
	off := 0
	for _, tr := range b.TracksOfNet(net) {
		if tr.Kind != board.KindVia && !onRoute[tr.UUID] {
			off++
		}
	}
	if off == 0 {
		t.Fatal("expected some of this net's copper to be off the route")
	}

	before := leg.Length
	res, err := tu.Tune(net, 2.4, onRoute)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added <= 0 {
		t.Skip("nothing could be fitted on the route itself")
	}
	var grown netlen.Path
	for _, p := range netlen.New(b).Measure(net).Paths {
		if p.Found && p.From == leg.From && p.To == leg.To {
			grown = p
		}
	}
	if !grown.Found {
		t.Fatal("the leg is no longer measurable")
	}
	if d := (grown.Length - before) - res.Added; d > 1e-4 || d < -1e-4 {
		t.Errorf("reported %.4f mm added, the leg grew %.4f mm", res.Added, grown.Length-before)
	}
	t.Logf("%d tracks on the leg, %d off it; the leg grew %.4f mm", len(leg.Tracks), off, grown.Length-before)
}

// TestTuningRefusesWhenTheRouteHasNoRoom checks the report when the restriction
// leaves nowhere to go: a shortfall with a reason, not silent success and not
// an error.
func TestTuningRefusesWhenTheRouteHasNoRoom(t *testing.T) {
	_, _, tu := fixture(t)
	res, err := tu.Tune("/ddr4/DDR_DQ24", 5, map[string]bool{"not-a-real-uuid": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 0 || res.Shortfall != 5 {
		t.Errorf("got %+v, want no progress", res)
	}
	if len(res.Notes) == 0 || !strings.Contains(res.Notes[0], "route being matched") {
		t.Errorf("notes = %v, expected one about the route", res.Notes)
	}
}
