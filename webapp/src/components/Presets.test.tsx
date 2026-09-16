import { describe, expect, it, vi } from 'vitest'
import userEvent from '@testing-library/user-event'
import { ParameterForm } from './ParameterForm'
import { renderUI, screen } from '../testRender'
import { applyPreset, matchingPreset } from '../lib/format'
import type { Params, Preset } from '../lib/analyzerApi'

// A preset is a vendor's table, not a suggestion: choosing one has to put its
// numbers in force, name the document they came from, and stop claiming that
// vendor as soon as somebody edits a value.

const params = (over: Partial<Params> = {}): Params =>
  ({
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
    strobe_to_clock_ps: 0,
    include_control: false,
    max_intra_pair_fix_mm: 1,
    open_clearance_mm: 0.2,
    open_margin_mm: 1,
    max_amplitude_mm: 1.5,
    min_amplitude_mm: 0.3,
    meander_gap_widths: 3,
    meander_chamfer_widths: 1,
    min_run_mm: 1,
    pad_keepout_mm: 0.3,
    ...over,
  }) as Params

const st: Preset = {
  id: 'st-stm32mp25-ddr4',
  name: 'STM32MP25x, DDR4',
  vendor: 'STMicroelectronics',
  memory: 'DDR4',
  source: 'ST DDR4 length equalization sheet for STM32MP25xxAI, with AN5724',
  params: {
    data_to_strobe_mm: 1.42,
    data_to_strobe_ps: 0,
    intra_pair_mm: 0.127,
    intra_pair_ps: 0,
    address_to_clock_mm: 3.55,
    address_to_clock_ps: 0,
    strobe_to_clock_mm: 12.07,
    strobe_to_clock_ps: 0,
    max_chip_delta_mm: 35,
    clock_offset_percent: 0,
  },
}

const rk: Preset = {
  id: 'rk3588-lpddr4-hdi',
  name: 'RK3588, LPDDR4/LPDDR4X (10-layer HDI)',
  vendor: 'Rockchip',
  memory: 'LPDDR4/LPDDR4X',
  source: 'RK3588 Hardware Design Guide V1.0 (2022-01-06), tables 3-8 and 3-9',
  url: 'https://example.invalid/rk3588.pdf',
  note: 'For the 10-layer HDI stackup, where the guide matches by length.',
  params: {
    data_to_strobe_mm: 0.635,
    data_to_strobe_ps: 0,
    intra_pair_mm: 0.127,
    intra_pair_ps: 0,
    address_to_clock_mm: 1.016,
    address_to_clock_ps: 0,
    strobe_to_clock_mm: 6.35,
    strobe_to_clock_ps: 0,
    max_chip_delta_mm: 35,
    clock_offset_percent: 0,
  },
}

const rk8: Preset = {
  ...rk,
  id: 'rk3588-lpddr4-8layer',
  name: 'RK3588, LPDDR4/LPDDR4X (8-layer, equal delay)',
  params: {
    data_to_strobe_mm: 0,
    data_to_strobe_ps: 16,
    intra_pair_mm: 0,
    intra_pair_ps: 1,
    address_to_clock_mm: 0,
    address_to_clock_ps: 16,
    strobe_to_clock_mm: 0,
    strobe_to_clock_ps: 40,
    max_chip_delta_mm: 35,
    clock_offset_percent: 0,
  },
}

describe('choosing a vendor preset', () => {
  it('replaces every limit, and clears the unit the guide did not use', () => {
    const out = applyPreset(params(), rk8)
    expect(out.data_to_strobe_ps).toBe(16)
    expect(out.data_to_strobe_mm).toBe(0)
    expect(out.strobe_to_clock_ps).toBe(40)
    expect(out.strobe_to_clock_mm).toBe(0)
    expect(out.intra_pair_ps).toBe(1)
    // Everything that is not the vendor's business is left alone.
    expect(out.open_clearance_mm).toBe(0.2)
    expect(out.max_intra_pair_fix_mm).toBe(1)
  })

  it('is recognised from the values, and let go of as soon as one is edited', () => {
    const on = applyPreset(params(), rk)
    expect(matchingPreset(on, [st, rk, rk8])?.id).toBe('rk3588-lpddr4-hdi')
    expect(matchingPreset({ ...on, address_to_clock_mm: 2 }, [st, rk, rk8])).toBeNull()
    expect(matchingPreset(params(), [st, rk, rk8])?.id).toBe('st-stm32mp25-ddr4')
    expect(matchingPreset(params(), [])).toBeNull()
  })
})

