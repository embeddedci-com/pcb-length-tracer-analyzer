/**
 * One matching group, member by member.
 *
 * Both units are shown. Millimetres are what a reviewer measures in pcbnew;
 * picoseconds are what the timing budget is written in, and the two are not
 * proportional across a net that changes layer -- a microstrip millimetre and a
 * stripline millimetre differ by about thirty per cent. Showing only one of
 * them would hide that.
 */

import { Badge, Card, Group, List, Stack, Table, Text, Title, Tooltip } from '@mantine/core'
import type { GroupInfo, MemberInfo } from '../lib/analyzerApi'
import { NOWRAP, groupSummary, memberSeverity, mm, ps, signedMM } from '../lib/format'
import { ToleranceBar } from './ToleranceBar'
import { NetName, SelectNetsButton } from './HostActions'

const SEVERITY_COLOR: Record<string, string | undefined> = {
  ok: undefined,
  warn: 'orange',
  bad: 'red',
  unrouted: 'gray',
}

function MemberRow({ m, tolerance }: { m: MemberInfo; tolerance: number }) {
  const sev = memberSeverity(m, tolerance)
  const color = SEVERITY_COLOR[sev]
  // A fly-by leg is part of a net, and KiCad only shows the whole net: give the
  // whole-net length to tune to once every leg of it is matched.
  const aim =
    (m.net_aim_mm ?? 0) > 0 && !m.in_tolerance && (m.need_mm > 0 || (m.excess_mm ?? 0) > 0)
      ? m.net_aim_mm!
      : null
  if (!m.routed) {
    return (
      <Table.Tr>
        <Table.Td>
          <Text size="sm" c="dimmed">
            <NetName net={m.net}>{m.label}</NetName>
          </Text>
        </Table.Td>
        <Table.Td colSpan={4}>
          <Text size="sm" c="dimmed">
            not routed to this device
          </Text>
        </Table.Td>
      </Table.Tr>
    )
  }
  return (
    <Table.Tr>
      <Table.Td>
        <Group gap={6}>
          <Text size="sm" fw={500}>
            <NetName net={m.net}>{m.label}</NetName>
          </Text>
          {m.reference && (
            <Tooltip label="One line of the reference pair. The target is the average of both lines.">
              <Badge size="xs" variant="light">
                reference
              </Badge>
            </Tooltip>
          )}
          {(m.excess_mm ?? 0) > 0 && (
            <Tooltip label="Longer than the target plus the tolerance. A meander cannot make a track shorter: reroute it shorter.">
              <Badge size="xs" color="red" variant="light">
                too long
              </Badge>
            </Tooltip>
          )}
          {sev === 'bad' && !m.reference && !((m.excess_mm ?? 0) > 0) && (
            <Tooltip label="More than twice the tolerance off. Usually needs rerouting.">
              <Badge size="xs" color="red" variant="light">
                far out
              </Badge>
            </Tooltip>
          )}
        </Group>
      </Table.Td>
      <Table.Td>
        <Text size="sm" ff="monospace" style={NOWRAP}>
          {mm(m.length_mm)}
        </Text>
      </Table.Td>
      <Table.Td>
        <Text size="sm" ff="monospace" style={NOWRAP} c="dimmed">
          {ps(m.delay_ps)}
        </Text>
      </Table.Td>
      <Table.Td>
        <Text size="sm" ff="monospace" style={NOWRAP} c={color}>
          {signedMM(m.deviation_mm)}
        </Text>
      </Table.Td>
      <Table.Td>
        {/* Nothing to add to a member that is already matched: within the band
            is done, and a number here would read as work left. */}
        <Text
          size="sm"
          ff="monospace"
          style={NOWRAP}
          c={!m.in_tolerance && (m.need_mm > 0 || (m.excess_mm ?? 0) > 0) ? color : 'dimmed'}
        >
          {m.in_tolerance
            ? '—'
            : m.need_mm > 0
              ? mm(m.need_mm)
              : (m.excess_mm ?? 0) > 0
                ? `shorten ${mm(m.excess_mm!)}`
                : '—'}
        </Text>
        {aim !== null && (
          <Tooltip label="KiCad shows the length of the whole net (without the package). Tune the whole net to this length.">
            <Text size="xs" ff="monospace" c="dimmed" style={NOWRAP}>
              (total {mm(aim)})
            </Text>
          </Tooltip>
        )}
      </Table.Td>
    </Table.Tr>
  )
}

