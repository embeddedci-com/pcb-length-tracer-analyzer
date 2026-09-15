/**
 * What is on this board, and what you want it to work on.
 *
 * The tool began as a DDR matcher and DDR is the hardest case rather than the
 * only one: Ethernet, USB, PCIe, MIPI and SD all want the same measurement over
 * simpler shapes. So the board is read for everything it carries and the choice
 * of what to touch is the user's, made before anything is measured in detail.
 *
 * Every row shows the evidence it was recognised by, because recognition is by
 * net name — a reading, not a proof. Nothing in the geometry says a pair is
 * PCIe rather than SATA, and the honest way to present that is to show the
 * names it matched on and let the user untick the box.
 */

import { useMemo, useState } from 'react'
import {
  Accordion,
  Alert,
  Badge,
  Button,
  Card,
  Checkbox,
  Group,
  Select,
  Stack,
  Table,
  Text,
  Title,
  Tooltip,
} from '@mantine/core'
import { LengthInput } from './LengthInput'
import type { DetectedInterface, FamilyInfo, InterfaceOverride } from '../lib/analyzerApi'
import { NOWRAP, mm } from '../lib/format'

export interface InterfacePickerProps {
  interfaces: DetectedInterface[]
  selected: string[]
  onChange: (ids: string[]) => void

  /** The families the tool can describe, for saying what an interface really is. */
  families?: FamilyInfo[]

  /** What has already been said about these interfaces. */
  overrides?: InterfaceOverride[]

  /** Called when the user changes an assignment or supplies a figure. */
  onOverride?: (next: InterfaceOverride[]) => void

  busy?: boolean
}

/**
 * The signal names a family usually carries, as a sentence.
 *
 * Written out because this is how somebody recognises their own nets in a list:
 * a board that calls its camera link after the sensor is not one the tool can
 * read, and "a MIPI link usually carries D0+/-, D1+/- and CK+/-" is a question
 * the person who drew it can answer.
 */
function typicalNames(f: FamilyInfo): string {
  const parts = [
    f.tx?.length ? `${f.tx.join(', ')} going out` : '',
    f.rx?.length ? `${f.rx.join(', ')} coming back` : '',
  ].filter(Boolean)
  if (f.common?.length) {
    const common = f.common.join(', ')
    parts.push(parts.length > 0 ? `and ${common} either way` : common)
  }
  return parts.join('; ')
}

/**
 * How far off a target an impedance is, as a colour.
 *
 * Ten per cent is the line because that is roughly where the closed-form model
 * stops being able to tell a real problem from its own error — closer than that
 * and only a field solver on the fabricator's stackup can say.
 */
function impedanceColour(got: number | undefined, target: number | undefined): string | undefined {
  if (!got || !target) return undefined
  const off = Math.abs(got - target) / target
  if (off <= 0.1) return 'green'
  if (off <= 0.2) return 'orange'
  return 'red'
}

/** A short word for the family, for the badge. */
const KIND_LABEL: Record<string, string> = {
  ddr: 'DDR',
  rgmii: 'Ethernet',
  rmii: 'Ethernet',
  pcie: 'PCIe',
  usb2: 'USB',
  'usb-ss': 'USB',
  mipi: 'MIPI',
  sdmmc: 'SD/eMMC',
  differential: 'pairs',
}

