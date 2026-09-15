/**
 * Millimetres or mils, for the whole app.
 *
 * Sits in the page header rather than in a settings panel because it is not a
 * setting about the work -- it changes nothing the tool does, only how it says
 * it -- and because somebody reading a datasheet in mils wants to switch in the
 * middle of reading a table, not before they start.
 */

import { SegmentedControl } from '@mantine/core'
import { setUnit, useUnit } from '../lib/units'

export function UnitToggle() {
  const unit = useUnit()
  return (
    <SegmentedControl
      size="xs"
      value={unit}
      onChange={(v) => setUnit(v === 'mil' ? 'mil' : 'mm')}
      data={[
        { value: 'mm', label: 'mm' },
        { value: 'mil', label: 'mil' },
      ]}
      aria-label="Units"
    />
  )
}
