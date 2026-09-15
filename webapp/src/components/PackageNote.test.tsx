import { describe, expect, it, vi } from 'vitest'
import userEvent from '@testing-library/user-event'
import { PackageNote, PackageWarning } from './PackageNote'
import { renderUI, screen } from '../testRender'
import { packageStatus } from '../lib/format'
import type { Analysis, PackagePad } from '../lib/analyzerApi'

const pad = (net: string, mm: number, source = 'STM32MP25xxAI'): PackagePad => ({
  net,
  pad: 'U27.' + net,
  default_mm: mm,
  mm,
  source: mm > 0 ? source : '',
})

function analysis(pads: PackagePad[]): Analysis {
  return {
    interface: { controller: 'U27', controller_value: 'STM32MP257FAI3', devices: ['U29'], width_bits: 32, lanes: 4, nets_found: 2, net_prefix: '' },
    groups: [{ name: 'byte lane 0' }],
    package_pads: pads,
  } as unknown as Analysis
}

describe('package lengths', () => {
  it('warns, with the part value and a way to fix it, when the controller has none', async () => {
    const onFix = vi.fn()
    const status = packageStatus(analysis([pad('DQ0', 0), pad('DQ1', 0)]))
    renderUI(<PackageWarning status={status} onFix={onFix} />)
    expect(screen.getByText('No package lengths for U27 (STM32MP257FAI3)')).toBeInTheDocument()
    expect(screen.getByText(/the results may be wrong/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Set package lengths' }))
    expect(onFix).toHaveBeenCalled()
  })

  it('warns when only some pads have one', () => {
    const status = packageStatus(analysis([pad('DQ0', 9.155), pad('DQ1', 0)]))
    expect(status?.state).toBe('partial')
    renderUI(<PackageWarning status={status} />)
    expect(screen.getByText(/missing for 1 of 2 pads on U27/)).toBeInTheDocument()
  })

  it('says nothing alarming when every pad has one, and names the source', () => {
    const status = packageStatus(analysis([pad('DQ0', 9.155), pad('DQ1', 8.851)]))
    renderUI(<PackageWarning status={status} />)
    expect(screen.queryByText(/No package lengths|missing for/)).not.toBeInTheDocument()
    renderUI(<PackageNote status={status} />)
    expect(screen.getByText(/STM32MP25xxAI table/)).toBeInTheDocument()
  })

  it('has no status on a board without DDR', () => {
    expect(packageStatus({ ...analysis([]), groups: [] } as Analysis)).toBeNull()
  })
})
