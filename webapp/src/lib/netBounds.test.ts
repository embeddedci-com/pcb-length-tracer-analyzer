import { describe, expect, it } from 'vitest'
import type { BoardDoc } from '@emi/lib/boardTypes'
import { centre, drawnNets, netBounds, zoomFor } from './netBounds'

// Two triangles: net A near the origin, net B further out, on two layers so the
// across-layers case is covered too.
function fixture(): { doc: BoardDoc; geometry: ArrayBuffer } {
  const verts = new Float32Array([
    // A on F.Cu: a triangle from (1,1) to (3,2)
    1, 1, 3, 1, 3, 2,
    // B on F.Cu: (10,10) to (12,11)
    10, 10, 12, 10, 12, 11,
    // A again, on B.Cu: (0,5) to (1,6) -- the same net, another layer
    0, 5, 1, 5, 1, 6,
  ])
  const doc = {
    geometry: {
      file: 'geometry.bin',
      dtype: 'float32',
      components: 2,
      primitive: 'triangles',
      vertex_count: 9,
      byte_length: verts.byteLength,
      groups: [
        { layer: 'F.Cu', net: 'A', offset: 0, count: 3 },
        { layer: 'F.Cu', net: 'B', offset: 3, count: 3 },
        { layer: 'B.Cu', net: 'A', offset: 6, count: 3 },
      ],
    },
  } as unknown as BoardDoc
  return { doc, geometry: verts.buffer }
}

describe('netBounds', () => {
  it('covers every layer the net is on', () => {
    const { doc, geometry } = fixture()
    expect(netBounds(doc, geometry, 'A')).toEqual({ minX: 0, minY: 1, maxX: 3, maxY: 6 })
  })

  it('reads only the net asked for', () => {
    const { doc, geometry } = fixture()
    expect(netBounds(doc, geometry, 'B')).toEqual({ minX: 10, minY: 10, maxX: 12, maxY: 11 })
  })

  it('is null for a net with no copper drawn', () => {
    const { doc, geometry } = fixture()
    expect(netBounds(doc, geometry, 'GND')).toBeNull()
  })

  it('centres on the middle of the extent', () => {
    expect(centre({ minX: 0, minY: 2, maxX: 4, maxY: 4 })).toEqual({ x: 2, y: 3 })
  })

  it('lists the nets that have something to show', () => {
    const { doc } = fixture()
    expect([...drawnNets(doc)].sort()).toEqual(['A', 'B'])
  })
})

describe('zoomFor', () => {
  const viewport = { width: 800, height: 600 }

  it('fits the extent with margin, not tight', () => {
    // 20 mm wide plus 3 mm each side is 26 mm across 800 px.
    const z = zoomFor({ minX: 0, minY: 0, maxX: 20, maxY: 2 }, viewport)
    expect(z).toBeCloseTo(800 / 26, 6)
  })

  it('is bounded by the tighter axis', () => {
    const z = zoomFor({ minX: 0, minY: 0, maxX: 2, maxY: 50 }, viewport)
    expect(z).toBeCloseTo(600 / 56, 6)
  })

  it('will not magnify a stub past recognition', () => {
    const z = zoomFor({ minX: 0, minY: 0, maxX: 0.5, maxY: 0.5 }, viewport)
    expect(z).toBe(60)
  })

  it('survives a viewport that has not been laid out yet', () => {
    expect(zoomFor({ minX: 0, minY: 0, maxX: 10, maxY: 10 }, { width: 0, height: 0 })).toBe(1)
  })
})
