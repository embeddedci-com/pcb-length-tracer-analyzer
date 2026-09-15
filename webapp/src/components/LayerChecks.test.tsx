import { describe, expect, it } from 'vitest'
import { LayerChecks } from './LayerChecks'
import { renderUI, screen } from '../testRender'

describe('LayerChecks', () => {
  it('lists nets on other layers as information, with the copper per layer', () => {
    renderUI(
      <LayerChecks
        findings={[
          {
            rule: 'data-top',
            expect: 'data lines (DQ, DQM, DQS) on the top layer (F.Cu) only',
            nets: [{ net: '/ddr4/DDR_DQ5', label: 'DQ5', by_layer_mm: { 'F.Cu': 10, 'In1.Cu': 3.2 }, share: 0.757 }],
          },
        ]}
      />,
    )
    expect(screen.getByText('for information')).toBeInTheDocument()
    expect(screen.getByText('Data lines not only on the top layer (1)')).toBeInTheDocument()
    expect(screen.getByText('76%')).toBeInTheDocument()
    expect(screen.getByText('F.Cu 10.000 mm, In1.Cu 3.200 mm')).toBeInTheDocument()
    expect(screen.getByText(/not an error if your board is built this way on purpose/)).toBeInTheDocument()
  })

  it('shows nothing when every net follows the guide', () => {
    renderUI(<LayerChecks findings={[]} />)
    expect(screen.queryByText('Layer use')).not.toBeInTheDocument()
  })
})
