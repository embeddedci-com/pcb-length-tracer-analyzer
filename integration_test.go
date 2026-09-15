//go:build integration

// Package main's integration test runs the whole flow against KiCad itself.
//
// Everything else in this repository checks the tool against its own model of a
// board. This checks it against pcbnew: the edited board is handed to
// kicad-cli, and two things have to hold. Its design rule check must report no
// violation it did not report before -- custom .kicad_dru rules included, which
// this tool deliberately does not interpret -- and its own measurement of every
// net must have grown by exactly what the tool said it added.
//
// It is behind a build tag because it needs kicad-cli installed and takes about
// half a minute:
//
//	go test -tags integration ./...
package main

import (
	"bytes"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/geom"
)

func requireKicad(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("kicad-cli"); err != nil {
		t.Skip("kicad-cli not on PATH")
	}
}

// stageBoard copies the demo board and the files DRC needs beside it into a
// temporary directory, so nothing here can touch the user's design.
// boards are the fixtures the end-to-end checks run over.
//
// Two of them, and the difference is the point. The first is a board still
// being laid out -- 44 of its 71 DDR nets joined end to end, no second fly-by
// hop anywhere -- which is what the tool says about unfinished work. The second
// is the same board further on: 69 of 71 routed, the chain complete on 25 of 26
// nets, and a second leg to match for the first time. A tool that behaves on
// one and not the other has been tested on one.
var boards = map[string]string{
	"part-routed": "demo-pcb",
	"further-on":  "demo-pcb-2",
}

func stageBoard(t *testing.T) (dir, board string) { return stageFrom(t, "demo-pcb") }

func stageFrom(t *testing.T, from string) (dir, board string) {
	t.Helper()
	dir = t.TempDir()
	src := filepath.Join(from, "ai-vision.kicad_pcb")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("board missing: %v", err)
	}
	for _, ext := range []string{"kicad_pcb", "kicad_pro", "kicad_dru"} {
		b, err := os.ReadFile(filepath.Join(from, "ai-vision."+ext))
		if err != nil {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, "before."+ext), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, filepath.Join(dir, "before.kicad_pcb")
}

func TestEndToEndAgainstKiCad(t *testing.T) {
	requireKicad(t)
	dir, before := stageBoard(t)
	after := filepath.Join(dir, "after.kicad_pcb")

	bin := filepath.Join(dir, "pcb-trace-length-analyzer")
	build := exec.Command("go", "build", "-o", bin, "./cmd/pcb-trace-length-analyzer")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("building the tool: %v", err)
	}

	var out bytes.Buffer
	cmd := exec.Command(bin, "-apply", "-yes", "-out", after, before)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("tuning failed: %v\n%s", err, out.String())
	}
	t.Logf("tool output tail:\n%s", tail(out.String(), 12))
	if !strings.Contains(out.String(), "Written to") {
		t.Fatalf("nothing was written:\n%s", out.String())
	}

	// The project and rules files have to sit beside the output too.
	for _, ext := range []string{"kicad_pro", "kicad_dru"} {
		b, err := os.ReadFile(filepath.Join(dir, "before."+ext))
		if err != nil {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, "after."+ext), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	verify := exec.Command("./scripts/verify-with-kicad.sh", before, after)
	var vout bytes.Buffer
	verify.Stdout = &vout
	verify.Stderr = &vout
	err := verify.Run()
	t.Logf("verification:\n%s", vout.String())
	if err != nil {
		t.Fatalf("KiCad disagrees: %v", err)
	}

	// And the lengths KiCad measured must be the ones the tool reported.
	reported := parseAdded(t, out.String())
	measured := parseMeasured(t, vout.String())
	if len(reported) == 0 {
		t.Fatal("the tool reported adding nothing")
	}
	for net, want := range reported {
		got, ok := measured[net]
		if !ok {
			if want > 1e-4 {
				t.Errorf("%s: tool added %.4f mm but KiCad sees no change", net, want)
			}
			continue
		}
		if diff := got - want; diff > 1e-3 || diff < -1e-3 {
			t.Errorf("%s: tool reported %.4f mm added, KiCad measures %.4f mm", net, want, got)
		}
	}
	for net, got := range measured {
		if _, ok := reported[net]; !ok {
			t.Errorf("%s changed by %.4f mm but the tool never reported touching it", net, got)
		}
	}
	t.Logf("cross-checked %d net length changes against pcbnew", len(measured))
}

