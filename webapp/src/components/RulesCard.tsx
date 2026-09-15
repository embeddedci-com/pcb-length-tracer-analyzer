/**
 * Where every figure this tool works to comes from.
 *
 * KiCad will tell you why a track is the width it is -- this net class, that
 * rule -- and a tool that quietly applies numbers of its own on top of the
 * board's owes the reader the same. There are three sources here and they are
 * not interchangeable: the board's own setup and net classes, the custom rules
 * in the .kicad_dru, and this tool's settings, which are the only ones that can
 * be changed from this page.
 *
 * The order is deliberate: what applies, then what the board says, then what
 * was read from the rules file and what was not. A rule left alone is the one
 * that matters most and is the easiest to bury, because it is the difference
 * between "there is no room here" and the room being there under a rule this
 * cannot read.
 */

import { Accordion, Badge, Card, Group, Table, Text, Title, Tooltip } from '@mantine/core'
import type { DesignRules } from '../lib/analyzerApi'
import { NOWRAP, mm } from '../lib/format'

export function RulesCard({ rules }: { rules?: DesignRules }) {
  if (!rules) return null
  const custom = rules.custom ?? []
  const skipped = custom.filter((r) => !r.applied)

  return (
    <Card withBorder padding="md">
      <Group justify="space-between" mb="xs">
        <Title order={4}>Rules, and where they come from</Title>
        <Group gap="xs">
          {(rules.classes?.length ?? 0) > 0 && (
            <Badge variant="light">{rules.classes!.length} net classes</Badge>
          )}
          {custom.length > 0 && (
            <Badge variant="light" color={skipped.length > 0 ? 'orange' : 'green'}>
              {custom.length - skipped.length} of {custom.length} custom rules read
            </Badge>
          )}
        </Group>
      </Group>

      {(rules.effective?.length ?? 0) > 0 && (
        <Table.ScrollContainer minWidth={520}>
          <Table verticalSpacing={4} fz="sm">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>What</Table.Th>
                <Table.Th>Value</Table.Th>
                <Table.Th>Set by</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {rules.effective!.map((e) => (
                <Table.Tr key={e.what}>
                  <Table.Td>
                    <Text size="sm">{e.what}</Text>
                    {e.note ? (
                      <Text size="xs" c="dimmed">
                        {e.note}
                      </Text>
                    ) : null}
                  </Table.Td>
                  <Table.Td style={NOWRAP}>
                    <Text size="sm" ff="monospace">
                      {mm(e.value_mm)}
                    </Text>
                  </Table.Td>
                  <Table.Td style={NOWRAP}>
                    <Text size="sm" c={e.source.startsWith('your') ? 'blue' : 'dimmed'}>
                      {e.source}
                    </Text>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      )}

      <Accordion variant="separated" mt="sm">
        <Accordion.Item value="board">
          <Accordion.Control>
            <Text size="sm">
              The board&rsquo;s own setup &mdash; minimum clearance {mm(rules.min_clearance_mm)},
              minimum track {mm(rules.min_track_width_mm)}
            </Text>
          </Accordion.Control>
          <Accordion.Panel>
            <Text size="xs" c="dimmed" mb="xs">
              From the .kicad_pro. These are the floor: a net class may ask for more, never less,
              and without the project file every clearance falls back to this.
            </Text>
            <Table.ScrollContainer minWidth={520}>
              <Table verticalSpacing={4} fz="xs">
                <Table.Thead>
                  <Table.Tr>
                    <Table.Th>Net class</Table.Th>
                    <Table.Th>Clearance</Table.Th>
                    <Table.Th>Track</Table.Th>
                    <Table.Th>Pair width / gap</Table.Th>
                    <Table.Th>Via</Table.Th>
                    <Table.Th>Nets here</Table.Th>
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {(rules.classes ?? []).map((c) => (
                    <Table.Tr key={c.name}>
                      <Table.Td>
                        <Text size="xs" fw={c.nets ? 600 : 400}>
                          {c.name}
                        </Text>
                      </Table.Td>
                      <Table.Td style={NOWRAP}>{mm(c.clearance_mm)}</Table.Td>
                      <Table.Td style={NOWRAP}>{mm(c.track_width_mm)}</Table.Td>
                      <Table.Td style={NOWRAP}>
                        {c.diff_pair_width_mm
                          ? `${mm(c.diff_pair_width_mm)} / ${mm(c.diff_pair_gap_mm ?? 0)}`
                          : '—'}
                      </Table.Td>
                      <Table.Td style={NOWRAP}>
                        {c.via_diameter_mm ? `${mm(c.via_diameter_mm)} / ${mm(c.via_drill_mm ?? 0)}` : '—'}
                      </Table.Td>
                      <Table.Td>{c.nets || '—'}</Table.Td>
                    </Table.Tr>
                  ))}
                </Table.Tbody>
              </Table>
            </Table.ScrollContainer>
          </Accordion.Panel>
        </Accordion.Item>

        {custom.length > 0 && (
          <Accordion.Item value="custom">
            <Accordion.Control>
              <Group gap="xs" wrap="nowrap">
                <Text size="sm">Custom rules from the .kicad_dru</Text>
                {skipped.length > 0 && (
                  <Badge size="xs" variant="light" color="orange" style={NOWRAP}>
                    {skipped.length} left alone
                  </Badge>
                )}
              </Group>
            </Accordion.Control>
            <Accordion.Panel>
              <Text size="xs" c="dimmed" mb="xs">
                A rule this understands is applied where it tightens a clearance and ignored
                where it would loosen one &mdash; a meander has no business being crammed into a
                BGA fanout because a rule there allows it, though the router does use them, since
                a ball that cannot be escaped at the net class clearance can be escaped at the
                one the board actually sets. A rule left alone is replaced by the net classes at
                their strictest, which can only be stricter than the board asks. KiCad is the
                authority on its own rule language.
              </Text>
              <Table.ScrollContainer minWidth={560}>
                <Table verticalSpacing={4} fz="xs">
                  <Table.Thead>
                    <Table.Tr>
                      <Table.Th>Rule</Table.Th>
                      <Table.Th>Sets</Table.Th>
                      <Table.Th>Where</Table.Th>
                    </Table.Tr>
                  </Table.Thead>
                  <Table.Tbody>
                    {custom.map((r) => (
                      <Table.Tr key={r.name}>
                        <Table.Td>
                          <Group gap={6} wrap="nowrap">
                            <Text size="xs" ff="monospace">
                              {r.name}
                            </Text>
                            {r.applied ? (
                              <Badge size="xs" variant="light" color="green">
                                applied
                              </Badge>
                            ) : (
                              <Tooltip label={r.why} multiline w={320} withArrow>
                                <Badge size="xs" variant="light" color="orange">
                                  left alone
                                </Badge>
                              </Tooltip>
                            )}
                          </Group>
                        </Table.Td>
                        <Table.Td style={NOWRAP}>
                          {r.clearance_mm ? `clearance ${mm(r.clearance_mm)}` : '—'}
                        </Table.Td>
                        <Table.Td>
                          <Text size="xs" c="dimmed" ff="monospace">
                            {r.condition || '—'}
                          </Text>
                        </Table.Td>
                      </Table.Tr>
                    ))}
                  </Table.Tbody>
                </Table>
              </Table.ScrollContainer>
            </Accordion.Panel>
          </Accordion.Item>
        )}
      </Accordion>
    </Card>
  )
}
