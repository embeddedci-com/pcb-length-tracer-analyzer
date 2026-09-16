/**
 * The chips this tool has published rules for, on the page before a board.
 *
 * Somebody deciding whether to upload a board wants to know whether their part
 * is one the tool knows, and a reviewer wants to see which document a number
 * came from before trusting it. Both are answered here: every preset, grouped
 * by vendor, each with the limits it sets and the guide and table they were
 * copied from.
 *
 * Folded away by default. It is a reference, not something to read on the way
 * to uploading a board.
 */

import { Accordion, Anchor, Badge, Group, Stack, Table, Text, Title } from '@mantine/core'
import type { Preset } from '../lib/analyzerApi'
import { PRESET_CATALOG } from '../lib/presets.generated'
import { mm } from '../lib/format'

/**
 * A delay exactly as its guide wrote it.
 *
 * Not the report's ps(), which rounds to a tenth: that is right for a measured
 * board, where a hundredth of a picosecond is noise, and wrong for quoting a
 * table, where TI's 0.75 ps would come out as 0.8 and no longer be the number
 * in the document beside it.
 */
function delay(v: number): string {
  return `${Number(v.toFixed(2))} ps`
}

/**
 * One limit, in the unit its guide used.
 *
 * A guide states a limit as a length or as a delay, never both, so whichever
 * is set is the vendor's own number and the other is zero.
 */
function limit(lengthMM: number, delayPS: number): string {
  return lengthMM > 0 ? `±${mm(lengthMM)}` : `±${delay(delayPS)}`
}

const ROWS: { label: string; mm: keyof Preset['params']; ps: keyof Preset['params'] }[] = [
  { label: 'Data to strobe', mm: 'data_to_strobe_mm', ps: 'data_to_strobe_ps' },
  { label: 'Address and command to clock', mm: 'address_to_clock_mm', ps: 'address_to_clock_ps' },
  { label: 'Strobe to clock', mm: 'strobe_to_clock_mm', ps: 'strobe_to_clock_ps' },
  { label: 'Pair (P to N)', mm: 'intra_pair_mm', ps: 'intra_pair_ps' },
]

/** Whether this row's figure is the tool's default rather than the guide's. */
function unstated(preset: Preset, key: keyof Preset['params']): boolean {
  return (preset.unstated ?? []).includes(key)
}

/** The headline numbers, so the row says something without being opened. */
function summary(p: Preset): string {
  return `data ${limit(p.params.data_to_strobe_mm, p.params.data_to_strobe_ps)}, address ${limit(
    p.params.address_to_clock_mm,
    p.params.address_to_clock_ps,
  )}`
}

function Chip({ preset }: { preset: Preset }) {
  return (
    <Accordion.Item value={preset.id}>
      <Accordion.Control>
        <Group gap="xs" wrap="wrap">
          <Text size="sm" fw={600}>
            {preset.name}
          </Text>
          <Badge size="xs" variant="light">
            {preset.memory}
          </Badge>
        </Group>
        <Text size="xs" c="dimmed">
          {summary(preset)}
        </Text>
      </Accordion.Control>
      <Accordion.Panel>
        <Stack gap="xs">
          <Table verticalSpacing={4} withTableBorder>
            <Table.Tbody>
              {ROWS.map((r) => (
                <Table.Tr key={r.label}>
                  <Table.Td>
                    <Text size="sm">{r.label}</Text>
                  </Table.Td>
                  <Table.Td ta="right">
                    <Text size="sm" ff="monospace">
                      {limit(preset.params[r.mm], preset.params[r.ps])}
                    </Text>
                    {unstated(preset, r.mm) && (
                      <Text size="xs" c="dimmed">
                        not stated, tool default
                      </Text>
                    )}
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
          {preset.parts && preset.parts.length > 0 && (
            <Text size="xs" c="dimmed">
              Parts: {preset.parts.join(', ')}
            </Text>
          )}
          <Text size="xs" c="dimmed">
            Source: {preset.source}
            {preset.url && (
              <>
                {' '}
                <Anchor size="xs" href={preset.url} target="_blank" rel="noreferrer">
                  open
                </Anchor>
              </>
            )}
          </Text>
          {preset.note && (
            <Text size="xs" c="dimmed">
              {preset.note}
            </Text>
          )}
        </Stack>
      </Accordion.Panel>
    </Accordion.Item>
  )
}

export function SupportedChips({
  /** The engine's list when it has answered, the built-in catalogue until then. */
  presets = PRESET_CATALOG,
}: {
  presets?: Preset[]
}) {
  if (presets.length === 0) return null

  const byVendor = new Map<string, Preset[]>()
  for (const p of presets) byVendor.set(p.vendor, [...(byVendor.get(p.vendor) ?? []), p])
  const vendors = [...byVendor.keys()]

  return (
    <Accordion variant="contained">
      <Accordion.Item value="chips">
        <Accordion.Control>
          <Text fw={600} size="sm">
            Chips with vendor rules ({presets.length})
          </Text>
          <Text size="xs" c="dimmed">
            {vendors.join(', ')}. Pick one in the rules to set every limit at once.
          </Text>
        </Accordion.Control>
        <Accordion.Panel>
          <Stack gap="md">
            {vendors.map((v) => (
              <div key={v}>
                <Title order={6} mb={4}>
                  {v}
                </Title>
                <Accordion variant="separated" chevronPosition="left">
                  {(byVendor.get(v) ?? []).map((p) => (
                    <Chip key={p.id} preset={p} />
                  ))}
                </Accordion>
              </div>
            ))}
            <Text size="xs" c="dimmed">
              Every number is the vendor&rsquo;s own, for the memory type named, except the few
              marked as the tool&rsquo;s default where a guide sets no limit. Where a guide asks for
              something this tool cannot express, the chip says so.
            </Text>
          </Stack>
        </Accordion.Panel>
      </Accordion.Item>
    </Accordion>
  )
}
