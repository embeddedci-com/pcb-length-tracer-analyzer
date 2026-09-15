import { describe, expect, it } from 'vitest'
import { AreaVerdict } from './AreaVerdict'
import { renderUI, screen } from '../testRender'
import type { Analysis, HeadroomResponse, MemberInfo } from '../lib/analyzerApi'

// The panel that answers "will this area do?".
//
// The arithmetic behind it is a tested function; what these check is what a
// reader is actually shown -- that the verdict matches the numbers, that a
// group with nothing is not quietly rounded away, and that the difference
// between "draw a bigger area" and "no area will help" survives the trip to
// the screen. Those are wiring, and wiring is what a pure function cannot be
// wrong about.

function candidate(over: Partial<MemberInfo> = {}): MemberInfo {
  return {
    net: '/ddr4/DDR_A0',
    label: 'A0',
    role: 'address',
    routed: true,
    length_mm: 40,
    delay_ps: 0,
    deviation_mm: -2,
    need_mm: 2,
    need_ps: 0,
    in_tolerance: false,
    headroom_mm: 0,
    needs_reroute: false,
    ...over,
  } as MemberInfo
}

function analysisWith(groups: { name: string; nets: string[] }[]): Analysis {
  return {
    board: {} as never,
    interface: {} as never,
    routing: {} as never,
    params: {} as never,
    groups: groups.map((g) => ({
      name: g.name,
      kind: 'byte-lane',
      lane: 0,
      leg: '',
      reference: 'DQS',
      reference_length_mm: 40,
      target_mm: 42,
      target_from_reference_mm: 42,
      tolerance_mm: 0.6,
      spread_before_mm: 0,
      members: g.nets.map((n) => candidate({ net: n })),
    })) as never,
    candidates: [],
    total_need_mm: 0,
    gettable_mm: 0,
  } as unknown as Analysis
}

function headroomWith(candidates: MemberInfo[]): HeadroomResponse {
  return {
    candidates,
    total_need_mm: candidates.reduce((s, c) => s + c.need_mm, 0),
    gettable_mm: 0,
    reroute_count: 0,
    bus_spare_mm: 0,
  } as HeadroomResponse
}

describe('AreaVerdict', () => {
  it('says the area is enough when every net has its room', () => {
    const rows = [candidate({ net: 'a', need_mm: 1, headroom_mm: 5 })]
    renderUI(
      <AreaVerdict
        analysis={analysisWith([{ name: 'byte lane 0', nets: ['a'] }])}
        headroom={headroomWith(rows)}
        loading={false}
        areaCount={1}
      />,
    )
    expect(screen.getByText(/enough room/i)).toBeInTheDocument()
    expect(screen.getByText(/Inside this area/)).toBeInTheDocument()
  })

  it('says how short it is, and by how much', () => {
    const rows = [candidate({ net: 'a', need_mm: 10, headroom_mm: 4 })]
    renderUI(
      <AreaVerdict
        analysis={analysisWith([{ name: 'byte lane 0', nets: ['a'] }])}
        headroom={headroomWith(rows)}
        loading={false}
        areaCount={2}
      />,
    )
    expect(screen.getByText(/short by 6\.000 mm/i)).toBeInTheDocument()
    // Two areas are "these 2 areas", not "this area".
    expect(screen.getByText(/Inside these 2 areas/)).toBeInTheDocument()
  })

  // The difference that decides what the user does next.
  it('separates what a bigger area could reach from what it never will', () => {
    const rows = [
      candidate({ net: 'tight', need_mm: 10, headroom_mm: 4 }),
      candidate({ net: 'far', need_mm: 50, headroom_mm: 0, needs_reroute: true }),
    ]
    renderUI(
      <AreaVerdict
        analysis={analysisWith([{ name: 'byte lane 0', nets: ['tight', 'far'] }])}
        headroom={headroomWith(rows)}
        loading={false}
        areaCount={1}
      />,
    )
    expect(screen.getByText(/a bigger area may reach it/i)).toBeInTheDocument()
    expect(screen.getByText(/need rerouting, not more space/i)).toBeInTheDocument()
  })

  // A total hides the case that matters: one group covered and another with
  // nothing is not "half covered".
  it('reports each group separately', () => {
    const rows = [
      candidate({ net: 'a', need_mm: 4, headroom_mm: 4 }),
      candidate({ net: 'b', need_mm: 4, headroom_mm: 0 }),
    ]
    renderUI(
      <AreaVerdict
        analysis={analysisWith([
          { name: 'byte lane 0', nets: ['a'] },
          { name: 'byte lane 3', nets: ['b'] },
        ])}
        headroom={headroomWith(rows)}
        loading={false}
        areaCount={1}
      />,
    )
    expect(screen.getByText('byte lane 0')).toBeInTheDocument()
    expect(screen.getByText('byte lane 3')).toBeInTheDocument()
    expect(screen.getByText('100%')).toBeInTheDocument()
    expect(screen.getByText('0%')).toBeInTheDocument()
  })

  it('tells the user plainly when the areas hold nothing at all', () => {
    const rows = [candidate({ net: 'a', need_mm: 4, headroom_mm: 0 })]
    renderUI(
      <AreaVerdict
        analysis={analysisWith([{ name: 'byte lane 0', nets: ['a'] }])}
        headroom={headroomWith(rows)}
        loading={false}
        areaCount={1}
      />,
    )
    expect(screen.getByText(/These areas hold no room at all/i)).toBeInTheDocument()
  })

  // Measuring takes a second, and a panel that renders nothing meanwhile looks
  // like a panel that has decided there is nothing to say.
  it('says it is measuring rather than showing nothing', () => {
    renderUI(
      <AreaVerdict analysis={analysisWith([])} headroom={null} loading areaCount={1} />,
    )
    expect(screen.getByText(/Measuring how much room these areas hold/i)).toBeInTheDocument()
  })

  it('shows nothing at all before anything has been asked for', () => {
    renderUI(
      <AreaVerdict analysis={analysisWith([])} headroom={null} loading={false} areaCount={0} />,
    )
    // The provider puts its own <style> in the container, so "nothing" is
    // measured as nothing of the component's: no verdict, no table.
    expect(screen.queryByText(/needed is reachable/)).not.toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })
})