describe('the preset dropdown', () => {
  it('names the document the numbers come from, and links it', async () => {
    renderUI(
      <ParameterForm value={applyPreset(params(), rk)} presets={[st, rk]} onApply={vi.fn()} />,
    )
    expect(screen.getByText(/RK3588 Hardware Design Guide V1.0/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'open' })).toHaveAttribute(
      'href',
      'https://example.invalid/rk3588.pdf',
    )
    expect(screen.getByDisplayValue(/RK3588, LPDDR4\/LPDDR4X \(10-layer HDI\)/)).toBeInTheDocument()
  })

  it('applies the chosen vendor rules to the form', async () => {
    const onApply = vi.fn()
    renderUI(<ParameterForm value={params()} presets={[st, rk]} onApply={onApply} />)
    await userEvent.click(screen.getByRole('textbox', { name: 'Preset' }))
    await userEvent.click(await screen.findByText(/RK3588, LPDDR4\/LPDDR4X \(10-layer HDI\)/))
    await userEvent.click(screen.getByRole('button', { name: /apply/i }))
    expect(onApply).toHaveBeenCalledTimes(1)
    const sent = onApply.mock.calls[0][0] as Params
    expect(sent.data_to_strobe_mm).toBeCloseTo(0.635)
    expect(sent.address_to_clock_mm).toBeCloseTo(1.016)
    expect(sent.strobe_to_clock_mm).toBeCloseTo(6.35)
  })

  it('says so plainly when the limits match no published set', () => {
    renderUI(
      <ParameterForm
        value={params({ data_to_strobe_mm: 0.9 })}
        presets={[st, rk]}
        onApply={vi.fn()}
      />,
    )
    expect(screen.getByText(/match no preset/)).toBeInTheDocument()
  })

  it('says where a limit went when the guide states it as a delay', async () => {
    renderUI(
      <ParameterForm value={applyPreset(params(), rk8)} presets={[st, rk, rk8]} onApply={vi.fn()} />,
    )
    // Data and address are both 16 ps in that table; strobe to clock is 40.
    expect(screen.getAllByText('set as a delay: 16 ps')).toHaveLength(2)
    expect(screen.getByText('set as a delay: 40 ps')).toBeInTheDocument()
  })

  it('is left out where there are no presets to offer', () => {
    renderUI(<ParameterForm value={params()} onApply={vi.fn()} />)
    expect(screen.queryByRole('textbox', { name: 'Preset' })).toBeNull()
  })
})

// One vendor table usually covers several part numbers, and the label cannot
// name them all. Somebody who types the part in front of them has to find it.
describe('finding a preset by part number', () => {
  const withParts: Preset = { ...rk, parts: ['RK3588', 'RK3588S'] }

  it('matches a part the label does not name', async () => {
    renderUI(<ParameterForm value={params()} presets={[st, withParts]} onApply={vi.fn()} />)
    const box = screen.getByRole('textbox', { name: 'Preset' })
    await userEvent.click(box)
    // The box shows the preset already in force, so a search replaces it.
    await userEvent.clear(box)
    await userEvent.type(box, 'RK3588S')
    expect(await screen.findByText(/RK3588, LPDDR4\/LPDDR4X \(10-layer HDI\)/)).toBeInTheDocument()
    expect(screen.queryByText(/STM32MP25x, DDR4 \(DDR4\)/)).not.toBeInTheDocument()
  })

  it('still matches the label, and says so when nothing matches', async () => {
    renderUI(<ParameterForm value={params()} presets={[st, withParts]} onApply={vi.fn()} />)
    const box = screen.getByRole('textbox', { name: 'Preset' })
    await userEvent.click(box)
    await userEvent.clear(box)
    await userEvent.type(box, '10-layer')
    expect(await screen.findByText(/RK3588, LPDDR4\/LPDDR4X \(10-layer HDI\)/)).toBeInTheDocument()

    await userEvent.clear(box)
    await userEvent.type(box, 'bcm2711')
    expect(await screen.findByText('No preset for that chip')).toBeInTheDocument()
  })
})

// The defaults are one vendor's figures, so a board nobody has touched shows
// that vendor's preset as selected. On somebody else's chip that is wrong and
// nothing else on the page would say so.
describe('matching the preset to the chip on the board', () => {
  const rkForPart: Preset = { ...rk, parts: ['RK3588', 'RK3588S'] }

  it('warns when the limits are not the detected chip’s, and loads the right one', async () => {
    const onApply = vi.fn()
    renderUI(
      <ParameterForm
        value={params()} // ST's defaults
        presets={[st, rkForPart]}
        controllerValue="RK3588S"
        presetsForPart={['rk3588-lpddr4-hdi']}
        onApply={onApply}
      />,
    )
    expect(screen.getByText(/RK3588S/)).toBeInTheDocument()
    expect(screen.getByText(/these limits are not its/i)).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: /^Load RK3588/ }))
    await userEvent.click(screen.getByRole('button', { name: /apply/i }))
    const sent = onApply.mock.calls[0][0] as Params
    expect(sent.data_to_strobe_mm).toBeCloseTo(0.635)
  })

  it('confirms when they do match', () => {
    renderUI(
      <ParameterForm
        value={params()}
        presets={[st, rkForPart]}
        controllerValue="STM32MP257DAI3"
        presetsForPart={['st-stm32mp25-ddr4']}
        onApply={vi.fn()}
      />,
    )
    expect(screen.getByText(/Matches the/)).toBeInTheDocument()
    expect(screen.queryByText(/are not its/i)).not.toBeInTheDocument()
  })

  it('says the part was not recognized rather than staying quiet', () => {
    renderUI(
      <ParameterForm
        value={params()}
        presets={[st, rkForPart]}
        controllerValue="BCM2711"
        presetsForPart={[]}
        onApply={vi.fn()}
      />,
    )
    expect(screen.getByText(/not a part with rules here/)).toBeInTheDocument()
  })

  it('says nothing at all when the board names no controller', () => {
    renderUI(<ParameterForm value={params()} presets={[st, rkForPart]} onApply={vi.fn()} />)
    expect(screen.queryByText(/not a part with rules here/)).not.toBeInTheDocument()
    expect(screen.queryByText(/Matches the/)).not.toBeInTheDocument()
  })
})
