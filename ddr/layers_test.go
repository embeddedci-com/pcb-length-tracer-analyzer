package ddr

import (
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/netlen"
)

func planOf(t *testing.T, b *board.Board) *Plan {
	t.Helper()
	iface, err := Classify(b, Options{NetPrefix: "/ddr4/"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := BuildPlan(iface, netlen.New(b), DefaultRules())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// The second demo board follows ST's layer rules, so nothing is reported;
// move a data line to the bottom layer and it is.
func TestLayerFindings(t *testing.T) {
	b, err := board.Load("../demo-pcb-2/ai-vision.kicad_pcb")
	if err != nil {
		t.Skipf("demo board missing: %v", err)
	}
	for _, f := range planOf(t, b).LayerFindings(b) {
		for _, n := range f.Nets {
			t.Logf("%s: %s %v", f.Rule, n.Net, n.ByLayer)
		}
	}
	before := len(planOf(t, b).LayerFindings(b))

	// Relabelled after measuring: moved for real, the route would no longer
	// reach its top-layer pads and the net would not be measured at all.
	const net = "/ddr4/DDR_DQ5"
	bottom := b.CopperLayers[len(b.CopperLayers)-1]
	p := planOf(t, b)
	for _, tr := range b.TracksOfNet(net) {
		if tr.Kind != board.KindVia {
			tr.Layer = bottom
		}
	}
	var found *NetLayers
	for _, f := range p.LayerFindings(b) {
		for i := range f.Nets {
			if f.Rule == "data-top" && f.Nets[i].Net == net {
				found = &f.Nets[i]
			}
		}
	}
	if before != 0 {
		t.Errorf("%d layer findings on a board that follows the rules", before)
	}
	if found == nil || found.Share > 0.01 || found.ByLayer[bottom] <= 0 {
		t.Fatalf("DQ5 on %s not reported: %+v", bottom, found)
	}

	// An address line routed all on the top layer is reported too.
	const a0 = "/ddr4/DDR_A0"
	for _, tr := range b.TracksOfNet(a0) {
		if tr.Kind != board.KindVia {
			tr.Layer = b.CopperLayers[0]
		}
	}
	var addr *NetLayers
	for _, f := range p.LayerFindings(b) {
		for i := range f.Nets {
			if f.Rule == "address-bottom" && f.Nets[i].Net == a0 {
				addr = &f.Nets[i]
			}
		}
	}
	if addr == nil || addr.Share != 0 {
		t.Errorf("A0 on the top layer not reported: %+v", addr)
	}
}
