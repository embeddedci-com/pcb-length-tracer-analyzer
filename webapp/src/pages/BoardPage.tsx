/**
 * One board, from report to download.
 *
 * The three steps are deliberately separate and in this order: read what the
 * board is, decide what the rules are, then choose what to change. The
 * judgement is all in the first two -- whether the nets were grouped correctly,
 * whether the tolerances suit the speed grade, whether an unrouted leg means
 * the board is not ready -- and none of it can be recovered from a diff
 * afterwards.
 */

import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useParams } from 'react-router'
import {
  Alert,
  Anchor,
  Badge,
  Button,
  Card,
  Group,
  Loader,
  Stack,
  Stepper,
  Table,
  Text,
  Title,
} from '@mantine/core'
import {
  ApiError,
  type ApplyResponse,
  type AnalyzerApi,
  type Params,
  type RouteResponse,
  type SessionResponse,
} from '../lib/analyzerApi'
import { BoardSummary } from '../components/BoardSummary'
import { GroupTable } from '../components/GroupTable'
import { ParameterForm } from '../components/ParameterForm'
import { CandidatePicker } from '../components/CandidatePicker'
import { ApplyResult } from '../components/ApplyResult'
import { BoardPreview } from '../components/BoardPreview'
import { InterfacePicker } from '../components/InterfacePicker'
import { AreaVerdict } from '../components/AreaVerdict'
import { Readiness } from '../components/Readiness'
import { RulesCard } from '../components/RulesCard'
import { ChecksCard } from '../components/ChecksCard'
import { LayerChecks } from '../components/LayerChecks'
import { PackageNote, PackageWarning } from '../components/PackageNote'
import { ToleranceBar } from '../components/ToleranceBar'
import { RouteResult } from '../components/RouteResult'
import { NOWRAP, boardWide, buildBrief, expiresIn, mm, packageStatus } from '../lib/format'
import { UnitToggle } from '../components/UnitToggle'
import { RescanButton, SelectNetsButton } from '../components/HostActions'
import { useHost } from '../lib/host'
import { HowWeMeasure } from '../components/HowWeMeasure'
import { candidatesCSV, download, exportName, missingCSV } from '../lib/export'
import { useUnit } from '../lib/units'

/**
 * Explicit navigation between the steps.
 *
 * The stepper's own headings are clickable, but relying on them alone leaves a
 * reader with nothing to say there is a next step at all -- which is how a
 * first attempt at this page managed to be a dead end on step one.
 */
function StepNav({
  onBack,
  onNext,
  nextLabel,
  hint,
  top = false,
}: {
  onBack?: () => void
  onNext?: () => void
  nextLabel?: string
  hint?: string
  /** Placed above the step's content rather than below it. */
  top?: boolean
}) {
  return (
    <Group justify="space-between" mt={top ? 0 : 'lg'}>
      <Text size="sm" c="dimmed">
        {hint}
      </Text>
      <Group>
        {onBack && (
          <Button variant="default" onClick={onBack}>
            Back
          </Button>
        )}
        {onNext && <Button onClick={onNext}>{nextLabel ?? 'Next'}</Button>}
      </Group>
    </Group>
  )
}

// sess0Params keys the room measurement to the parameters it was taken under,
// so changing a tolerance does not leave a stale figure on screen.
function sess0Params(data: SessionResponse | undefined): string {
  return data ? JSON.stringify(data.analysis.params) : ''
}

