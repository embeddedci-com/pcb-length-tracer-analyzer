/**
 * What actually happened.
 *
 * Shortfalls are shown first, and shown plainly. A tool that quietly reported
 * success on a board it had only partly corrected would be worse than one that
 * refused, so the numbers here are the board's own -- the re-measurement comes
 * from the edited copper, not from the tuner's bookkeeping -- and the page ends
 * by handing over KiCad's DRC command rather than claiming the result is sound.
 */

import { Alert, Anchor, Badge, Button, Card, Code, Group, Stack, Table, Text, Title } from '@mantine/core'
import type { Analysis, ApplyResponse } from '../lib/analyzerApi'
import { NOWRAP, mm } from '../lib/format'
import { useHost } from '../lib/host'
import { ApplyToBoard, NetName } from './HostActions'

export interface ApplyResultProps {
  result: ApplyResponse
  /** The session the result belongs to, for applying it to a board open in an editor. */
  sessionId?: string
  onDownload: () => void
  onStartOver: () => void
}

function GroupsAfter({ after }: { after: Analysis }) {
  return (
    <Table.ScrollContainer minWidth={420}>
      <Table verticalSpacing={4}>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Group</Table.Th>
            <Table.Th>Spread</Table.Th>
            <Table.Th>Out of tolerance</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {after.groups.map((g) => (
            <Table.Tr key={g.name}>
              <Table.Td>
                <Text size="sm" tt="capitalize">
                  {g.name}
                </Text>
              </Table.Td>
              <Table.Td>
                <Text size="sm" ff="monospace" style={NOWRAP}>
                  {mm(g.spread_mm)}
                </Text>
              </Table.Td>
              <Table.Td>
                <Badge
                  size="sm"
                  variant="light"
                  color={g.out_of_tolerance === 0 ? 'green' : g.out_of_tolerance > 4 ? 'red' : 'orange'}
                >
                  {g.out_of_tolerance} of {g.members.length}
                </Badge>
              </Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
    </Table.ScrollContainer>
  )
}

export function ApplyResult({ result, sessionId, onDownload, onStartOver }: ApplyResultProps) {
  const short = result.results.filter((r) => r.shortfall_mm > 1e-4)
  // Inside an editor the result goes onto the open board, not into a file.
  const host = useHost()
  return (
    <Stack gap="md">
      <Group justify="space-between" align="flex-start">
        <div>
          <Title order={4}>{result.changed ? 'Applied' : 'Nothing could be changed'}</Title>
          <Text size="sm" c="dimmed">
            {mm(result.added_mm)} added across {result.results.length} net
            {result.results.length === 1 ? '' : 's'}
            {result.shortfall_mm > 1e-4 && `, ${mm(result.shortfall_mm)} still missing`}
          </Text>
        </div>
        <Group>
          <Button variant="default" onClick={onStartOver}>
            Start over
          </Button>
          {result.changed && !host && (
            <Button onClick={onDownload}>Download the board</Button>
          )}
        </Group>
      </Group>

      {result.changed && host && sessionId && <ApplyToBoard sessionId={sessionId} />}
      {result.changed && host && !host.applyToBoard && (
        <Alert color="blue" variant="light" title={`Not applied to the board in ${host.name}`}>
          <Text size="sm">
            Writing the result into the open board is not enabled in this version. Nothing on your
            board has changed: the figures and the preview below are what the change would do.
          </Text>
        </Alert>
      )}

      {!result.changed && (
        <Alert color="orange" variant="light" title="No board to download">
          Not a millimeter could be fitted, so the board was left exactly as it was and there is
          nothing to download. Handing back the file you uploaded as though it had been corrected
          is how a board gets fabricated in the belief that it was.
        </Alert>
      )}

      {short.length > 0 && result.changed && (
        <Alert color="orange" variant="light" title={`${short.length} net${short.length === 1 ? '' : 's'} could not be fully corrected`}>
          <Text size="sm">
            The tool added what fitted. Closing the rest means opening space in the layout: a bus
            routed at its minimum clearance has nowhere beside it for a meander, and nothing here
            moves other nets’ copper or reroutes anything.
          </Text>
        </Alert>
      )}

      <Card withBorder padding="md">
        <Title order={5} mb="xs">
          Per net
        </Title>
        <Table.ScrollContainer minWidth={520}>
          <Table striped verticalSpacing={4}>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Net</Table.Th>
                <Table.Th>Asked</Table.Th>
                <Table.Th>Added</Table.Th>
                <Table.Th>Short</Table.Th>
                <Table.Th>Meanders</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {result.results.map((r) => (
                <Table.Tr key={`${r.net}|${r.leg ?? ''}`}>
                  <Table.Td>
                    <Text size="sm" fw={500}>
                      <NetName net={r.net}>{r.label}</NetName>
                      {/* A net matched over two legs of the chain is two rows,
                          and without the span they read as the same work done
                          twice. */}
                      {r.leg ? (
                        <Text span size="xs" c="dimmed" ff="monospace">
                          {' '}
                          {r.leg}
                        </Text>
                      ) : null}
                    </Text>
                    {(r.notes?.length ?? 0) > 0 && (
                      <Text size="xs" c="dimmed">
                        {r.notes!.join(' ')}
                      </Text>
                    )}
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm" ff="monospace" style={NOWRAP}>
                      {mm(r.requested_mm)}
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm" ff="monospace" style={NOWRAP} c={r.added_mm > 0 ? 'green' : 'dimmed'}>
                      {mm(r.added_mm)}
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm" ff="monospace" style={NOWRAP} c={r.shortfall_mm > 1e-4 ? 'orange' : 'dimmed'}>
                      {r.shortfall_mm > 1e-4 ? mm(r.shortfall_mm) : '—'}
                    </Text>
                  </Table.Td>
                  <Table.Td>
                    <Text size="sm" ff="monospace" c="dimmed">
                      {r.meanders}
                    </Text>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Table.ScrollContainer>
      </Card>

      <Card withBorder padding="md">
        <Title order={5} mb="xs">
          Re-measured from the edited board
        </Title>
        <Text size="sm" c="dimmed" mb="xs">
          These are the board’s own numbers, read back from the copper that was written rather than
          from what the tuner believed it did.
        </Text>
        <GroupsAfter after={result.after} />
      </Card>

      <Alert color="blue" variant="light" title="Check it in KiCad before you use it">
        <Stack gap="xs">
          {host ? (
            <Text size="sm">
              This tool’s clearance check does not interpret every custom design rule, and KiCad’s DRC
              is what a reviewer will run. Once it is applied, run Inspect → Design Rules Checker.
            </Text>
          ) : (
            <>
              <Text size="sm">
                This tool’s clearance check does not interpret custom design rules, and KiCad’s DRC is
                what a reviewer will run. Download the board and ask it:
              </Text>
              <Code block>{result.verify_command}</Code>
            </>
          )}
          <Text size="sm">
            The violation count should not have risen. See{' '}
            <Anchor href="https://github.com/embeddedci-com/pcb-trace-length-analyzer/blob/main/docs/design.md" target="_blank">
              the design notes
            </Anchor>{' '}
            for why the individual entries may be worded differently even when nothing moved.
          </Text>
        </Stack>
      </Alert>
    </Stack>
  )
}
