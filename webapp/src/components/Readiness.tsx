/**
 * What has to happen before a length match means anything.
 *
 * Everything else in this app answers a question the user asked. This one
 * answers the question they have not asked yet, which is "what am I looking
 * at, and what do I do about it" -- and it answers it in the order the work
 * actually happens: copper that is missing first, because a length cannot be
 * matched over a trace that is not there; then how much length is wanted; then
 * how much board that needs; then, net by net, which of the three different
 * things a person can do about it applies.
 *
 * The three are not interchangeable and lumping them into one "shortfall" is
 * what makes a tool feel like it is guessing. A net with room beside it is a
 * button press. A net short of room is a question about where to find some. A
 * net asking for more length than its own route could ever carry is a reroute,
 * and no setting in this app changes that -- so it says what length to route it
 * to instead, which is the thing somebody can actually act on.
 */

import {
  Accordion,
  Alert,
  Badge,
  Button,
  Card,
  Divider,
  Group,
  Loader,
  Menu,
  Progress,
  SimpleGrid,
  Stack,
  Table,
  Text,
  ThemeIcon,
  Title,
  Tooltip,
} from '@mantine/core'
import type { Analysis, HeadroomResponse } from '../lib/analyzerApi'
import { type LegRow, NOWRAP, buildBrief, mm, mm2, signedMM, triageLegs } from '../lib/format'

export interface ReadinessProps {
  analysis: Analysis

  /** The room measurement, once it has arrived. */
  headroom: HeadroomResponse | null
  measuring: boolean

  /** Runs the experimental router over the connections that are missing. */
  onRoute?: () => void
  routing?: boolean

  /** Takes the user to the step where they choose what to change. */
  onContinue?: () => void

  /**
   * Which interfaces the user ticked. The DDR planner's own work is always
   * included; this decides which of the others the briefing covers.
   */
  selected?: string[]

  /** Hands the reader a file of what is on screen. */
  onExport?: (what: 'candidates' | 'missing' | 'analysis') => void
}

function Step({
  n,
  title,
  status,
  children,
}: {
  n: number
  title: string
  status: 'done' | 'todo' | 'blocked'
  children: React.ReactNode
}) {
  const colour = status === 'done' ? 'green' : status === 'blocked' ? 'red' : 'orange'
  return (
    <div>
      <Group gap="sm" mb={6} wrap="nowrap" align="center">
        <ThemeIcon size="sm" radius="xl" variant="light" color={colour}>
          <Text size="xs" fw={700}>
            {status === 'done' ? '✓' : n}
          </Text>
        </ThemeIcon>
        <Text fw={600}>{title}</Text>
      </Group>
      <div style={{ paddingLeft: 34 }}>{children}</div>
    </div>
  )
}

/** A number with a label under it, for the figures worth reading first. */
function Figure({ value, label, colour }: { value: string; label: string; colour?: string }) {
  return (
    <div>
      <Text size="xl" fw={700} c={colour} style={NOWRAP}>
        {value}
      </Text>
      <Text size="xs" c="dimmed">
        {label}
      </Text>
    </div>
  )
}

/**
 * How long the room measurement takes, roughly.
 *
 * It probes the design rules along every candidate track, so it scales with how
 * much copper there is: about 400 tracks a second on the boards it was built
 * against. A spinner with no number beside it is indistinguishable from a
 * spinner that is never going to stop.
 */
function roughly(tracks: number): string {
  const seconds = Math.max(2, Math.round(tracks / 400))
  if (seconds < 10) return `usually about ${seconds} seconds`
  if (seconds < 60) return `usually ${Math.round(seconds / 5) * 5} seconds or so`
  return 'a minute or more'
}

function netList(nets: string[], limit = 12): string {
  if (nets.length <= limit) return nets.join(', ')
  return `${nets.slice(0, limit).join(', ')} and ${nets.length - limit} more`
}

