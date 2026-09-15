/**
 * The confirmation step: which traces to change.
 *
 * This is the point of the whole two-stage flow. The user has read what the
 * board is and how far each net sits from where it should be; here they say
 * what to touch, and only then does anything get written.
 *
 * Until the room beside each route has been measured, the list is split by how
 * much length each net needs and nothing more -- whether a correction fits
 * depends on the space beside that particular track, and labelling the halves
 * "will fit" and "will not" would be a prediction this cannot make.
 *
 * Once it has been measured, the split becomes the three things a user can
 * actually do about a shortfall. A net with room is worth applying. A net short
 * of room might be helped by opening some in the layout. A net asking for more
 * length than its route could ever carry needs rerouting, and no amount of room
 * beside it changes that -- so those are listed apart and left unticked,
 * because applying them can only produce a shortfall.
 */

import { useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Badge,
  Button,
  Card,
  Checkbox,
  Group,
  Stack,
  Table,
  Text,
  Title,
} from '@mantine/core'
import type { Analysis, HeadroomResponse, MemberInfo } from '../lib/analyzerApi'
import { NOWRAP, gettable, mm, partitionCandidates, ps, totalNeed, triage } from '../lib/format'

export interface CandidatePickerProps {
  analysis: Analysis
  /** Once measured, how much room there is beside each route. */
  headroom?: HeadroomResponse | null
  headroomLoading?: boolean
  onMeasureHeadroom?: () => void
  busy?: boolean
  error?: string | null
  onApply: (nets: string[]) => void
}

function roomColour(c: MemberInfo): string {
  if (c.needs_reroute) return 'red'
  return c.headroom_mm + 1e-6 >= c.need_mm ? 'green' : 'orange'
}

function Head({ showRoom }: { showRoom?: boolean }) {
  return (
    <Table.Thead>
      <Table.Tr>
        <Table.Th>Net</Table.Th>
        <Table.Th>Role</Table.Th>
        <Table.Th>Length now</Table.Th>
        <Table.Th>To add</Table.Th>
        <Table.Th>as delay</Table.Th>
        {showRoom && <Table.Th>Room</Table.Th>}
      </Table.Tr>
    </Table.Thead>
  )
}

