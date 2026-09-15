import { describe, expect, it, vi } from 'vitest'
import userEvent from '@testing-library/user-event'
import { InterfacePicker } from './InterfacePicker'
import { renderUI, screen, within } from '../testRender'
import type { DetectedInterface, FamilyInfo } from '../lib/analyzerApi'

// Picking what to work on, and saying what it really is.
//
// Recognition is by net name, which is a reading rather than a proof, so the
// two things that must survive to the screen are the evidence it was read from
// and the user's ability to overrule it. Both are wiring: a checkbox that never
// reaches its handler, or an override collected into a draft that Apply then
// fails to hand back, looks perfectly fine in a screenshot.

function iface(over: Partial<DetectedInterface> = {}): DetectedInterface {
  return {
    id: 'rgmii:0',
    kind: 'rgmii',
    name: 'Ethernet (RGMII)',
    nets: 12,
    routed: 12,
    pairs: 0,
    evidence: 'RGMII_TXD0, RGMII_RXD0, RGMII_TXC',
    summary: '2 out of tolerance',
    actionable: true,
    unroutable: 0,
    geometry: { width_mm: 0.15, gap_mm: 0, measured: ['width'], needs_width: false, needs_gap: false },
    impedance: { computed: true, microstrip: true, in_range: true, single_ended_ohms: 52, layer: 'F.Cu' },
    ...over,
  }
}

const families: FamilyInfo[] = [
  {
    kind: 'rgmii',
    label: 'Ethernet RGMII',
    summary: 'Gigabit MII',
    tx: ['TXD0..3', 'TX_CTL', 'TXC'],
    rx: ['RXD0..3', 'RX_CTL', 'RXC'],
    single_ended_ohms: 50,
  },
  { kind: 'usb2', label: 'USB 2.0', summary: 'High speed', diff_ohms: 90, differential: true },
]

