import { afterEach, describe, expect, it, vi } from 'vitest'
import userEvent from '@testing-library/user-event'
import { ToleranceBar } from './ToleranceBar'
import { renderUI, screen } from '../testRender'
import { setUnit } from '../lib/units'

afterEach(() => setUnit('mm'))

// One group's tolerance, set by dragging. What matters: it hands back
// millimetres whatever the page shows, it says whether the value is the
// default or the user's, and going back to the default is one press.
describe('ToleranceBar', () => {
  it('shows the tolerance in force, and that it is the default', () => {
    renderUI(<ToleranceBar valueMM={0.635} overridden={false} onChange={vi.fn()} label="byte lane 0" />)
    expect(screen.getByText('±0.635 mm')).toBeInTheDocument()
    expect(screen.getByText('default')).toBeInTheDocument()
  })

  it('hands back millimetres when moved, even when the page is in mils', async () => {
    setUnit('mil')
    const onChange = vi.fn()
    renderUI(<ToleranceBar valueMM={0.635} overridden={false} onChange={onChange} label="byte lane 0" />)
    expect(screen.getByText('±25.0 mil')).toBeInTheDocument()

    const thumb = screen.getByRole('slider', { name: 'Tolerance for byte lane 0' })
    thumb.focus()
    await userEvent.keyboard('{ArrowRight}')
    expect(onChange).toHaveBeenCalled()
    const mm = onChange.mock.calls.at(-1)![0] as number
    expect(mm).toBeGreaterThan(0.635)
    expect(mm).toBeLessThan(0.7)
  })

  it('offers to go back to the default once the group has its own', async () => {
    const onChange = vi.fn()
    renderUI(<ToleranceBar valueMM={0.2} overridden onChange={onChange} />)
    await userEvent.click(screen.getByRole('button', { name: 'reset' }))
    expect(onChange).toHaveBeenCalledWith(null)
  })
})
