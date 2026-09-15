import { describe, expect, it } from 'vitest'
import {
  areaVerdict,
  boardWide,
  bytes,
  caveats,
  expiresIn,
  groupCandidates,
  groupNeed,
  groupSummary,
  legRows,
  memberSeverity,
  mm,
  mm2,
  partitionCandidates,
  ps,
  signedMM,
  targetWasRaised,
  totalNeed,
  triage,
  triageLegs,
  gettable,
} from './format'
import type { Analysis, GroupInfo, HeadroomResponse, MemberInfo } from './analyzerApi'

function member(over: Partial<MemberInfo> = {}): MemberInfo {
  return {
    net: '/ddr4/DDR_DQ0',
    label: 'DQ0',
    role: 'data',
    routed: true,
    length_mm: 22.745,
    delay_ps: 129.8,
    deviation_mm: -0.423,
    need_mm: 0.423,
    need_ps: 2.4,
    in_tolerance: true,
    headroom_mm: 0,
    needs_reroute: false,
    ...over,
  }
}

function group(over: Partial<GroupInfo> = {}): GroupInfo {
  return {
    name: 'byte lane 0',
    kind: 'byte-lane',
    lane: 0,
    reference: 'DQS0_P',
    reference_length_mm: 23.167,
    target_mm: 23.167,
    target_from_reference_mm: 23.167,
    tolerance_mm: 0.635,
    spread_mm: 0.423,
    total_need_mm: 2.98,
    out_of_tolerance: 0,
    members: [member()],
    ...over,
  }
}

describe('formatting', () => {
  it('quotes lengths to the micrometre', () => {
    // Two decimals would hide a third of a 0.635 mm budget.
    expect(mm(22.7448)).toBe('22.745 mm')
    expect(mm(0)).toBe('0.000 mm')
    expect(ps(129.84)).toBe('129.8 ps')
  })

  it('keeps the sign on a deviation, so short and long are distinguishable', () => {
    expect(signedMM(-0.423)).toBe('-0.423 mm')
    expect(signedMM(0.423)).toBe('+0.423 mm')
    expect(signedMM(0)).toBe('0.000 mm')
  })

  it('formats sizes', () => {
    expect(bytes(512)).toBe('512 B')
    expect(bytes(2048)).toBe('2.0 kB')
    expect(bytes(2_472_180)).toBe('2.4 MB')
  })

  it('reports how long a session has left', () => {
    const now = new Date('2026-09-12T10:00:00Z')
    expect(expiresIn('2026-09-12T10:30:00Z', now)).toBe('30 min')
    expect(expiresIn('2026-09-12T12:00:00Z', now)).toBe('2 h')
    expect(expiresIn('2026-09-12T12:30:00Z', now)).toBe('2 h 30 min')
    expect(expiresIn('2026-09-12T09:00:00Z', now)).toBe('expired')
    expect(expiresIn('nonsense', now)).toBe('expired')
  })
})

describe('severity', () => {
  it('separates just-outside from needs-a-reroute', () => {
    const tol = 0.635
    expect(memberSeverity(member({ in_tolerance: true }), tol)).toBe('ok')
    expect(memberSeverity(member({ in_tolerance: false, deviation_mm: -0.7 }), tol)).toBe('warn')
    // 7.6 mm out on a 0.635 mm budget is not a tuning problem.
    expect(memberSeverity(member({ in_tolerance: false, deviation_mm: -7.609 }), tol)).toBe('bad')
    expect(memberSeverity(member({ routed: false }), tol)).toBe('unrouted')
  })
})

describe('group presentation', () => {
  it('summarises a matched group and an unmatched one differently', () => {
    expect(groupSummary(group())).toContain('all within tolerance')
    // A matched group has nothing to add, even if its members are not exactly
    // on the target.
    expect(groupSummary(group())).not.toContain('to add')
    const bad = group({ out_of_tolerance: 8, members: Array.from({ length: 11 }, () => member()) })
    expect(groupSummary(bad)).toContain('8 of 11 out')
  })

  it('notices when the target had to be raised above the reference', () => {
    expect(targetWasRaised(group())).toBe(0)
    // A member already longer than the strobe drags the whole group up.
    const raised = group({ target_mm: 38.601, target_from_reference_mm: 38.315 })
    expect(targetWasRaised(raised)).toBeCloseTo(0.286, 6)
  })

  it('lists only a group’s members that are actually candidates', () => {
    const g = group({
      members: [
        member({ net: 'a', in_tolerance: true, need_mm: 0 }),
        member({ net: 'b', in_tolerance: false, need_mm: 1 }),
      ],
    })
    const candidates = [member({ net: 'b', need_mm: 1 })]
    expect(groupCandidates(g, candidates)).toEqual(['b'])
  })
})

