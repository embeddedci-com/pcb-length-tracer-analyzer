/**
 * Where a net is on the board.
 *
 * board.json says which run of vertices belongs to which net but not where
 * that net sits, so "show me DQ23" has to be answered from the geometry
 * itself. That is cheap -- a net's copper is a few thousand floats out of a
 * couple of million -- and it is exact, which a bounding box computed on the
 * server from the pre-tuning board would not be.
 */

import type { BoardDoc } from '@emi/lib/boardTypes'

export interface Bounds {
  minX: number
  minY: number
  maxX: number
  maxY: number
}

export function centre(b: Bounds): { x: number; y: number } {
  return { x: (b.minX + b.maxX) / 2, y: (b.minY + b.maxY) / 2 }
}

/**
 * The extent of one net's copper, or null when the net has none drawn.
 *
 * Layers are not separated: a net that changes layer through a via is one
 * object to look at, and framing only the part on the top layer would put the
 * rest off screen.
 */
export function netBounds(doc: BoardDoc, geometry: ArrayBuffer, net: string): Bounds | null {
  const verts = new Float32Array(geometry)
  let minX = Infinity
  let minY = Infinity
  let maxX = -Infinity
  let maxY = -Infinity
  let seen = false
  for (const group of doc.geometry.groups) {
    if (group.net !== net) continue
    const end = (group.offset + group.count) * 2
    for (let i = group.offset * 2; i + 1 < end && i + 1 < verts.length; i += 2) {
      const x = verts[i]
      const y = verts[i + 1]
      if (x < minX) minX = x
      if (y < minY) minY = y
      if (x > maxX) maxX = x
      if (y > maxY) maxY = y
      seen = true
    }
  }
  return seen ? { minX, minY, maxX, maxY } : null
}

/**
 * A zoom that puts a net's extent on screen with room around it.
 *
 * A net is usually long and thin, so fitting it exactly would fill the view
 * with one trace and no context: what the user is looking for is the trace
 * *and* what it runs beside. Hence the margin, and hence the cap -- zooming to
 * fit a 2 mm stub would otherwise land at a magnification where nothing is
 * recognisable.
 */
export function zoomFor(
  b: Bounds,
  viewport: { width: number; height: number },
  opts: { margin?: number; max?: number } = {},
): number {
  const margin = opts.margin ?? 3
  const max = opts.max ?? 60
  const w = Math.max(b.maxX - b.minX, 1) + 2 * margin
  const h = Math.max(b.maxY - b.minY, 1) + 2 * margin
  if (viewport.width <= 0 || viewport.height <= 0) return 1
  return Math.min(viewport.width / w, viewport.height / h, max)
}

/** Nets in the document that carry copper, worst-first order preserved. */
export function drawnNets(doc: BoardDoc): Set<string> {
  const out = new Set<string>()
  for (const g of doc.geometry.groups) out.add(g.net)
  return out
}
