import { describe, expect, it } from 'vitest'
import userEvent from '@testing-library/user-event'
import { ProtocolSections, interfaceStatus } from './ProtocolSections'
import { renderUI, screen, within } from '../testRender'
import type { DetectedInterface } from '../lib/analyzerApi'

// A board with six protocols on it ran them together under one heading that
// was really only DDR's. Each one has to say where it ends and what it found.

const iface = (over: Partial<DetectedInterface>): DetectedInterface => ({
  id: 'Ethernet RGMII (ETH1)',
  kind: 'rgmii',
  name: 'Ethernet RGMII (ETH1)',
  nets: 12,
  routed: 12,
  pairs: 0,
  evidence: 'named RGMII-style',
  summary: 'each direction is matched to its own clock',
  actionable: false,
  unroutable: 0,
  geometry: { width_mm: 0.2, gap_mm: 0.2, needs_width: false, needs_gap: false },
  impedance: {} as DetectedInterface['impedance'],
  groups: [
    {
      name: 'transmit',
      reference: 'ETH1.GTX_CLK',
      reference_mm: 40,
      spread_mm: 2,
      limit_mm: 10,
      out_of_tolerance: 0,
      unroutable: 0,
      members: 6,
    },
    {
      name: 'receive',
      reference: 'ETH1.RX_CLK',
      reference_mm: 38,
      spread_mm: 22,
      limit_mm: 10,
      out_of_tolerance: 2,
      unroutable: 0,
      members: 6,
    },
  ],
  ...over,
})

describe('ProtocolSections', () => {
  it('gives each protocol its own section, named and folded', async () => {
    const user = userEvent.setup()
    renderUI(
      <ProtocolSections
        ddr={<div>the DDR tables</div>}
        ddrStatus={{ text: '40 out of tolerance', tone: 'orange' }}
        interfaces={[
          iface({}),
          iface({ id: 'USB', name: 'USB', kind: 'usb2', groups: [], pair_skew: [{ name: 'D', p: 'D+', n: 'D-', skew_mm: 0.1, limit_mm: 0.5, routed: true, in_tolerance: true }] }),
        ]}
      />,
    )
    // DDR opens: it is the one with the most to read.
    const ddr = screen.getByRole('button', { name: /DDR memory/ })
    expect(ddr).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByText('40 out of tolerance')).toBeInTheDocument()

    // The one with something out of tolerance opens too.
    expect(screen.getByRole('button', { name: /Ethernet RGMII/ })).toHaveAttribute(
      'aria-expanded',
      'true',
    )
    // The one that is fine stays folded.
    const usb = screen.getByRole('button', { name: /USB/ })
    expect(usb).toHaveAttribute('aria-expanded', 'false')
    await user.click(usb)
    expect(usb).toHaveAttribute('aria-expanded', 'true')
  })

  it('shows what each group of a protocol is matched to', () => {
    renderUI(<ProtocolSections interfaces={[iface({})]} />)
    const tx = screen.getByText('transmit').closest('tr') as HTMLElement
    expect(within(tx).getByText('ETH1.GTX_CLK')).toBeInTheDocument()
    expect(within(tx).getByText('±10.000 mm')).toBeInTheDocument()
    const rx = screen.getByText('receive').closest('tr') as HTMLElement
    expect(within(rx).getByText('ETH1.RX_CLK')).toBeInTheDocument()
    // Transmit and receive are never matched to each other.
    expect(within(rx).queryByText('ETH1.GTX_CLK')).not.toBeInTheDocument()
  })

  it('leaves out the planner’s own interface and anything with nothing measured', () => {
    renderUI(
      <ProtocolSections
        ddr={<div>the DDR tables</div>}
        interfaces={[
          iface({ id: 'DDR memory', name: 'DDR memory', kind: 'ddr', planner: 'the DDR analyser' }),
          iface({ id: 'Bare', name: 'Bare', groups: [], pair_skew: [] }),
        ]}
      />,
    )
    // DDR appears once, as the section with the tables, not twice.
    expect(screen.getAllByText(/DDR memory/)).toHaveLength(1)
    expect(screen.queryByText('Bare')).not.toBeInTheDocument()
  })

  it('summarises a protocol in one line', () => {
    expect(interfaceStatus(iface({})).text).toBe('2 out of tolerance')
    expect(interfaceStatus(iface({ groups: [] })).text).toMatch(/within tolerance/)
    expect(interfaceStatus(iface({ groups: [], unroutable: 12 })).text).toMatch(/12 of 12/)
  })
})
