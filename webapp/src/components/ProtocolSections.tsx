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
 * found. They all start folded: DDR alone runs to several screens, and a board
 * with six protocols was a long scroll even with only the broken ones open.
 * ProtocolNav, placed near the top of the page, lists them with a short status
 * each; picking one opens that section and scrolls to it.
 */

import { useEffect, useState } from 'react'
import { Accordion, Anchor, Badge, Button, Card, Group, Stack, Table, Text, Title } from '@mantine/core'
import type { DetectedInterface, GroupSkewInfo, PairSkewInfo } from '../lib/analyzerApi'
import { NOWRAP, mm, signedMM } from '../lib/format'
import { NetName, SelectNetsButton } from './HostActions'

/**
 * Where a protocol stands: a sentence for its header, and a word or two for
 * the list at the top of the page.
 */
export interface ProtocolStatus {
  text: string
  tone: string
  short?: string
}

/** The one-line state of a protocol, for its header. */
export function interfaceStatus(i: DetectedInterface): ProtocolStatus {
  const out = (i.groups ?? []).reduce((n, g) => n + g.out_of_tolerance, 0)
  const pairsOut = (i.pair_skew ?? []).filter((p) => p.routed && !p.in_tolerance).length
  if (out > 0 || pairsOut > 0) {
    const bits = []
    if (out > 0) bits.push(`${out} out of tolerance`)
    if (pairsOut > 0) bits.push(`${pairsOut} pair${pairsOut === 1 ? '' : 's'} out`)
    return { text: bits.join(', '), tone: 'orange', short: `${out + pairsOut} out` }
  }
  if (i.unroutable > 0) {
    return {
      text: `${i.unroutable} of ${i.nets} nets not routed end to end`,
      tone: 'gray',
      short: i.unroutable >= i.nets ? 'not routed' : `${i.unroutable} not routed`,
    }
  }
  return { text: 'every net measured is within tolerance', tone: 'teal', short: 'ok' }
}

/** One protocol as the page lists it. */
export interface ProtocolEntry {
  id: string
  title: string
  kind?: string
  status: ProtocolStatus
}

/** The key the DDR section goes by. */
export const DDR_SECTION = 'ddr'

/**
 * The protocols that get a section, in page order: DDR first when the board
 * has it, then every other interface with something measured. An interface
 * with neither a group nor a pair has nothing measured against anything.
 */
export function protocolEntries(
  interfaces: DetectedInterface[],
  ddrStatus?: ProtocolStatus | null,
): ProtocolEntry[] {
  const out: ProtocolEntry[] = []
  if (ddrStatus) out.push({ id: DDR_SECTION, title: 'DDR memory', kind: 'ddr', status: ddrStatus })
  for (const i of interfaces) {
    if (i.planner || ((i.groups?.length ?? 0) === 0 && (i.pair_skew?.length ?? 0) === 0)) continue
    out.push({ id: i.id, title: i.name, kind: i.kind, status: interfaceStatus(i) })
  }
  return out
}

/** The element id a section is scrolled to by. */
export function sectionAnchor(id: string): string {
  return `protocol-${id.toLowerCase().replace(/[^a-z0-9]+/g, '-')}`
}

/**
 * Which sections are open, and a jump that opens one and brings it into view.
 *
 * Shared by the list at the top and the sections below, which sit a long way
 * apart on the page. A jump closes the others: arriving at USB with DDR's
 * tables still open above it is how the page got long in the first place.
 */
export function useProtocolSections() {
  const [open, setOpen] = useState<string[]>([])
  const [target, setTarget] = useState<string | null>(null)
  useEffect(() => {
    if (target === null) return
    // After the render that opened it, so the section is where it will stay.
    document.getElementById(sectionAnchor(target))?.scrollIntoView({ behavior: 'smooth', block: 'start' })
    setTarget(null)
  }, [target])
  return {
    open,
    setOpen,
    jump: (id: string) => {
      setOpen([id])
      setTarget(id)
    },
  }
}

const TONE_COLOR: Record<string, string> = { orange: 'orange', teal: 'teal', gray: 'gray' }

/** The anchor ProtocolNav sits at, for the links back to it. */
export const PROTOCOL_NAV_ID = 'protocols'

