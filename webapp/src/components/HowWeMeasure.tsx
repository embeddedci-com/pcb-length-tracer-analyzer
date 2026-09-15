/**
 * How a length is worked out, in formulas, before and beside the numbers.
 *
 * A length on this page is not only track. It can include the via barrels the
 * route crosses, the run from each end pad's centre to the track, and the
 * wiring inside the chip package -- each of which can be millimetres, and each
 * of which is a reason a figure here differs from one somebody measures by
 * hand. So the formula is stated up front, on the home page before anything is
 * uploaded, and again on a board with what was and was not counted on that
 * board.
 *
 * Every figure here must match the engine: netlen (route length, via rule,
 * pads, package), board/stackup.go (delay), ddr/plan.go (targets, verdict).
 */

import type { ReactNode } from 'react'
import { Accordion, Anchor, List, SimpleGrid, Stack, Text, Title } from '@mantine/core'
import type { Analysis, LengthParts } from '../lib/analyzerApi'
import { mm, packageStatus } from '../lib/format'

const FORMULA: React.CSSProperties = {
  fontFamily: 'var(--mantine-font-family-monospace)',
  fontSize: 'var(--mantine-font-size-sm)',
  background: 'var(--mantine-color-gray-0)',
  border: '1px solid var(--mantine-color-gray-3)',
  borderRadius: 'var(--mantine-radius-sm)',
  padding: '6px 10px',
  overflowX: 'auto',
}

function Formula({ children }: { children: ReactNode }) {
  return <div style={FORMULA}>{children}</div>
}

/** A variable with a subscript: L<sub>track</sub>. */
function V({ n, s }: { n: string; s?: string }) {
  return (
    <>
      <i>{n}</i>
      {s ? <sub>{s}</sub> : null}
    </>
  )
}

/** What the board in front of the reader actually counted. */
export interface MeasureFacts {
  viaCounted: boolean
  viaBarrelMM: number
  routes: number
  routesWithVias: number
  maxPadMM: number
  packageLine: string | null
}

export function measureFacts(a: Analysis): MeasureFacts {
  const parts: LengthParts[] = []
  for (const g of a.groups ?? []) {
    for (const m of g.members) if (m.routed && m.parts) parts.push(m.parts)
  }
  for (const i of a.interfaces ?? []) {
    if (i.planner) continue
    for (const c of i.candidates ?? []) if (c.parts) parts.push(c.parts)
  }
  const pkg = packageStatus(a)
  let packageLine: string | null = null
  if (pkg) {
    packageLine =
      pkg.state === 'none'
        ? `No package lengths: ${pkg.controller}'s lengths are board copper only.`
        : pkg.state === 'partial'
          ? `Package lengths on ${pkg.withLength} of ${pkg.total} of ${pkg.controller}'s pads (${pkg.source}); the rest count 0 mm.`
          : `Package lengths included for ${pkg.controller} (${pkg.source}).`
  } else if (parts.some((p) => p.package_mm > 0)) {
    packageLine = 'Package lengths included where a footprint declares a die length.'
  }
  return {
    viaCounted: a.board.via_length_counted,
    viaBarrelMM: a.board.via_barrel_mm ?? 0,
    routes: parts.length,
    routesWithVias: parts.filter((p) => p.vias > 0).length,
    maxPadMM: parts.reduce((m, p) => Math.max(m, p.pad_mm), 0),
    packageLine,
  }
}

function BoardFacts({ f }: { f: MeasureFacts }) {
  return (
    <List size="sm" spacing={4}>
      <List.Item>
        {f.viaCounted ? (
          <>
            <b>Vias are counted.</b> The board&rsquo;s <code>use_height_for_length_calcs</code> setting
            is on, so each via crossed adds its height
            {f.viaBarrelMM > 0 ? ` (${mm(f.viaBarrelMM)} for a via through the whole board)` : ''}.{' '}
            {f.routes > 0 &&
              (f.routesWithVias > 0
                ? `${f.routesWithVias} of the ${f.routes} matched routes cross at least one via.`
                : `None of the ${f.routes} matched routes crosses a via.`)}
          </>
        ) : (
          <>
            <b>Vias add nothing.</b> The board&rsquo;s <code>use_height_for_length_calcs</code> setting
            is off, so a via counts 0&nbsp;mm, as it does in KiCad.
          </>
        )}
      </List.Item>
      <List.Item>
        <b>Pads are counted.</b> Each end adds the distance from the pad centre to the track
        {f.maxPadMM > 0 ? `: at most ${mm(f.maxPadMM)} per route on this board.` : '.'}
      </List.Item>
      {f.packageLine && (
        <List.Item>
          <b>Package.</b> {f.packageLine}
        </List.Item>
      )}
    </List>
  )
}