describe('candidate selection', () => {
  const candidates = [
    member({ net: 'a', need_mm: 7.609 }),
    member({ net: 'b', need_mm: 2.258 }),
    member({ net: 'c', need_mm: 0.423 }),
  ]

  it('splits by how much length is needed, not by a promise about fit', () => {
    const { small, large } = partitionCandidates(candidates, 3)
    expect(small.map((c) => c.net)).toEqual(['b', 'c'])
    expect(large.map((c) => c.net)).toEqual(['a'])
  })

  it('totals only the selected nets', () => {
    expect(totalNeed(candidates, ['b', 'c'])).toBeCloseTo(2.681, 6)
    expect(totalNeed(candidates, [])).toBe(0)
    // An unknown net contributes nothing rather than NaN.
    expect(totalNeed(candidates, ['nope'])).toBe(0)
  })
})

describe('caveats', () => {
  function analysis(over: Partial<Analysis> = {}): Analysis {
    return {
      board: {
        filename: 'b.kicad_pcb',
        copper_layers: ['F.Cu', 'B.Cu'],
        stackup_mm: 1.594,
        footprints: 1,
        pads: 2,
        tracks: 3,
        vias: 0,
        net_classes: ['DDR'],
        has_custom_dru: false,
        via_length_counted: true,
        has_project_file: true,
      },
      interface: {
        net_prefix: '/ddr4/',
        controller: 'U3',
        devices: ['U4'],
        width_bits: 32,
        lanes: 4,
        nets_found: 71,
      },
      routing: { complete: 71, incomplete: 0 },
      params: {} as Analysis['params'],
      groups: [],
      candidates: [],
      total_need_mm: 0,
      gettable_mm: 0,
      reroute_count: 0,
      bus_spare_mm: 0,
      ...over,
    }
  }

  it('says nothing when there is nothing to warn about', () => {
    expect(caveats(analysis())).toEqual([])
  })

  it('warns about a missing project file, because clearances silently change meaning', () => {
    const out = caveats(analysis({ board: { ...analysis().board, has_project_file: false } }))
    expect(out.join(' ')).toContain('net classes')
  })

  it('warns that custom design rules are not interpreted', () => {
    const out = caveats(analysis({ board: { ...analysis().board, has_custom_dru: true } }))
    expect(out.join(' ')).toContain('kicad_dru')
    expect(out.join(' ')).toContain('KiCad’s own DRC')
  })

  it('warns about unrouted nets, and that KiCad will not', () => {
    const out = caveats(analysis({ routing: { complete: 44, incomplete: 27 } }))
    expect(out.join(' ')).toContain('27 of 71')
    expect(out.join(' ')).toContain('does not report these gaps')
  })

  it('passes through the classifier’s own notes', () => {
    const out = caveats(analysis({ interface: { ...analysis().interface, notes: ['odd pair'] } }))
    expect(out).toContain('odd pair')
  })
})

describe('triage', () => {
  const m = (over: Partial<MemberInfo>): MemberInfo => member({ headroom_mm: 0, needs_reroute: false, ...over })

  it('separates the three things a user can do about a shortfall', () => {
    const { fits, tight, reroute } = triage([
      m({ net: 'a', need_mm: 1, headroom_mm: 5 }),
      m({ net: 'b', need_mm: 4, headroom_mm: 0.5 }),
      m({ net: 'c', need_mm: 14, headroom_mm: 0, needs_reroute: true }),
      m({ net: 'd', need_mm: 2, headroom_mm: 2 }),
    ])
    expect(fits.map((x) => x.net)).toEqual(['a', 'd'])
    expect(tight.map((x) => x.net)).toEqual(['b'])
    expect(reroute.map((x) => x.net)).toEqual(['c'])
  })

  it('calls a reroute a reroute even when there happens to be room', () => {
    // The verdict is about the route being too short for the target, not about
    // the space beside it, so room must not override it.
    const { fits, reroute } = triage([m({ net: 'x', need_mm: 14, headroom_mm: 99, needs_reroute: true })])
    expect(fits).toHaveLength(0)
    expect(reroute.map((x) => x.net)).toEqual(['x'])
  })

  it('totals only what each net can actually hold', () => {
    // Room beside one net cannot be lent to another: 5 mm next to a net that
    // needs 1 counts for 1.
    expect(
      gettable([
        m({ need_mm: 1, headroom_mm: 5 }),
        m({ need_mm: 4, headroom_mm: 0.5 }),
        m({ need_mm: 2, headroom_mm: 0 }),
      ]),
    ).toBeCloseTo(1.5, 9)
    expect(gettable([])).toBe(0)
  })
})

