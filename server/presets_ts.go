package server

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// The preset catalogue as the front end's own source file.
//
// The home page lists the chips the tool has rules for, and that page is
// rendered to static HTML at build time with the API blocked, so a list
// fetched from /defaults would be missing from the file a search engine or a
// reader with a slow connection sees first. Rather than keep a second,
// hand-maintained copy of the numbers in TypeScript, the file is generated
// from these presets and a test fails when it falls behind.
//
// The form still takes its presets from the API: there the numbers are being
// applied to a board, and the engine that will apply them is the authority.

// PresetCatalogTS is the generated TypeScript file, byte for byte.
func PresetCatalogTS() ([]byte, error) {
	body, err := json.MarshalIndent(KnownPresets(), "", "  ")
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	fmt.Fprint(&out, `/**
 * The vendors' published DDR rules, for the list of supported chips.
 *
 * Generated from ddr/presets.go by cmd/gen-presets; run `+"`make presets`"+` after
 * changing a preset. server/presets_ts_test.go fails while this is stale.
 *
 * The rules form does not use this: it reads /defaults, so the numbers it
 * applies are the ones the engine will judge the board by. This copy exists so
 * the catalogue is in the statically rendered page.
 */

import type { Preset } from './analyzerApi'

export const PRESET_CATALOG: Preset[] = `)
	out.Write(body)
	out.WriteString("\n")
	return out.Bytes(), nil
}
