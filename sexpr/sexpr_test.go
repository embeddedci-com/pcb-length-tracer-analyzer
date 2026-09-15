package sexpr

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// demoBoard is the real six-layer DDR4 board the whole tool is developed
// against. Round-tripping it is the acceptance test for this package: if a
// 2.4 MB user-owned file does not come back byte for byte, nothing built on
// top of it can be trusted to leave the rest of the design alone.
func demoBoard(t *testing.T) []byte {
	t.Helper()
	p := filepath.Join("..", "demo-pcb", "ai-vision.kicad_pcb")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Skipf("demo board not available: %v", err)
	}
	return b
}

func TestRoundTripDemoBoardIsByteExact(t *testing.T) {
	src := demoBoard(t)
	doc, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Fatalf("round trip differs: %d bytes in, %d out; first diff at %d",
			len(src), len(got), firstDiff(src, got))
	}
}

func firstDiff(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

func TestRoundTripOtherKiCadFiles(t *testing.T) {
	// The .kicad_dru and the schematics are the same dialect, and the tool
	// reads the rules file, so hold them to the same standard.
	for _, name := range []string{"ai-vision.kicad_dru", "ai-vision.kicad_sch", "ddr4.kicad_sch"} {
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("..", "demo-pcb", name))
			if err != nil {
				t.Skipf("not available: %v", err)
			}
			doc, err := Parse(src)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if !bytes.Equal(src, doc.Bytes()) {
				t.Fatalf("round trip differs; first diff at %d", firstDiff(src, doc.Bytes()))
			}
		})
	}
}

func TestParseAccessors(t *testing.T) {
	doc, err := Parse([]byte("(segment\n\t(start 1.5 -2)\n\t(net \"a\\\"b\")\n)"))
	if err != nil {
		t.Fatal(err)
	}
	r := doc.Root()
	if r.Name() != "segment" {
		t.Errorf("Name = %q", r.Name())
	}
	x, ok := r.Child("start").ArgFloat(0)
	if !ok || x != 1.5 {
		t.Errorf("start x = %v %v", x, ok)
	}
	y, ok := r.Child("start").ArgFloat(1)
	if !ok || y != -2 {
		t.Errorf("start y = %v %v", y, ok)
	}
	if got := r.ChildString("net"); got != `a"b` {
		t.Errorf("net = %q, want %q", got, `a"b`)
	}
}

func TestFormatFloatMatchesKiCad(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0"},
		{89.71, "89.71"},
		{-2, "-2"},
		{76.017107, "76.017107"},
		{103.030244, "103.030244"},
		{0.09, "0.09"},
		{0.09000000000000001, "0.09"},
		{1.0, "1"},
		{1.5000004, "1.5"}, // rounds onto the 1 nm grid
		{0.0000004, "0"},   // below 1 nm
		{-0.0000006, "-0.000001"},
		{2.0 / 3.0, "0.666667"},
	}
	for _, c := range cases {
		if got := FormatFloat(c.in); got != c.want {
			t.Errorf("FormatFloat(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDirtyNodeIsReformattedAndCleanSiblingsAreNot(t *testing.T) {
	// A hand-written file with deliberately odd spacing: editing one child must
	// reformat that child only, leaving the siblings' quirky layout alone.
	src := []byte("(kicad_pcb\n\t(a   1    2)\n\t(b 3)\n)")
	doc, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	doc.Root().Child("b").SetFloat(0, 9)
	got := string(doc.Bytes())
	want := "(kicad_pcb\n\t(a   1    2)\n\t(b 9)\n)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuiltSubtreeFormatting(t *testing.T) {
	seg := Sym("segment",
		Sym("start", Float(1), Float(2)),
		Sym("end", Float(3), Float(4)),
		Sym("width", Float(0.09)),
		Sym("layer", String("F.Cu")),
		Sym("net", String("/ddr4/DDR_DQ0")),
	)
	seg.SetDepth(1)
	want := "(segment\n\t\t(start 1 2)\n\t\t(end 3 4)\n\t\t(width 0.09)\n\t\t(layer \"F.Cu\")\n\t\t(net \"/ddr4/DDR_DQ0\")\n\t)"
	if got := string(seg.Bytes()); got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestReplaceSplicesNodes(t *testing.T) {
	doc, _ := Parse([]byte("(root\n\t(x 1)\n\t(y 2)\n\t(z 3)\n)"))
	root := doc.Root()
	// Index 0 is the head symbol "root", so (y 2) is at index 2. Find it rather
	// than hard-coding the offset, which is how callers should do it too.
	root.Replace(root.IndexOf(root.Children("y")[0]), Sym("a"), Sym("b"))
	got := string(doc.Bytes())
	want := "(root\n\t(x 1)\n\t(a)\n\t(b)\n\t(z 3)\n)"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	for _, s := range []string{"", "   ", "abc", "(a", `(a "unterminated`, "(a) b"} {
		if _, err := Parse([]byte(s)); err == nil {
			t.Errorf("Parse(%q) succeeded, want error", s)
		}
	}
}

func TestMultiRootDocument(t *testing.T) {
	// A .kicad_dru is a sequence of top-level forms, not one root.
	src := []byte("(version 1)\n\n(rule \"a\"\n  (constraint clearance (min 0.1mm))\n)\n")
	doc, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Roots) != 2 {
		t.Fatalf("Roots = %d, want 2", len(doc.Roots))
	}
	if doc.Root().Name() != "version" || doc.Roots[1].Name() != "rule" {
		t.Errorf("names = %q %q", doc.Root().Name(), doc.Roots[1].Name())
	}
	if !bytes.Equal(src, doc.Bytes()) {
		t.Errorf("round trip differs: %q", doc.Bytes())
	}
}

func BenchmarkParseDemoBoard(b *testing.B) {
	src, err := os.ReadFile(filepath.Join("..", "demo-pcb", "ai-vision.kicad_pcb"))
	if err != nil {
		b.Skip(err)
	}
	for b.Loop() {
		if _, err := Parse(src); err != nil {
			b.Fatal(err)
		}
	}
}