export function GroupTable({
  group,
  overridden = false,
  onTolerance,
  busy = false,
}: {
  group: GroupInfo
  /** True when this group has a tolerance of its own. */
  overridden?: boolean
  /** Sets this group's tolerance in mm, or null for the default. Omit for a read-only table. */
  onTolerance?: (mm: number | null) => void
  busy?: boolean
}) {
  const halves = group.reference_members ?? []
  const offset = group.target_mm - group.reference_length_mm
  return (
    <Card withBorder padding="md">
      <Stack gap="xs">
        <Group justify="space-between" align="flex-start">
          <div>
            <Title order={5} tt="capitalize">
              {group.name}
            </Title>
            <Text size="xs" c="dimmed">
              {groupSummary(group)}
            </Text>
          </div>
          <Group gap="xs">
            <SelectNetsButton
              nets={group.members.filter((m) => m.routed && !m.in_tolerance && !m.reference).map((m) => m.net)}
            >
              Select the ones out
            </SelectNetsButton>
            <Badge
              variant="light"
              color={group.out_of_tolerance === 0 ? 'green' : group.out_of_tolerance > 4 ? 'red' : 'orange'}
            >
              {group.out_of_tolerance === 0 ? 'matched' : `${group.out_of_tolerance} out`}
            </Badge>
          </Group>
        </Group>

        {/* The target, and what it is made of, before any row: every offset in
            the table is against this one number, and a reader who cannot see
            where it comes from cannot tell whether an offset is right. */}
        <Group gap="lg" align="flex-end" wrap="wrap">
          <div>
            <Text size="xs" c="dimmed" tt="uppercase">
              Target
            </Text>
            <Text size="lg" fw={700} ff="monospace" style={NOWRAP}>
              {mm(group.target_mm)}
            </Text>
          </div>
          <div style={{ flex: 1, minWidth: 220 }}>
            <Text size="sm" c="dimmed">
              {halves.length === 2 ? (
                <>
                  the mean of {halves[0].label} ({mm(halves[0].length_mm)}) and {halves[1].label} (
                  {mm(halves[1].length_mm)})
                </>
              ) : halves.length === 1 ? (
                <>
                  {halves[0].label} ({mm(halves[0].length_mm)})
                </>
              ) : (
                <>no reference is routed, so the target is the longest net</>
              )}
              {Math.abs(offset) > 1e-6 ? `, with the clock offset ${signedMM(offset)}` : ''}
              {`; tolerance ±${mm(group.tolerance_mm)}`}
            </Text>
          </div>
        </Group>

        {onTolerance && (
          <ToleranceBar
            valueMM={group.tolerance_mm}
            overridden={overridden}
            onChange={onTolerance}
            busy={busy}
          />
        )}

        <Table.ScrollContainer minWidth={560}>
          <Table striped highlightOnHover withTableBorder={false} verticalSpacing={4}>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Net</Table.Th>
                <Table.Th>Length</Table.Th>
                <Table.Th>Delay</Table.Th>
                <Table.Th>vs {halves.length > 0 ? 'the reference' : 'target'}</Table.Th>
                <Table.Th>To add</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {group.members.map((m) => (
                <MemberRow key={m.net} m={m} tolerance={group.tolerance_mm} />
              ))}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>

        {(group.notes?.length ?? 0) > 0 && (
          <List size="sm" spacing={4} c="dimmed">
            {group.notes!.map((n) => (
              <List.Item key={n}>{n}</List.Item>
            ))}
          </List>
        )}
      </Stack>
    </Card>
  )
}
