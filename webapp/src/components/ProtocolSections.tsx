/**
 * The measurements, one protocol at a time.
 *
 * A board with DDR, Ethernet, MIPI, PCIe, eMMC and USB on it produced one
 * running page: five DDR tables under a heading called "Groups", then a
 * differential pair card that was also DDR's, then a to-do list covering
 * everything. Nothing said where one protocol ended and the next began, and
 * the protocols that are not DDR had no measurements on the page at all.
 *
 * So each protocol is its own section, and each says in its own header what it
 * found. DDR opens by default because it is the one with a planner behind it
 * and the most to read; the rest open when they have something to fix, and are
 * folded away when they do not, which is what keeps the page short on a board
 * where most interfaces are fine.
 */

import { Accordion, Badge, Card, Group, Stack, Table, Text, Title } from '@mantine/core'
import type { DetectedInterface, GroupSkewInfo, PairSkewInfo } from '../lib/analyzerApi'
import { mm } from '../lib/format'

/** The one-line state of a protocol, for its header. */
export function interfaceStatus(i: DetectedInterface): { text: string; tone: string } {
  const out = (i.groups ?? []).reduce((n, g) => n + g.out_of_tolerance, 0)
  const pairsOut = (i.pair_skew ?? []).filter((p) => p.routed && !p.in_tolerance).length
  if (out > 0 || pairsOut > 0) {
    const bits = []
    if (out > 0) bits.push(`${out} out of tolerance`)
    if (pairsOut > 0) bits.push(`${pairsOut} pair${pairsOut === 1 ? '' : 's'} out`)
    return { text: bits.join(', '), tone: 'orange' }
  }
  if (i.unroutable > 0) {
    return { text: `${i.unroutable} of ${i.nets} nets not routed end to end`, tone: 'gray' }
  }
  return { text: 'every net measured is within tolerance', tone: 'teal' }
}

function GroupRows({ groups }: { groups: GroupSkewInfo[] }) {
  return (
    <Table.ScrollContainer minWidth={520}>
      <Table verticalSpacing={6}>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Group</Table.Th>
            <Table.Th>Matched to</Table.Th>
            <Table.Th>Nets</Table.Th>
            <Table.Th>Spread</Table.Th>
            <Table.Th>Tolerance</Table.Th>
            <Table.Th>Out</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {groups.map((g) => (
            <Table.Tr key={g.name}>
              <Table.Td>
                <Text size="sm" fw={500}>
                  {g.name}
                </Text>
                {g.why && (
                  <Text size="xs" c="dimmed">
                    {g.why}
                  </Text>
                )}
              </Table.Td>
              <Table.Td>
                <Text size="sm">{g.reference || 'its longest net'}</Text>
                {g.reference_mm > 0 && (
                  <Text size="xs" c="dimmed" ff="monospace">
                    {mm(g.reference_mm)}
                  </Text>
                )}
              </Table.Td>
              <Table.Td>
                <Text size="sm">{g.members ?? 0}</Text>
              </Table.Td>
              <Table.Td>
                <Text size="sm" ff="monospace">
                  {mm(g.spread_mm)}
                </Text>
              </Table.Td>
              <Table.Td>
                <Text size="sm" ff="monospace">
                  {g.limit_mm > 0 ? `±${mm(g.limit_mm)}` : 'none'}
                </Text>
              </Table.Td>
              <Table.Td>
                {g.out_of_tolerance > 0 ? (
                  <Badge size="sm" color="orange" variant="light">
                    {g.out_of_tolerance}
                  </Badge>
                ) : (
                  <Text size="sm" c="dimmed">
                    —
                  </Text>
                )}
              </Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
    </Table.ScrollContainer>
  )
}

export function PairRows({ pairs, note }: { pairs: PairSkewInfo[]; note?: string }) {
  return (
    <Card withBorder padding="sm">
      <Title order={6}>Differential pair skew</Title>
      {note && (
        <Text size="xs" c="dimmed" mb="xs">
          {note}
        </Text>
      )}
      <Table.ScrollContainer minWidth={320}>
        <Table verticalSpacing={4}>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Pair</Table.Th>
              <Table.Th>P to N difference</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {pairs.map((p) => (
              <Table.Tr key={p.name || `${p.p}-${p.n}`}>
                <Table.Td>
                  <Text size="sm">{p.name || p.p}</Text>
                </Table.Td>
                <Table.Td>
                  <Group gap="xs">
                    <Text size="sm" ff="monospace">
                      {mm(p.skew_mm)}
                    </Text>
                    {p.routed && !p.in_tolerance && p.limit_mm > 0 && (
                      <Badge size="xs" color="orange" variant="light">
                        over ±{mm(p.limit_mm)}
                      </Badge>
                    )}
                  </Group>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>
    </Card>
  )
}

/** One protocol's header: its name, what it is, and where it stands. */
function SectionControl({ title, kind, status }: { title: string; kind?: string; status: { text: string; tone: string } }) {
  return (
    <>
      <Group gap="xs" wrap="wrap">
        <Text fw={600}>{title}</Text>
        {kind && (
          <Badge size="xs" variant="light">
            {kind}
          </Badge>
        )}
      </Group>
      <Text size="xs" c={status.tone === 'orange' ? 'orange' : 'dimmed'}>
        {status.text}
      </Text>
    </>
  )
}

export interface ProtocolSectionsProps {
  /** Rendered inside the DDR section, which has its own detailed tables. */
  ddr?: React.ReactNode
  ddrStatus?: { text: string; tone: string }
  /** Every interface the board has, DDR included; the DDR one is skipped here. */
  interfaces: DetectedInterface[]
}

export function ProtocolSections({ ddr, ddrStatus, interfaces }: ProtocolSectionsProps) {
  // Only the ones there is something to show for. An interface with neither a
  // group nor a pair has nothing measured against anything.
  const others = interfaces.filter(
    (i) => !i.planner && ((i.groups?.length ?? 0) > 0 || (i.pair_skew?.length ?? 0) > 0),
  )
  if (!ddr && others.length === 0) return null

  const open = [
    ...(ddr ? ['ddr'] : []),
    ...others.filter((i) => interfaceStatus(i).tone === 'orange').map((i) => i.id),
  ]

  return (
    <Accordion variant="separated" multiple defaultValue={open}>
      {ddr && (
        <Accordion.Item value="ddr">
          <Accordion.Control>
            <SectionControl
              title="DDR memory"
              kind="ddr"
              status={ddrStatus ?? { text: '', tone: 'dimmed' }}
            />
          </Accordion.Control>
          <Accordion.Panel>{ddr}</Accordion.Panel>
        </Accordion.Item>
      )}
      {others.map((i) => (
        <Accordion.Item key={i.id} value={i.id}>
          <Accordion.Control>
            <SectionControl title={i.name} kind={i.kind} status={interfaceStatus(i)} />
          </Accordion.Control>
          <Accordion.Panel>
            <Stack gap="sm">
              <Text size="sm" c="dimmed">
                {i.summary}
              </Text>
              {(i.groups?.length ?? 0) > 0 && <GroupRows groups={i.groups!} />}
              {(i.pair_skew?.length ?? 0) > 0 && <PairRows pairs={i.pair_skew!} />}
              {i.unroutable > 0 && (
                <Text size="xs" c="dimmed">
                  {i.unroutable} of {i.nets} nets have no complete route yet, so they are measured
                  as far as the copper goes.
                </Text>
              )}
            </Stack>
          </Accordion.Panel>
        </Accordion.Item>
      ))}
    </Accordion>
  )
}
