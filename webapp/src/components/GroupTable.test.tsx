import { describe, expect, it } from 'vitest'
import { GroupTable } from './GroupTable'
import { renderUI, screen, within } from '../testRender'
import type { GroupInfo, MemberInfo } from '../lib/analyzerApi'

// A byte lane is matched to its strobe -- the mean of the pair -- and every
// offset in the table is against that. It used to be matched to whichever bit
// was longest, and the table gave no way to tell.

const m = (over: Partial<MemberInfo>): MemberInfo => ({
  net: '/ddr4/DDR_DQ16',
  label: 'DQ16',
  role: 'data',
  routed: true,
  length_mm: 55.511,
  delay_ps: 316.8,
  deviation_mm: -0.182,
  need_mm: 0.182,
  need_ps: 1,
  in_tolerance: true,
  headroom_mm: 0,
  needs_reroute: false,
  ...over,
})

const lane: GroupInfo = {
  name: 'byte lane 2',
  kind: 'byte-lane',
  lane: 2,
  reference: 'DQS2_N / DQS2_P',
  reference_length_mm: 55.693,
  reference_members: [
    { net: '/ddr4/DDR_DQS2_N', label: 'DQS2_N', length_mm: 55.472 },
    { net: '/ddr4/DDR_DQS2_P', label: 'DQS2_P', length_mm: 55.914 },
  ],
  target_mm: 55.693,
  target_from_reference_mm: 55.693,
  tolerance_mm: 0.635,
  spread_mm: 13.711,
  total_need_mm: 0.182,
  out_of_tolerance: 1,
  members: [
    m({}),
    m({ net: '/ddr4/DDR_DQS2_P', label: 'DQS2_P', length_mm: 55.914, deviation_mm: 0.221, need_mm: 0, reference: true }),
    m({ net: '/ddr4/DDR_DQ9', label: 'DQ9', length_mm: 57.5, deviation_mm: 1.807, need_mm: 0, in_tolerance: false, excess_mm: 1.172, needs_reroute: true }),
  ],
}

describe('GroupTable', () => {
  it('shows nothing to add for a member already within tolerance', () => {
    renderUI(<GroupTable group={lane} />)
    // DQ16 is 0.182 mm short of the target but inside the ±0.635 mm band.
    const row = screen.getByText('DQ16').closest('tr')!
    expect(within(row).queryByText('0.182 mm')).not.toBeInTheDocument()
    expect(within(row).getByText('—')).toBeInTheDocument()
  })

  it('says what the target is and what it is the mean of, before any row', () => {
    renderUI(<GroupTable group={lane} />)
    expect(screen.getByText('55.693 mm')).toBeInTheDocument()
    expect(screen.getByText(/the mean of DQS2_N \(55.472 mm\) and DQS2_P \(55.914 mm\)/)).toBeInTheDocument()
    expect(screen.getByText(/tolerance ±0.635 mm/)).toBeInTheDocument()
  })

  it('marks the halves of the reference, which are not tuned towards their own mean', () => {
    renderUI(<GroupTable group={lane} />)
    const row = screen.getByText('DQS2_P').closest('tr')!
    expect(within(row).getByText('reference')).toBeInTheDocument()
    expect(within(row).getByText('+0.221 mm')).toBeInTheDocument()
  })

  // Longer than the strobe: nothing can be added, and the table says what to do instead.
  it('says a member longer than the reference is too long, and by how much to shorten it', () => {
    renderUI(<GroupTable group={lane} />)
    const row = screen.getByText('DQ9').closest('tr')!
    expect(within(row).getByText('too long')).toBeInTheDocument()
    expect(within(row).getByText('+1.807 mm')).toBeInTheDocument()
    expect(within(row).getByText('shorten 1.172 mm')).toBeInTheDocument()
  })

  // Address/command is matched per leg, but KiCad measures the whole net.
  it('gives the whole-net length to aim for when the row is one leg of a net', () => {
    const leg: GroupInfo = {
      ...lane,
      members: [
        m({ net: '/ddr4/DDR_A0', label: 'A0', length_mm: 20.438, need_mm: 2.5, in_tolerance: false, net_total_mm: 52.1, net_aim_mm: 54.6 }),
        m({ net: '/ddr4/DDR_A1', label: 'A1', length_mm: 22.904, need_mm: 1, in_tolerance: false }),
      ],
    }
    renderUI(<GroupTable group={leg} />)
    expect(within(screen.getByText('A0').closest('tr')!).getByText('(total 54.600 mm)')).toBeInTheDocument()
    expect(within(screen.getByText('A1').closest('tr')!).queryByText(/total/)).not.toBeInTheDocument()
  })
})
