/**
 * Whether the areas you have drawn are big enough.
 *
 * Drawing a region and being told nothing is only half a tool: the question the
 * drawing is asking is "will this do?", and the answer is a measurement the
 * tool can make in under a second. So it makes it, every time the areas change,
 * and says plainly how much of what is needed is reachable inside them.
 *
 * The answer separates two things that look alike in a total and are not the
 * same problem. A net short of room might be helped by drawing a bigger area.
 * A net asking for more length than its whole route could ever carry cannot be
 * — no area is large enough, because the limit is the track, not the space —
 * and offering "draw more" for those would waste somebody's afternoon.
 */

import { Alert, Badge, Group, Progress, Stack, Table, Text } from '@mantine/core'
import type { Analysis, HeadroomResponse } from '../lib/analyzerApi'
import { NOWRAP, areaVerdict, groupNeed, mm } from '../lib/format'

export interface AreaVerdictProps {
  analysis: Analysis
  headroom: HeadroomResponse | null
  loading: boolean
  /** How many areas are drawn; zero means anywhere. */
  areaCount: number
}

export function AreaVerdict({ analysis, headroom, loading, areaCount }: AreaVerdictProps) {
  if (loading && !headroom) {
    return (
      <Text size="sm" c="dimmed">
        Measuring how much room {areaCount > 0 ? 'these areas hold' : 'the board has'}…
      </Text>
    )
  }
  if (!headroom) return null

  const candidates = headroom.candidates
  const { need, have, short, reachable, unreachable, fits, reroute, enough } =
    areaVerdict(candidates)

  const pct = need > 0 ? Math.min(100, (have / need) * 100) : 100

  return (
    <Stack gap="xs">
      <Group justify="space-between" align="flex-end">
        <Text size="sm" fw={500}>
          {areaCount > 0
            ? `Inside ${areaCount === 1 ? 'this area' : `these ${areaCount} areas`}`
            : 'Anywhere it fits'}
        </Text>
        <Badge variant="light" color={enough ? 'green' : short < need / 2 ? 'orange' : 'red'}>
          {enough ? 'enough room' : `short by ${mm(short)}`}
        </Badge>
      </Group>

      <Progress.Root size="lg">
        <Progress.Section value={pct} color={enough ? 'green' : 'orange'}>
          <Progress.Label>{mm(have)}</Progress.Label>
        </Progress.Section>
        {!enough && (
          <Progress.Section value={100 - pct} color="gray.4">
            <Progress.Label>{mm(short)} short</Progress.Label>
          </Progress.Section>
        )}
      </Progress.Root>

      <Text size="xs" c="dimmed">
        {mm(have)} of the {mm(need)} needed is reachable{areaCount > 0 ? ' inside the areas' : ''}:{' '}
        {fits} of {candidates.length} nets have all the room they need.
        {reachable > 1e-6
          ? ` Of the ${mm(short)} still missing, ${mm(reachable)} is on nets that are merely short of room. A bigger area may reach it.`
          : ''}
        {unreachable > 1e-6
          ? ` ${mm(unreachable)} is on ${reroute} net${
              reroute === 1 ? '' : 's'
            } asking for more than a quarter of their own length, which no area can supply: those need rerouting, not more space.`
          : ''}
      </Text>

      {/* Per group, because a total hides the case that matters: one group
          fully covered and another with nothing is not "half covered". */}
      <Table.ScrollContainer minWidth={420}>
        <Table verticalSpacing={4} fz="xs">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Group</Table.Th>
              <Table.Th>Needs</Table.Th>
              <Table.Th>Reachable</Table.Th>
              <Table.Th>Covered</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {analysis.groups.map((g) => {
              // Per leg, not per net: an address line belongs to every group in
              // the chain, and charging its whole requirement to each of them
              // made the table add up to more than the board needs.
              const { nets, need: n, reachable: h } = groupNeed(g, candidates)
              if (nets === 0) return null
              const cov = n > 0 ? Math.round((h / n) * 100) : 100
              return (
                <Table.Tr key={g.name}>
                  <Table.Td>{g.name}</Table.Td>
                  <Table.Td style={NOWRAP}>{mm(n)}</Table.Td>
                  <Table.Td style={NOWRAP}>{mm(h)}</Table.Td>
                  <Table.Td>
                    <Text size="xs" c={cov >= 100 ? 'green' : cov === 0 ? 'red' : 'orange'}>
                      {cov}%
                    </Text>
                  </Table.Td>
                </Table.Tr>
              )
            })}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>

      {areaCount > 0 && have <= 1e-6 && (
        <Alert color="red" variant="light" title="These areas hold no room at all">
          <Text size="sm">
            No net that needs length has free space inside them. Draw them over the tracks you want
            to lengthen. Pick a net above to find it on the board.
          </Text>
        </Alert>
      )}
    </Stack>
  )
}