describe('InterfacePicker', () => {
  // Mantine keeps a collapsed accordion panel mounted, so a panel's contents
  // are queryable without opening it -- but every interface on screen
  // contributes its own copy of every label, which is why the tests that look
  // inside a panel render one interface at a time.
  it('shows what each interface was recognised by', () => {
    renderUI(<InterfacePicker interfaces={[iface()]} selected={[]} onChange={vi.fn()} />)
    expect(screen.getByText(/Recognised by:/)).toBeInTheDocument()
    expect(screen.getByText(/RGMII_TXD0, RGMII_RXD0, RGMII_TXC/)).toBeInTheDocument()
  })

  it('says "You said" once the user has assigned it', () => {
    renderUI(
      <InterfacePicker
        interfaces={[iface({ assigned: true, evidence: 'assigned by you' })]}
        selected={[]}
        onChange={vi.fn()}
      />,
    )
    expect(screen.getByText(/You said:/)).toBeInTheDocument()
  })

  it('hands back the whole selection when a box is ticked', async () => {
    const onChange = vi.fn()
    const a = iface({ id: 'a', name: 'A' })
    const b = iface({ id: 'b', name: 'B' })
    renderUI(<InterfacePicker interfaces={[a, b]} selected={['a']} onChange={onChange} />)

    await userEvent.click(screen.getByLabelText('Work on B'))
    // Not just the one clicked: the caller replaces its list wholesale.
    expect(onChange).toHaveBeenCalledWith(['a', 'b'])
  })

  it('drops one from the selection when it is unticked', async () => {
    const onChange = vi.fn()
    renderUI(
      <InterfacePicker
        interfaces={[iface({ id: 'a', name: 'A' }), iface({ id: 'b', name: 'B' })]}
        selected={['a', 'b']}
        onChange={onChange}
      />,
    )
    await userEvent.click(screen.getByLabelText('Work on A'))
    expect(onChange).toHaveBeenCalledWith(['b'])
  })

  it('counts how many have something to fix, not just how many exist', () => {
    renderUI(
      <InterfacePicker
        interfaces={[iface({ id: 'a' }), iface({ id: 'b', actionable: false, summary: 'all matched' })]}
        selected={[]}
        onChange={vi.fn()}
      />,
    )
    expect(screen.getByText('2 found')).toBeInTheDocument()
    expect(screen.getByText('1 with something to fix')).toBeInTheDocument()
  })

  // A figure that came off the board and one the user has to invent are
  // different requests, and the label is the only thing that says which.
  it('distinguishes a measured width from one it needs to be told', () => {
    const { unmount } = renderUI(
      <InterfacePicker interfaces={[iface()]} selected={[]} onChange={vi.fn()} families={families} />,
    )
    expect(screen.getByText('measured — confirm')).toBeInTheDocument()
    unmount()

    renderUI(
      <InterfacePicker
        interfaces={[
          iface({ routed: 0, geometry: { width_mm: 0, gap_mm: 0, measured: [], needs_width: true, needs_gap: false } }),
        ]}
        selected={[]}
        onChange={vi.fn()}
        families={families}
      />,
    )
    expect(screen.getByText('nothing routed; please give it')).toBeInTheDocument()
  })

  it('only asks about pair spacing when there are pairs', () => {
    const { unmount } = renderUI(
      <InterfacePicker interfaces={[iface({ pairs: 0 })]} selected={[]} onChange={vi.fn()} families={families} />,
    )
    expect(screen.queryByText('Gap between the pair')).not.toBeInTheDocument()
    unmount()

    renderUI(
      <InterfacePicker interfaces={[iface({ pairs: 1 })]} selected={[]} onChange={vi.fn()} families={families} />,
    )
    expect(screen.getByText('Gap between the pair')).toBeInTheDocument()
  })

  // The point of the whole override path: the user's answer has to come back
  // out, and only when they press Apply -- re-reading the board per keystroke
  // is what the draft exists to avoid.
  it('holds a reassignment until Apply, then hands it over', async () => {
    const onOverride = vi.fn()
    renderUI(
      <InterfacePicker
        interfaces={[iface()]}
        selected={[]}
        onChange={vi.fn()}
        families={families}
        onOverride={onOverride}
      />,
    )
    const apply = screen.getByRole('button', { name: 'Apply' })
    expect(apply).toBeDisabled()

    // A collapsed panel is display:none, and a dropdown only opens for a
    // control that is on screen, so this one has to be opened first.
    await userEvent.click(screen.getByText('Ethernet (RGMII)'))
    // Addressed by what it currently reads as: Mantine labels both the input
    // and its dropdown with the field's label, so the label alone is ambiguous.
    await userEvent.click(screen.getByDisplayValue('Ethernet RGMII'))
    await userEvent.click(await screen.findByRole('option', { name: 'USB 2.0' }))

    expect(onOverride).not.toHaveBeenCalled()
    expect(apply).toBeEnabled()

    await userEvent.click(apply)
    expect(onOverride).toHaveBeenCalledWith([{ id: 'rgmii:0', kind: 'usb2' }])
  })

  it('carries an earlier override through a new edit rather than dropping it', async () => {
    const onOverride = vi.fn()
    renderUI(
      <InterfacePicker
        interfaces={[iface()]}
        selected={[]}
        onChange={vi.fn()}
        families={families}
        overrides={[{ id: 'rgmii:0', kind: 'usb2' }]}
        onOverride={onOverride}
      />,
    )
    const width = screen.getByLabelText('Track width')
    await userEvent.clear(width)
    await userEvent.type(width, '0.2')
    await userEvent.click(screen.getByRole('button', { name: 'Apply' }))

    expect(onOverride).toHaveBeenCalledWith([{ id: 'rgmii:0', kind: 'usb2', width_mm: 0.2 }])
  })

  it('says what the family is usually drawn to when nothing is routed', () => {
    renderUI(
      <InterfacePicker
        interfaces={[
          iface({
            name: 'Front camera link',
            kind: 'usb2',
            pairs: 1,
            routed: 0,
            impedance: { computed: false, microstrip: true, in_range: true },
          }),
        ]}
        selected={[]}
        onChange={vi.fn()}
        families={families}
      />,
    )
    expect(screen.getByText(/usually drawn to 90 Ω differential/)).toBeInTheDocument()
  })

  it('shows the measured impedance beside what the family wants', () => {
    renderUI(
      <InterfacePicker interfaces={[iface()]} selected={[]} onChange={vi.fn()} families={families} />,
    )
    expect(screen.getByText('52 Ω')).toBeInTheDocument()
    expect(screen.getByText('(usually 50 Ω)')).toBeInTheDocument()
    // And that it is an estimate, every time, not in a tooltip.
    expect(screen.getByText(/not a field solve/)).toBeInTheDocument()
  })

  it('lists the matched groups with their spread against the limit', () => {
    renderUI(
      <InterfacePicker
        interfaces={[
          iface({
            groups: [
              {
                name: 'TX',
                reference: 'TXC',
                reference_mm: 30,
                spread_mm: 1.2,
                limit_mm: 0.635,
                out_of_tolerance: 2,
                unroutable: 1,
              },
            ],
          }),
        ]}
        selected={[]}
        onChange={vi.fn()}
      />,
    )
    const row = screen.getByText('TX').closest('tr')!
    expect(within(row).getByText('1.200 mm')).toBeInTheDocument()
    expect(within(row).getByText('0.635 mm')).toBeInTheDocument()
    expect(within(row).getByText('2 (+1 unrouted)')).toBeInTheDocument()
  })

  it('says a pair is not joined up rather than showing a skew of zero', () => {
    renderUI(
      <InterfacePicker
        interfaces={[
          iface({
            pairs: 1,
            pair_skew: [
              {
                name: 'USB_D',
                p: 'USB_D_P',
                n: 'USB_D_N',
                routed: false,
                skew_mm: 0,
                limit_mm: 0.15,
                in_tolerance: false,
              },
            ],
          }),
        ]}
        selected={[]}
        onChange={vi.fn()}
      />,
    )
    expect(screen.getByText('not joined up yet')).toBeInTheDocument()
  })
})
