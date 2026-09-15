package ddr

import (
	"bytes"
	"os"
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/board"
)

// A DDR bus with flat net names ("DDR_DQ0", global labels rather than a
// sheet) has no path to guess a prefix from. It is still DDR: the interface
// detector says so, and the analysis has to agree with it.
func TestScopeFindsDDRWithFlatNetNames(t *testing.T) {
	raw, err := os.ReadFile("../demo-pcb/ai-vision.kicad_pcb")
	if err != nil {
		t.Skipf("demo board missing: %v", err)
	}
	flat, err := board.Parse(bytes.ReplaceAll(raw, []byte(`"/ddr4/`), []byte(`"`)), "flat")
	if err != nil {
		t.Fatal(err)
	}

	prefix, nets := Scope(flat, "")
	if prefix != "" || len(nets) < 60 {
		t.Fatalf("Scope = %q with %d nets; want the detected DDR nets", prefix, len(nets))
	}
	got, err := Classify(flat, Options{Nets: nets})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	want := classify(t)
	if got.Controller != want.Controller || got.Width != want.Width || got.Lanes != want.Lanes ||
		len(got.Signals) != len(want.Signals) {
		t.Errorf("flat names: controller %s, %d bits, %d lanes, %d signals; "+
			"hierarchical: %s, %d, %d, %d", got.Controller, got.Width, got.Lanes, len(got.Signals),
			want.Controller, want.Width, want.Lanes, len(want.Signals))
	}
}

func TestScopePrefersTheGivenAndTheGuessedPrefix(t *testing.T) {
	b := loadBoard(t)
	if p, n := Scope(b, "/x/"); p != "/x/" || n != nil {
		t.Errorf("given prefix: %q %v", p, n)
	}
	if p, n := Scope(b, ""); p != "/ddr4/" || n != nil {
		t.Errorf("guessed prefix: %q %d nets", p, len(n))
	}
}