describe('areaVerdict', () => {
  const net = (need: number, headroom: number, reroute = false) =>
    ({
      net: `/n/${need}-${headroom}`,
      label: 'n',
      role: 'data',
      routed: true,
      length_mm: 100,
      delay_ps: 0,
      deviation_mm: -need,
      need_mm: need,
      need_ps: 0,
      in_tolerance: false,
      headroom_mm: headroom,
      needs_reroute: reroute,
    }) as MemberInfo

  // The invariant the whole panel rests on, and the one an earlier version
  // broke: a reader has to be able to add the parts up and get the total.
  it('splits the shortfall into parts that add up to it', () => {
    const rows = [net(1, 5), net(4, 1), net(50, 2, true)]
    const v = areaVerdict(rows)
    expect(v.need).toBeCloseTo(55, 9)
    expect(v.have).toBeCloseTo(1 + 1 + 2, 9)
    expect(v.have + v.short).toBeCloseTo(v.need, 9)
    expect(v.reachable + v.unreachable).toBeCloseTo(v.short, 9)
  })

  it('separates what a bigger area could reach from what it could not', () => {
    const v = areaVerdict([net(4, 1), net(50, 2, true)])
    // Short of room by 3, which more area might supply.
    expect(v.reachable).toBeCloseTo(3, 9)
    // Asking for more than its route can carry: 48 missing, and no area helps.
    expect(v.unreachable).toBeCloseTo(48, 9)
    expect(v.reroute).toBe(1)
  })

  it('says enough when every net has its room', () => {
    const v = areaVerdict([net(1, 5), net(2, 9)])
    expect(v.enough).toBe(true)
    expect(v.short).toBeCloseTo(0, 9)
    expect(v.fits).toBe(2)
  })

  it('reports nothing reachable when the areas hold no room', () => {
    const v = areaVerdict([net(3, 0), net(2, 0)])
    expect(v.have).toBeCloseTo(0, 9)
    expect(v.short).toBeCloseTo(5, 9)
    expect(v.enough).toBe(false)
  })

  it('never counts more room than a net needs', () => {
    // 9 mm of room against a 1 mm requirement is 1 mm of use, not 9.
    expect(areaVerdict([net(1, 9)]).have).toBeCloseTo(1, 9)
  })
})

// A net on a fly-by bus is short over each span of the chain separately, and
// almost every figure a reader acts on belongs to a span rather than to the
// net: the room beside that copper, the length to route that span to, which
// group is short by how much. These check the two places that got that wrong.

describe('per-leg requirements', () => {
  const twoLeg = member({
    net: '/ddr4/DDR_A11',
    label: 'A11',
    length_mm: 16.888,
    need_mm: 16.892,
    headroom_mm: 3,
    needs_reroute: true,
    legs: [
      {
        group: 'address/command U3->U4',
        leg: 'U3->U4',
        length_mm: 16.888,
        need_mm: 14.251,
        headroom_mm: 1,
        needs_reroute: true,
      },
      {
        group: 'address/command U4->U5',
        leg: 'U4->U5',
        length_mm: 29.5,
        need_mm: 2.641,
        headroom_mm: 4,
        needs_reroute: false,
      },
    ],
  })
  const oneLeg = member({
    net: '/ddr4/DDR_DQ20',
    label: 'DQ20',
    length_mm: 42.561,
    need_mm: 13.711,
    headroom_mm: 0,
    needs_reroute: true,
    legs: [
      {
        group: 'byte lane 2',
        length_mm: 42.561,
        need_mm: 13.711,
        headroom_mm: 0,
        needs_reroute: true,
      },
    ],
  })

  it('gives each span its own length, so a target can be worked out from it', () => {
    const rows = legRows([twoLeg, oneLeg])
    expect(rows).toHaveLength(3)
    const first = rows.find((r) => r.leg === 'U3->U4')!
    // The thing that reads as nonsense when the legs are added together:
    // 16.888 mm long, 14.251 short, so route it to 31.139 -- not to 33.780,
    // which is one leg's length plus both legs' requirements.
    expect(first.length_mm + first.need_mm).toBeCloseTo(31.139, 3)
    const second = rows.find((r) => r.leg === 'U4->U5')!
    expect(second.length_mm + second.need_mm).toBeCloseTo(32.141, 3)
  })

  it('sorts the worst span first, whichever net it belongs to', () => {
    expect(legRows([oneLeg, twoLeg])[0].label).toBe('A11')
  })

  it('triages per span: a net can be fine on one leg and hopeless on the next', () => {
    const { fits, reroute } = triageLegs(legRows([twoLeg]))
    expect(reroute.map((r) => r.leg)).toEqual(['U3->U4'])
    expect(fits.map((r) => r.leg)).toEqual(['U4->U5'])
  })

  // The bug this is here for: an address line belongs to every group in the
  // chain, so charging its whole requirement to each of them made two groups
  // report the same figure and the table add up to more than the board needs.
  it('charges a span to the group that measured it and to no other', () => {
    const first = group({
      name: 'address/command U3->U4',
      members: [member({ net: '/ddr4/DDR_A11' })],
    })
    const second = group({
      name: 'address/command U4->U5',
      members: [member({ net: '/ddr4/DDR_A11' })],
    })
    const candidates = [twoLeg]
    expect(groupNeed(first, candidates).need).toBeCloseTo(14.251, 9)
    expect(groupNeed(second, candidates).need).toBeCloseTo(2.641, 9)
    // And the parts still come to the whole.
    expect(groupNeed(first, candidates).need + groupNeed(second, candidates).need).toBeCloseTo(
      twoLeg.need_mm,
      9,
    )
  })

  it('counts room per span too, capped at what that span needs', () => {
    const g = group({
      name: 'address/command U4->U5',
      members: [member({ net: '/ddr4/DDR_A11' })],
    })
    // 4 mm of room against 2.641 of requirement is 2.641 of use.
    expect(groupNeed(g, [twoLeg]).reachable).toBeCloseTo(2.641, 9)
  })

  it('names the worst net in the group by that group\'s figure', () => {
    const g = group({
      name: 'address/command U4->U5',
      members: [member({ net: '/ddr4/DDR_A11' }), member({ net: '/ddr4/DDR_DQ20' })],
    })
    expect(groupNeed(g, [twoLeg, oneLeg]).worst).toEqual({ label: 'A11', need: 2.641 })
  })
})