// TestReportOnlyWritesNothing checks the default: reading a board must never
// change it.
func TestReportOnlyWritesNothing(t *testing.T) {
	dir, before := stageBoard(t)
	orig, err := os.ReadFile(before)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "pcb-trace-length-analyzer")
	if err := exec.Command("go", "build", "-o", bin, "./cmd/pcb-trace-length-analyzer").Run(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cmd := exec.Command(bin, before)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	now, err := os.ReadFile(before)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(orig, now) {
		t.Error("reporting modified the board")
	}
	if !strings.Contains(out.String(), "Nothing was written") {
		t.Errorf("expected the report to say it wrote nothing:\n%s", tail(out.String(), 6))
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), "tuned") {
			t.Errorf("a report-only run created %s", e.Name())
		}
	}
}

// TestDeclinedConfirmationWritesNothing checks that answering the prompt with
// anything but yes leaves the board alone.
func TestDeclinedConfirmationWritesNothing(t *testing.T) {
	dir, before := stageBoard(t)
	bin := filepath.Join(dir, "pcb-trace-length-analyzer")
	if err := exec.Command("go", "build", "-o", bin, "./cmd/pcb-trace-length-analyzer").Run(); err != nil {
		t.Fatal(err)
	}
	for _, answer := range []string{"n\n", "\n", "no\n", "maybe\n"} {
		out := filepath.Join(dir, "declined.kicad_pcb")
		os.Remove(out)
		cmd := exec.Command(bin, "-apply", "-out", out, before)
		cmd.Stdin = strings.NewReader(answer)
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		if err := cmd.Run(); err != nil {
			t.Fatalf("answer %q: %v\n%s", answer, err, buf.String())
		}
		if !strings.Contains(buf.String(), "Cancelled") {
			t.Errorf("answer %q: expected a cancellation:\n%s", answer, tail(buf.String(), 5))
		}
		if _, err := os.Stat(out); err == nil {
			t.Errorf("answer %q: the board was written anyway", answer)
		}
	}
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// parseAdded reads the per-net "added mm" column out of the tool's own report.
func parseAdded(t *testing.T, out string) map[string]float64 {
	t.Helper()
	res := map[string]float64{}
	in := false
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if strings.Contains(line, "asked mm") {
			in = true
			continue
		}
		if in && strings.Contains(line, "net(s) met") {
			break
		}
		if !in || len(f) < 5 {
			continue
		}
		v, err := parseFloat(f[2])
		if err != nil || v <= 1e-4 {
			continue
		}
		res["/ddr4/DDR_"+f[0]] = v
	}
	return res
}

// parseMeasured reads the per-net deltas out of the verification script.
func parseMeasured(t *testing.T, out string) map[string]float64 {
	t.Helper()
	res := map[string]float64{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) != 5 || f[2] != "->" {
			continue
		}
		v, err := parseFloat(strings.TrimPrefix(f[4], "+"))
		if err != nil {
			continue
		}
		res["/ddr4/DDR_"+f[0]] = v
	}
	return res
}

func parseFloat(s string) (float64, error) { return strconv.ParseFloat(s, 64) }

// TestNoNetPairEndsUpTooClose is one half of the geometric safety check.
//
// KiCad cannot answer "did anything end up too close": its choice of which item
// to name in a violation is not stable across an edit, and its violation count
// is a count of item pairs that a track split multiplies. This measures both
// boards directly instead. The other half is TestNoNetPairRunsCloserForLonger.
//
// The rule is not that nothing may get closer: folding a meander into a track
// moves copper around, and moving it closer to something while staying inside
// the clearance is exactly what it is allowed to do. The rule is that no pair of
// nets may end up closer than their clearance requires, and that where a pair
// was already too close -- this board has 687 violations -- it may not get
// tighter.
// modes are the ways of editing the board that these checks have to hold for.
//
// Spreading a bus is the one that matters most here. It moves copper the user
// did not select -- every trace in the bus, not only the ones being lengthened
// -- so it is where a clearance regression would do the most damage and be the
// least expected. Both checks run over both.
var modes = map[string][]string{
	"tuning only":             nil,
	"spreading buses as well": {"-expand"},
}

