/**
 * Every value the analysis works to, for this board.
 *
 * DDR values come first and together: each shows its default and can be put
 * back. Meander and spacing settings follow, folded away.
 */

import {
  Accordion,
  Alert,
  Anchor,
  Badge,
  Button,
  Group,
  NumberInput,
  Select,
  SimpleGrid,
  Stack,
  Switch,
  Table,
  Text,
  Title,
} from '@mantine/core'
import type { ComboboxItem, ComboboxParsedItem, OptionsFilter } from '@mantine/core'
import { useEffect, useState, type ReactNode } from 'react'
import { LengthInput } from './LengthInput'
import type { PackageLength, PackagePad, Params, Preset } from '../lib/analyzerApi'
import { PARAM_HELP, applyPreset, matchingPreset, mm } from '../lib/format'

export interface ParameterFormProps {
  value: Params
  /** The tool's defaults, for showing and restoring them. */
  defaults?: Params
  /** Show the DDR section. */
  ddr?: boolean
  packagePads?: PackagePad[]
  packageLengths?: PackageLength[]
  controller?: string
  /** Parts with a package length table, to choose from. */
  packageParts?: string[]
  /** The vendors' published rules, for the preset dropdown. */
  presets?: Preset[]
  /** The controller footprint's value, e.g. "STM32MP257DAI3". */
  controllerValue?: string
  /** Ids of the presets for that part; empty means it was not recognised. */
  presetsForPart?: string[]
  busy?: boolean
  onApply: (p: Params) => void
  /** Rendered at the end of the DDR section. */
  children?: ReactNode
}

type NumKey =
  | 'clock_offset_percent'
  | 'data_to_strobe_mm'
  | 'intra_pair_mm'
  | 'address_to_clock_mm'
  | 'strobe_to_clock_mm'
  | 'strobe_to_clock_ps'
  | 'max_chip_delta_mm'
  | 'data_to_strobe_ps'
  | 'intra_pair_ps'
  | 'address_to_clock_ps'
  | 'max_intra_pair_fix_mm'
  | 'open_clearance_mm'
  | 'open_margin_mm'
  | 'max_amplitude_mm'
  | 'min_amplitude_mm'
  | 'meander_gap_widths'
  | 'meander_chamfer_widths'
  | 'min_run_mm'
  | 'pad_keepout_mm'

/** "default 1.420 mm", and a reset link once the value differs. */
function DefaultHint({
  value,
  def,
  show,
  onReset,
}: {
  value: number | undefined
  def: number | undefined
  show: (v: number) => string
  onReset: () => void
}) {
  if (def === undefined) return null
  const changed = Math.abs((value ?? 0) - def) > 1e-9
  return (
    <Group gap={6} mt={4}>
      <Text size="xs" c="dimmed">
        default {show(def)}
      </Text>
      {changed && (
        <>
          <Badge size="xs" variant="light" color="orange">
            changed
          </Badge>
          <Anchor component="button" type="button" size="xs" onClick={onReset}>
            reset
          </Anchor>
        </>
      )}
    </Group>
  )
}

/**
 * Whether the rules in force are the ones for the chip on the board.
 *
 * The defaults are one vendor's figures, not every vendor's, and a board
 * nobody has touched matches them -- so without this a Rockchip board would
 * show ST's preset selected and nothing would say it was the wrong table.
 */
function PartMatch({
  controllerValue,
  forPart,
  active,
  onLoad,
}: {
  controllerValue?: string
  forPart: Preset[]
  active: Preset | null
  onLoad: (p: Preset) => void
}) {
  if (!controllerValue) return null

  if (forPart.length === 0) {
    return (
      <Text size="xs" c="dimmed" mt={4}>
        The controller reads <b>{controllerValue}</b>, which is not a part with rules here. Check
        the preset is the right one for it.
      </Text>
    )
  }

  if (active && forPart.some((x) => x.id === active.id)) {
    return (
      <Text size="xs" c="dimmed" mt={4}>
        Matches the <b>{controllerValue}</b> on this board.
      </Text>
    )
  }

  return (
    <Alert color="yellow" variant="light" mt="xs" p="xs">
      <Text size="xs">
        This board&rsquo;s controller reads <b>{controllerValue}</b>, and these limits are not its
        vendor&rsquo;s.
      </Text>
      <Group gap="xs" mt={6}>
        {forPart.map((x) => (
          <Button key={x.id} size="compact-xs" variant="light" onClick={() => onLoad(x)}>
            Load {x.name} ({x.memory})
          </Button>
        ))}
      </Group>
    </Alert>
  )
}