export function InterfacePicker({
  interfaces,
  selected,
  onChange,
  families = [],
  overrides = [],
  onOverride,
  busy = false,
}: InterfacePickerProps) {
  const chosen = useMemo(() => new Set(selected), [selected])
  const byId = useMemo(() => new Map(overrides.map((o) => [o.id, o])), [overrides])
  const familyOf = useMemo(() => new Map(families.map((f) => [f.kind, f])), [families])

  // Edits are held here until they are applied, because re-reading the board
  // costs a request and nobody wants one per keystroke.
  const [draft, setDraft] = useState<Map<string, InterfaceOverride>>(new Map())
  const dirty = draft.size > 0

  const edit = (id: string, patch: Partial<InterfaceOverride>) => {
    setDraft((prev) => {
      const next = new Map(prev)
      next.set(id, { ...(byId.get(id) ?? { id }), ...(prev.get(id) ?? {}), ...patch, id })
      return next
    })
  }
  const current = (id: string): InterfaceOverride => draft.get(id) ?? byId.get(id) ?? { id }

  const apply = () => {
    if (!onOverride) return
    const merged = new Map(byId)
    for (const [id, o] of draft) merged.set(id, o)
    onOverride([...merged.values()])
    setDraft(new Map())
  }

  const toggle = (id: string, on: boolean) => {
    const next = new Set(chosen)
    if (on) next.add(id)
    else next.delete(id)
    onChange(interfaces.filter((i) => next.has(i.id)).map((i) => i.id))
  }

  const actionable = interfaces.filter((i) => i.actionable)

  return (
    <Card withBorder padding="md">
      <Group justify="space-between" mb="xs">
        <Title order={4}>Interfaces on this board</Title>
        <Group gap="xs">
          <Badge variant="light">{interfaces.length} found</Badge>
          <Badge variant="light" color={actionable.length > 0 ? 'orange' : 'green'}>
            {actionable.length} with something to fix
          </Badge>
        </Group>
      </Group>
      <Text size="sm" c="dimmed" mb="sm">
        Each of these was recognized from the net names on the board, so each one shows what it
        matched on. Check the ones you want worked on; uncheck anything the tool has read wrongly.
      </Text>

      <Accordion variant="separated" multiple>
        {interfaces.map((i) => (
          <Accordion.Item key={i.id} value={i.id}>
            <Accordion.Control>
              <Group gap="sm" wrap="nowrap" align="flex-start">
                <Checkbox
                  checked={chosen.has(i.id)}
                  onChange={(e) => toggle(i.id, e.currentTarget.checked)}
                  onClick={(e) => e.stopPropagation()}
                  aria-label={`Work on ${i.name}`}
                />
                <div style={{ flex: 1, minWidth: 0 }}>
                  <Group gap="xs" wrap="nowrap">
                    <Text fw={500}>{i.name}</Text>
                    <Badge size="sm" variant="light" style={NOWRAP}>
                      {KIND_LABEL[i.kind] ?? i.kind}
                    </Badge>
                    <Badge
                      size="sm"
                      variant="light"
                      color={i.actionable ? 'orange' : i.unroutable > 0 ? 'gray' : 'green'}
                      style={NOWRAP}
                    >
                      {i.summary}
                    </Badge>
                  </Group>
                  <Text size="xs" c="dimmed">
                    {i.nets} nets, {i.routed} with copper
                    {i.pairs > 0 ? `, ${i.pairs} differential pair${i.pairs === 1 ? '' : 's'}` : ''}
                  </Text>
                </div>
              </Group>
            </Accordion.Control>
            <Accordion.Panel>
              <Stack gap="sm">
                <Text size="xs" c="dimmed">
                  {i.assigned ? 'You said: ' : 'Recognized by: '}
                  {i.evidence}
                </Text>
                {i.planner ? (
                  <Text size="xs" c="dimmed">
                    Handled by {i.planner}.
                  </Text>
                ) : null}

                {families.length > 0 && (
                  <Group align="flex-end" gap="md" wrap="wrap">
                    <Select
                      size="xs"
                      w={260}
                      label="What this actually is"
                      description="The tool read this from your net names. You know your board."
                      data={families.map((f) => ({ value: f.kind, label: f.label }))}
                      value={current(i.id).kind ?? i.kind}
                      onChange={(v) => v && edit(i.id, { kind: v })}
                    />
                    <LengthInput
                      size="xs"
                      w={170}
                      label="Track width"
                      description={
                        i.geometry.measured?.includes('width')
                          ? 'measured, confirm'
                          : i.geometry.needs_width
                            ? 'nothing routed; please give it'
                            : undefined
                      }
                      step={0.01}
                      min={0}
                      value={current(i.id).width_mm ?? i.geometry.width_mm ?? 0}
                      onChange={(v) => edit(i.id, { width_mm: typeof v === 'number' ? v : Number(v) })}
                    />
                    {i.pairs > 0 && (
                      <LengthInput
                        size="xs"
                        w={190}
                        label="Gap between the pair"
                        description={
                          i.geometry.measured?.includes('pair spacing')
                            ? 'measured, confirm'
                            : i.geometry.needs_gap
                              ? 'nothing coupled yet; please give it'
                              : undefined
                        }
                        step={0.01}
                        min={0}
                        value={current(i.id).gap_mm ?? i.geometry.gap_mm ?? 0}
                        onChange={(v) => edit(i.id, { gap_mm: typeof v === 'number' ? v : Number(v) })}
                      />
                    )}
                  </Group>
                )}

                {i.geometry.note ? (
                  <Text size="xs" c="dimmed">
                    {i.geometry.note}.
                  </Text>
                ) : null}

                <ImpedanceRow iface={i} family={familyOf.get(current(i.id).kind ?? i.kind)} />

                {(i.groups?.length ?? 0) > 0 && (
                  <div>
                    <Text size="sm" fw={500} mb={4}>
                      Matched against a reference
                    </Text>
                    <Table.ScrollContainer minWidth={480}>
                      <Table verticalSpacing={4} fz="xs">
                        <Table.Thead>
                          <Table.Tr>
                            <Table.Th>Group</Table.Th>
                            <Table.Th>Reference</Table.Th>
                            <Table.Th>Spread</Table.Th>
                            <Table.Th>Limit</Table.Th>
                            <Table.Th>Out</Table.Th>
                          </Table.Tr>
                        </Table.Thead>
                        <Table.Tbody>
                          {i.groups!.map((g) => (
                            <Table.Tr key={g.name}>
                              <Table.Td>
                                {g.why ? (
                                  <Tooltip label={g.why} multiline w={360} withArrow>
                                    <Text size="xs" style={{ textDecoration: 'underline dotted' }}>
                                      {g.name}
                                    </Text>
                                  </Tooltip>
                                ) : (
                                  <Text size="xs">{g.name}</Text>
                                )}
                              </Table.Td>
                              <Table.Td>
                                <Text size="xs" ff="monospace">
                                  {g.reference || '—'}
                                </Text>
                              </Table.Td>
                              <Table.Td style={NOWRAP}>{mm(g.spread_mm)}</Table.Td>
                              <Table.Td style={NOWRAP}>{mm(g.limit_mm)}</Table.Td>
                              <Table.Td>
                                <Text size="xs" c={g.out_of_tolerance > 0 ? 'orange' : undefined}>
                                  {g.out_of_tolerance}
                                  {g.unroutable > 0 ? ` (+${g.unroutable} unrouted)` : ''}
                                </Text>
                              </Table.Td>
                            </Table.Tr>
                          ))}
                        </Table.Tbody>
                      </Table>
                    </Table.ScrollContainer>
                  </div>
                )}

                {(i.pair_skew?.length ?? 0) > 0 && (
                  <div>
                    <Text size="sm" fw={500} mb={4}>
                      Differential pairs
                    </Text>
                    <Table.ScrollContainer minWidth={380}>
                      <Table verticalSpacing={4} fz="xs">
                        <Table.Thead>
                          <Table.Tr>
                            <Table.Th>Pair</Table.Th>
                            <Table.Th>Skew</Table.Th>
                            <Table.Th>Limit</Table.Th>
                          </Table.Tr>
                        </Table.Thead>
                        <Table.Tbody>
                          {i.pair_skew!.map((p) => (
                            <Table.Tr key={p.name}>
                              <Table.Td>
                                <Text size="xs" ff="monospace">
                                  {p.name}
                                </Text>
                              </Table.Td>
                              <Table.Td style={NOWRAP}>
                                {p.routed ? (
                                  <Text size="xs" c={p.in_tolerance ? undefined : 'orange'}>
                                    {mm(p.skew_mm)}
                                  </Text>
                                ) : (
                                  <Text size="xs" c="dimmed">
                                    not joined up yet
                                  </Text>
                                )}
                              </Table.Td>
                              <Table.Td style={NOWRAP}>{p.limit_mm > 0 ? mm(p.limit_mm) : 'none'}</Table.Td>
                            </Table.Tr>
                          ))}
                        </Table.Tbody>
                      </Table>
                    </Table.ScrollContainer>
                  </div>
                )}
              </Stack>
            </Accordion.Panel>
          </Accordion.Item>
        ))}
      </Accordion>

      {onOverride && (
        <Group justify="flex-end" mt="sm">
          <Text size="xs" c="dimmed">
            {dirty
              ? 'Changing what an interface is, or what it is drawn to, re-reads the board.'
              : 'Change anything above and this will re-read the board.'}
          </Text>
          <Button size="xs" disabled={!dirty || busy} loading={busy} onClick={apply}>
            Apply
          </Button>
        </Group>
      )}
    </Card>
  )
}

