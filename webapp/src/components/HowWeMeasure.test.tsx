import { describe, expect, it } from 'vitest'
import { HowWeMeasure, measureFacts } from './HowWeMeasure'
import { GroupTable } from './GroupTable'
import { renderUI, screen, within } from '../testRender'
import { countsMoreThanTrack, lengthSum } from '../lib/format'
import type { Analysis, GroupInfo, LengthParts, MemberInfo, PackagePad } from '../lib/analyzerApi'

// A length on the page can include via barrels, pad entry and the package.
// The page has to say so before the numbers and next to them, and say what
// this board actually counted -- not a generic paragraph that is wrong for
// half the boards it is shown on.

const parts = (over: Partial<LengthParts> = {}): LengthParts => ({
  track_mm: 40,
  via_mm: 0,
  vias: 0,
  pad_mm: 0.2,
  package_mm: 0,
  ...over,
})

const member = (over: Partial<MemberInfo>): MemberInfo => ({
  net: '/ddr4/DDR_A0',
  label: 'A0',
  role: 'address',
  routed: true,
  length_mm: 40.2,
  delay_ps: 250,
  deviation_mm: 0,
  need_mm: 0,
  need_ps: 0,
  in_tolerance: true,
  headroom_mm: 0,
  needs_reroute: false,
  parts: parts(),
  ...over,
})

function analysis(over: {
  viaCounted?: boolean
  members?: MemberInfo[]
  pads?: PackagePad[]
}): Analysis {
  return {
    board: { via_length_counted: over.viaCounted ?? true, via_barrel_mm: 1.594 },
    interface: { controller: 'U3', devices: ['U4'], width_bits: 16, lanes: 2, nets_found: 2, net_prefix: '' },
    groups: [{ name: 'address/command U3->U4', members: over.members ?? [member({})] }],
    package_pads: over.pads ?? [],
    interfaces: [],
  } as unknown as Analysis
}

describe('the length formula before any board', () => {
  it('names every part of a length in the header, and each in the formulas', () => {
    renderUI(<HowWeMeasure defaultOpen />)
    expect(screen.getByText('length = track + vias + pads + package')).toBeInTheDocument()
    const text = document.body.textContent ?? ''
    for (const term of ['centerline', 'via', 'pad center', 'die length', 'use_height_for_length_calcs', 'DQS_P', 'CLK_N', 'offset']) {
      expect(text).toContain(term)
    }
    // Short, US spelling, no em-dashes.
    expect(text).not.toMatch(/centre|—/)
    // Nothing about "this board" without one.
    expect(screen.queryByText('On this board')).toBeNull()
  })
})

describe('what a board counted', () => {
  it('says vias are counted, how tall a through via is, and how many routes cross one', () => {
    const a = analysis({
      members: [member({ parts: parts({ vias: 2, via_mm: 3.188 }) }), member({ net: '/ddr4/DDR_A1', label: 'A1' })],
    })
    renderUI(<HowWeMeasure analysis={a} defaultOpen />)
    const text = document.body.textContent ?? ''
    expect(text).toContain('Vias: counted, 1.594 mm per through via.')
    expect(text).toContain('1 of 2 routes cross a via.')
  })

  it('says vias add nothing when the board does not count their height', () => {
    renderUI(<HowWeMeasure analysis={analysis({ viaCounted: false })} defaultOpen />)
    const text = document.body.textContent ?? ''
    expect(text).toContain('Vias: not counted (use_height_for_length_calcs is off).')
    expect(text).not.toContain('per through via')
  })

  it('says where package lengths came from, or that there are none', () => {
    const pad = (mm: number): PackagePad => ({ net: 'x', pad: 'U3.A1', default_mm: mm, mm, source: mm > 0 ? 'STM32MP25xxAI' : '' })
    expect(measureFacts(analysis({ pads: [pad(9.1)] })).packageLine).toBe('U3, STM32MP25xxAI table.')
    expect(measureFacts(analysis({ pads: [pad(0)] })).packageLine).toBe('none for U3.')
  })

  it('reports the largest pad entry on the board', () => {
    const a = analysis({ members: [member({ parts: parts({ pad_mm: 0.31 }) }), member({ parts: parts({ pad_mm: 0.12 }) })] })
    expect(measureFacts(a).maxPadMM).toBeCloseTo(0.31)
  })
})

describe('a length written as its sum', () => {
  it('shows only the parts that contribute', () => {
    expect(lengthSum(parts())).toBe('40.000 mm track + 0.200 mm pads')
    expect(lengthSum(parts({ vias: 2, via_mm: 3.188, package_mm: 4.1 }))).toBe(
      '40.000 mm track + 3.188 mm 2 vias + 0.200 mm pads + 4.100 mm package',
    )
    expect(lengthSum(parts({ vias: 1, via_mm: 1.594 }))).toContain('1.594 mm via +')
    // A via that counts for nothing (via height off) is not shown as one.
    expect(lengthSum(parts({ vias: 2, via_mm: 0 }))).not.toContain('via')
    expect(countsMoreThanTrack(parts({ vias: 2, via_mm: 0 }))).toBe(false)
    expect(lengthSum(undefined)).toBeNull()
  })

  it('appears under the length in the group table when vias or package are counted', () => {
    const group: GroupInfo = {
      name: 'address/command U3->U4',
      kind: 'address-command',
      lane: -1,
      reference: 'CLK',
      reference_length_mm: 40,
      target_mm: 40,
      target_from_reference_mm: 40,
      tolerance_mm: 3.55,
      spread_mm: 1,
      total_need_mm: 0,
      out_of_tolerance: 0,
      members: [
        member({ label: 'A0', length_mm: 44.988, parts: parts({ vias: 3, via_mm: 4.782, pad_mm: 0.206 }) }),
        member({ net: '/ddr4/DDR_A1', label: 'A1', length_mm: 40.2 }),
      ],
    }
    renderUI(<GroupTable group={group} />)
    const a0 = screen.getByText('A0').closest('tr')!
    expect(within(a0).getByText('= 40.000 mm track + 4.782 mm 3 vias + 0.206 mm pads')).toBeInTheDocument()
    const a1 = screen.getByText('A1').closest('tr')!
    expect(within(a1).getByText('track + pads')).toBeInTheDocument()
    expect(screen.getByText(/length = track \+ vias \+ pads \+ package/)).toBeInTheDocument()
  })
})