export function ParameterForm({
  value,
  defaults,
  ddr = true,
  packagePads,
  packageLengths,
  controller,
  packageParts,
  presets,
  controllerValue,
  presetsForPart,
  busy,
  onApply,
  children,
}: ParameterFormProps) {
  const [p, setP] = useState<Params>(value)

  // Follow the server: after a re-plan it is the authority on what is in force.
  useEffect(() => setP(value), [value])

  const set = <K extends keyof Params>(k: K) => (v: Params[K]) => setP((prev) => ({ ...prev, [k]: v }))
  const num = (k: NumKey) => (v: string | number) => {
    const n = typeof v === 'number' ? v : Number.parseFloat(v)
    setP((prev) => ({ ...prev, [k]: Number.isFinite(n) ? n : 0 }))
  }
  const changed = JSON.stringify(p) !== JSON.stringify(value)

  // `asDelay` is the same limit's picosecond field, where it has one. A guide
  // states a limit in one unit or the other, so when it is the delay that is
  // set, this says where the number went instead of showing a bare 0 mm.
  const lengthField = (k: NumKey, label: string, step: number, asDelay?: NumKey) => {
    const delay = asDelay ? ((p[asDelay] as number | undefined) ?? 0) : 0
    const inDelay = delay > 0 && ((p[k] as number | undefined) ?? 0) === 0
    return (
      <div>
        <LengthInput
          label={label}
          description={PARAM_HELP[k]}
          step={step}
          min={0}
          value={(p[k] as number | undefined) ?? 0}
          onChange={num(k)}
        />
        {inDelay ? (
          <Text size="xs" c="dimmed" mt={4}>
            set as a delay: {delay} ps
          </Text>
        ) : (
          <DefaultHint
            value={p[k] as number | undefined}
            def={defaults?.[k] as number | undefined}
            show={mm}
            onReset={() => num(k)((defaults?.[k] as number | undefined) ?? 0)}
          />
        )}
      </div>
    )
  }

  // Which guide these limits are those of, worked out from the values, so it
  // cannot claim a vendor for numbers somebody has since edited.
  const preset = matchingPreset(p, presets)
  const presetOptions = Object.entries(
    (presets ?? []).reduce<Record<string, { value: string; label: string }[]>>((by, x) => {
      ;(by[x.vendor] ||= []).push({ value: x.id, label: `${x.name} (${x.memory})` })
      return by
    }, {}),
  ).map(([group, items]) => ({ group, items }))

  // Search the part numbers as well as the label. One table usually covers
  // several parts and the label cannot name them all, so somebody typing the
  // part in front of them (RK3588S, AM625, STM32MP157) would otherwise be told
  // there is nothing for their chip when there is.
  const searchText = new Map(
    (presets ?? []).map((x) => [
      x.id,
      [x.name, x.memory, x.vendor, ...(x.parts ?? [])].join(' ').toLowerCase(),
    ]),
  )
  // The presets for the chip the board actually carries, in the order the
  // catalogue lists them.
  const forPart = (presets ?? []).filter((x) => (presetsForPart ?? []).includes(x.id))

  const filterPresets: OptionsFilter = ({ options, search }) => {
    const q = search.trim().toLowerCase()
    if (!q) return options
    const keep = (o: ComboboxItem) =>
      (searchText.get(o.value) ?? o.label.toLowerCase()).includes(q)
    return (options as ComboboxParsedItem[])
      .map((o) => ('group' in o ? { ...o, items: o.items.filter(keep) } : o))
      .filter((o) => ('group' in o ? o.items.length > 0 : keep(o)))
  }

  const psField = (k: NumKey, label: string) => (
    <NumberInput
      label={label}
      suffix=" ps"
      step={0.5}
      decimalScale={1}
      min={0}
      value={(p[k] as number | undefined) ?? 0}
      onChange={num(k)}
    />
  )

  // Package lengths: the value in force is the override, or the default.
  const overrides = p.package_lengths_mm ?? {}
  const setPackage = (pad: PackagePad, v: number | '') => {
    setP((prev) => {
      const next = { ...(prev.package_lengths_mm ?? {}) }
      if (v === '' || Math.abs(v - pad.default_mm) < 1e-9) delete next[pad.net]
      else next[pad.net] = v
      return { ...prev, package_lengths_mm: next }
    })
  }
  const pads = packagePads ?? []
  const nChanged = pads.filter((pad) => pad.net in overrides).length
  const table = (packageLengths ?? []).find((t) => t.ref === controller && t.pads > 0)

  return (
    <Stack gap="lg">
      {ddr && (
        <Stack gap="md">
          <div>
            <Title order={4}>DDR rules for this board</Title>
            <Text size="sm" c="dimmed">
              Pick your chip below to load its vendor&rsquo;s limits, or set any value by hand for
              this board only.
            </Text>
          </div>

          {presetOptions.length > 0 && (
            <div>
              <Select
                label="Preset"
                description="The chip vendor's own DDR rules. Choosing one sets every limit below."
                data={presetOptions}
                value={preset?.id ?? null}
                placeholder="Custom (no vendor guide)"
                clearable={false}
                searchable
                filter={filterPresets}
                nothingFoundMessage="No preset for that chip"
                onChange={(id) => {
                  const chosen = (presets ?? []).find((x) => x.id === id)
                  if (chosen) setP((prev) => applyPreset(prev, chosen))
                }}
              />
              <PartMatch
                controllerValue={controllerValue}
                forPart={forPart}
                active={preset}
                onLoad={(x) => setP((prev) => applyPreset(prev, x))}
              />
              {preset ? (
                <Text size="xs" c="dimmed" mt={4}>
                  {preset.source}
                  {preset.url && (
                    <>
                      {' '}
                      <Anchor size="xs" href={preset.url} target="_blank">
                        open
                      </Anchor>
                    </>
                  )}
                  {preset.note ? ` ${preset.note}` : ''}
                </Text>
              ) : (
                <Text size="xs" c="dimmed" mt={4}>
                  These limits match no preset. Pick one to load a vendor's numbers, or keep yours.
                </Text>
              )}
            </div>
          )}

          <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="md" verticalSpacing="md">
            {lengthField('data_to_strobe_mm', 'Data to strobe (DQ, DQM to DQS)', 0.05, 'data_to_strobe_ps')}
            {lengthField('address_to_clock_mm', 'Address and command to clock (A/C to CLK)', 0.05, 'address_to_clock_ps')}
            {lengthField('strobe_to_clock_mm', 'Strobe to clock (DQS to CLK)', 0.5, 'strobe_to_clock_ps')}
            {lengthField('max_chip_delta_mm', 'Memory chip to memory chip', 1)}
            <div>
              <NumberInput
                label="Clock offset"
                description={PARAM_HELP.clock_offset_percent}
                suffix=" %"
                step={0.5}
                decimalScale={2}
                min={-50}
                max={50}
                value={p.clock_offset_percent}
                onChange={num('clock_offset_percent')}
              />
              <DefaultHint
                value={p.clock_offset_percent}
                def={defaults?.clock_offset_percent}
                show={(v) => `${v} %`}
                onReset={() => num('clock_offset_percent')(defaults?.clock_offset_percent ?? 0)}
              />
            </div>
            <Switch
              mt="lg"
              label="Match reset lines too"
              description={PARAM_HELP.include_control}
              checked={p.include_control}
              onChange={(e) => set('include_control')(e.currentTarget.checked)}
            />
          </SimpleGrid>

          <Accordion variant="separated">
            <Accordion.Item value="ps">
              <Accordion.Control>Limits as a delay (ps)</Accordion.Control>
              <Accordion.Panel>
                <Text size="xs" c="dimmed" mb="sm">
                  Optional. 0 means not used. If a length and a delay are both set, the stricter
                  one is used.
                </Text>
                <SimpleGrid cols={{ base: 1, sm: 2 }}>
                  {psField('data_to_strobe_ps', 'Data to strobe')}
                  {psField('address_to_clock_ps', 'Address and command to clock')}
                  {psField('strobe_to_clock_ps', 'Strobe to clock')}
                  {psField('intra_pair_ps', 'Pair (P to N)')}
                </SimpleGrid>
              </Accordion.Panel>
            </Accordion.Item>

            <Accordion.Item value="package">
              <Accordion.Control>
                <Group gap="xs">
                  <Text>Package lengths{controller ? ` (${controller})` : ''}</Text>
                  <Badge size="xs" variant="light" color={pads.some((x) => x.mm > 0) ? 'blue' : 'orange'}>
                    {pads.some((x) => x.mm > 0) ? `${pads.length} pads` : 'none set'}
                  </Badge>
                  {nChanged > 0 && (
                    <Badge size="xs" variant="light" color="orange">
                      {nChanged} changed
                    </Badge>
                  )}
                </Group>
              </Accordion.Control>
              <Accordion.Panel>
                <Select
                  label="Package length table"
                  description="Found from the chip's part number. Choose one if it was not found."
                  mb="sm"
                  maw={420}
                  allowDeselect={false}
                  data={[
                    { value: '', label: 'Automatic (from the footprint)' },
                    ...(packageParts ?? []).map((part) => ({ value: part, label: part })),
                    { value: 'none', label: 'None (board copper only)' },
                  ]}
                  value={p.package_part ?? ''}
                  onChange={(v) =>
                    setP((prev) => {
                      // A different table changes every default, so typed-in values start again.
                      const next: Params = { ...prev, package_lengths_mm: {} }
                      if (v) next.package_part = v
                      else delete next.package_part
                      return next
                    })
                  }
                />
                <Text size="xs" c="dimmed" mb="sm">
                  {PARAM_HELP.package_lengths_mm}{' '}
                  {table
                    ? `Defaults: ${table.source}.`
                    : 'Defaults come from the pad die length in the footprint.'}{' '}
                  KiCad only counts these if the footprint pads have a die length.
                </Text>
                {pads.length === 0 ? (
                  <Text size="sm" c="dimmed">
                    No DDR pads found on the controller.
                  </Text>
                ) : (
                  <Table.ScrollContainer minWidth={480} mah={420}>
                    <Table verticalSpacing={2} fz="sm" stickyHeader>
                      <Table.Thead>
                        <Table.Tr>
                          <Table.Th>Net</Table.Th>
                          <Table.Th>Ball</Table.Th>
                          <Table.Th>Length</Table.Th>
                          <Table.Th>Default</Table.Th>
                          <Table.Th />
                        </Table.Tr>
                      </Table.Thead>
                      <Table.Tbody>
                        {pads.map((pad) => {
                          const over = overrides[pad.net]
                          return (
                            <Table.Tr key={pad.net}>
                              <Table.Td>{pad.net.slice(pad.net.lastIndexOf('/') + 1)}</Table.Td>
                              <Table.Td>
                                <Text size="xs" c="dimmed">
                                  {pad.ball ?? ''} {pad.pad}
                                </Text>
                              </Table.Td>
                              <Table.Td w={140}>
                                <LengthInput
                                  size="xs"
                                  aria-label={`Package length of ${pad.net}`}
                                  step={0.01}
                                  min={0}
                                  value={over ?? pad.default_mm}
                                  onChange={(v) => setPackage(pad, v)}
                                />
                              </Table.Td>
                              <Table.Td>
                                <Text size="xs" c="dimmed">
                                  {mm(pad.default_mm)}
                                </Text>
                              </Table.Td>
                              <Table.Td>
                                {over !== undefined && (
                                  <Anchor
                                    component="button"
                                    type="button"
                                    size="xs"
                                    onClick={() => setPackage(pad, pad.default_mm)}
                                  >
                                    reset
                                  </Anchor>
                                )}
                              </Table.Td>
                            </Table.Tr>
                          )
                        })}
                      </Table.Tbody>
                    </Table>
                  </Table.ScrollContainer>
                )}
              </Accordion.Panel>
            </Accordion.Item>
          </Accordion>

          {children}
        </Stack>
      )}
      {!ddr && children}

      <div>
        <Title order={4} mb="xs">
          Meanders and spacing
        </Title>
        <Accordion variant="separated">
          <Accordion.Item value="spacing">
            <Accordion.Control>Spacing away from components</Accordion.Control>
            <Accordion.Panel>
              <SimpleGrid cols={{ base: 1, sm: 2 }}>
                {lengthField('open_clearance_mm', 'Gap between traces', 0.05)}
                {lengthField('open_margin_mm', 'Board rules apply within', 0.25)}
              </SimpleGrid>
            </Accordion.Panel>
          </Accordion.Item>

          <Accordion.Item value="meander">
            <Accordion.Control>Meander shape</Accordion.Control>
            <Accordion.Panel>
              <SimpleGrid cols={{ base: 1, sm: 2 }}>
                <NumberInput
                  label="Gap between loops"
                  description={PARAM_HELP.meander_gap_widths}
                  suffix=" track widths"
                  step={0.5}
                  decimalScale={2}
                  min={1}
                  value={p.meander_gap_widths}
                  onChange={num('meander_gap_widths')}
                />
                <NumberInput
                  label="Corner cut"
                  description={PARAM_HELP.meander_chamfer_widths}
                  suffix=" track widths"
                  step={0.25}
                  decimalScale={2}
                  min={0}
                  value={p.meander_chamfer_widths}
                  onChange={num('meander_chamfer_widths')}
                />
                {lengthField('max_amplitude_mm', 'Largest meander', 0.1)}
                {lengthField('min_amplitude_mm', 'Smallest meander', 0.01)}
                {lengthField('min_run_mm', 'Shortest track to meander', 0.1)}
                {lengthField('pad_keepout_mm', 'Distance from pads', 0.1)}
                {lengthField('max_intra_pair_fix_mm', 'Largest pair difference a meander may fix (not DDR)', 0.1)}
              </SimpleGrid>
            </Accordion.Panel>
          </Accordion.Item>
        </Accordion>
      </div>

      <Group justify="flex-end">
        <Button variant="default" disabled={!changed || busy} onClick={() => setP(value)}>
          Undo changes
        </Button>
        <Button disabled={!changed || busy} loading={busy} onClick={() => onApply(p)}>
          Apply to this board
        </Button>
      </Group>
    </Stack>
  )
}
