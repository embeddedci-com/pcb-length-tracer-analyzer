/**
 * Layers that differ from what the routing guide expects.
 *
 * For information only. ST's AN5724 puts data lines on the top layer and
 * address, command and clock mainly on the bottom layer, but a board can be
 * built another way on purpose, so nothing here counts as out of tolerance.
 */

import { Badge, Card, Group, Stack, Table, Text, Title } from '@mantine/core'
import type { LayerFinding } from '../lib/analyzerApi'
import { NOWRAP, mm } from '../lib/format'
import { NetName } from './HostActions'

const TITLE: Record<string, string> = {
  'data-top': 'Data lines not only on the top layer',
  'address-bottom': 'Address and command lines not mainly on the bottom layer',
}

export function LayerChecks({ findings }: { findings?: LayerFinding[] }) {
  if (!findings || findings.length === 0) return null
  return (
    <Card withBorder padding="md">
      <Group justify="space-between" mb={4}>
        <Title order={4}>Layer use</Title>
        <Badge variant="light" color="blue">
          for information
        </Badge>
      </Group>
      <Text size="sm" c="dimmed" mb="sm">
        We found routing on other layers than ST&rsquo;s routing guide (AN5724) expects. This is not an error if
        your board is built this way on purpose.
      </Text>
      <Stack gap="md">
        {findings.map((f) => (
          <div key={f.rule}>
            <Text size="sm" fw={600}>
              {TITLE[f.rule] ?? f.rule} ({f.nets.length})
            </Text>
            <Text size="xs" c="dimmed" mb={4}>
              Expected: {f.expect}.
            </Text>
            <Table.ScrollContainer minWidth={420} mah={260}>
              <Table verticalSpacing={2} fz="sm">
                <Table.Thead>
                  <Table.Tr>
                    <Table.Th>Net</Table.Th>
                    <Table.Th>On the expected layer</Table.Th>
                    <Table.Th>Copper per layer</Table.Th>
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {f.nets.map((n) => (
                    <Table.Tr key={n.net}>
                      <Table.Td>
                        <NetName net={n.net}>{n.label}</NetName>
                      </Table.Td>
                      <Table.Td style={NOWRAP}>{Math.round(n.share * 100)}%</Table.Td>
                      <Table.Td>
                        <Text size="xs" c="dimmed">
                          {Object.entries(n.by_layer_mm)
                            .sort(([a], [b]) => a.localeCompare(b))
                            .map(([layer, len]) => `${layer} ${mm(len)}`)
                            .join(', ')}
                        </Text>
                      </Table.Td>
                    </Table.Tr>
                  ))}
                </Table.Tbody>
              </Table>
            </Table.ScrollContainer>
          </div>
        ))}
      </Stack>
    </Card>
  )
}
