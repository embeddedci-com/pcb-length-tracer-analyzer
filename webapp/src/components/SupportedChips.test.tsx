import { describe, expect, it } from 'vitest'
import userEvent from '@testing-library/user-event'
import { SupportedChips } from './SupportedChips'
import { PRESET_CATALOG } from '../lib/presets.generated'
import { renderUI, screen, within } from '../testRender'
import type { Preset } from '../lib/analyzerApi'

// The catalogue is a claim about other people's documents, so what matters is
// that a reader can get from a chip's name to the table the numbers came from,
// and that the numbers shown are the ones in the preset.

const preset = (over: Partial<Preset> = {}): Preset => ({
  id: 'x-lpddr4',
  name: 'Example 1, LPDDR4',
  vendor: 'Example Semiconductor',
  memory: 'LPDDR4',
  source: 'Example Design Guide V1.0, table 3-1',
  url: 'https://example.com/guide.pdf',
  note: 'The guide also holds CS to 1 ps, which this tool does not separate.',
  params: {
    data_to_strobe_mm: 0,
    data_to_strobe_ps: 50,
    intra_pair_mm: 0,
    intra_pair_ps: 1,
    address_to_clock_mm: 0,
    address_to_clock_ps: 40,
    strobe_to_clock_mm: 0,
    strobe_to_clock_ps: 75,
    max_chip_delta_mm: 35,
    clock_offset_percent: 0,
  },
  ...over,
})