/**
 * What the geometry comes out at, beside what the family usually asks for.
 *
 * Shown as an estimate and labelled as one. The closed forms here are good for
 * catching a width nowhere near its target; a board that has to hold its
 * impedance to a few percent needs the fabricator's stackup and a field solver,
 * and saying so is more useful than a figure to three decimal places.
 */
function ImpedanceRow({ iface, family }: { iface: DetectedInterface; family?: FamilyInfo }) {
  const z = iface.impedance
  const target = {
    se: family?.single_ended_ohms ?? z.target_single_ended_ohms,
    diff: family?.diff_ohms ?? z.target_diff_ohms,
  }
  const note = family?.ohms_note ?? z.target_note

  if (!z.computed) {
    return (
      <Alert color="gray" variant="light" title="Impedance">
        <Text size="sm">
          Nothing is routed here, so there is no width to work from. Give the track width above
          and this will say what it comes out at.
          {target.diff ? ` This family is usually drawn to ${target.diff} Ω differential.` : ''}
          {target.se ? ` Single-ended, usually ${target.se} Ω.` : ''}
          {note ? ` ${note}.` : ''}
        </Text>
      </Alert>
    )
  }

  return (
    <div>
      <Group gap="xs" mb={4}>
        <Text size="sm" fw={500}>
          Impedance
        </Text>
        <Badge size="xs" variant="light" color={z.layer_assumed ? 'orange' : undefined}>
          {z.microstrip ? 'microstrip' : 'stripline'} on {z.layer}
          {z.layer_assumed ? ' (assumed)' : ''}
        </Badge>
        {!z.in_range && (
          <Tooltip label={z.note} multiline w={320} withArrow>
            <Badge size="xs" variant="light" color="orange" style={NOWRAP}>
              outside the model&rsquo;s range
            </Badge>
          </Tooltip>
        )}
      </Group>
      <Group gap="lg">
        {z.single_ended_ohms ? (
          <Group gap={6}>
            <Text size="sm" c="dimmed">
              single-ended
            </Text>
            <Text size="sm" fw={500} c={impedanceColour(z.single_ended_ohms, target.se)}>
              {z.single_ended_ohms} Ω
            </Text>
            {target.se ? (
              <Text size="sm" c="dimmed">
                (usually {target.se} Ω)
              </Text>
            ) : null}
          </Group>
        ) : null}
        {z.diff_ohms ? (
          <Group gap={6}>
            <Text size="sm" c="dimmed">
              differential
            </Text>
            <Text size="sm" fw={500} c={impedanceColour(z.diff_ohms, target.diff)}>
              {z.diff_ohms} Ω
            </Text>
            {target.diff ? (
              <Text size="sm" c="dimmed">
                (usually {target.diff} Ω)
              </Text>
            ) : null}
          </Group>
        ) : null}
      </Group>
      <Text size="xs" c="dimmed" mt={4}>
        Estimated from the stackup in the board file, not a field solve. Good for catching a
        width nowhere near its target, not for holding one to a few percent.
        {z.layer_assumed
          ? ` Nothing is routed, so this assumes ${z.layer}: an inner layer would come out quite different.`
          : ''}
        {note ? ` ${note}.` : ''}
      </Text>
      {family && typicalNames(family) ? (
        <Text size="xs" c="dimmed" mt={4}>
          A {family.label} link usually carries {typicalNames(family)}.
        </Text>
      ) : null}
    </div>
  )
}
