import { describe, expect, it, vi } from 'vitest'
import userEvent from '@testing-library/user-event'
import { Readiness } from './Readiness'
import { renderUI, screen, within } from '../testRender'
import type { Analysis, GroupInfo, HeadroomResponse, MemberInfo } from '../lib/analyzerApi'

// The briefing, which is the one screen that has to be right on its own: a
// reader decides from it whether to route more copper, open some space, or
// press the button. The figures on it come from tested arithmetic; what these
// check is that the right figure reaches the right place, that a measurement
// still running says so rather than showing a zero, and that the three things
// a person can do about a shortfall stay separate.

function member(over: Partial<MemberInfo> = {}): MemberInfo {
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

function group(over: Partial<GroupInfo> = {}): GroupInfo {
  return {
    name: 'address/command U3->U4',
    kind: 'address-command',
    lane: -1,
    leg: 'U3->U4',
    reference: 'CLK_P',
    reference_length_mm: 28.7,
    target_mm: 31.139,
    target_from_reference_mm: 28.7,
    tolerance_mm: 1.27,
    spread_mm: 14.251,
    total_need_mm: 14.251,
    out_of_tolerance: 1,
    members: [member()],
    ...over,
  }
}

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
      net_classes: [],
      has_custom_dru: false,
      via_length_counted: true,
      has_project_file: true,
    },
    interface: {
      net_prefix: '/ddr4/',
      controller: 'U3',
      devices: ['U4', 'U5'],
      width_bits: 32,
      lanes: 4,
      nets_found: 71,
    },
    routing: { complete: 71, incomplete: 0 },
    params: {} as Analysis['params'],
    groups: [group()],
    candidates: [member()],
    total_need_mm: 14.251,
    gettable_mm: 0,
    reroute_count: 1,
    bus_spare_mm: 0,
    ...over,
  } as Analysis
}

function headroom(candidates: MemberInfo[], over: Partial<HeadroomResponse> = {}): HeadroomResponse {
  return {
    candidates,
    total_need_mm: candidates.reduce((s, c) => s + c.need_mm, 0),
    gettable_mm: 0,
    reroute_count: candidates.filter((c) => c.needs_reroute).length,
    bus_spare_mm: 0,
    run_needed_mm: 4.6,
    space_needed_mm2: 7.31,
    ...over,
  } as HeadroomResponse
}

const unrouted = {
  complete: 69,
  incomplete: 2,
  gaps: [{ islands: ['U3+U4', 'U5+termination'], nets: ['CKE', 'RESETN'] }],
  missing: [
    { net: '/ddr4/DDR_CKE', label: 'CKE', from: 'U4.K2', to: 'U5.K2', hop: 'U4 -> U5' },
    { net: '/ddr4/DDR_RESETN', label: 'RESETN', from: 'U4.P1', to: 'U5.P1', hop: 'U4 -> U5' },
  ],
}

describe('Readiness: copper that is missing', () => {
  it('leads with what is not joined up, because nothing else can be trusted until it is', () => {
    renderUI(
      <Readiness analysis={analysis({ routing: unrouted })} headroom={null} measuring={false} />,
    )
    // Counted over the whole board, not only the bus with a planner.
    expect(screen.getAllByText(/2 nets/).length).toBeGreaterThan(0)
    // Grouped by where: a span of the chain, or an interface. 26 nets missing
    // one hop is one decision, not 26.
    const row = screen.getByText('U4 -> U5').closest('tr')!
    expect(within(row).getByText('CKE, RESETN')).toBeInTheDocument()
    expect(within(row).getByText('a span of the fly-by chain')).toBeInTheDocument()
    // And the reason KiCad will not have told them.
    expect(screen.getByText(/KiCad’s DRC does not always report this/)).toBeInTheDocument()
  })

  it('says route it in KiCad first, and offers the router second', async () => {
    const onRoute = vi.fn()
    renderUI(
      <Readiness
        analysis={analysis({ routing: unrouted })}
        headroom={null}
        measuring={false}
        onRoute={onRoute}
      />,
    )
    expect(screen.getByText('Route these first')).toBeInTheDocument()
    // The offer is honest about what it does: this is the part a user would
    // otherwise find out by waiting ten seconds for nothing.
    expect(screen.getByText(/often cannot find a path/)).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Try the experimental router' }))
    expect(onRoute).toHaveBeenCalled()
  })

  it('says so plainly when there is nothing missing', () => {
    renderUI(<Readiness analysis={analysis()} headroom={null} measuring={false} />)
    // Said in the subtitle and again in the step itself.
    expect(screen.getAllByText(/Every net is joined end to end/).length).toBeGreaterThan(0)
    expect(screen.queryByText('Route these first')).not.toBeInTheDocument()
  })
})

