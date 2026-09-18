/**
 * The requirements across groups.
 *
 * Every group matches its members to its own reference. These are about the
 * references: each byte lane's strobe against the clock that reaches the same
 * device, and one device's byte lanes against another's. Neither is fixed by a
 * meander on one net -- moving a strobe moves its whole lane -- so they are
 * shown as checks with their arithmetic rather than as length to add.
 */

import { Badge, Card, Group, Table, Text, Title, Tooltip } from '@mantine/core'
import type { CheckInfo } from '../lib/analyzerApi'
import { NOWRAP, mm, signedMM } from '../lib/format'

export function ChecksCard({ checks }: { checks?: CheckInfo[] }) {
  if (!checks || checks.length === 0) return null
  const failed = checks.filter((c) => !c.ok).length
  return (
    <Card withBorder padding="md">
      <Group justify="space-between" mb="xs">
        <Title order={4}>Across groups</Title>
        <Badge variant="light" color={failed ? 'red' : 'green'}>
          {failed ? `${failed} outside the limit` : 'all within limits'}
        </Badge>
      </Group>
      <Text size="sm" c="dimmed" mb="sm">
        Each strobe (DQS) against the clock (CLK) at the same memory chip, and one memory
        chip&rsquo;s data lines against another&rsquo;s. Fixing these means changing a whole byte,
        not one net.
      </Text>
      <Table.ScrollContainer minWidth={520}>
        <Table verticalSpacing={4} fz="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Check</Table.Th>
              <Table.Th>Difference</Table.Th>
              <Table.Th>Limit</Table.Th>
              <Table.Th />
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {checks.map((c) => (
              <Table.Tr key={c.name}>
                <Table.Td>
                  <Text size="sm">{c.name}</Text>
                  <Text size="xs" c="dimmed">
                    {c.detail}
                  </Text>
                </Table.Td>
                <Table.Td style={NOWRAP}>
                  <Text size="sm" ff="monospace" c={c.ok ? undefined : 'red'}>
                    {c.kind === 'chip-delta' ? mm(c.value_mm) : signedMM(c.value_mm)}
                  </Text>
                </Table.Td>
                <Table.Td style={NOWRAP}>
                  <Text size="sm" ff="monospace" c="dimmed">
                    {c.kind === 'chip-delta' ? `≤ ${mm(c.limit_mm)}` : `±${mm(c.limit_mm)}`}
                  </Text>
                </Table.Td>
                {/* Kept whole: inside the DDR section the table is narrower,
                    and this column was being squeezed to "o…". */}
                <Table.Td style={NOWRAP}>
                  {c.ok ? (
                    <Badge size="xs" variant="light" color="green" style={{ minWidth: 'max-content' }}>
                      ok
                    </Badge>
                  ) : (
                    <Tooltip
                      label={
                        c.kind === 'chip-delta'
                          ? 'The memory chips’ data lines differ by more than this. Make the shorter ones longer or the longer ones shorter.'
                          : 'This strobe is too far from the clock. Make the whole byte, strobe included, longer or shorter.'
                      }
                      multiline
                      w={320}
                      withArrow
                    >
                      <Badge size="xs" variant="light" color="red" style={{ minWidth: 'max-content' }}>
                        outside
                      </Badge>
                    </Tooltip>
                  )}
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>
    </Card>
  )
}