describe('SupportedChips', () => {
  it('is folded away until it is asked for', async () => {
    const user = userEvent.setup()
    renderUI(<SupportedChips presets={[preset()]} />)

    // Mantine keeps a collapsed panel in the document, so the state that
    // matters is the one the control reports and a reader sees.
    const control = screen.getByRole('button', { name: /Chips with vendor rules/ })
    expect(control).toHaveAttribute('aria-expanded', 'false')
    await user.click(control)
    expect(control).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByText('Example 1, LPDDR4')).toBeInTheDocument()
    // The vendor is named without opening a chip.
    expect(screen.getAllByText('Example Semiconductor').length).toBeGreaterThan(0)
  })

  it('shows a chip its own limits, its source and its caveat', async () => {
    const user = userEvent.setup()
    renderUI(<SupportedChips presets={[preset()]} />)

    await user.click(screen.getByText(/Chips with vendor rules/))
    // The headline numbers are on the row, before it is opened.
    expect(screen.getByText('data ±50 ps, address ±40 ps')).toBeInTheDocument()

    await user.click(screen.getByText('Example 1, LPDDR4'))
    expect(screen.getByText('Strobe to clock')).toBeInTheDocument()
    expect(screen.getByText('±75 ps')).toBeInTheDocument()
    expect(screen.getByText(/Example Design Guide V1.0, table 3-1/)).toBeInTheDocument()
    // findBy, not getBy: a panel Mantine is still opening is hidden from the
    // accessibility tree, so the link only has its role once the fold is done.
    expect(await screen.findByRole('link', { name: 'open' })).toHaveAttribute(
      'href',
      'https://example.com/guide.pdf',
    )
    expect(screen.getByText(/does not separate/)).toBeInTheDocument()
  })

  it('shows a length preset in millimetres and a delay preset in picoseconds', async () => {
    const user = userEvent.setup()
    renderUI(
      <SupportedChips
        presets={[
          preset({
            id: 'len',
            name: 'Example 2, DDR4',
            params: {
              ...preset().params,
              data_to_strobe_ps: 0,
              data_to_strobe_mm: 0.635,
              address_to_clock_ps: 0,
              address_to_clock_mm: 1.016,
            },
          }),
        ]}
      />,
    )
    await user.click(screen.getByText(/Chips with vendor rules/))
    expect(screen.getByText('data ±0.635 mm, address ±1.016 mm')).toBeInTheDocument()
  })

  // The report rounds a delay to a tenth of a picosecond, which is right for a
  // measured board and wrong here: TI's pair limit is 0.75 ps, and 0.8 ps is
  // not a number in anybody's table.
  it('quotes a delay exactly as its guide wrote it', async () => {
    const user = userEvent.setup()
    renderUI(
      <SupportedChips presets={[preset({ params: { ...preset().params, intra_pair_ps: 0.75 } })]} />,
    )
    await user.click(screen.getByText(/Chips with vendor rules/))
    await user.click(screen.getByText('Example 1, LPDDR4'))
    expect(await screen.findByText('±0.75 ps')).toBeInTheDocument()
    expect(screen.queryByText('±0.8 ps')).not.toBeInTheDocument()
  })

  // A guide that sets no limit leaves the tool's default in force. Showing it
  // as though it were the vendor's figure would put a number under a citation
  // that does not back it.
  it('marks a limit the guide does not state', async () => {
    const user = userEvent.setup()
    renderUI(
      <SupportedChips
        presets={[
          preset({
            params: { ...preset().params, strobe_to_clock_ps: 0, strobe_to_clock_mm: 12.07 },
            unstated: ['strobe_to_clock_mm'],
          }),
        ]}
      />,
    )
    await user.click(screen.getByText(/Chips with vendor rules/))
    await user.click(screen.getByText('Example 1, LPDDR4'))
    expect(await screen.findByText('±12.070 mm')).toBeInTheDocument()
    expect(screen.getByText('not stated, tool default')).toBeInTheDocument()
    // The limits the guide does give are not marked.
    expect(screen.getAllByText('not stated, tool default')).toHaveLength(1)
  })

  it('groups by vendor', async () => {
    const user = userEvent.setup()
    renderUI(
      <SupportedChips
        presets={[
          preset({ id: 'a', name: 'A', vendor: 'One' }),
          preset({ id: 'b', name: 'B', vendor: 'Two' }),
          preset({ id: 'c', name: 'C', vendor: 'One' }),
        ]}
      />,
    )
    await user.click(screen.getByText(/Chips with vendor rules \(3\)/))
    const one = screen.getByText('One').parentElement as HTMLElement
    expect(within(one).getByText('A')).toBeInTheDocument()
    expect(within(one).getByText('C')).toBeInTheDocument()
    expect(within(one).queryByText('B')).not.toBeInTheDocument()
  })

  // The home page is rendered to static HTML with the API blocked, so the
  // catalogue has to be there without one.
  it('falls back to the built-in catalogue before the engine answers', async () => {
    const user = userEvent.setup()
    renderUI(<SupportedChips />)
    await user.click(screen.getByText(new RegExp(`Chips with vendor rules \\(${PRESET_CATALOG.length}\\)`)))
    expect(screen.getByText('STM32MP25x, DDR4')).toBeInTheDocument()
    expect(screen.getByText(/i.MX 8M Mini and i.MX 8M Quad, LPDDR4$/)).toBeInTheDocument()
  })

  it("shows each chip's impedance figures and names a chip it has none for", async () => {
    const user = userEvent.setup()
    renderUI(
      <SupportedChips
        presets={[
          preset({
            impedance: [
              {
                chip: 'Example 1',
                targets: [
                  {
                    kind: 'ddr',
                    label: 'DDR memory',
                    single_ended_ohms: 40,
                    diff_ohms: 80,
                    diff_max_ohms: 90,
                    source: 'Example Design Guide V1.0, table 3-7',
                  },
                  { kind: 'usb2', label: 'USB 2.0', diff_ohms: 90, source: 'Example Design Guide V1.0, table 3-18' },
                ],
              },
              { chip: 'Example 2' },
            ],
          }),
        ]}
      />,
    )
    await user.click(screen.getByText(/Chips with vendor rules/))
    await user.click(screen.getByText('Example 1, LPDDR4'))
    expect(screen.getByText('40 Ω, 80 to 90 Ω differential')).toBeInTheDocument()
    expect(screen.getByText('90 Ω differential')).toBeInTheDocument()
    expect(screen.getByText(/table 3-7; Example Design Guide V1.0, table 3-18/)).toBeInTheDocument()
    expect(screen.getByText(/No figures from the Example 2 guide yet/)).toBeInTheDocument()
  })

  it("puts ST's DDR impedance for the STM32MP25 on its summary line", async () => {
    const user = userEvent.setup()
    renderUI(<SupportedChips presets={PRESET_CATALOG.filter((p) => p.id === 'st-stm32mp25-ddr4')} />)
    await user.click(screen.getByText(/Chips with vendor rules/))
    expect(screen.getByText(/^data .*, 55 Ω, 100 Ω differential$/)).toBeInTheDocument()
  })
})
