/**
 * Whether the lengths on the page include the package.
 *
 * DDR limits are on the path from die to die. Without the package (the wiring
 * inside the chip) a length is board copper only: several millimetres short
 * per net, by a different amount on each, so nets can read out of tolerance
 * when they are not. The warning says so where the results are, with a way to
 * fix it.
 */

import { Alert, Button, Group, Text } from '@mantine/core'
import type { PackageStatus } from '../lib/format'

function who(s: PackageStatus) {
  return s.value ? `${s.controller} (${s.value})` : s.controller
}

/** A short line: where the lengths came from, or that they are missing. */
export function PackageNote({ status }: { status: PackageStatus | null }) {
  if (!status) return null
  if (status.state === 'full') {
    return (
      <Text size="sm" c="dimmed">
        Lengths include {status.controller}&rsquo;s package (the wiring inside the chip): {status.source}.
      </Text>
    )
  }
  return (
    <Text size="sm" c="orange">
      {status.state === 'none'
        ? `Lengths do not include ${status.controller}'s package, so they may be off by several mm.`
        : `${status.total - status.withLength} of ${status.total} pads on ${status.controller} have no package length.`}
    </Text>
  )
}

/** The warning at the top of the results, when package lengths are missing. */
export function PackageWarning({
  status,
  onFix,
}: {
  status: PackageStatus | null
  onFix?: () => void
}) {
  if (!status || status.state === 'full') return null
  const none = status.state === 'none'
  return (
    <Alert
      color="orange"
      variant="light"
      title={
        none
          ? `No package lengths for ${who(status)}`
          : `Package lengths missing for ${status.total - status.withLength} of ${status.total} pads on ${who(status)}`
      }
    >
      <Group justify="space-between" align="flex-end" gap="sm">
        <Text size="sm" style={{ flex: 1, minWidth: 240 }}>
          DDR limits include the chip package (the wiring inside the chip, several mm per net). Without it the lengths
          below are board copper only, so the results may be wrong: nets can show as out of tolerance when they are
          not, or the other way round. Choose the part or enter the lengths under Rules.
        </Text>
        {onFix && (
          <Button size="xs" variant="light" color="orange" onClick={onFix}>
            Set package lengths
          </Button>
        )}
      </Group>
    </Alert>
  )
}
