import { describe, expect, it } from 'vitest'
import { candidatesCSV, exportName, missingCSV } from './export'
import type { Analysis, HeadroomResponse, MemberInfo } from './analyzerApi'

// An analysis that only exists on a page is one somebody has to screenshot
// into a review. These check the two things a file has to get right: the
// numbers are numbers a spreadsheet can add up, and the verdict is the same
// one the page showed.

function candidate(over: Partial<MemberInfo> = {}): MemberInfo {
  return {
    net: '/ddr4/DDR_A11',
    label: 'A11',
    role: 'address',
    routed: true,
    length_mm: 16.888,
    delay_ps: 0,
    deviation_mm: -14.251,
    need_mm: 14.251,
    need_ps: 0,
    in_tolerance: false,
    headroom_mm: 0,
    needs_reroute: true,
    legs: [
      {
        group: 'address/command U3->U4',
        leg: 'U3->U4',
        length_mm: 16.888,
        need_mm: 14.251,
        headroom_mm: 0,
        needs_reroute: true,
      },
    ],
    ...over,
  }
}

const analysis = {
  board: { filename: 'ai-vision.kicad_pcb', tracks: 10 },
  interface: {},
  routing: {
    complete: 69,
    incomplete: 2,
    missing: [{ net: '/ddr4/DDR_CKE', label: 'CKE', from: 'U4.K2', to: 'U5.K2', hop: 'U4 -> U5' }],
  },
  groups: [],
  candidates: [candidate()],
  total_need_mm: 14.251,
  gettable_mm: 0,
  interfaces: [],
} as unknown as Analysis

describe('export', () => {
  it('writes lengths as numbers, not as text with a unit on it', () => {
    const csv = candidatesCSV(analysis, null)
    const [head, row] = csv.split('\n')
    expect(head.split(',')).toContain('length_mm')
    const cells = row.split(',')
    expect(cells[0]).toBe('/ddr4/DDR_A11')
    expect(Number(cells[3])).toBeCloseTo(16.888, 6)
    // The target, worked out so the reader does not have to: length + need.
    expect(Number(cells[5])).toBeCloseTo(31.139, 6)
  })

  it('carries the same verdict the page shows', () => {
    expect(candidatesCSV(analysis, null)).toContain('reroute')
    const roomy = {
      ...analysis,
      candidates: [candidate({ need_mm: 1, headroom_mm: 5, needs_reroute: false,
        legs: [{ group: 'g', length_mm: 20, need_mm: 1, headroom_mm: 5, needs_reroute: false }] })],
    }
    expect(candidatesCSV(roomy, null)).toContain('fits')
  })

  it('lists what is not joined up, with where it belongs', () => {
    const csv = missingCSV(analysis)
    expect(csv).toContain('CKE,U4 -> U5,fly-by span')
  })

  it('quotes a field that would otherwise split the row', () => {
    const odd = {
      ...analysis,
      routing: {
        ...analysis.routing,
        missing: [{ net: 'x', label: 'a,b', from: '', to: '', hop: 'U4 -> U5' }],
      },
    } as unknown as Analysis
    expect(missingCSV(odd)).toContain('"a,b"')
  })

  it('names the file after the board and the day', () => {
    const name = exportName('ai-vision.kicad_pcb', 'candidates', 'csv')
    expect(name).toMatch(/^ai-vision-candidates-\d{4}-\d{2}-\d{2}\.csv$/)
  })

  it('takes the room measurement when there is one, per span', () => {
    // Per span, because that is where the room is: the figure on the net is a
    // sum of spans and would not tell a reader which one has the space.
    const measured = {
      candidates: [
        candidate({
          headroom_mm: 3,
          legs: [
            {
              group: 'address/command U3->U4',
              leg: 'U3->U4',
              length_mm: 16.888,
              need_mm: 14.251,
              headroom_mm: 3,
              needs_reroute: true,
            },
          ],
        }),
      ],
    } as HeadroomResponse
    expect(candidatesCSV(analysis, measured).split('\n')[1].split(',')[6]).toBe('3')
  })
})