describe('areas as figures', () => {
  it('rounds an area to something a person can picture', () => {
    expect(mm2(175.4)).toBe('175 mm²')
    expect(mm2(7.31)).toBe('7.3 mm²')
    expect(mm2(0)).toBe('0.0 mm²')
  })
})

// The candidate list, the area verdict and the apply all read the DDR plan's
// shape. boardWide folds every other interface into that shape, so they treat
// an Ethernet net exactly like a DQ bit instead of not seeing it.
describe('boardWide', () => {
  const eth = member({
    net: '/ethernet/ETH1.TXD0',
    label: 'ETH1.TXD0',
    length_mm: 26,
    need_mm: 4,
    headroom_mm: 4,
    needs_reroute: false,
    legs: [{ group: 'transmit', length_mm: 26, need_mm: 4, headroom_mm: 4, needs_reroute: false }],
  })
  const iface = {
    id: 'rgmii:0',
    kind: 'rgmii',
    name: 'Ethernet',
    nets: 12,
    routed: 12,
    pairs: 0,
    evidence: '',
    summary: '',
    actionable: true,
    unroutable: 0,
    candidates: [eth],
    groups: [
      { name: 'transmit', reference: 'GTX_CLK', reference_mm: 30, spread_mm: 4, limit_mm: 1,
        out_of_tolerance: 1, unroutable: 0, members: 6, target_mm: 30, need_mm: 4 },
    ],
    geometry: { width_mm: 0.1, gap_mm: 0, needs_width: false, needs_gap: false },
    impedance: { computed: false, microstrip: true, in_range: true },
  }
  const base = {
    candidates: [member({ net: '/ddr4/DDR_DQ0', need_mm: 1 })],
    groups: [group()],
    total_need_mm: 1,
    interfaces: [iface],
  } as unknown as Analysis

  it('adds the interface nets to the candidates and the totals', () => {
    const { analysis } = boardWide(base, null)
    expect(analysis.candidates.map((c) => c.net)).toEqual(['/ddr4/DDR_DQ0', '/ethernet/ETH1.TXD0'])
    expect(analysis.total_need_mm).toBeCloseTo(5, 9)
  })

  it('names an interface group after its interface, so it cannot be mistaken for another', () => {
    const { analysis } = boardWide(base, null)
    const g = analysis.groups.find((x) => x.name === 'Ethernet / transmit')!
    expect(g).toBeDefined()
    expect(groupNeed(g, analysis.candidates).need).toBeCloseTo(4, 9)
  })

  it('leaves out an interface the user unticked', () => {
    const { analysis } = boardWide(base, null, [])
    expect(analysis.candidates).toHaveLength(1)
  })

  it('takes the measured room from the headroom response', () => {
    const measured = {
      candidates: base.candidates,
      interfaces: [{ ...iface, candidates: [{ ...eth, headroom_mm: 1,
        legs: [{ ...eth.legs![0], headroom_mm: 1 }] }] }],
      total_need_mm: 1, gettable_mm: 0, reroute_count: 0, bus_spare_mm: 0,
    } as unknown as HeadroomResponse
    const { headroom } = boardWide(base, measured)
    expect(headroom!.candidates).toHaveLength(2)
    expect(headroom!.total_need_mm).toBeCloseTo(5, 9)
  })
})