// The gap this closed: the briefing used to be about DDR, and a board whose
// Ethernet is not joined up got a page that said nothing to do.
describe('Readiness: interfaces the DDR planner does not cover', () => {
  const ethernet = {
    id: 'rgmii:0',
    kind: 'rgmii',
    name: 'Ethernet RGMII (ETH1)',
    nets: 17,
    routed: 2,
    pairs: 0,
    evidence: 'ETH1.TXD0, ETH1.RXD0',
    summary: 'none of it is joined end to end yet',
    actionable: true,
    unroutable: 17,
    unrouted_nets: ['ETH1.TXD0', 'ETH1.TXD1', 'ETH1.RX_CLK'],
    geometry: { width_mm: 0.15, gap_mm: 0, needs_width: false, needs_gap: false },
    impedance: { computed: false, microstrip: true, in_range: true },
  } as Analysis['interfaces'] extends (infer T)[] | undefined ? T : never

  it('lists what is not joined up on any interface, not only on the bus with a planner', () => {
    renderUI(
      <Readiness
        analysis={analysis({ routing: unrouted, interfaces: [ethernet] })}
        headroom={null}
        measuring={false}
      />,
    )
    const row = screen.getByText('Ethernet RGMII (ETH1)').closest('tr')!
    expect(within(row).getByText('3')).toBeInTheDocument()
    expect(within(row).getByText(/ETH1.TXD0, ETH1.TXD1, ETH1.RX_CLK/)).toBeInTheDocument()
  })

  it('counts its shortfall in the totals and its groups in the table', () => {
    const short = {
      ...ethernet,
      unrouted_nets: [],
      unroutable: 0,
      total_need_mm: 4,
      groups: [
        {
          name: 'transmit',
          reference: 'GTX_CLK',
          reference_mm: 30,
          spread_mm: 4,
          limit_mm: 10,
          out_of_tolerance: 1,
          unroutable: 0,
          members: 6,
        },
      ],
      candidates: [
        member({
          net: '/ethernet/ETH1.TXD0',
          label: 'ETH1.TXD0',
          length_mm: 26,
          need_mm: 4,
          headroom_mm: 4,
          needs_reroute: false,
          legs: [
            { group: 'transmit', length_mm: 26, need_mm: 4, headroom_mm: 4, needs_reroute: false },
          ],
        }),
      ],
    }
    renderUI(
      <Readiness
        analysis={analysis({ interfaces: [short], candidates: [], total_need_mm: 0, groups: [] })}
        headroom={headroom([])}
        measuring={false}
      />,
    )
    // Its group is named, and said to belong to the interface it came from.
    const row = screen.getByText('transmit').closest('tr')!
    expect(within(row).getByText('Ethernet RGMII (ETH1)')).toBeInTheDocument()
    expect(within(row).getByText('1 of 6')).toBeInTheDocument()
    // Needed and reachable, both 4 mm: the room beside it covers it.
    expect(within(row).getAllByText('4.000 mm')).toHaveLength(2)
    // And it reaches the buckets at the bottom.
    expect(screen.getByText(/can be matched as they are: 4.000 mm/)).toBeInTheDocument()
  })

  it('leaves an interface the user unticked out of it', () => {
    renderUI(
      <Readiness
        analysis={analysis({ routing: unrouted, interfaces: [ethernet] })}
        headroom={null}
        measuring={false}
        selected={[]}
      />,
    )
    expect(screen.queryByText('Ethernet RGMII (ETH1)')).not.toBeInTheDocument()
  })
})

