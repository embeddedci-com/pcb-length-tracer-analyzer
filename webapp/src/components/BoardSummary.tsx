/**
 * What the board is, and what was made of it.
 *
 * What is missing from the routing is not here: it is the first thing the
 * briefing above says, because it is the first thing to do something about.
 * What is left here is what the board is -- its stackup, its parts, its copper
 * -- and the fly-by topology, which is a fact about the board rather than a
 * task, and the one every per-leg length rests on.
 */

import { Alert, Badge, Card, Group, List, SimpleGrid, Stack, Text, Title } from '@mantine/core'
import type { Analysis, ChainInfo } from '../lib/analyzerApi'
import { NOWRAP, caveats, mm, packageStatus } from '../lib/format'

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <Text size="xs" c="dimmed" tt="uppercase">
        {label}
      </Text>
      <Text size="sm" fw={500}>
        {value}
      </Text>
    </div>
  )
}

/**
 * The fly-by chain, above everything about lengths, because it decides whether
 * any of those lengths mean anything.
 *
 * Address, command, control and clock reach the devices in series: controller
 * to the first, that one to the next, terminated at the end. It is not a style
 * — the topology is what makes write levelling work, and a T or a star instead
 * leaves a stub on every one of those nets. So a missing hop is not an omission
 * to tidy up later: until it exists there is no topology to match, and KiCad
 * will not tell you, because each of those nets has copper on it and
 * connectivity between the pieces is not what its DRC checks.
 */
function ChainCard({ chain }: { chain: ChainInfo }) {
  return (
    <Card withBorder padding="md">
      <Group justify="space-between" mb="xs">
        <Title order={4}>Fly-by chain</Title>
        <Badge color={chain.complete ? 'green' : 'orange'} variant="light">
          {chain.complete ? 'complete' : 'incomplete'}
        </Badge>
      </Group>
      <Text size="sm" ff="monospace" mb="xs">
        {[...chain.order, 'termination'].join(' \u2192 ')}
      </Text>
      <Stack gap={4} mb="xs">
        {chain.hops.map((h) => (
          <Group key={`${h.from}->${h.to}`} gap="xs" wrap="nowrap">
            <Badge
              size="sm"
              variant="light"
              color={h.routed ? 'green' : h.nets === 0 ? 'red' : 'orange'}
              style={NOWRAP}
            >
              {h.from} &rarr; {h.to}
            </Badge>
            <Text size="sm" c="dimmed">
              {h.routed
                ? `routed on all ${h.of} nets`
                : h.nets === 0
                  ? `not routed on any of ${h.of} nets`
                  : `routed on ${h.nets} of ${h.of} nets`}
            </Text>
          </Group>
        ))}
      </Stack>
      {chain.order_from ? (
        <Text size="xs" c="dimmed">
          {chain.order_from}.
        </Text>
      ) : null}
      {chain.note ? (
        <Text size="xs" c="orange" mt={4}>
          {chain.note}.
        </Text>
      ) : null}
      {!chain.complete ? (
        <Alert color="orange" variant="light" mt="sm" title="This bus is not routed fly-by yet">
          <Text size="sm">
            Address, command and clock lines must run from the controller to each memory chip
            in turn and end at a termination resistor (fly-by routing). Some of these hops are
            missing. Route them first: until then, the groups below only cover the parts that
            exist.
          </Text>
        </Alert>
      ) : null}
    </Card>
  )
}

export function BoardSummary({ analysis }: { analysis: Analysis }) {
  const { board, interface: iface, routing } = analysis
  const notes = caveats(analysis)
  const pkg = packageStatus(analysis)

  return (
    <Stack gap="md">
      <Card withBorder padding="md">
        <SimpleGrid cols={{ base: 2, sm: 3, md: 4 }} spacing="md">
          <Fact label="Board" value={board.filename} />
          {iface.controller ? <Fact label="Controller" value={iface.controller} /> : null}
          {iface.controller ? (
            <Fact label="Memory" value={(iface.devices ?? []).join(', ') || '—'} />
          ) : null}
          {iface.controller ? (
            <Fact label="Interface" value={`x${iface.width_bits} in ${iface.lanes} byte lanes`} />
          ) : null}
          <Fact label="Copper layers" value={`${board.copper_layers.length} (${board.copper_layers.join(', ')})`} />
          <Fact label="Stack-up" value={mm(board.stackup_mm)} />
          {pkg ? (
            <Fact
              label="Package lengths"
              value={
                pkg.state === 'none'
                  ? 'none'
                  : `${pkg.withLength} of ${pkg.total} pads${pkg.source ? ` · ${pkg.source}` : ''}`
              }
            />
          ) : null}
          {iface.nets_found ? (
            <Fact label="Nets classified" value={String(iface.nets_found)} />
          ) : null}
          <Fact
            label="Copper"
            value={`${board.tracks} tracks, ${board.vias} vias`}
          />
        </SimpleGrid>
      </Card>

      {routing.chain ? <ChainCard chain={routing.chain} /> : null}

      {notes.length > 0 && (
        <Alert color="blue" variant="light" title="Worth knowing before you trust this">
          <List size="sm" spacing="xs">
            {notes.map((n) => (
              <List.Item key={n}>{n}</List.Item>
            ))}
          </List>
        </Alert>
      )}

      {(iface.unclassified?.length ?? 0) > 0 && (
        <Alert color="yellow" variant="light" title="Not classified">
          <Text size="sm">
            These nets look like they belong to the interface but could not be placed, so they are
            in no group: {iface.unclassified!.join(', ')}
          </Text>
        </Alert>
      )}
    </Stack>
  )
}