/** One tickable group of candidates. */
function Section({
  title,
  colour,
  explain,
  rows,
  selected,
  toggle,
  setAll,
  showRoom,
}: {
  title: string
  colour: string
  explain?: string
  rows: MemberInfo[]
  selected: Set<string>
  toggle: (net: string) => void
  setAll: (rows: MemberInfo[], on: boolean) => void
  showRoom?: boolean
}) {
  if (rows.length === 0) return null
  return (
    <Card withBorder padding="md">
      <Group justify="space-between" mb="xs">
        <Group gap="xs">
          <Title order={5}>{title}</Title>
          <Badge variant="light" color={colour}>
            {rows.length}
          </Badge>
        </Group>
        <Group gap="xs">
          <Button size="compact-xs" variant="subtle" onClick={() => setAll(rows, true)}>
            all
          </Button>
          <Button size="compact-xs" variant="subtle" onClick={() => setAll(rows, false)}>
            none
          </Button>
        </Group>
      </Group>
      {explain && (
        <Text size="sm" c="dimmed" mb="xs">
          {explain}
        </Text>
      )}
      <Table.ScrollContainer minWidth={520}>
        <Table verticalSpacing={4}>
          <Head showRoom={showRoom} />
          <Table.Tbody>
            {rows.map((c) => (
              <Table.Tr key={c.net}>
                <Table.Td>
                  <Checkbox
                    checked={selected.has(c.net)}
                    onChange={() => toggle(c.net)}
                    label={c.label}
                    aria-label={`change ${c.label}`}
                  />
                  {/* An address line is matched over each span of the fly-by
                      chain separately, so one net can be short twice, in
                      different copper. The figures beside it are the sum, and a
                      reader comparing them against a group table needs to know
                      that. */}
                  {(c.legs?.length ?? 0) > 1 && (
                    <Text size="xs" c="dimmed" ml={30}>
                      {c.legs!.map((l) => `${l.leg || l.group} ${mm(l.need_mm)}`).join(' + ')}
                    </Text>
                  )}
                </Table.Td>
                <Table.Td>
                  <Text size="sm" c="dimmed">
                    {c.role}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Text size="sm" ff="monospace" style={NOWRAP}>
                    {mm(c.length_mm)}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Text size="sm" ff="monospace" fw={500} style={NOWRAP}>
                    {mm(c.need_mm)}
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Text size="sm" ff="monospace" style={NOWRAP} c="dimmed">
                    {ps(c.need_ps)}
                  </Text>
                </Table.Td>
                {showRoom && (
                  <Table.Td>
                    <Text size="sm" ff="monospace" style={NOWRAP} c={roomColour(c)}>
                      {c.needs_reroute ? '—' : mm(c.headroom_mm)}
                    </Text>
                  </Table.Td>
                )}
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>
    </Card>
  )
}

export function CandidatePicker({
  analysis,
  headroom,
  headroomLoading,
  onMeasureHeadroom,
  busy,
  error,
  onApply,
}: CandidatePickerProps) {
  const measured = headroom?.candidates ?? null
  const rows = measured ?? analysis.candidates
  const { small, large } = useMemo(() => partitionCandidates(rows), [rows])
  const { fits, tight, reroute } = useMemo(() => triage(rows), [rows])

  // Start with the small corrections ticked; once the room is known, start with
  // the ones that have it. Ticking everything by default would mostly produce
  // shortfalls, which makes the tool look broken rather than honest about a
  // board that has no room.
  const [selected, setSelected] = useState<Set<string>>(() => new Set(small.map((c) => c.net)))
  const haveMeasurement = measured !== null
  useEffect(() => {
    if (haveMeasurement) setSelected(new Set(fits.map((c) => c.net)))
    // Only when the measurement arrives, so a later tick is not undone.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [haveMeasurement])

  const toggle = (net: string) =>
    setSelected((prev) => {
      const next = new Set(prev)
      if (!next.delete(net)) next.add(net)
      return next
    })
  const setAll = (group: MemberInfo[], on: boolean) =>
    setSelected((prev) => {
      const next = new Set(prev)
      for (const r of group) {
        if (on) next.add(r.net)
        else next.delete(r.net)
      }
      return next
    })

  if (analysis.candidates.length === 0) {
    return (
      <Alert color="green" variant="light" title="Nothing to change">
        Every net in every group is already within tolerance under these parameters.
      </Alert>
    )
  }

  const chosen = totalNeed(rows, selected)

  return (
    <Stack gap="md">
      <div>
        <Title order={4}>Choose what to change</Title>
        <Text size="sm" c="dimmed">
          {analysis.candidates.length} net
          {analysis.candidates.length === 1 ? ' is' : 's are'} out of tolerance, needing{' '}
          {mm(analysis.total_need_mm)} between them. Nothing is written until you apply.
        </Text>
      </div>

      {!headroom ? (
        <Alert color="blue" variant="light" title="How much of this is achievable?">
          <Stack gap="xs" align="flex-start">
            <Text size="sm">
              A meander needs free space next to its track. Measuring shows which nets fit, which
              need more room, and which need rerouting. It takes a few seconds.
            </Text>
            <Button
              size="compact-sm"
              loading={headroomLoading}
              onClick={onMeasureHeadroom}
              disabled={!onMeasureHeadroom}
            >
              Measure the room
            </Button>
          </Stack>
        </Alert>
      ) : (
        <Alert
          color={gettable(rows) >= headroom.total_need_mm - 1e-6 ? 'green' : 'orange'}
          variant="light"
          title={`${mm(gettable(rows))} of ${mm(headroom.total_need_mm)} is achievable`}
        >
          <Text size="sm">
            Room beside one net cannot be lent to another, so that is each net&rsquo;s own room
            capped at what it needs.
            {headroom.reroute_count > 0 &&
              ` ${headroom.reroute_count} net${headroom.reroute_count === 1 ? '' : 's'} ask for more length than a meander can supply, whatever room is opened beside them.`}
            {headroom.bus_spare_mm > 0.5 &&
              ` The buses these nets run in have ${mm(headroom.bus_spare_mm)} of clear space beside them, which bounds what re-spacing them could add.`}
          </Text>
        </Alert>
      )}

      {measured ? (
        <>
          <Section
            title="Have the room"
            colour="green"
            rows={fits}
            selected={selected}
            toggle={toggle}
            setAll={setAll}
            showRoom
          />
          <Section
            title="Short of room"
            colour="orange"
            explain="There is space beside these, but less than they need. The tool adds what fits and reports the rest; opening space in the layout is what closes it."
            rows={tight}
            selected={selected}
            toggle={toggle}
            setAll={setAll}
            showRoom
          />
          <Section
            title="Need rerouting, not tuning"
            colour="red"
            explain="These ask for more than a quarter of their own length. A meander folds extra path into the space beside a track; it cannot make a short route into a long one. Applying them can only produce a shortfall."
            rows={reroute}
            selected={selected}
            toggle={toggle}
            setAll={setAll}
            showRoom
          />
        </>
      ) : (
        <>
          <Section
            title="Small corrections"
            colour="green"
            rows={small}
            selected={selected}
            toggle={toggle}
            setAll={setAll}
          />
          <Section
            title="Large corrections"
            colour="orange"
            explain="These need several millimeters each. On a bus routed at its minimum clearance there is nowhere beside the track for a meander, so expect a shortfall."
            rows={large}
            selected={selected}
            toggle={toggle}
            setAll={setAll}
          />
        </>
      )}

      {error && (
        <Alert color="red" variant="light" title="That did not work">
          {error}
        </Alert>
      )}

      <Group justify="space-between">
        <Text size="sm" c="dimmed">
          {selected.size} net{selected.size === 1 ? '' : 's'} selected, adding up to {mm(chosen)}
        </Text>
        <Button
          disabled={selected.size === 0 || busy}
          loading={busy}
          onClick={() => onApply([...selected])}
        >
          Apply to {selected.size} net{selected.size === 1 ? '' : 's'}
        </Button>
      </Group>
    </Stack>
  )
}
