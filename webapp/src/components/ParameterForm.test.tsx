import { describe, expect, it, vi } from 'vitest'
import userEvent from '@testing-library/user-event'
import { ParameterForm } from './ParameterForm'
import { renderUI, screen } from '../testRender'
import type { Params, PackagePad } from '../lib/analyzerApi'

const defaults: Params = {
  net_prefix: '',
  controller: '',
  clock_offset_percent: 0,
  data_to_strobe_mm: 1.42,
  intra_pair_mm: 0.127,
  address_to_clock_mm: 3.55,
  strobe_to_clock_mm: 12.07,
  max_chip_delta_mm: 35,
  data_to_strobe_ps: 0,
  intra_pair_ps: 0,
  address_to_clock_ps: 0,
  include_control: false,
  max_intra_pair_fix_mm: 1,
  open_clearance_mm: 0,
  open_margin_mm: 1,
  max_amplitude_mm: 1,
  min_amplitude_mm: 0.1,
  meander_gap_widths: 3,
  meander_chamfer_widths: 1,
  min_run_mm: 1,
  pad_keepout_mm: 0.5,
}

const pads: PackagePad[] = [
  { net: '/ddr4/DDR_DQ3', pad: 'U3.AA22', ball: 'DDR_DQ3', default_mm: 9.924, mm: 9.924, source: 'STM32MP25xxAI' },
]

// Every DDR value shows its default, can be changed for this board, and put back.
describe('ParameterForm', () => {
  it('marks a value changed from its default and resets it', async () => {
    const onApply = vi.fn()
    renderUI(
      <ParameterForm
        value={{ ...defaults, data_to_strobe_mm: 2 }}
        defaults={defaults}
        packagePads={pads}
        controller="U3"
        onApply={onApply}
      />,
    )
    expect(screen.getByText('default 1.420 mm')).toBeInTheDocument()
    expect(screen.getByText('changed')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'reset' }))
    await userEvent.click(screen.getByRole('button', { name: 'Apply to this board' }))
    expect(onApply.mock.calls[0][0].data_to_strobe_mm).toBe(1.42)
  })

  it('sends a package length override for one net', async () => {
    const onApply = vi.fn()
    renderUI(<ParameterForm value={defaults} defaults={defaults} packagePads={pads} controller="U3" onApply={onApply} />)
    await userEvent.click(screen.getByRole('button', { name: /Package lengths/ }))
    const input = screen.getByLabelText('Package length of /ddr4/DDR_DQ3')
    await userEvent.clear(input)
    await userEvent.type(input, '8')
    await userEvent.click(screen.getByRole('button', { name: 'Apply to this board' }))
    expect(onApply.mock.calls.at(-1)![0].package_lengths_mm).toEqual({ '/ddr4/DDR_DQ3': 8 })
  })
})

describe('ParameterForm package table', () => {
  it('lets the part be chosen when it was not recognised', async () => {
    const onApply = vi.fn()
    renderUI(
      <ParameterForm
        value={defaults}
        defaults={defaults}
        packagePads={[]}
        packageParts={['STM32MP25xxAI']}
        controller="U27"
        onApply={onApply}
      />,
    )
    await userEvent.click(screen.getByRole('button', { name: /Package lengths/ }))
    await userEvent.click(screen.getByDisplayValue('Automatic (from the footprint)'))
    await userEvent.click(screen.getByRole('option', { name: 'STM32MP25xxAI' }))
    await userEvent.click(screen.getByRole('button', { name: 'Apply to this board' }))
    expect(onApply.mock.calls.at(-1)![0].package_part).toBe('STM32MP25xxAI')
  })
})