// applyTo runs the tool over a staged copy of the board and returns the paths.
func applyTo(t *testing.T, from string, extra []string) (beforePath, afterPath string, proj *board.Project) {
	t.Helper()
	dir, beforePath := stageFrom(t, from)
	afterPath = filepath.Join(dir, "after.kicad_pcb")

	bin := filepath.Join(dir, "pcb-trace-length-analyzer")
	if err := exec.Command("go", "build", "-o", bin, "./cmd/pcb-trace-length-analyzer").Run(); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"-apply", "-yes", "-out", afterPath}, extra...)
	var out bytes.Buffer
	cmd := exec.Command(bin, append(args, beforePath)...)
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	proj, err := board.LoadProject(beforePath)
	if err != nil {
		t.Fatal(err)
	}
	return beforePath, afterPath, proj
}

func TestNoNetPairEndsUpTooClose(t *testing.T) {
	for board, dir := range boards {
		for name, extra := range modes {
			t.Run(board+"/"+name, func(t *testing.T) { noNetPairEndsUpTooClose(t, dir, extra) })
		}
	}
}

func noNetPairEndsUpTooClose(t *testing.T, from string, extra []string) {
	beforePath, afterPath, proj := applyTo(t, from, extra)
	before := closestApproaches(t, beforePath)
	after := closestApproaches(t, afterPath)

	// A pair absent from a map is further apart than the observation window,
	// which is wider than any clearance this board asks for.
	const window = 1.0
	distBefore := func(p [2]string) float64 {
		if d, ok := before[p]; ok {
			return d
		}
		return window
	}

	bad, closer := 0, 0
	for pair, now := range after {
		required := proj.ClearanceBetween(pair[0], pair[1])
		was := distBefore(pair)
		if now < was-1e-6 {
			closer++
		}
		if now >= required-1e-6 {
			continue // inside the rules, whatever it was before
		}
		if was >= required-1e-6 {
			bad++
			t.Errorf("%s and %s are now %.4f mm apart, inside the %.4f mm they require (was %.4f)",
				pair[0], pair[1], now, required, was)
			continue
		}
		if now < was-1e-6 {
			bad++
			t.Errorf("%s and %s were already too close and got tighter: %.4f -> %.4f mm (require %.4f)",
				pair[0], pair[1], was, now, required)
		}
	}
	if bad != 0 {
		t.Fatalf("%d net pair(s) end up too close", bad)
	}
	t.Logf("checked %d net pairs: none is inside its clearance; %d moved closer while staying legal",
		len(after), closer)
}

// TestNoNetPairRunsCloserForLonger is the strongest statement available about
// whether the board got worse, and the one that survives an edit.
//
// A violation count is a count of item pairs, not of problems. Inserting a
// meander splits one long track into several, so KiCad reports up to three
// entries where it reported one -- at the same distance, with no copper having
// moved. On this board that took the count from 687 to 688 while the offending
// stretch of DDR_A11 beside DDR_CLK_N actually got shorter, 12.153 mm to
// 8.853 mm.
//
// What does mean something is how much copper runs too close. For every pair of
// nets, the total length of one net's tracks sitting inside the clearance they
// require must not increase. That covers both the case the minimum-distance
// check catches and the case it does not: copper that was already too close
// being extended.
func TestNoNetPairRunsCloserForLonger(t *testing.T) {
	for board, dir := range boards {
		for name, extra := range modes {
			t.Run(board+"/"+name, func(t *testing.T) { noNetPairRunsCloserForLonger(t, dir, extra) })
		}
	}
}

func noNetPairRunsCloserForLonger(t *testing.T, from string, extra []string) {
	beforePath, afterPath, proj := applyTo(t, from, extra)
	before, err := board.Load(beforePath)
	if err != nil {
		t.Fatal(err)
	}
	after, err := board.Load(afterPath)
	if err != nil {
		t.Fatal(err)
	}

	// Only the nets the tool could have touched, and their neighbours: a pair
	// neither of whose nets changed cannot have moved.
	changed := changedNets(before, after)
	if len(changed) == 0 {
		t.Fatal("no net changed, so this proves nothing")
	}

	nets := before.Nets()
	worse, checked := 0, 0
	for _, a := range changed {
		for _, b := range nets {
			if a == b {
				continue
			}
			req := proj.ClearanceBetween(a, b)
			was := tooCloseLength(before, a, b, req)
			now := tooCloseLength(after, a, b, req)
			checked++
			// A micrometre of slack: the meander's own points are snapped to
			// KiCad's nanometre grid.
			if now > was+1e-3 {
				worse++
				if worse <= 8 {
					t.Errorf("%s beside %s: copper inside %.3f mm went from %.3f to %.3f mm",
						a, b, req, was, now)
				}
			}
		}
	}
	if worse != 0 {
		t.Fatalf("%d net pairs run too close for longer than they did", worse)
	}
	t.Logf("checked %d net pairs against %d changed nets; none runs too close for longer",
		checked, len(changed))
}

