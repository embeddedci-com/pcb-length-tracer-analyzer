import { describe, expect, it, vi } from 'vitest'
import userEvent from '@testing-library/user-event'
import {
  ProtocolNav,
  ProtocolSections,
  interfaceStatus,
  protocolEntries,
  useProtocolSections,
} from './ProtocolSections'
import { renderUI, screen, waitFor, within } from '../testRender'
import { HostProvider, type BoardHost } from '../lib/host'
import type { DetectedInterface } from '../lib/analyzerApi'

// Selecting on the board is an editor's control: it renders nothing on the
// site, and inside KiCad it has to reach the host with exactly the nets it
// names. The same rule DDR's group table follows.
function fakeHost(): BoardHost {
  return {
    name: 'KiCad',
    selectNets: vi.fn().mockResolvedValue(undefined),
    applyToBoard: vi.fn().mockResolvedValue({ removed: 0, added: 0, message: '' }),
    rescan: vi.fn().mockResolvedValue(undefined),
  }
}

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
    // Every one starts folded, DDR too: it alone runs to several screens.
    for (const name of [/DDR memory/, /Ethernet RGMII/, /USB/]) {
      expect(screen.getByRole('button', { name })).toHaveAttribute('aria-expanded', 'false')
    }
    expect(screen.getByText('40 out of tolerance')).toBeInTheDocument()
    const usb = screen.getByRole('button', { name: /USB/ })
    await user.click(usb)
    expect(usb).toHaveAttribute('aria-expanded', 'true')
  })

  it('opens the section picked in the list, and only that one', async () => {
    const user = userEvent.setup()
    const scrolled = vi.fn()
    Element.prototype.scrollIntoView = scrolled
    const interfaces = [
      iface({}),
      iface({ id: 'USB', name: 'USB', kind: 'usb2', groups: [], unroutable: 2, nets: 2, pair_skew: [{ name: 'D', p: 'D+', n: 'D-', skew_mm: 0, limit_mm: 0.5, routed: false, in_tolerance: false }] }),
    ]
    const ddrStatus = { text: 'every matched net is within tolerance', tone: 'teal', short: 'ok' }
    function Page() {
      const s = useProtocolSections()
      const entries = protocolEntries(interfaces, ddrStatus)
      return (
        <>
          <ProtocolNav
            entries={entries}
            open={s.open}
            onJump={s.jump}
            onOpenAll={() => s.setOpen(entries.map((e) => e.id))}
            onCloseAll={() => s.setOpen([])}
          />
          <ProtocolSections
            ddr={<div>the DDR tables</div>}
            ddrStatus={ddrStatus}
            interfaces={interfaces}
            open={s.open}
            onOpenChange={s.setOpen}
            withNav
          />
        </>
      )
    }
    renderUI(<Page />)
    const nav = within(screen.getByText('Protocols').closest('.mantine-Card-root') as HTMLElement)
    // Each says where it stands in a word or two.
    expect(nav.getByRole('button', { name: /DDR memory\s*ok/ })).toBeInTheDocument()
    expect(nav.getByRole('button', { name: /Ethernet RGMII \(ETH1\)\s*2 out/ })).toBeInTheDocument()
    expect(nav.getByRole('button', { name: /USB\s*not routed/ })).toBeInTheDocument()

    const section = (name: RegExp) =>
      screen.getAllByRole('button', { name }).find((b) => b.hasAttribute('aria-expanded'))!
    await user.click(section(/DDR memory/))
    await user.click(nav.getByRole('button', { name: /Ethernet RGMII/ }))
    expect(section(/Ethernet RGMII/)).toHaveAttribute('aria-expanded', 'true')
    // The one open before is folded, so the page does not grow with each jump.
    expect(section(/DDR memory/)).toHaveAttribute('aria-expanded', 'false')
    await waitFor(() => expect(scrolled).toHaveBeenCalled())

    await user.click(nav.getByRole('button', { name: /Open all/ }))
    for (const n of [/DDR memory/, /Ethernet RGMII/, /USB/]) {
      expect(section(n)).toHaveAttribute('aria-expanded', 'true')
    }
    await user.click(nav.getByRole('button', { name: /Close all/ }))
    expect(section(/USB/)).toHaveAttribute('aria-expanded', 'false')
  })

  it('shows what each group is matched to, and never the other direction', () => {
    renderUI(<ProtocolSections interfaces={[iface({})]} open={['Ethernet RGMII (ETH1)']} />)
    const tx = screen.getByText('transmit').closest('div') as HTMLElement
    expect(within(tx).getByText(/matched to ETH1.GTX_CLK/)).toBeInTheDocument()
    expect(within(tx).getByText(/±10.000 mm/)).toBeInTheDocument()
    const rx = screen.getByText('receive').closest('div') as HTMLElement
    expect(within(rx).getByText(/matched to ETH1.RX_CLK/)).toBeInTheDocument()
    expect(within(rx).queryByText(/GTX_CLK/)).not.toBeInTheDocument()
  })

  // A count of "2 out" with no way to see which two is not actionable.
  it('names the nets that are out and offers to select them', () => {
    const rows = [
      { net: '/eth/RXD0', label: 'RXD0', role: '', routed: true, length_mm: 40, delay_ps: 0, deviation_mm: 0.4, need_mm: 0, need_ps: 0, in_tolerance: true, headroom_mm: 0, needs_reroute: false },
      { net: '/eth/RXD1', label: 'RXD1', role: '', routed: true, length_mm: 62, delay_ps: 0, deviation_mm: 22, need_mm: 0, excess_mm: 12, need_ps: 0, in_tolerance: false, headroom_mm: 0, needs_reroute: false },
      { net: '/eth/RX_CLK', label: 'RX_CLK', role: 'reference', routed: true, length_mm: 38, delay_ps: 0, deviation_mm: 0, need_mm: 0, need_ps: 0, in_tolerance: true, headroom_mm: 0, needs_reroute: false, through: ['R80'] },
    ]
    renderUI(
      <ProtocolSections
        open={['Ethernet RGMII (ETH1)']}
        interfaces={[
          iface({
            groups: [
              { name: 'receive', reference: 'ETH1.RX_CLK', reference_mm: 38, spread_mm: 24, limit_mm: 10, out_of_tolerance: 1, unroutable: 0, members: 3, rows },
            ],
          }),
        ]}
      />,
    )
    expect(screen.getByText('RXD1')).toBeInTheDocument()
    expect(screen.getByText('+22.000 mm')).toBeInTheDocument()
    expect(screen.getByText('shorten 12.000 mm')).toBeInTheDocument()
    // The net that passes through a series part says so.
    expect(screen.getByText('through R80')).toBeInTheDocument()
    // No editor here, so no select button -- the same as a DDR group.
    expect(screen.queryByRole('button', { name: /Select the ones out/ })).not.toBeInTheDocument()
  })

  it('selects exactly the nets that are out, inside an editor', async () => {
    const user = userEvent.setup()
    const host = fakeHost()
    const rows = [
      { net: '/eth/RXD0', label: 'RXD0', role: '', routed: true, length_mm: 40, delay_ps: 0, deviation_mm: 0.4, need_mm: 0, need_ps: 0, in_tolerance: true, headroom_mm: 0, needs_reroute: false },
      { net: '/eth/RXD1', label: 'RXD1', role: '', routed: true, length_mm: 62, delay_ps: 0, deviation_mm: 22, need_mm: 0, excess_mm: 12, need_ps: 0, in_tolerance: false, headroom_mm: 0, needs_reroute: false },
      { net: '/eth/RXD2', label: 'RXD2', role: '', routed: false, length_mm: 0, delay_ps: 0, deviation_mm: 0, need_mm: 0, need_ps: 0, in_tolerance: false, headroom_mm: 0, needs_reroute: false },
      { net: '/eth/RX_CLK', label: 'RX_CLK', role: 'reference', routed: true, length_mm: 38, delay_ps: 0, deviation_mm: 0, need_mm: 0, need_ps: 0, in_tolerance: true, headroom_mm: 0, needs_reroute: false },
    ]
    renderUI(
      <HostProvider host={host}>
        <ProtocolSections
          open={['Ethernet RGMII (ETH1)']}
          interfaces={[
            iface({
              groups: [
                { name: 'receive', reference: 'ETH1.RX_CLK', reference_mm: 38, spread_mm: 24, limit_mm: 10, out_of_tolerance: 1, unroutable: 1, members: 4, rows },
              ],
            }),
          ]}
        />
      </HostProvider>,
    )
    await user.click(screen.getByRole('button', { name: /Select the ones out/ }))
    // The one that is out. Not the reference, and not one with no copper to
    // select.
    await waitFor(() => expect(host.selectNets).toHaveBeenCalledWith(['/eth/RXD1']))
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
