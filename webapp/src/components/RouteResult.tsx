/**
 * What the router made of the connections that were missing.
 *
 * Shown in full, including every refusal and its reason, because this is the
 * part of the tool most likely to disappoint: it will not move copper that
 * belongs to somebody else, and on a board where the space for a bus was never
 * left, that means it reports a full corridor rather than making the
 * connection. A count would hide that. The reasons are what tell a layout
 * engineer whether the answer is a different layer, a different placement, or
 * simply doing it by hand.
 */

import { Alert, Anchor, Badge, Button, Card, Group, Stack, Table, Text, Title } from '@mantine/core'
import type { RouteResponse } from '../lib/analyzerApi'
import { NOWRAP, mm } from '../lib/format'

export function RouteResult({
  result,
  onDownload,
  onDismiss,
}: {
  result: RouteResponse
  onDownload: () => void
  onDismiss?: () => void
}) {
  const made = (result.hops ?? []).filter((h) => h.routed)
  const refused = (result.hops ?? []).filter((h) => !h.routed)

  return (
    <Card withBorder padding="md">
      <Group justify="space-between" align="flex-start" mb="xs">
        <div>
          <Title order={4}>The experimental router</Title>
          <Text size="sm" c="dimmed">
            {result.connected} of {result.requested} connection
            {result.requested === 1 ? '' : 's'} made
            {result.connected > 0
              ? `, ${mm(result.added_mm)} of new copper and ${result.vias} via${
                  result.vias === 1 ? '' : 's'
                }`
              : ''}
          </Text>
        </div>
        <Group gap="xs">
          <Badge
            size="lg"
            variant="light"
            color={result.connected === 0 ? 'red' : result.connected < result.requested ? 'orange' : 'green'}
          >
            {result.connected} of {result.requested}
          </Badge>
          {onDismiss && (
            <Button size="xs" variant="subtle" onClick={onDismiss}>
              Dismiss
            </Button>
          )}
        </Group>
      </Group>

      {(result.notes ?? []).map((n) => (
        <Text key={n} size="sm" c="dimmed" mb="xs">
          {n}.
        </Text>
      ))}

      {result.changed && (
        <Alert color="blue" variant="light" title="Check it before you trust it" mb="sm">
          <Text size="sm">
            The board in this session is now the routed one, and everything below is measured
            over the new copper.{' '}
            <Anchor component="button" type="button" onClick={onDownload}>
              Download it
            </Anchor>{' '}
            and open it in KiCad: what this wrote is a grid search&rsquo;s idea of where a bus
            should run, not a person&rsquo;s.
          </Text>
        </Alert>
      )}

      {made.length > 0 && (
        <>
          <Text size="sm" fw={500} mb={4}>
            Made
          </Text>
          <Table.ScrollContainer minWidth={480}>
            <Table verticalSpacing={4} fz="xs" mb="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Net</Table.Th>
                  <Table.Th>From</Table.Th>
                  <Table.Th>To</Table.Th>
                  <Table.Th>Length</Table.Th>
                  <Table.Th>Vias</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {made.map((h) => (
                  <Table.Tr key={`${h.net}|${h.from}|${h.to}`}>
                    <Table.Td>{h.label}</Table.Td>
                    <Table.Td style={NOWRAP}>{h.from}</Table.Td>
                    <Table.Td style={NOWRAP}>{h.to}</Table.Td>
                    <Table.Td style={NOWRAP}>{mm(h.length_mm ?? 0)}</Table.Td>
                    <Table.Td>{h.vias ?? 0}</Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Table.ScrollContainer>
        </>
      )}

      {refused.length > 0 && (
        <>
          <Text size="sm" fw={500} mb={4}>
            Not made
          </Text>
          <Stack gap={2}>
            {refused.map((h) => (
              <Text key={`${h.net}|${h.from}|${h.to}`} size="xs" c="dimmed">
                <Text span fw={500}>
                  {h.label}
                </Text>{' '}
                {h.from} &rarr; {h.to}: {h.reason}
              </Text>
            ))}
          </Stack>
        </>
      )}
    </Card>
  )
}
