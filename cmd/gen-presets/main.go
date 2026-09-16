// Command gen-presets writes the preset catalogue the front end embeds.
//
//	go run ./cmd/gen-presets [path]
//
// Run by `make presets`. server/presets_ts_test.go fails when the file on disk
// is not what this would write.
package main

import (
	"fmt"
	"os"

	"github.com/embeddedci-com/pcb-autorouter/server"
)

const defaultPath = "webapp/src/lib/presets.generated.ts"

func main() {
	path := defaultPath
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	body, err := server.PresetCatalogTS()
	if err == nil {
		err = os.WriteFile(path, body, 0o644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "gen-presets:", err)
		os.Exit(1)
	}
	fmt.Println("wrote", path)
}
