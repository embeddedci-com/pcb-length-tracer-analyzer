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
import { NOWRAP, mm, signedMM } from '../lib/format'
import { NetName, SelectNetsButton } from './HostActions'

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

/**
 * One group of a protocol, member by member.
 *
 * The same reading a DDR group gives: what each net measures, how far it is
 * from what it is matched to, and which ones are out -- with the button that
 * selects those on the board. A count of "2 out" with no way to see which two
 * is not something anybody can act on.
 */
function GroupCard({ group }: { group: GroupSkewInfo }) {
  const rows = group.rows ?? []
  const out = rows.filter((m) => m.routed && !m.in_tolerance && m.role !== 'reference')
  return (
    <Card withBorder padding="md">
      <Stack gap="xs">
        <Group justify="space-between" align="flex-start" wrap="nowrap">
          {/* minWidth 0 so the heading shrinks rather than squeezing the badge
              beside it into an ellipsis. */}
          <div style={{ flex: 1, minWidth: 0 }}>
            <Title order={6} tt="capitalize">
              {group.name}
            </Title>
            <Text size="xs" c="dimmed">
              matched to {group.reference || 'its longest net'}
              {group.reference_mm > 0 ? ` (${mm(group.reference_mm)})` : ''} · spread{' '}
              {mm(group.spread_mm)} · tolerance{' '}
              {group.limit_mm > 0 ? `±${mm(group.limit_mm)}` : 'none'}
            </Text>
            {group.why && (
              <Text size="xs" c="dimmed">
                {group.why}
              </Text>
            )}
          </div>
          <Group gap="xs" wrap="nowrap">
            {out.length > 0 && (
              <SelectNetsButton nets={out.map((m) => m.net)}>Select the ones out</SelectNetsButton>
            )}
            <Badge
              variant="light"
              style={{ flexShrink: 0 }}
              color={group.out_of_tolerance === 0 ? 'green' : 'orange'}
            >
              {group.out_of_tolerance === 0 ? 'matched' : `${group.out_of_tolerance} out`}
            </Badge>
          </Group>
        </Group>

        {rows.length > 0 && (
          <Table.ScrollContainer minWidth={460}>
            <Table verticalSpacing={4}>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Net</Table.Th>
                  <Table.Th>Length</Table.Th>
                  <Table.Th>vs the reference</Table.Th>
                  <Table.Th>To add</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {rows.map((m) => {
                  const bad = m.routed && !m.in_tolerance && m.role !== 'reference'
                  return (
                    <Table.Tr key={m.net}>
                      <Table.Td>
                        <Group gap={6} wrap="nowrap">
                          <Text size="sm" c={bad ? 'orange' : undefined}>
                            <NetName net={m.net}>{m.label}</NetName>
                          </Text>
                          {m.role === 'reference' && (
                            <Badge size="xs" variant="light">
                              reference
                            </Badge>
                          )}
                        </Group>
                        {(m.through?.length ?? 0) > 0 && (
                          <Text size="xs" c="dimmed">
                            through {m.through!.join(', ')}
                          </Text>
                        )}
                      </Table.Td>
                      <Table.Td>
                        <Text size="sm" ff="monospace" style={NOWRAP}>
                          {m.routed ? mm(m.length_mm) : 'not routed'}
                        </Text>
                      </Table.Td>
                      <Table.Td>
                        <Text size="sm" ff="monospace" c={bad ? 'orange' : undefined} style={NOWRAP}>
                          {m.routed && m.role !== 'reference' ? signedMM(m.deviation_mm) : '—'}
                        </Text>
                      </Table.Td>
                      <Table.Td>
                        <Text size="sm" ff="monospace" style={NOWRAP}>
                          {m.need_mm > 0
                            ? mm(m.need_mm)
                            : (m.excess_mm ?? 0) > 0
                              ? `shorten ${mm(m.excess_mm!)}`
                              : '—'}
                        </Text>
                      </Table.Td>
                    </Table.Tr>
                  )
                })}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        )}
        {rows.length === 0 && (
          <Text size="xs" c="dimmed">
            {group.members ?? 0} nets, none of them routed end to end yet.
          </Text>
        )}
      </Stack>
    </Card>
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
              {(i.groups ?? []).map((g) => (
                <GroupCard key={g.name} group={g} />
              ))}
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
