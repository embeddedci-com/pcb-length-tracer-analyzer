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
        ? `none for ${pkg.controller}.`
        : pkg.state === 'partial'
          ? `${pkg.withLength} of ${pkg.total} pads on ${pkg.controller} (${pkg.source}).`
          : `${pkg.controller}, ${pkg.source}.`
  } else if (parts.some((p) => p.package_mm > 0)) {
    packageLine = 'from footprint die lengths.'
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
    <List size="sm" spacing={2}>
      <List.Item>
        {f.viaCounted ? (
          <>
            <b>Vias:</b> counted{f.viaBarrelMM > 0 ? `, ${mm(f.viaBarrelMM)} per through via` : ''}.
            {f.routes > 0 && ` ${f.routesWithVias} of ${f.routes} routes cross a via.`}
          </>
        ) : (
          <>
            <b>Vias:</b> not counted (<code>use_height_for_length_calcs</code> is off).
          </>
        )}
      </List.Item>
      <List.Item>
        <b>Pads:</b> counted{f.maxPadMM > 0 ? `, up to ${mm(f.maxPadMM)} per route` : ''}.
      </List.Item>
      {f.packageLine && (
        <List.Item>
          <b>Package:</b> {f.packageLine}
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
                <Title order={6}>Length</Title>
                <Formula>
                  <V n="L" /> = <V n="L" s="track" /> + <V n="n" s="via" />·<V n="h" s="via" /> +{' '}
                  <V n="L" s="pad" /> + <V n="L" s="package" />
                </Formula>
                <List size="sm" spacing={2} c="dimmed">
                  <List.Item>
                    <V n="L" s="track" />: track centerline, pad to pad
                  </List.Item>
                  <List.Item>
                    <V n="h" s="via" />: via height from the stackup, 0 if{' '}
                    <code>use_height_for_length_calcs</code> is off
                  </List.Item>
                  <List.Item>
                    <V n="L" s="pad" />: pad center to track, both ends
                  </List.Item>
                  <List.Item>
                    <V n="L" s="package" />: die length, both ends
                  </List.Item>
                  <List.Item>Fly-by lines: per leg</List.Item>
                </List>
              </Stack>

              <Stack gap={6}>
                <Title order={6}>Target</Title>
                <Formula>
                  <V n="T" s="byte lane" /> = (<V n="L" s="DQS_P" /> + <V n="L" s="DQS_N" />) / 2
                </Formula>
                <Formula>
                  <V n="T" s="addr/cmd" /> = (<V n="L" s="CLK_P" /> + <V n="L" s="CLK_N" />) / 2 · (1 +{' '}
                  <V n="offset" />
                  /100)
                </Formula>
                <Formula>
                  Δ = <V n="L" /> − <V n="T" />, OK if |Δ| ≤ <V n="tol" />
                </Formula>
                <List size="sm" spacing={2} c="dimmed">
                  <List.Item>No reference routed: the longest net</List.Item>
                  <List.Item>Lone differential pair: the longer half</List.Item>
                  <List.Item>Meanders only add length</List.Item>
                </List>
              </Stack>
            </SimpleGrid>

            <Stack gap={6}>
              <Title order={6}>Delay</Title>
              <Formula>
                <V n="t" /> = Σ <V n="ℓ" s="i" />·√<V n="ε" s="eff,i" /> / <V n="c" />
              </Formula>
              <Formula>
                outer: <V n="ε" s="eff" /> = (<V n="ε" s="r" />+1)/2 + (<V n="ε" s="r" />−1)/2 · (1+10
                <V n="h" />/<V n="w" />)<sup>−½</sup>, inner: <V n="ε" s="eff" /> = <V n="ε" s="r" />
              </Formula>
            </Stack>

            <Text size="xs" c="dimmed">
              KiCad&rsquo;s net inspector adds up all copper on the net, so it can differ.{' '}
              <Anchor
                size="xs"
                href="https://github.com/embeddedci-com/pcb-trace-length-analyzer/blob/main/docs/design.md"
                target="_blank"
              >
                Details
              </Anchor>
            </Text>
          </Stack>
        </Accordion.Panel>
      </Accordion.Item>
    </Accordion>
  )
}
