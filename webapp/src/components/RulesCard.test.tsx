import { describe, expect, it } from 'vitest'
import userEvent from '@testing-library/user-event'
import { RulesCard } from './RulesCard'
import { renderUI, screen, within } from '../testRender'
import type { DesignRules } from '../lib/analyzerApi'

// The tool applies numbers of its own on top of the board's, and KiCad will
// tell you why a track is the width it is. These check that this does too --
// in particular that a figure the user chose is not passed off as one the
// board set, and that a rule this cannot read is visible rather than buried.

const rules: DesignRules = {
  min_clearance_mm: 0.1,
  min_track_width_mm: 0.089,
  edge_clearance_mm: 0.3,
  classes: [
    { name: 'DDR', clearance_mm: 0.1, track_width_mm: 0.09, nets: 61 },
    { name: 'Default', clearance_mm: 0.2, track_width_mm: 0.25 },
  ],
  custom: [
    {
      name: 'bga_fanout_tight',
      condition: "A.intersectsCourtyard('U3')",
      clearance_mm: 0.1,
      applied: true,
    },
    {
      name: 'ddr_fanout_tight_all_copper',
      condition: "A.insideArea('fanout')",
      applied: false,
      why: 'this does not implement insideArea(), so the rule is left alone',
    },
  ],
  effective: [
    { what: 'track width', value_mm: 0.09, source: 'net class DDR' },
    { what: 'clearance away from the components', value_mm: 0.2, source: 'your setting' },
  ],
}

describe('RulesCard', () => {
  it('says what each figure is and who set it', () => {
    renderUI(<RulesCard rules={rules} />)
    const width = screen.getByText('track width').closest('tr')!
    expect(within(width).getByText('0.090 mm')).toBeInTheDocument()
    expect(within(width).getByText('net class DDR')).toBeInTheDocument()

    // The distinction that matters: this one is the tool's, not the board's.
    const open = screen.getByText('clearance away from the components').closest('tr')!
    expect(within(open).getByText('your setting')).toBeInTheDocument()
  })

  it('counts the custom rules it could read, and flags the ones it could not', async () => {
    renderUI(<RulesCard rules={rules} />)
    // Two of the two in this fixture; the count is of what it read, not of
    // what the file holds.
    expect(screen.getByText('1 of 2 custom rules read')).toBeInTheDocument()
    expect(screen.getByText('1 left alone')).toBeInTheDocument()

    await userEvent.click(screen.getByText('Custom rules from the .kicad_dru'))
    const row = screen.getByText('ddr_fanout_tight_all_copper').closest('tr')!
    expect(within(row).getByText('left alone')).toBeInTheDocument()
    // Shown as written, so it can be read against the file.
    expect(within(row).getByText("A.insideArea('fanout')")).toBeInTheDocument()
  })

  it('lists the net classes with the board minimums they sit above', async () => {
    renderUI(<RulesCard rules={rules} />)
    await userEvent.click(
      screen.getByText(/The board’s own setup: minimum clearance 0.100 mm/),
    )
    const ddr = screen.getByText('DDR').closest('tr')!
    expect(within(ddr).getByText('0.090 mm')).toBeInTheDocument()
    expect(within(ddr).getByText('61')).toBeInTheDocument()
  })

  it('shows nothing at all when the board came without rules', () => {
    renderUI(<RulesCard />)
    expect(screen.queryByText('Rules, and where they come from')).not.toBeInTheDocument()
  })
})
