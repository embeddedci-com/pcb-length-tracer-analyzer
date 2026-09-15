import { describe, expect, it } from 'vitest'
import { ChecksCard } from './ChecksCard'
import { renderUI, screen, within } from '../testRender'
import type { CheckInfo } from '../lib/analyzerApi'

const checks: CheckInfo[] = [
  {
    kind: 'strobe-to-clock',
    name: 'byte lane 3 strobe vs CLK at U5',
    value_mm: -18.164,
    limit_mm: 12.06,
    ok: false,
    detail: 'strobe mean 41.509 mm, clock to U5 59.673 mm (U3->U4 28.486 + U4->U5 31.187)',
  },
  {
    kind: 'chip-delta',
    name: 'U4 bytes vs U5 bytes',
    value_mm: 17.959,
    limit_mm: 35,
    ok: true,
    detail: 'U4 lanes 0, 1 average 30.642 mm; U5 lanes 2, 3 average 48.601 mm',
  },
]

describe('ChecksCard', () => {
  it('shows each check with its difference, limit, verdict and arithmetic', () => {
    renderUI(<ChecksCard checks={checks} />)
    expect(screen.getByText('1 outside the limit')).toBeInTheDocument()

    const lane = screen.getByText('byte lane 3 strobe vs CLK at U5').closest('tr')!
    expect(within(lane).getByText('-18.164 mm')).toBeInTheDocument()
    expect(within(lane).getByText('±12.060 mm')).toBeInTheDocument()
    expect(within(lane).getByText('outside')).toBeInTheDocument()
    expect(within(lane).getByText(/clock to U5 59.673 mm/)).toBeInTheDocument()

    const chips = screen.getByText('U4 bytes vs U5 bytes').closest('tr')!
    expect(within(chips).getByText('17.959 mm')).toBeInTheDocument()
    expect(within(chips).getByText('≤ 35.000 mm')).toBeInTheDocument()
    expect(within(chips).getByText('ok')).toBeInTheDocument()
  })

  it('shows nothing when there are no checks', () => {
    renderUI(<ChecksCard checks={[]} />)
    expect(screen.queryByText('Across groups')).not.toBeInTheDocument()
  })
})