describe('Readiness: length and space', () => {
  it('reports each group against its own requirement', () => {
    const two = [
      member(),
      member({
        net: '/ddr4/DDR_DQ20',
        label: 'DQ20',
        length_mm: 42.561,
        need_mm: 13.711,
        legs: [
          {
            group: 'byte lane 2',
            length_mm: 42.561,
            need_mm: 13.711,
            headroom_mm: 2,
            needs_reroute: true,
          },
        ],
      }),
    ]
    renderUI(
      <Readiness
        analysis={analysis({
          candidates: two,
          total_need_mm: 27.962,
          groups: [group(), group({ name: 'byte lane 2', leg: '', members: [two[1]] })],
        })}
        headroom={headroom(two)}
        measuring={false}
      />,
    )
    const lane = screen.getByText('byte lane 2').closest('tr')!
    expect(within(lane).getByText('13.711 mm')).toBeInTheDocument()
    // Room beside that route, capped at what it needs.
    expect(within(lane).getByText('2.000 mm')).toBeInTheDocument()
  })

  it('turns the requirement into an area, and says how much of it is still to find', () => {
    renderUI(
      <Readiness
        analysis={analysis()}
        headroom={headroom([member({ headroom_mm: 0 })], { run_needed_mm: 4.6, space_needed_mm2: 7.31 })}
        measuring={false}
      />,
    )
    // The whole requirement, and -- since none of it is reachable -- the same
    // figure again as the part still to be found.
    expect(screen.getAllByText('7.3 mm²')).toHaveLength(2)
    expect(screen.getByText('4.600 mm')).toBeInTheDocument()
    expect(screen.getByText('of that still to be found')).toBeInTheDocument()
    // And it is labelled as the estimate it is.
    expect(screen.getByText(/A best-case estimate/)).toBeInTheDocument()
  })

  // A panel that renders nothing while a measurement runs looks like a panel
  // that has decided there is nothing to say.
  it('says it is measuring rather than showing a zero', () => {
    renderUI(<Readiness analysis={analysis()} headroom={null} measuring />)
    expect(
      screen.getByText(/Measuring how much room there is beside every route/),
    ).toBeInTheDocument()
    expect(screen.getByText(/Working out which nets can be matched/)).toBeInTheDocument()
  })
})

describe('Readiness: what to do', () => {
  const rows = [
    member({ net: 'a', label: 'FITS', need_mm: 1, headroom_mm: 5, needs_reroute: false,
      legs: [{ group: 'byte lane 0', length_mm: 20, need_mm: 1, headroom_mm: 5, needs_reroute: false }] }),
    member({ net: 'b', label: 'TIGHT', need_mm: 4, headroom_mm: 1, needs_reroute: false,
      legs: [{ group: 'byte lane 0', length_mm: 20, need_mm: 4, headroom_mm: 1, needs_reroute: false }] }),
    member(),
  ]

  it('keeps the three answers separate, because they are three different jobs', () => {
    renderUI(<Readiness analysis={analysis()} headroom={headroom(rows)} measuring={false} />)
    expect(screen.getByText(/can be matched as they are: 1.000 mm/)).toBeInTheDocument()
    expect(screen.getByText(/need more room: 4.000 mm/)).toBeInTheDocument()
    expect(screen.getByText(/need rerouting, not tuning: 14.251 mm/)).toBeInTheDocument()
  })

  // The one instruction a person can act on without doing arithmetic in their
  // head against another window.
  it('tells a net that needs rerouting what length to route it to', () => {
    renderUI(<Readiness analysis={analysis()} headroom={headroom(rows)} measuring={false} />)
    const row = screen.getByText('A11').closest('tr')!
    expect(within(row).getByText('16.888 mm')).toBeInTheDocument()
    expect(within(row).getByText('31.139 mm')).toBeInTheDocument()
    expect(within(row).getByText('84%')).toBeInTheDocument()
    // And which span of the chain that is, since the net has more than one.
    expect(within(row).getByText('U3->U4')).toBeInTheDocument()
  })

  it('offers the next step only when there is something to do', async () => {
    const onContinue = vi.fn()
    const { unmount } = renderUI(
      <Readiness
        analysis={analysis()}
        headroom={headroom(rows)}
        measuring={false}
        onContinue={onContinue}
      />,
    )
    await userEvent.click(screen.getByRole('button', { name: 'Choose what to change' }))
    expect(onContinue).toHaveBeenCalled()
    // Said where the decision is made, not only once the result is on screen:
    // everything above this button reads the board, everything past it writes.
    expect(screen.getByText(/Changing the board is the experimental half/)).toBeInTheDocument()
    unmount()

    renderUI(
      <Readiness
        analysis={analysis({ candidates: [], total_need_mm: 0, groups: [] })}
        headroom={headroom([])}
        measuring={false}
        onContinue={onContinue}
      />,
    )
    expect(screen.getByText(/There is nothing to match/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Choose what to change' })).not.toBeInTheDocument()
  })
})