// changedNets lists the nets whose copper differs between two boards.
func changedNets(before, after *board.Board) []string {
	total := func(b *board.Board, net string) float64 {
		var l float64
		for _, tr := range b.TracksOfNet(net) {
			l += tr.Length(b.Stackup)
		}
		return l
	}
	var out []string
	for _, net := range after.Nets() {
		if math.Abs(total(after, net)-total(before, net)) > 1e-6 {
			out = append(out, net)
		}
	}
	return out
}

// tooCloseLength is the total length of net a's tracks that run within req of
// any of net b's copper, on a shared layer.
func tooCloseLength(b *board.Board, na, nb string, req float64) float64 {
	var total float64
	for _, a := range b.TracksOfNet(na) {
		if a.Kind == board.KindVia {
			continue
		}
		for _, c := range b.TracksOfNet(nb) {
			if c.Kind == board.KindVia || c.Layer != a.Layer {
				continue
			}
			if !a.Box().Grow(req + a.Width).Overlaps(c.Box().Grow(c.Width)) {
				continue
			}
			hit := false
			for _, sa := range a.Shape(b.MaxError) {
				for _, sc := range c.Shape(b.MaxError) {
					if sa.Dist(sc) < req {
						hit = true
						break
					}
				}
				if hit {
					break
				}
			}
			if hit {
				total += a.Length(b.Stackup)
				break
			}
		}
	}
	return total
}

// closestApproaches measures, for every pair of nets whose copper comes within
// a millimetre of each other, how close they get.
//
// A millimetre cut-off keeps this to the pairs that could matter: the tightest
// clearance on this board is 0.1 mm and the loosest rule asks 0.5 mm.
func closestApproaches(t *testing.T, path string) map[[2]string]float64 {
	t.Helper()
	b, err := board.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	const window = 1.0

	// Bucket copper by layer and a coarse grid, so this does not become a
	// comparison of every shape against every other.
	type cell struct {
		layer string
		x, y  int
	}
	type item struct {
		net   string
		shape geom.RoundPoly
	}
	const step = 2.0
	grid := map[cell][]item{}
	addShape := func(net, layer string, sh geom.RoundPoly) {
		box := sh.Box().Grow(window)
		for x := int(math.Floor(box.MinX / step)); x <= int(math.Floor(box.MaxX/step)); x++ {
			for y := int(math.Floor(box.MinY / step)); y <= int(math.Floor(box.MaxY/step)); y++ {
				grid[cell{layer, x, y}] = append(grid[cell{layer, x, y}], item{net, sh})
			}
		}
	}
	for _, tr := range b.Tracks {
		for _, sh := range tr.Shape(b.MaxError) {
			for _, l := range tr.Layers(b.Stackup) {
				addShape(tr.Net, l, sh)
			}
		}
	}
	for _, p := range b.Pads {
		if p.Net == "" {
			continue
		}
		for _, l := range b.CopperLayers {
			if p.OnLayer(l) {
				addShape(p.Net, l, p.Shape)
			}
		}
	}

	out := map[[2]string]float64{}
	for _, items := range grid {
		for i := range items {
			for j := i + 1; j < len(items); j++ {
				a, c := items[i], items[j]
				if a.net == c.net {
					continue
				}
				d := a.shape.Dist(c.shape)
				if d > window {
					continue
				}
				key := [2]string{a.net, c.net}
				if key[0] > key[1] {
					key[0], key[1] = key[1], key[0]
				}
				if cur, ok := out[key]; !ok || d < cur {
					out[key] = d
				}
			}
		}
	}
	return out
}
