package server

import (
	"os"
	"path/filepath"
	"testing"
)

// The front end embeds the catalogue so the home page lists the chips before
// any request is made. Two copies of the same numbers are only safe while
// something checks they agree, and this is that check.
func TestGeneratedPresetCatalogIsCurrent(t *testing.T) {
	path := filepath.Join("..", "webapp", "src", "lib", "presets.generated.ts")
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	want, err := PresetCatalogTS()
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != string(want) {
		t.Errorf("%s is out of date with ddr/presets.go; run `make presets`", path)
	}
}