/**
 * Every protocol on the board in one row, each with where it stands. Picking
 * one opens its section and goes there.
 */
export function ProtocolNav({
  entries,
  open,
  onJump,
  onOpenAll,
  onCloseAll,
}: {
  entries: ProtocolEntry[]
  open: string[]
  onJump: (id: string) => void
  onOpenAll: () => void
  onCloseAll: () => void
}) {
  if (entries.length === 0) return null
  return (
    <Card withBorder padding="sm" id={PROTOCOL_NAV_ID} style={{ scrollMarginTop: 80 }}>
      <Group justify="space-between" mb="xs" wrap="nowrap">
        <Text fw={600} size="sm">
          Protocols
        </Text>
        <Group gap="sm" wrap="nowrap">
          <Anchor component="button" type="button" size="xs" onClick={onOpenAll}>
            Open all
          </Anchor>
          <Anchor component="button" type="button" size="xs" onClick={onCloseAll} disabled={open.length === 0}>
            Close all
          </Anchor>
        </Group>
      </Group>
      <Group gap="xs">
        {entries.map((e) => (
          <Button
            key={e.id}
            size="xs"
            variant={open.includes(e.id) ? 'filled' : 'light'}
            color={TONE_COLOR[e.status.tone] ?? 'gray'}
            onClick={() => onJump(e.id)}
            rightSection={
              e.status.short ? (
                <Text span size="xs" fw={400} style={{ opacity: 0.85 }}>
                  {e.status.short}
                </Text>
              ) : undefined
            }
          >
            {e.title}
          </Button>
        ))}
      </Group>
    </Card>
  )
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
function SectionControl({ title, kind, status }: { title: string; kind?: string; status: ProtocolStatus }) {
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
  ddrStatus?: ProtocolStatus
  /** Every interface the board has, DDR included; the DDR one is skipped here. */
  interfaces: DetectedInterface[]
  /** The open sections, when the page controls them. Folded when it does not. */
  open?: string[]
  onOpenChange?: (open: string[]) => void
  /** Set when a ProtocolNav is on the page: each section links back to it. */
  withNav?: boolean
}

export function ProtocolSections({
  ddr,
  ddrStatus,
  interfaces,
  open,
  onOpenChange,
  withNav = false,
}: ProtocolSectionsProps) {
  const [own, setOwn] = useState<string[]>([])
  const entries = protocolEntries(interfaces, ddr ? (ddrStatus ?? { text: '', tone: 'dimmed' }) : null)
  if (entries.length === 0) return null
  const byId = new Map(interfaces.map((i) => [i.id, i]))
  const value = open ?? own
  const setValue = onOpenChange ?? setOwn
  const close = (id: string) => setValue(value.filter((v) => v !== id))

  // Under each open section: fold it, or go back to the list. DDR alone is
  // several screens long, and the list is the way to the next protocol.
  const footer = (id: string) => (
    <Group justify="flex-end" gap="md" mt="sm">
      <Anchor component="button" type="button" size="xs" onClick={() => close(id)}>
        Close
      </Anchor>
      {withNav && (
        <Anchor
          component="button"
          type="button"
          size="xs"
          onClick={() =>
            document.getElementById(PROTOCOL_NAV_ID)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
          }
        >
          Back to the list
        </Anchor>
      )}
    </Group>
  )

  return (
    // No open/close animation: a jump scrolls to a section while the others
    // fold, and an animated fold moves the target after the scroll has aimed.
    <Accordion variant="separated" multiple value={value} onChange={setValue} transitionDuration={0}>
      {entries.map((e) => {
        const i = byId.get(e.id)
        return (
          <Accordion.Item key={e.id} value={e.id} id={sectionAnchor(e.id)} style={{ scrollMarginTop: 80 }}>
            <Accordion.Control>
              <SectionControl title={e.title} kind={e.kind} status={e.status} />
            </Accordion.Control>
            <Accordion.Panel>
              {e.id === DDR_SECTION || !i ? (
                ddr
              ) : (
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
              )}
              {footer(e.id)}
            </Accordion.Panel>
          </Accordion.Item>
        )
      })}
    </Accordion>
  )
}
