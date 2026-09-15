import { describe, expect, it, vi } from 'vitest'
import { fireEvent, renderUI, screen, waitFor } from '../testRender'
import { HostProvider, type BoardHost } from '../lib/host'
import { ApplyToBoard, NetName, RescanButton, SelectNetsButton } from './HostActions'
import { GroupTable } from './GroupTable'
import type { GroupInfo, MemberInfo } from '../lib/analyzerApi'

// The same components run on the site, where there is no editor, and inside
// KiCad, where there is. On the site none of these controls may appear at all;
// in KiCad each must reach the host with exactly the nets it names.

function fakeHost(over: Partial<BoardHost> = {}): BoardHost {
  return {
    name: 'KiCad',
    selectNets: vi.fn().mockResolvedValue(undefined),
    applyToBoard: vi.fn().mockResolvedValue({ removed: 4, added: 19, message: 'Length-match 2 net(s)' }),
    rescan: vi.fn().mockResolvedValue(undefined),
    ...over,
  }
}

const member = (over: Partial<MemberInfo>): MemberInfo => ({
  net: '/ddr4/DDR_DQ1',
  label: 'DQ1',
  role: 'data',
  routed: true,
  length_mm: 50,
  delay_ps: 300,
  deviation_mm: 0,
  need_mm: 0,
  need_ps: 0,
  in_tolerance: true,
  headroom_mm: 0,
  needs_reroute: false,
  ...over,
})

const group: GroupInfo = {
  name: 'byte lane 0',
  kind: 'byte-lane',
  lane: 0,
  reference: 'DQS0',
  reference_length_mm: 50,
  target_mm: 50,
  target_from_reference_mm: 50,
  tolerance_mm: 0.635,
  spread_mm: 3,
  total_need_mm: 3,
  out_of_tolerance: 2,
  members: [
    member({}),
    member({ net: '/ddr4/DDR_DQ2', label: 'DQ2', in_tolerance: false, need_mm: 2, deviation_mm: -2 }),
    member({ net: '/ddr4/DDR_DQ3', label: 'DQ3', in_tolerance: false, need_mm: 1, deviation_mm: -1 }),
    member({ net: '/ddr4/DDR_DQS0_P', label: 'DQS0_P', in_tolerance: false, reference: true }),
    member({ net: '/ddr4/DDR_DQ4', label: 'DQ4', routed: false, in_tolerance: false }),
  ],
}

describe('host controls in a browser', () => {
  it('render nothing, and a net name is plain text', () => {
    renderUI(
      <>
        <NetName net="/x">DQ1</NetName>
        <SelectNetsButton nets={['/x']}>Select</SelectNetsButton>
        <RescanButton />
        <ApplyToBoard sessionId="s1" />
      </>,
    )
    expect(screen.getByText('DQ1').tagName).not.toBe('BUTTON')
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('leave the group table as it was', () => {
    renderUI(<GroupTable group={group} />)
    expect(screen.queryByRole('button', { name: /select/i })).toBeNull()
  })
})

describe('host controls inside KiCad', () => {
  it('select one net by its full name when its label is clicked', async () => {
    const host = fakeHost()
    renderUI(
      <HostProvider host={host}>
        <GroupTable group={group} />
      </HostProvider>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'DQ2' }))
    await waitFor(() => expect(host.selectNets).toHaveBeenCalledWith(['/ddr4/DDR_DQ2']))
  })

  it("select a group's out-of-tolerance members, not its reference or unrouted nets", async () => {
    const host = fakeHost()
    renderUI(
      <HostProvider host={host}>
        <GroupTable group={group} />
      </HostProvider>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Select the ones out' }))
    await waitFor(() =>
      expect(host.selectNets).toHaveBeenCalledWith(['/ddr4/DDR_DQ2', '/ddr4/DDR_DQ3']),
    )
  })

  it('offer no group selection when every member is matched', () => {
    renderUI(
      <HostProvider host={fakeHost()}>
        <GroupTable group={{ ...group, out_of_tolerance: 0, members: [member({})] }} />
      </HostProvider>,
    )
    expect(screen.queryByRole('button', { name: 'Select the ones out' })).toBeNull()
  })

  it('offer no apply when the host does not provide it', () => {
    renderUI(
      <HostProvider host={fakeHost({ applyToBoard: undefined })}>
        <ApplyToBoard sessionId="s1" />
      </HostProvider>,
    )
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('apply to the board once, and say what the edit was', async () => {
    const host = fakeHost()
    renderUI(
      <HostProvider host={host}>
        <ApplyToBoard sessionId="s1" />
      </HostProvider>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Apply to the board in KiCad' }))
    await screen.findByText(/4 tracks replaced by 19/)
    expect(host.applyToBoard).toHaveBeenCalledWith('s1')
    expect(screen.getByRole('button', { name: 'Applied in KiCad' })).toBeDisabled()
  })

  it('show a refusal and leave the button usable', async () => {
    const host = fakeHost({
      applyToBoard: vi.fn().mockRejectedValue(new Error('3 tracks have changed in KiCad since the analysis')),
    })
    renderUI(
      <HostProvider host={host}>
        <ApplyToBoard sessionId="s1" />
      </HostProvider>,
    )
    const button = screen.getByRole('button', { name: 'Apply to the board in KiCad' })
    fireEvent.click(button)
    await screen.findByText(/3 tracks have changed/)
    expect(button).not.toBeDisabled()
  })
})
