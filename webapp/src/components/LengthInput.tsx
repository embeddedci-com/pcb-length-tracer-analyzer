/**
 * A length field that speaks the reader's unit and stores millimetres.
 *
 * The toggle in the header changed every figure on the page and none of the
 * fields, so somebody working in mils read "25.0 mil" in a table and typed into
 * a box that said "0.635 mm". Every field that takes a length goes through this
 * instead: it shows and accepts the chosen unit, and hands millimetres back, so
 * nothing behind it -- the parameters, the API, the saved session -- ever sees
 * anything but millimetres.
 */

import { NumberInput, type NumberInputProps } from '@mantine/core'
import { MM_PER_MIL, useUnit } from '../lib/units'

export interface LengthInputProps
  extends Omit<NumberInputProps, 'value' | 'onChange' | 'suffix' | 'step' | 'decimalScale' | 'max' | 'min'> {
  /** In millimetres, or empty. */
  value: number | '' | undefined
  /** Called with millimetres, or with '' when the field is cleared. */
  onChange: (mm: number | '') => void
  /** In millimetres. */
  step?: number
  min?: number
  max?: number
}

export function LengthInput({ value, onChange, step = 0.01, min, max, ...rest }: LengthInputProps) {
  const unit = useUnit()
  const mil = unit === 'mil'
  const toShown = (v: number) => (mil ? v / MM_PER_MIL : v)
  return (
    <NumberInput
      {...rest}
      suffix={mil ? ' mil' : ' mm'}
      // A tenth of a mil is the same order as a micrometre; three decimals of
      // a mil would be a pretence of precision nobody can draw.
      decimalScale={mil ? 1 : 3}
      step={mil ? Math.max(0.1, Math.round(toShown(step) * 10) / 10) : step}
      min={min === undefined ? undefined : toShown(min)}
      max={max === undefined ? undefined : toShown(max)}
      value={value === '' || value === undefined ? '' : Math.round(toShown(value) * 1000) / 1000}
      onChange={(v) => {
        const n = typeof v === 'number' ? v : Number.parseFloat(String(v))
        if (!Number.isFinite(n)) {
          onChange('')
          return
        }
        onChange(mil ? n * MM_PER_MIL : n)
      }}
    />
  )
}