export function HowWeMeasure({
  analysis,
  defaultOpen = false,
}: {
  /** The board, for saying what was counted on it. Omit before anything is uploaded. */
  analysis?: Analysis
  defaultOpen?: boolean
}) {
  const facts = analysis ? measureFacts(analysis) : null
  return (
    <Accordion variant="contained" defaultValue={defaultOpen ? 'how' : null}>
      <Accordion.Item value="how">
        <Accordion.Control>
          <Text fw={600} size="sm">
            How lengths are measured
          </Text>
          <Text size="xs" c="dimmed" ff="monospace">
            length = track + vias + pads + package
          </Text>
        </Accordion.Control>
        <Accordion.Panel>
          <Stack gap="md">
            {facts && (
              <div>
                <Title order={6} mb={4}>
                  On this board
                </Title>
                <BoardFacts f={facts} />
              </div>
            )}

            <SimpleGrid cols={{ base: 1, md: 2 }} spacing="lg">
              <Stack gap={6}>
                <Title order={6}>Length of a net</Title>
                <Formula>
                  <V n="L" /> = <V n="L" s="track" /> + <V n="n" s="via" />·<V n="h" s="via" /> +{' '}
                  <V n="L" s="pad" /> + <V n="L" s="package" />
                </Formula>
                <List size="sm" spacing={4} c="dimmed">
                  <List.Item>
                    <V n="L" s="track" />: the centreline of every track and arc on the route from one
                    end pad to the other. Stubs and unconnected copper are not on the route and are not
                    counted.
                  </List.Item>
                  <List.Item>
                    <V n="n" s="via" />·<V n="h" s="via" />: each via the route crosses adds its height,
                    from the top of its top copper layer to the bottom of its bottom one, taken from the
                    stack-up. Only when the board&rsquo;s <code>use_height_for_length_calcs</code> is on;
                    otherwise 0, as in KiCad.
                  </List.Item>
                  <List.Item>
                    <V n="L" s="pad" />: at each end, the straight distance from the pad centre to where
                    the track touches the pad.
                  </List.Item>
                  <List.Item>
                    <V n="L" s="package" />: the wiring inside the chip at each end, from the
                    pad&rsquo;s die length in the footprint, or a known part&rsquo;s package table, or
                    what you enter. 0 when there is none.
                  </List.Item>
                  <List.Item>
                    A fly-by address or clock line is measured per leg: from the controller to each
                    memory chip, over that part of the route.
                  </List.Item>
                </List>
              </Stack>

              <Stack gap={6}>
                <Title order={6}>Target and verdict</Title>
                <Formula>
                  <V n="T" s="byte lane" /> = (<V n="L" s="DQS_P" /> + <V n="L" s="DQS_N" />) / 2
                </Formula>
                <Formula>
                  <V n="T" s="addr/cmd" /> = (<V n="L" s="CLK_P" /> + <V n="L" s="CLK_N" />) / 2 · (1 +{' '}
                  <V n="offset" /> / 100)
                </Formula>
                <Formula>
                  Δ = <V n="L" /> − <V n="T" /> &nbsp;&nbsp; within tolerance when |Δ| ≤ <V n="tol" />
                </Formula>
                <Formula>
                  to add = <V n="T" /> − <V n="L" /> &nbsp;&nbsp; too long by = Δ − <V n="tol" />
                </Formula>
                <List size="sm" spacing={4} c="dimmed">
                  <List.Item>
                    The clock is measured over the same leg as the line matched to it. With no routed
                    reference, the target is the group&rsquo;s longest net.
                  </List.Item>
                  <List.Item>
                    Other interfaces match to their clock, or the mean of a clock pair. A differential
                    pair on its own matches its shorter half to the longer.
                  </List.Item>
                  <List.Item>
                    A tolerance given in picoseconds is turned into millimetres at the group&rsquo;s
                    propagation speed; with both given, the tighter one applies.
                  </List.Item>
                  <List.Item>A meander can only add length: a net too long has to be rerouted.</List.Item>
                </List>
              </Stack>
            </SimpleGrid>

            <Stack gap={6}>
              <Title order={6}>Delay</Title>
              <Formula>
                <V n="t" /> = Σ <V n="ℓ" s="i" /> · √<V n="ε" s="eff,i" /> / <V n="c" /> &nbsp;&nbsp;
                <V n="c" /> = 0.2998 mm/ps
              </Formula>
              <Formula>
                outer layer: <V n="ε" s="eff" /> = (<V n="ε" s="r" /> + 1)/2 + (<V n="ε" s="r" /> − 1)/2
                · (1 + 10<V n="h" />/<V n="w" />)<sup>−½</sup> &nbsp;&nbsp; inner layer:{' '}
                <V n="ε" s="eff" /> = <V n="ε" s="r" />
              </Formula>
              <Text size="sm" c="dimmed">
                Each piece of the route uses the permittivity of its own layer, from the board&rsquo;s
                stack-up (<V n="h" />: dielectric height, <V n="w" />: track width); a via uses the
                stack&rsquo;s average. A millimetre on an outer layer is faster than one on an inner
                layer, so equal lengths on different layers are not equal delays.
              </Text>
            </Stack>

            <Text size="sm" c="dimmed">
              KiCad&rsquo;s net inspector shows a different figure: all copper on the net added up,
              stubs and unconnected pieces included, with a via counted only where the net has copper
              on two of its layers. On a finished point-to-point net the two agree; on a fly-by leg or
              an unfinished net they do not. See the{' '}
              <Anchor
                href="https://github.com/embeddedci-com/pcb-trace-length-analyzer/blob/main/docs/design.md"
                target="_blank"
              >
                design notes
              </Anchor>
              .
            </Text>
          </Stack>
        </Accordion.Panel>
      </Accordion.Item>
    </Accordion>
  )
}
