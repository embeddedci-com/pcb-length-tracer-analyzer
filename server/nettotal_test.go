package server

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/board"
)

// A fly-by leg is part of a net, but KiCad shows the whole net. Each leg row
// carries the whole-net length, and what it lands on once every leg is matched.
func TestLegRowsCarryTheWholeNetLength(t *testing.T) {
	path := filepath.Join("../demo-pcb-2", "ai-vision.kicad_pcb")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skip(err)
	}
	b, err := board.Parse(raw, path)
	if err != nil {
		t.Fatal(err)
	}
	proj, _ := board.LoadProject(path)
	a, _, err := analyse(b, proj, "ai-vision.kicad_pcb", Params{}.withDefaults(), nil)
	if err != nil {
		t.Fatal(err)
	}
	change := map[string]float64{}
	var legs, whole int
	for _, g := range a.Groups {
		for _, m := range g.Members {
			if m.Routed && !m.InTolerance && !m.Reference {
				change[m.Net] += m.NeedMM - m.ExcessMM
			}
		}
	}
	for _, g := range a.Groups {
		for _, m := range g.Members {
			if !m.Routed {
				continue
			}
			if m.NetTotalMM == 0 {
				if g.Leg != "" {
					t.Errorf("%s in %s: a fly-by leg without a net total", m.Label, g.Name)
				}
				whole++
				continue
			}
			legs++
			if want := m.NetTotalMM + change[m.Net]; abs(m.NetAimMM-want) > 1e-6 {
				t.Errorf("%s in %s: aim %.3f, want %.3f", m.Label, g.Name, m.NetAimMM, want)
			}
		}
	}
	if legs == 0 {
		t.Fatal("no member is part of a longer net; the fly-by legs should be")
	}
	t.Logf("%d leg rows, %d whole-net rows", legs, whole)
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// A package length typed in for one net changes that net's lengths by the
// difference, and the page is told the default it replaced.
func TestAPackageLengthOverrideMovesThatNetOnly(t *testing.T) {
	path := filepath.Join("../demo-pcb-2", "ai-vision.kicad_pcb")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skip(err)
	}
	run := func(over map[string]float64) *Analysis {
		b, err := board.Parse(raw, path)
		if err != nil {
			t.Fatal(err)
		}
		proj, _ := board.LoadProject(path)
		p := DefaultParams()
		p.PackageLengthsMM = over
		a, _, err := analyse(b, proj, "ai-vision.kicad_pcb", p, nil)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	const net = "/ddr4/DDR_DQ3"
	before := run(nil)
	after := run(map[string]float64{net: 1})

	var row *struct{ def, mm float64 }
	for _, pad := range after.PackagePads {
		if pad.Net == net {
			row = &struct{ def, mm float64 }{pad.DefaultMM, pad.MM}
			if pad.Source != "override" {
				t.Errorf("source %q", pad.Source)
			}
		}
	}
	if row == nil || row.mm != 1 || row.def < 9 {
		t.Fatalf("DQ3's package pad: %+v", row)
	}
	lengths := func(a *Analysis) map[string]float64 {
		out := map[string]float64{}
		for _, g := range a.Groups {
			for _, m := range g.Members {
				out[g.Name+" "+m.Net] = m.LengthMM
			}
		}
		return out
	}
	was, now := lengths(before), lengths(after)
	for k, l := range was {
		d := now[k] - l
		want := 0.0
		if strings.HasSuffix(k, " "+net) {
			want = 1 - row.def
		}
		if math.Abs(d-want) > 1e-6 {
			t.Errorf("%s moved %.3f mm, want %.3f", k, d, want)
		}
	}
}

// RESETN is on the controller but not length matched, so it is not a pad
// missing a package length.
func TestPackagePadsAreTheMatchedNetsOnly(t *testing.T) {
	path := filepath.Join("../demo-pcb-2", "ai-vision.kicad_pcb")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skip(err)
	}
	b, err := board.Parse(raw, path)
	if err != nil {
		t.Fatal(err)
	}
	proj, _ := board.LoadProject(path)
	a, _, err := analyse(b, proj, "ai-vision.kicad_pcb", DefaultParams(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range a.PackagePads {
		if strings.HasSuffix(p.Net, "RESETN") {
			t.Errorf("RESETN listed: %+v", p)
		}
		if p.MM <= 0 {
			t.Errorf("%s has no package length", p.Net)
		}
	}
	if len(a.PackagePads) < 60 {
		t.Errorf("only %d pads", len(a.PackagePads))
	}
}