export function BoardPage({ api }: { api: AnalyzerApi }) {
  // Subscribed here so that changing the unit re-renders the whole page: every
  // figure below is formatted by a function that reads the choice, and nothing
  // else would know to ask again.
  useUnit()
  const { sessionId = '' } = useParams()
  const host = useHost()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [step, setStep] = useState(0)
  const [result, setResult] = useState<ApplyResponse | null>(null)
  // Which interfaces the user wants worked on. Null until the board has been
  // read, then everything with something to fix -- the useful default, and one
  // the user can narrow rather than having to build up from nothing.
  const [pickedRaw, setPicked] = useState<string[] | null>(null)

  const session = useQuery({
    queryKey: ['analyzer', 'session', sessionId],
    queryFn: () => api.getSession(sessionId),
    enabled: sessionId !== '',
  })

  // The catalogue of interface families, for saying what a set of nets really
  // is. It does not change, so it is fetched once and kept.
  const defaults = useQuery({
    queryKey: ['analyzer', 'defaults'],
    queryFn: () => api.defaults(),
    staleTime: Infinity,
  })

  const replan = useMutation({
    mutationFn: (p: Params) => api.plan(sessionId, p),
    onSuccess: (res) => qc.setQueryData(['analyzer', 'session', sessionId], res),
  })
  // Measuring the room takes about a second, so it does not run on upload. On
  // the step where the question is "will this area do?", it runs by itself and
  // again whenever the areas change -- drawing a region and being told nothing
  // is only half a tool, and the answer is a measurement rather than a guess.
  // Measuring the room takes about a second, so it does not run on upload. It
  // runs on the step that asks "what needs doing" and again on the step that
  // asks "will this area do" -- both of which are unanswerable without it, and
  // a briefing that says "measure it yourself first" is not a briefing.
  const headroom = useQuery({
    queryKey: ['analyzer', 'headroom', sessionId, sess0Params(session.data)],
    queryFn: () => api.headroom(sessionId),
    enabled: step === 0 || step === 2,
  })
  // Where the tool would have put the areas. Fetched on demand: it costs a
  // second and is only wanted when somebody is about to draw.
  const suggest = useMutation({
    mutationFn: () => api.suggestedAreas(sessionId),
    onSuccess: (res) => {
      if (res.areas.length === 0) return
      replan.mutate({
        ...(session.data?.analysis.params ?? {}),
        meander_areas: res.areas.map((a) => ({
          min_x: a.min_x,
          min_y: a.min_y,
          max_x: a.max_x,
          max_y: a.max_y,
        })),
      } as Params)
    },
  })

  // Making the copper the chain is missing. A different kind of change from
  // everything else here -- it writes new traces, and it replaces the board
  // this session works on -- so it is asked for explicitly and its result is
  // shown in full rather than folded into a count.
  const [routeResult, setRouteResult] = useState<RouteResponse | null>(null)
  // Only what the user ticked: the DDR chain's hops, and the unjoined nets of
  // each ticked interface. Set below, once the analysis is known.
  let routeNets: string[] = []
  // The result lands below the panel the button is in, which on a long page is
  // off screen: a run that takes ten seconds and then appears to do nothing is
  // worse than no button at all.
  const routeCard = useRef<HTMLDivElement | null>(null)
  // Back to the top whenever there is a new page's worth of content: arriving
  // from the upload form, moving to another step, landing on the result after
  // an apply. The router keeps the old scroll position, so without this the
  // report opens half way down, below the part that says what was read.
  useEffect(() => {
    window.scrollTo({ top: 0, behavior: 'auto' })
  }, [sessionId, step])
  useEffect(() => {
    if (routeResult) routeCard.current?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }, [routeResult])
  const routeMissing = useMutation({
    mutationFn: () => api.route(sessionId, routeNets),
    onSuccess: (res) => {
      setRouteResult(res)
      if (res.changed) {
        void qc.invalidateQueries({ queryKey: ['analyzer', 'session', sessionId] })
        void qc.invalidateQueries({ queryKey: ['analyzer', 'headroom', sessionId] })
        void qc.invalidateQueries({ queryKey: ['analyzer', 'preview', sessionId] })
      }
    },
  })

  // Every net out of tolerance, whichever interface it is on, for selecting on
  // the open board in one go. Only asked for inside an editor.
  const attention = useQuery({
    queryKey: ['analyzer', 'attention', sessionId, sess0Params(session.data)],
    queryFn: () => api.nets(sessionId, { attention: true }),
    enabled: host !== null && session.data !== undefined,
  })

  const apply = useMutation({
    mutationFn: (nets: string[]) => api.apply(sessionId, { nets }),
    onSuccess: (res) => {
      setResult(res)
      setStep(3)
      void qc.invalidateQueries({ queryKey: ['analyzer', 'sessions'] })
    },
  })

  // Through the API client, not a link, so the host's credentials go with it.
  const saveResult = () => {
    void api
      .downloadResult(sessionId)
      .then(({ filename, blob }) => download(filename, blob, 'application/octet-stream'))
  }

  if (session.isLoading) {
    return (
      <Group justify="center" p="xl">
        <Loader />
      </Group>
    )
  }
  if (session.error) {
    const missing = session.error instanceof ApiError && session.error.isMissing
    return (
      <Alert color={missing ? 'yellow' : 'red'} variant="light" title={missing ? 'That board is gone' : 'Could not load it'}>
        <Stack gap="xs">
          <Text size="sm">
            {missing
              ? 'An uploaded board is kept for a few hours and then dropped. Upload it again to carry on.'
              : session.error.message}
          </Text>
          <Anchor onClick={() => void navigate('..')}>Back to the start</Anchor>
        </Stack>
      </Alert>
    )
  }
  if (!session.data) return null

  const { session: sess } = session.data
  // After an apply, the board has changed, and the header must say what it is
  // now rather than what it was when the page loaded.
  const analysis = result?.after ?? session.data.analysis

  // What the viewer offers to find: the nets that need something, worst first,
  // then every other net in a group. Naming them the way the tables do matters
  // more than it sounds -- the tables say DQ23 and the board file says
  // /ddr4/DDR_DQ23, and a picker that said the second would not look like part
  // of the same tool.
  // Ticked by default: anything a length matcher could improve, and anything
  // that is not joined up. The second half matters -- an interface with no
  // copper on it has nothing out of tolerance, so "actionable" is false for the
  // one thing on the board most in need of doing.
  const picked =
    pickedRaw ??
    (analysis.interfaces ?? [])
      .filter((i) => i.actionable || (i.unrouted_nets?.length ?? 0) > 0)
      .map((i) => i.id)

  // A board with no DDR gets no groups and no candidates. That is not an error
  // and not an empty state to apologise for: the interfaces below are still
  // measured, and everything DDR-shaped simply has nothing to say.
  const hasDDR = (analysis.groups?.length ?? 0) > 0

  // The whole board in the shape the DDR half has: every ticked interface's
  // short nets are candidates and its groups are groups, so the picker, the
  // area verdict and the apply treat an Ethernet net exactly like a DQ bit.
  const wide = boardWide(analysis, headroom.data ?? null, picked)
  const brief = buildBrief(analysis, headroom.data ?? null, picked)

  // Per-group tolerances. One place that sets them, so the bar on a group's own
  // table and the list on the Rules step can never disagree.
  const groupTol = analysis.params.group_tolerance_mm ?? {}
  const setGroupTolerance = (name: string, value: number | null) => {
    const next = { ...groupTol }
    if (value === null) delete next[name]
    else next[name] = value
    replan.mutate({ ...analysis.params, group_tolerance_mm: next })
  }
  routeNets = [
    ...(analysis.routing.missing ?? []).map((m) => m.net),
    ...(analysis.interfaces ?? [])
      .filter((i) => !i.planner && picked.includes(i.id))
      .flatMap((i) => (i.missing ?? []).map((m) => m.net)),
  ]

  const previewNets = [
    ...wide.analysis.candidates.map((c) => ({ net: c.net, label: c.label })),
    ...(analysis.groups ?? []).flatMap((g) =>
      g.members.map((m) => ({ net: m.net, label: m.label })),
    ),
  ]

  // A result outlives the page. The session record is what says whether one
  // exists, so reloading -- or coming back to a board tuned yesterday -- still
  // offers the After view and still knows which nets to mark.
  const applied = result?.changed ? null : (sess.applied ?? null)
  const hasResult = Boolean(result?.changed) || applied !== null
  const changedNets = result?.changed
    ? result.results.filter((r) => r.added_mm > 0).map((r) => r.net)
    : (applied?.nets ?? [])

  const pkgStatus = packageStatus(analysis)

  return (
    <Stack gap="lg">
      <Group justify="space-between" align="flex-start">
        <div>
          <Title order={2}>{sess.filename}</Title>
          {/* An uploaded board is dropped after a few hours, and somebody who
              comes back to a link tomorrow should find out from the page
              rather than from a 404. */}
          <Text c="dimmed" size="sm">
            {(() => {
              const left = expiresIn(sess.expires_at)
              return left === 'expired' ? '' : `kept for another ${left} · `
            })()}
            {hasDDR
              ? `${analysis.interface.controller} with ${(analysis.interface.devices ?? []).join(
                  ' and ',
                )} · x${analysis.interface.width_bits} · ${analysis.interface.nets_found} nets`
              : `${analysis.board.copper_layers.length} copper layers · ${analysis.board.tracks} tracks · ${
                  analysis.interfaces?.length ?? 0
                } interface${(analysis.interfaces?.length ?? 0) === 1 ? '' : 's'} recognized`}
          </Text>
        </div>
        <Group gap="sm" align="center">
        <RescanButton />
        <SelectNetsButton
          size="sm"
          variant="filled"
          nets={[...new Set((attention.data?.nets ?? []).map((n) => n.net))]}
        >
          Select what needs attention
        </SelectNetsButton>
        <UnitToggle />
        {/* The whole board, the same figures the briefing below adds up:
            a badge that counted only DDR sat above a panel that counted
            everything, and the two disagreed on the first screen. */}
        {pkgStatus && pkgStatus.state !== 'full' && (
          <Badge size="lg" variant="light" color="orange">
            {pkgStatus.state === 'none' ? 'no package lengths' : 'package lengths incomplete'}
          </Badge>
        )}
        {brief.missing.length > 0 && (
          <Badge size="lg" variant="light" color="red">
            {brief.missingCount} not joined up
          </Badge>
        )}
        {brief.nets > 0 ? (
          <Badge size="lg" variant="light" color="orange">
            {brief.nets} out of tolerance, {mm(brief.need)} to add
          </Badge>
        ) : brief.missing.length === 0 ? (
          <Badge size="lg" variant="light" color="green">
            all within tolerance
          </Badge>
        ) : null}
        </Group>
      </Group>

      <Stepper active={step} onStepClick={setStep}>
        <Stepper.Step label="Analyze" description="what the board needs">
          <Stack gap="lg" mt="md">
            {/* At the top: the rules are what gets changed most, and the
                report below is long. */}
            <StepNav
              top
              onNext={() => setStep(1)}
              nextLabel="Set the rules"
              hint="Tolerances, clock offset and package lengths."
            />
            <PackageWarning status={pkgStatus} onFix={() => setStep(1)} />
            <BoardSummary analysis={analysis} />
            {(analysis.notes?.length ?? 0) > 0 && (
              <Alert color="blue" variant="light" title="About this board">
                <Stack gap="xs">
                  {analysis.notes!.map((n) => (
                    <Text key={n} size="sm">
                      {n}
                    </Text>
                  ))}
                </Stack>
              </Alert>
            )}
            {(analysis.interfaces?.length ?? 0) > 0 && (
              <InterfacePicker
                interfaces={analysis.interfaces!}
                selected={picked}
                onChange={setPicked}
                families={defaults.data?.families ?? []}
                overrides={analysis.params.interfaces ?? []}
                busy={replan.isPending}
                onOverride={(next) =>
                  replan.mutate({ ...analysis.params, interfaces: next })
                }
              />
            )}
            {/* Before any figure: what a length on this page is made of, and
                what this board counted -- vias, pads, package. */}
            <HowWeMeasure analysis={analysis} defaultOpen />
            {hasDDR && (
            <div>
              <Title order={4} mb="xs">
                Groups
              </Title>
              <Text size="sm" c="dimmed" mb="sm">
                Data lines (DQ) are matched to the strobe (DQS) of their byte. Address and command
                lines are matched to the clock (CLK) at each memory chip. All lengths are measured
                from the controller.
              </Text>
              <div style={{ marginBottom: 'var(--mantine-spacing-sm)' }}>
                <PackageNote status={pkgStatus} />
              </div>

              <Stack gap="md">
                {analysis.groups.map((g) => (
                  <GroupTable
                    key={g.name}
                    group={g}
                    overridden={g.name in groupTol}
                    onTolerance={(v) => setGroupTolerance(g.name, v)}
                    busy={replan.isPending}
                  />
                ))}
              </Stack>
            </div>
            )}
            {(analysis.pair_skew?.length ?? 0) > 0 && (
              <Card withBorder padding="md">
                <Title order={5}>Differential pair skew</Title>
                <Text size="sm" c="dimmed" mb="xs">
                  For information. ST&rsquo;s routing guide (AN5724) sets no limit on the difference
                  between the P and N line of a DQS or CLK pair, and does not allow adding length to
                  only one of them. The pair&rsquo;s length is the average of both.
                </Text>
                <Table.ScrollContainer minWidth={380}>
                  <Table verticalSpacing={4}>
                    <Table.Thead>
                      <Table.Tr>
                        <Table.Th>Pair</Table.Th>
                        <Table.Th>P to N difference</Table.Th>
                      </Table.Tr>
                    </Table.Thead>
                    <Table.Tbody>
                      {analysis.pair_skew!.map((p) => (
                        <Table.Tr key={p.pair}>
                          <Table.Td>
                            <Text size="sm">{p.pair}</Text>
                          </Table.Td>
                          <Table.Td>
                            <Text size="sm" ff="monospace">
                              {mm(p.skew_mm)}
                            </Text>
                          </Table.Td>
                        </Table.Tr>
                      ))}
                    </Table.Tbody>
                  </Table>
                </Table.ScrollContainer>
              </Card>
            )}
            <ChecksCard checks={analysis.checks} />
            <LayerChecks findings={analysis.layers} />
            {/* What needs doing comes after what was found. Having parsed a board, the
                first thing to check is whether it was read right -- the interfaces it
                recognised and the groups it matched -- and only then what to do about them. */}
            {/* Shown for any board, not only one with DDR on it: an Ethernet
                link that is not joined up is the same problem, and the panel
                covers every interface the user has ticked. */}
            <Readiness
              analysis={analysis}
              headroom={headroom.data ?? null}
              measuring={headroom.isFetching}
              onRoute={routeNets.length > 0 ? () => routeMissing.mutate() : undefined}
              routing={routeMissing.isPending}
              onContinue={!host && brief.need > 1e-6 ? () => setStep(2) : undefined}
              selected={picked}
              onExport={(what) => {
                if (what === 'analysis') {
                  download(
                    exportName(sess.filename, 'analysis', 'json'),
                    JSON.stringify(analysis, null, 2),
                    'application/json',
                  )
                  return
                }
                const csv =
                  what === 'missing'
                    ? missingCSV(analysis, picked)
                    : candidatesCSV(analysis, headroom.data ?? null, picked)
                download(exportName(sess.filename, what, 'csv'), csv, 'text/csv')
              }}
            />
            {routeMissing.error && (
              <Alert color="red" variant="light" title="The router could not run">
                {routeMissing.error.message}
              </Alert>
            )}
            {routeResult && (
              <div ref={routeCard}>
                <RouteResult
                  result={routeResult}
                  onDownload={saveResult}
                  onDismiss={() => setRouteResult(null)}
                />
              </div>
            )}
            <div>
              <Title order={4} mb="xs">
                The board
              </Title>
              <Text size="sm" c="dimmed" mb="sm">
                Pick a net to find it on the board. The worst nets are listed first.
              </Text>
              <BoardPreview
                api={api}
                sessionId={sessionId}
                nets={previewNets}
                changed={changedNets}
                hasResult={hasResult}
              />
            </div>
          </Stack>
        </Stepper.Step>

        <Stepper.Step label="Rules" description="limits and package lengths">
          <Card withBorder padding="lg" mt="md">
            <Stack gap="lg">
              <ParameterForm
                value={analysis.params}
                defaults={defaults.data?.params}
                ddr={hasDDR}
                packagePads={analysis.package_pads}
                packageLengths={analysis.package_lengths}
                controller={analysis.interface?.controller}
                packageParts={defaults.data?.package_parts}
                presets={defaults.data?.presets}
                controllerValue={analysis.interface?.controller_value}
                presetsForPart={analysis.interface?.presets_for_part}
                busy={replan.isPending}
                onApply={(p) => replan.mutate(p)}
              >
                {wide.analysis.groups.length > 0 && (
                  <div>
                    <Title order={5} mb={4}>
                      Limit per group
                    </Title>
                    <Text size="sm" c="dimmed" mb="sm">
                      Drag a bar to give one group its own limit. This applies straight away.
                    </Text>
                    <Stack gap="xs">
                      {wide.analysis.groups.map((g) => (
                        <ToleranceBar
                          key={g.name}
                          label={g.name}
                          valueMM={g.tolerance_mm}
                          overridden={g.name in groupTol}
                          onChange={(v) => setGroupTolerance(g.name, v)}
                          busy={replan.isPending}
                        />
                      ))}
                    </Stack>
                  </div>
                )}
              </ParameterForm>
              {replan.error && (
                <Alert color="red" variant="light" title="Those values were refused">
                  {replan.error.message}
                </Alert>
              )}
              {/* What the board itself sets, for reference. */}
              <RulesCard rules={analysis.rules} />
            </Stack>
            <StepNav
              onBack={() => setStep(0)}
              onNext={host ? undefined : () => setStep(2)}
              nextLabel="Choose what to change"
              hint="These are the rules the report was produced with."
            />
          </Card>
        </Stepper.Step>

        {/* Inside KiCad the tool only reads: analyze the board and set the
            rules. Changing copper is the website's experimental half, and
            applying to the open board is not enabled yet. */}
        {!host && (
          <>
        <Stepper.Step
          label={
            <Group gap={6} wrap="nowrap">
              <span>Change</span>
              <Badge size="xs" variant="light" color="orange" style={NOWRAP}>
                experimental
              </Badge>
            </Group>
          }
          description="where, then confirm"
        >
          <Stack gap="lg" mt="md">
            {/* Reading a board and writing to one are not the same kind of
                claim, and this is the half that writes. What it produces has
                been checked against KiCad's own DRC on the boards here and
                behaves; what it has not had is a fabricated board coming back
                and measuring right. Saying so is cheaper than somebody finding
                out from a fab house. */}
            <Alert color="orange" variant="light" title="This half is experimental">
              <Text size="sm">
                The steps before this only read the board. This step adds meanders (zigzags that
                add length) to your existing tracks. It does not move vias, change layers or make
                new connections. Your original file is not changed. Run KiCad&rsquo;s DRC (design
                rule check) on the result before you use it.
              </Text>
            </Alert>
            {/* Where before how much. The plan says how much length each net
                needs, which is arithmetic; where to put it is a judgement
                about the board, and the person who drew it is the one who can
                make it. Leaving this empty means anywhere, which is what the
                tool did before and is right for a first look. */}
            <Card withBorder padding="md">
              <Title order={4} mb="xs">
                Where copper may be added
              </Title>
              <Text size="sm" c="dimmed" mb="sm">
                Draw the areas where meanders may go. Leave it empty to allow anywhere that fits.
                Keep them out of places like a BGA fanout (the escape routing under a chip) or
                under a connector.
              </Text>
              <BoardPreview
                api={api}
                sessionId={sessionId}
                nets={previewNets}
                changed={changedNets}
                hasResult={hasResult}
                areas={analysis.params.meander_areas ?? []}
                onAreasChange={(next) =>
                  replan.mutate({ ...analysis.params, meander_areas: next })
                }
                onSuggest={() => suggest.mutate()}
                suggesting={suggest.isPending}
                height={420}
              />
              {suggest.data && suggest.data.areas.length === 0 ? (
                <Alert color="yellow" variant="light" mt="sm" title="Nothing to propose">
                  <Text size="sm">{suggest.data.note}</Text>
                </Alert>
              ) : null}
              {suggest.data && suggest.data.areas.length > 0 ? (
                <Text size="xs" c="dimmed" mt="sm">
                  {suggest.data.note}
                </Text>
              ) : null}
              <Card.Section withBorder inheritPadding py="sm" mt="md">
                <AreaVerdict
                  analysis={wide.analysis}
                  headroom={wide.headroom}
                  loading={headroom.isFetching || replan.isPending}
                  areaCount={(analysis.params.meander_areas ?? []).length}
                />
              </Card.Section>
            </Card>
          <Card withBorder padding="lg">
            <CandidatePicker
              analysis={wide.analysis}
              headroom={wide.headroom}
              headroomLoading={headroom.isFetching}
              onMeasureHeadroom={() => void headroom.refetch()}
              busy={apply.isPending}
              error={apply.error ? apply.error.message : null}
              onApply={(nets) => apply.mutate(nets)}
            />
            <StepNav
              onBack={() => setStep(1)}
              hint="This is the step that writes. Check the result in KiCad before you build from it."
            />
          </Card>
          </Stack>
        </Stepper.Step>

        <Stepper.Completed>
          <Card withBorder padding="lg" mt="md">
            {result ? (
              <Stack gap="lg">
                <ApplyResult
                  result={result}
                  sessionId={sessionId}
                  onDownload={saveResult}
                  onStartOver={() => {
                    setResult(null)
                    setStep(0)
                    void session.refetch()
                  }}
                />
                {/* The meanders themselves. A length in a table is a claim;
                    this is the copper it was written into, and flipping to
                    Before is the only way to see what was there instead. */}
                <div>
                  <Title order={4} mb="xs">
                    What changed
                  </Title>
                  <BoardPreview
                    api={api}
                    sessionId={sessionId}
                    nets={previewNets}
                    changed={changedNets}
                    hasResult={hasResult}
                  />
                </div>
              </Stack>
            ) : (
              <Text c="dimmed">Nothing has been applied yet.</Text>
            )}
          </Card>
        </Stepper.Completed>
          </>
        )}
      </Stepper>
    </Stack>
  )
}
