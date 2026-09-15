/**
 * How far one group may sit from its reference, as a bar to drag.
 *
 * The family defaults -- 1.42 mm for a byte lane, 3.55 mm for address and
 * command -- are where a layout guide starts, not where it ends. A lane routed
 * past a switching supply may be given less; a slower leg more. So each group
 * gets its own, and the bar says whether it is the default or yours.
 *
 * The group is re-judged when the bar is let go, not while it moves: every
 * change re-reads the board, and a request per pixel would be a request per
 * pixel. The value beside it follows the drag, in whichever unit the page is
 * showing.
 */

import { useEffect, useState } from 'react'
import { Badge, Button, Group, Slider, Text, Tooltip } from '@mantine/core'
import { NOWRAP, mm } from '../lib/format'

export interface ToleranceBarProps {
  /** The tolerance in force now, in millimetres. */
  valueMM: number
  /** True when this group has its own tolerance rather than its family's. */
  overridden: boolean
  /** Called with millimetres when the bar is let go, or null to go back to the default. */
  onChange: (mm: number | null) => void
  busy?: boolean
  label?: string
}

export function ToleranceBar({ valueMM, overridden, onChange, busy = false, label }: ToleranceBarProps) {
  const [live, setLive] = useState(valueMM)
  // A new analysis arriving -- this bar's change, or anybody else's -- is the
  // truth; the local value is only for while a drag is in progress.
  useEffect(() => setLive(valueMM), [valueMM])

  // Wide enough to loosen a tight group several times over and to tighten a
  // loose one to almost nothing, without making the useful range a sliver.
  const max = Math.max(1, Math.ceil(Math.max(valueMM * 3, 2.5) * 4) / 4)

  return (
    <Group gap="sm" wrap="nowrap" align="center">
      {label && (
        <Text size="sm" style={{ minWidth: 190 }}>
          {label}
        </Text>
      )}
      <Slider
        style={{ flex: 1, minWidth: 140 }}
        min={0.01}
        max={max}
        step={0.005}
        value={live}
        onChange={setLive}
        onChangeEnd={(v) => {
          if (Math.abs(v - valueMM) > 1e-6) onChange(Math.round(v * 1000) / 1000)
        }}
        label={(v) => `±${mm(v)}`}
        disabled={busy}
        thumbLabel={label ? `Tolerance for ${label}` : 'Tolerance'}
      />
      <Text size="sm" ff="monospace" fw={600} style={{ ...NOWRAP, minWidth: 90, textAlign: 'right' }}>
        ±{mm(live)}
      </Text>
      {overridden ? (
        <Tooltip label="Back to the family's default tolerance" withArrow>
          <Button size="compact-xs" variant="subtle" onClick={() => onChange(null)} disabled={busy}>
            reset
          </Button>
        </Tooltip>
      ) : (
        <Badge size="xs" variant="light" color="gray" style={NOWRAP}>
          default
        </Badge>
      )}
    </Group>
  )
}