export function Readiness({
  analysis,
  headroom,
  measuring,
  onRoute,
  routing = false,
  onContinue,
  selected,
  onExport,
}: ReadinessProps) {
  const { routing: routed } = analysis
  // One briefing for the board, not for DDR with the rest in a footnote: the
  // fly-by chain's missing spans and every other interface's unjoined nets are
  // the same problem, and a length that cannot be matched is the same problem
  // whichever bus it is on.
  const brief = buildBrief(analysis, headroom, selected)
  const total = routed.complete + routed.incomplete
  const groups = brief.groups

  const need = brief.need
  const have = headroom ? brief.have : 0
  // Per span, not per net. Everything in the last section is a decision about
  // one span of one net -- the room beside that copper, the length to route
  // that span to -- and a net summed across its legs answers none of them.
  const rows = brief.rows
  const { fits, tight, reroute } = triageLegs(rows)
  const sum = (rs: LegRow[]) => rs.reduce((s, c) => s + c.need_mm, 0)

  const area = brief.area
  const run = brief.run
  // The area figure scales with the length it is for, so the part still to be
  // found is the same proportion of it as the length still to be found.
  const areaShort = need > 0 ? (area * Math.max(0, need - have)) / need : 0

  const blocked = brief.missing.length > 0
  const failedChecks = (analysis.checks ?? []).filter((c) => !c.ok)

  return (
    <Card withBorder padding="lg">
      <Group justify="space-between" align="flex-start" mb="md">
        <div>
          <Title order={3}>What needs doing</Title>
          <Text size="sm" c="dimmed">
            {blocked
              ? 'Some nets are not fully routed yet, so some lengths below are incomplete.'
              : 'Every net is joined end to end, so what follows is the whole picture.'}
          </Text>
        </div>
        <Group gap="xs" wrap="nowrap">
        {onExport && (
          <Menu position="bottom-end" withinPortal>
            <Menu.Target>
              <Button size="xs" variant="default">
                Export
              </Button>
            </Menu.Target>
            <Menu.Dropdown>
              <Menu.Label>Take this away</Menu.Label>
              <Menu.Item onClick={() => onExport('candidates')}>
                What needs length (CSV)
              </Menu.Item>
              <Menu.Item onClick={() => onExport('missing')}>
                What is not joined up (CSV)
              </Menu.Item>
              <Menu.Item onClick={() => onExport('analysis')}>
                The whole analysis (JSON)
              </Menu.Item>
            </Menu.Dropdown>
          </Menu>
        )}
        <Badge size="lg" variant="light" color={blocked ? 'orange' : need > 0 ? 'blue' : 'green'}>
          {blocked
            ? `${brief.missingCount} net${brief.missingCount === 1 ? '' : 's'} not joined up`
            : need > 0
              ? `${mm(need)} to add`
              : 'nothing to do'}
        </Badge>
        </Group>
      </Group>

      <Stack gap="lg">
        <Step
          n={1}
          title="Copper that is missing"
          status={blocked ? 'blocked' : 'done'}
        >
          {blocked ? (
            <Stack gap="xs">
              <Text size="sm">
                <b>
                  {brief.missingCount} net{brief.missingCount === 1 ? '' : 's'}
                </b>{' '}
                {brief.missingCount === 1 ? 'is' : 'are'} not joined end to end
                {routed.incomplete > 0 ? `, ${routed.incomplete} of them on the ${total}-net bus below` : ''}
                . Only the routed parts are measured. KiCad&rsquo;s DRC does not always report this.
              </Text>

              <Table.ScrollContainer minWidth={520}>
                <Table verticalSpacing={4} fz="sm">
                  <Table.Thead>
                    <Table.Tr>
                      <Table.Th>Where</Table.Th>
                      <Table.Th>Nets</Table.Th>
                      <Table.Th>Which</Table.Th>
                    </Table.Tr>
                  </Table.Thead>
                  <Table.Tbody>
                    {brief.missing.map((m) => (
                      <Table.Tr key={m.key}>
                        <Table.Td style={NOWRAP}>
                          <Text size="sm" ff={m.hop ? 'monospace' : undefined}>
                            {m.where}
                          </Text>
                          {m.hop ? (
                            <Text size="xs" c="dimmed">
                              a span of the fly-by chain
                            </Text>
                          ) : null}
                        </Table.Td>
                        <Table.Td style={NOWRAP}>{m.nets.length}</Table.Td>
                        <Table.Td>
                          <Text size="xs" c="dimmed">
                            {netList(m.nets)}
                          </Text>
                        </Table.Td>
                      </Table.Tr>
                    ))}
                  </Table.Tbody>
                </Table>
              </Table.ScrollContainer>

              <Alert color="orange" variant="light" title="Route these first">
                <Stack gap="xs">
                  <Text size="sm">
                    Route them in KiCad, then match lengths. Matching now would tune tracks you will
                    redraw.
                  </Text>
                  {onRoute && (
                    <Group gap="sm" align="center">
                      <Button
                        size="xs"
                        variant="default"
                        loading={routing}
                        onClick={onRoute}
                      >
                        Try the experimental router
                      </Button>
                      <Text size="xs" c="dimmed" style={{ flex: 1, minWidth: 0 }}>
                        It never moves your tracks, so on a dense board it often cannot find a path.
                        The result replaces the board in this session and can be downloaded.
                      </Text>
                    </Group>
                  )}
                </Stack>
              </Alert>
            </Stack>
          ) : (
            <Text size="sm" c="dimmed">
              Every net is joined end to end
              {routed.chain ? ', and the fly-by chain has every hop' : ''}.
            </Text>
          )}
        </Step>

        <Divider />

        <Step
          n={2}
          title="Length to add"
          status={need > 0 || failedChecks.length > 0 ? 'todo' : 'done'}
        >
          {failedChecks.length > 0 && (
            <Alert color="red" variant="light" mb="sm" title={`${failedChecks.length} check${failedChecks.length === 1 ? '' : 's'} across groups outside the limit`}>
              <Stack gap={2}>
                {failedChecks.map((c) => (
                  <Text key={c.name} size="sm">
                    <b>{c.name}</b>:{' '}
                    {c.kind === 'chip-delta' ? mm(c.value_mm) : signedMM(c.value_mm)} against{' '}
                    {c.kind === 'chip-delta' ? `at most ${mm(c.limit_mm)}` : `±${mm(c.limit_mm)}`}.
                    {c.kind === 'chip-delta'
                      ? ' Bring the memory chips\u2019 data lines closer in length.'
                      : ' Make the whole byte, strobe included, longer or shorter.'}
                  </Text>
                ))}
              </Stack>
            </Alert>
          )}
          {need <= 0 ? (
            <Text size="sm" c="dimmed">
              Every net is within tolerance of its group. There is nothing to match.
            </Text>
          ) : (
            <Stack gap="sm">
              <SimpleGrid cols={{ base: 2, sm: 4 }} spacing="md">
                <Figure value={mm(need)} label="to add in total" />
                <Figure value={String(brief.nets)} label="nets out of tolerance" />
                <Figure value={String(groups.length)} label="groups affected" />
                <Figure
                  value={headroom ? mm(have) : '—'}
                  label="reachable where it is needed"
                  colour={headroom && have < need / 2 ? 'orange' : undefined}
                />
              </SimpleGrid>

              <Table.ScrollContainer minWidth={520}>
                <Table verticalSpacing={4} fz="sm">
                  <Table.Thead>
                    <Table.Tr>
                      <Table.Th>Group</Table.Th>
                      <Table.Th>Nets out</Table.Th>
                      <Table.Th>Needs</Table.Th>
                      <Table.Th>Reachable</Table.Th>
                      <Table.Th>Worst net</Table.Th>
                    </Table.Tr>
                  </Table.Thead>
                  <Table.Tbody>
                    {groups.map((g) => {
                      const { netsOut: nets, need: n, reachable: h, worst } = g
                      return (
                        <Table.Tr key={g.key}>
                          <Table.Td>
                            {g.name}
                            {g.source ? (
                              <Text size="xs" c="dimmed">
                                {g.source}
                              </Text>
                            ) : null}
                          </Table.Td>
                          <Table.Td style={NOWRAP}>
                            {nets} of {g.members}
                          </Table.Td>
                          <Table.Td style={NOWRAP}>{mm(n)}</Table.Td>
                          <Table.Td style={NOWRAP}>
                            {headroom ? (
                              <Text size="sm" c={h + 1e-6 >= n ? 'green' : h > 0 ? 'orange' : 'red'}>
                                {mm(h)}
                              </Text>
                            ) : (
                              <Text size="sm" c="dimmed">
                                —
                              </Text>
                            )}
                          </Table.Td>
                          <Table.Td>
                            <Text size="xs" c="dimmed">
                              {worst ? `${worst.label} needs ${mm(worst.need)}` : '—'}
                            </Text>
                          </Table.Td>
                        </Table.Tr>
                      )
                    })}
                  </Table.Tbody>
                </Table>
              </Table.ScrollContainer>
            </Stack>
          )}
        </Step>

        {need > 0 && (
          <>
            <Divider />
            <Step n={3} title="Space it needs" status={headroom && have + 1e-6 >= need ? 'done' : 'todo'}>
              {!headroom ? (
                <Group gap="xs">
                  {measuring && <Loader size="xs" />}
                  <Text size="sm" c="dimmed">
                    {measuring
                      ? `Measuring how much room there is beside every route: ${roughly(
                          analysis.board.tracks,
                        )} on a board this size.`
                      : 'Not measured yet.'}
                  </Text>
                </Group>
              ) : (
                <Stack gap="xs">
                  <SimpleGrid cols={{ base: 2, sm: 3 }} spacing="md">
                    <Figure value={mm2(area)} label="of clear board, at a guess" />
                    <Figure value={mm(run)} label="of straight track to fold it into" />
                    <Figure
                      value={mm2(areaShort)}
                      label="of that still to be found"
                      colour={areaShort > 0 ? 'orange' : 'green'}
                    />
                  </SimpleGrid>
                  <Progress.Root size="lg">
                    <Progress.Section value={need > 0 ? (have / need) * 100 : 100} color="green">
                      <Progress.Label>{mm(have)} reachable</Progress.Label>
                    </Progress.Section>
                    {have < need && (
                      <Progress.Section
                        value={((need - have) / need) * 100}
                        color="gray.4"
                      >
                        <Progress.Label>{mm(need - have)} short</Progress.Label>
                      </Progress.Section>
                    )}
                  </Progress.Root>
                  <Text size="xs" c="dimmed">
                    A best-case estimate: it assumes the largest meanders. The space must be next
                    to each net, so check the table above per net.
                    {analysis.bus_spare_mm > 0.01 ? (
                      <>
                        {' '}
                        Spreading these buses apart could free {mm(analysis.bus_spare_mm)} of room.
                      </>
                    ) : null}
                  </Text>
                </Stack>
              )}
            </Step>

            <Divider />

            <Step
              n={4}
              title="What to do, net by net"
              status={headroom && reroute.length === 0 && tight.length === 0 ? 'done' : 'todo'}
            >
              {headroom && rows.length > brief.nets && (
                <Text size="xs" c="dimmed" mb="xs">
                  An address line appears once per memory chip, because each part of the track
                  is matched separately.
                </Text>
              )}
              {!headroom ? (
                <Text size="sm" c="dimmed">
                  {measuring
                    ? 'Working out which nets can be matched where they are…'
                    : 'Measure the room to see which nets can be matched where they are.'}
                </Text>
              ) : (
                <Accordion variant="separated" multiple defaultValue={['reroute']}>
                  <Accordion.Item value="fits">
                    <Accordion.Control>
                      <Group gap="sm">
                        <Badge color="green" variant="light">
                          {fits.length}
                        </Badge>
                        <Text size="sm">
                          can be matched as they are: {mm(sum(fits))}
                        </Text>
                      </Group>
                    </Accordion.Control>
                    <Accordion.Panel>
                      <Text size="sm" c="dimmed" mb="xs">
                        There is enough room next to these tracks.
                      </Text>
                      <NetTable rows={fits} kind="fits" />
                    </Accordion.Panel>
                  </Accordion.Item>

                  <Accordion.Item value="tight">
                    <Accordion.Control>
                      <Group gap="sm">
                        <Badge color="orange" variant="light">
                          {tight.length}
                        </Badge>
                        <Text size="sm">need more room: {mm(sum(tight))}</Text>
                      </Group>
                    </Accordion.Control>
                    <Accordion.Panel>
                      <Text size="sm" c="dimmed" mb="xs">
                        Not enough free space next to these tracks. Move nearby tracks or spread the bus
                        to make room.
                      </Text>
                      <NetTable rows={tight} kind="tight" />
                    </Accordion.Panel>
                  </Accordion.Item>

                  <Accordion.Item value="reroute">
                    <Accordion.Control>
                      <Group gap="sm">
                        <Badge color="red" variant="light">
                          {reroute.length}
                        </Badge>
                        <Text size="sm">
                          need rerouting, not tuning: {mm(sum(reroute))}
                        </Text>
                      </Group>
                    </Accordion.Control>
                    <Accordion.Panel>
                      <Text size="sm" c="dimmed" mb="xs">
                        These need more than a quarter of their own length added, or are already too
                        long. A meander cannot fix that. Reroute them to the length in the last
                        column.
                      </Text>
                      <NetTable rows={reroute} kind="reroute" />
                    </Accordion.Panel>
                  </Accordion.Item>
                </Accordion>
              )}
            </Step>
          </>
        )}
      </Stack>

      {onContinue && need > 0 && (
        <Group justify="flex-end" align="center" mt="lg" gap="sm">
          {/* Everything above this button reads the board. Everything past it
              writes to a copy of it, and that half has not been through a fab
              yet -- worth saying where the decision is made, not only once the
              result is on screen. */}
          <Text size="xs" c="dimmed">
            Changing the board is the experimental half: check the result in KiCad.
          </Text>
          <Button onClick={onContinue}>Choose what to change</Button>
        </Group>
      )}
    </Card>
  )
}

