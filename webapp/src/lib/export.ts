/**
 * Taking the analysis away with you.
 *
 * An analysis that only exists on a page is one somebody has to screenshot into
 * a review. Two shapes, because two things are done with it: the table of nets,
 * as CSV, for a spreadsheet or a mail to whoever is doing the routing; and the
 * whole analysis as JSON, which is exactly what the API returned and is what a
 * script would want.
 *
 * Lengths in the CSV are plain numbers in millimetres, whatever the page is
 * displaying. A spreadsheet column with "0.635 mm" in it is text, and a reader
 * who wants mils can convert a column; a reader who wants to sum a column
 * cannot un-format one.
 */

import type { Analysis, HeadroomResponse } from './analyzerApi'
import { buildBrief } from './format'

/** One row per span of one net: what it is, what it needs, and what to do. */
export function candidatesCSV(
  analysis: Analysis,
  headroom: HeadroomResponse | null,
  selected?: readonly string[],
): string {
  const brief = buildBrief(analysis, headroom, selected)
  const head = [
    'net',
    'group',
    'leg',
    'length_mm',
    'need_mm',
    'target_mm',
    'room_mm',
    'verdict',
    'run_needed_mm',
    'space_needed_mm2',
  ]
  const rows = brief.rows.map((r) => [
    r.net,
    r.group,
    r.leg ?? '',
    r.length_mm,
    r.need_mm,
    r.target_mm,
    r.headroom_mm,
    r.excess_mm > 0
      ? 'too long'
      : r.needs_reroute
        ? 'reroute'
        : r.headroom_mm + 1e-6 >= r.need_mm
          ? 'fits'
          : 'needs room',
    r.run_needed_mm ?? '',
    r.space_needed_mm2 ?? '',
  ])
  return [head, ...rows].map((r) => r.map(cell).join(',')).join('\n')
}

/** The nets that are not joined end to end, and where they belong. */
export function missingCSV(
  analysis: Analysis,
  selected?: readonly string[],
): string {
  const brief = buildBrief(analysis, null, selected)
  const rows = brief.missing.flatMap((m) =>
    m.nets.map((n) => [n, m.where, m.hop ? 'fly-by span' : 'interface']),
  )
  return [['net', 'where', 'kind'], ...rows].map((r) => r.map(cell).join(',')).join('\n')
}

function cell(v: string | number): string {
  if (typeof v === 'number') return String(Math.round(v * 1e6) / 1e6)
  return /[",\n]/.test(v) ? `"${v.replace(/"/g, '""')}"` : v
}

/**
 * Hand a file to the browser.
 *
 * A blob and a click, which is the only way to produce a file the user keeps
 * without a round trip to a server that already gave us everything in it.
 */
export function download(name: string, body: string | Blob, type: string): void {
  const url = URL.createObjectURL(typeof body === 'string' ? new Blob([body], { type }) : body)
  const a = document.createElement('a')
  a.href = url
  a.download = name
  document.body.append(a)
  a.click()
  a.remove()
  // Revoked on the next tick: Safari needs the element gone first.
  setTimeout(() => URL.revokeObjectURL(url), 0)
}

/** A filename that says which board and which day, without a colon in it. */
export function exportName(filename: string, what: string, ext: string): string {
  const base = filename.replace(/\.kicad_pcb$/i, '') || 'board'
  const day = new Date().toISOString().slice(0, 10)
  return `${base}-${what}-${day}.${ext}`
}