/** The spans in one bucket, with the figures that bucket is about. */
function NetTable({ rows, kind }: { rows: LegRow[]; kind: 'fits' | 'tight' | 'reroute' }) {
  if (rows.length === 0) {
    return (
      <Text size="sm" c="dimmed">
        None.
      </Text>
    )
  }
  return (
    <Table.ScrollContainer minWidth={480}>
      <Table verticalSpacing={4} fz="xs" striped>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Net</Table.Th>
            <Table.Th>Now</Table.Th>
            <Table.Th>Needs</Table.Th>
            {kind === 'reroute' ? (
              <>
                <Table.Th>Of its own length</Table.Th>
                <Table.Th>Route it to</Table.Th>
              </>
            ) : (
              <>
                <Table.Th>Room</Table.Th>
                <Table.Th>{kind === 'tight' ? 'Short of room by' : 'Space it takes'}</Table.Th>
              </>
            )}
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {rows.map((c) => (
            <Table.Tr key={`${c.net}|${c.group}`}>
              <Table.Td>
                <Text size="xs" fw={500}>
                  {c.label}
                </Text>
                {c.leg && (
                  <Text size="xs" c="dimmed" ff="monospace">
                    {c.leg}
                  </Text>
                )}
              </Table.Td>
              <Table.Td style={NOWRAP}>{mm(c.length_mm)}</Table.Td>
              <Table.Td style={NOWRAP}>{mm(c.need_mm)}</Table.Td>
              {kind === 'reroute' ? (
                <>
                  <Table.Td style={NOWRAP}>
                    {c.length_mm > 0 ? Math.round((c.need_mm / c.length_mm) * 100) : 0}%
                  </Table.Td>
                  <Table.Td style={NOWRAP}>
                    <Text size="xs" fw={600} ff="monospace">
                      {mm(c.length_mm + c.need_mm)}
                    </Text>
                  </Table.Td>
                </>
              ) : (
                <>
                  <Table.Td style={NOWRAP}>{mm(c.headroom_mm)}</Table.Td>
                  <Table.Td style={NOWRAP}>
                    {kind === 'tight' ? (
                      <Text size="xs" c="orange">
                        {mm(Math.max(0, c.need_mm - c.headroom_mm))}
                      </Text>
                    ) : (
                      <Tooltip
                        label={`about ${mm(c.run_needed_mm ?? 0)} of straight track`}
                        withArrow
                      >
                        <Text size="xs" c="dimmed">
                          {mm2(c.space_needed_mm2 ?? 0)}
                        </Text>
                      </Tooltip>
                    )}
                  </Table.Td>
                </>
              )}
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
    </Table.ScrollContainer>
  )
}
