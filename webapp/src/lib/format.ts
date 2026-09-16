/**
 * Presentation helpers, and the small amount of judgement that goes with them.
 *
 * Two things here are not merely cosmetic. Lengths are quoted to three decimal
 * places because a DDR tolerance is a few tenths of a millimetre and rounding
 * to two would hide a third of the budget. And a deviation is always shown with
 * its sign, because "0.4 mm out" does not say whether a track has to grow or
 * whether it is the one everything else is being matched up to.
 */

import type { CSSProperties } from 'react'
import type {
  Analysis,
  DetectedInterface,
  GroupInfo,
  HeadroomResponse,
  LengthParts,
  Preset,
  MemberInfo,
  Params,
} from './analyzerApi'
import { MM_PER_MIL, getUnit } from './units'

/**
 * Keeps a measurement on one line.
 *
 * "2.905 mm" broken across two lines reads as two numbers, and in a table of
 * lengths that is worse than a narrow column.
 */
export const NOWRAP: CSSProperties = { whiteSpace: 'nowrap' }

/**
 * A length, in whichever unit the reader chose.
 *
 * Three decimals of a millimetre is a micrometre, finer than any feature on a
 * board; a tenth of a mil is 2.5 micrometres, which is the same order. Both are
 * past the point where the number means anything physical, and neither hides a
 * third of a 0.635 mm budget the way two decimals of a millimetre would.
 */
export function mm(v: number): string {
  return getUnit() === 'mil' ? `${(v / MM_PER_MIL).toFixed(1)} mil` : `${v.toFixed(3)} mm`
}

/** A signed length, so short and long are distinguishable at a glance. */
export function signedMM(v: number): string {
  const s = mm(v)
  return v > 0 ? `+${s}` : s
}

export function ps(v: number): string {
  return `${v.toFixed(1)} ps`
}

/**
 * An area, to whole square millimetres.
 *
 * Three decimals would be a lie here: the figure is an estimate that assumes a
 * meander gets the whole amplitude it is allowed, and a reader deciding whether
 * they have the board for it is thinking in patches, not micrometres.
 */
export function mm2(v: number): string {
  if (getUnit() === 'mil') {
    const mil2 = v / (MM_PER_MIL * MM_PER_MIL)
    return `${Math.round(mil2).toLocaleString()} mil²`
  }
  return `${v < 10 ? v.toFixed(1) : Math.round(v)} mm²`
}

/**
 * The length a net has to be routed to, rather than the length it is missing.
 *
 * For a net that needs rerouting, "make this 31.138 mm" is the instruction;
 * "it is 14.251 mm short" is the same fact in a form somebody has to do
 * arithmetic on while looking at a different window.
 */
export function targetLength(c: MemberInfo): number {
  return c.length_mm + c.need_mm
}

/** What share of its own length a net is being asked to grow by. */
export function growthShare(c: MemberInfo): number {
  return c.length_mm > 0 ? c.need_mm / c.length_mm : 0
}

export function bytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} kB`
  return `${(n / (1024 * 1024)).toFixed(1)} MB`
}

/** Relative time, for when a session expires. */
export function expiresIn(iso: string, now = new Date()): string {
  const ms = new Date(iso).getTime() - now.getTime()
  if (!Number.isFinite(ms) || ms <= 0) return 'expired'
  const mins = Math.round(ms / 60000)
  if (mins < 60) return `${mins} min`
  const hours = Math.floor(mins / 60)
  const rem = mins % 60
  return rem === 0 ? `${hours} h` : `${hours} h ${rem} min`
}

/** How a member should be coloured: what the user has to look at first. */
export type Severity = 'ok' | 'warn' | 'bad' | 'unrouted'

export function memberSeverity(m: MemberInfo, tolerance: number): Severity {
  if (!m.routed) return 'unrouted'
  if (m.in_tolerance) return 'ok'
  // More than twice over the tolerance is a different kind of problem from
  // just outside it: one is tuning, the other usually means a reroute.
  return Math.abs(m.deviation_mm) > 2 * tolerance ? 'bad' : 'warn'
}

/** A one-line summary of a group, for a collapsed header. */
export function groupSummary(g: GroupInfo): string {
  const parts = [`spread ${mm(g.spread_mm)}`, `tolerance ${mm(g.tolerance_mm)}`]
  if (g.out_of_tolerance > 0) {
    parts.push(`${g.out_of_tolerance} of ${g.members.length} out`)
  } else {
    parts.push('all within tolerance')
  }
  // Only what the out-of-tolerance members need: a member already inside the
  // band is matched, and length to add to it is not work anybody has to do.
  const toAdd = g.members
    .filter((m) => m.routed && !m.in_tolerance)
    .reduce((s, m) => s + m.need_mm, 0)
  if (toAdd > 1e-6) parts.push(`${mm(toAdd)} to add`)
  return parts.join(' · ')
}

/**
 * Whether a group's target had to be raised above what its reference asked for.
 *
 * This happens when a member is already longer than the reference, and it is
 * worth calling out: the group ends up matched to itself rather than to the
 * strobe or the clock, and no amount of meandering fixes that because nothing
 * here can shorten a track.
 */
export function targetWasRaised(g: GroupInfo): number {
  const raised = g.target_mm - g.target_from_reference_mm
  return raised > 1e-6 ? raised : 0
}

/** The nets a group contributes to the tick list. */
export function groupCandidates(g: GroupInfo, candidates: MemberInfo[]): string[] {
  const eligible = new Set(candidates.map((c) => c.net))
  return g.members.filter((m) => eligible.has(m.net)).map((m) => m.net)
}

/**
 * Everything the briefing needs, for the whole board rather than for DDR.
 *
 * The DDR half comes from the plan, which measures per leg of the fly-by chain
 * and knows what each net needs. Every other interface comes from the detector,
 * which measures the same things over simpler shapes: nets that are not joined
 * up, groups against their reference, and -- once the room has been measured --
 * what each net short of its target could actually get. Putting them through
 * one shape is what makes the page a briefing about a board instead of a
 * briefing about DDR with a footnote.
 */
export interface Brief {
  /** Copper that is missing, grouped by where: a span of a chain, or an interface. */
  missing: MissingGroup[]
  missingCount: number
  groups: BriefGroup[]
  /** One row per span of one net, worst first. */
  rows: LegRow[]
  need: number
  have: number
  run: number
  area: number
  busSpare: number
  nets: number
}

export interface MissingGroup {
  key: string
  /** The span or the interface these belong to. */
  where: string
  nets: string[]
  /** Set for a span of a chain, where the pads are known. */
  hop?: boolean
}

export interface BriefGroup {
  key: string
  name: string
  /** The interface it belongs to, when that is not obvious from the name. */
  source?: string
  netsOut: number
  members: number
  need: number
  reachable: number
  worst?: { label: string; need: number }
}

/**
 * The analysis and the room measurement with every ticked interface folded in,
 * in the shape the DDR half already has.
 *
 * The candidate list, the area verdict and the apply all grew up reading
 * analysis.candidates and analysis.groups, which only the DDR plan fills. Rather
 * than teach each of them about interfaces, this hands them an analysis in
 * which an Ethernet group is just another group and its short nets are just
 * more candidates -- which is what they are. Group names are prefixed with the
 * interface, because "transmit" on its own could belong to anything.
 */
export function boardWide(
  analysis: Analysis,
  headroom: HeadroomResponse | null,
  selected?: readonly string[],
): { analysis: Analysis; headroom: HeadroomResponse | null } {
  const wanted = selected ? new Set(selected) : null
  const fold = (ifaces: DetectedInterface[] | undefined) => {
    const candidates: MemberInfo[] = []
    const groups: GroupInfo[] = []
    for (const i of ifaces ?? []) {
      if (i.planner) continue
      if (wanted && !wanted.has(i.id)) continue
      const renamed = (i.candidates ?? []).map((c) => ({
        ...c,
        legs: c.legs?.map((l) => ({ ...l, group: `${i.name} / ${l.group}` })),
      }))
      candidates.push(...renamed)
      for (const g of i.groups ?? []) {
        const name = `${i.name} / ${g.name}`
        const members = renamed.filter((c) => c.legs?.some((l) => l.group === name))
        groups.push({
          name,
          kind: 'interface',
          lane: -1,
          reference: g.reference,
          reference_length_mm: g.reference_mm,
          target_mm: g.target_mm ?? g.reference_mm,
          target_from_reference_mm: g.reference_mm,
          tolerance_mm: g.limit_mm,
          spread_mm: g.spread_mm,
          total_need_mm: g.need_mm ?? 0,
          out_of_tolerance: g.out_of_tolerance,
          members,
        })
      }
    }
    return { candidates, groups }
  }

  const fromAnalysis = fold(analysis.interfaces)
  const merged: Analysis = {
    ...analysis,
    candidates: [...(analysis.candidates ?? []), ...fromAnalysis.candidates],
    groups: [...(analysis.groups ?? []), ...fromAnalysis.groups],
    total_need_mm:
      (analysis.total_need_mm ?? 0) + fromAnalysis.candidates.reduce((s, c) => s + c.need_mm, 0),
  }
  if (!headroom) return { analysis: merged, headroom: null }

  const fromHeadroom = fold(headroom.interfaces ?? analysis.interfaces)
  const candidates = [...headroom.candidates, ...fromHeadroom.candidates]
  return {
    analysis: merged,
    headroom: {
      ...headroom,
      candidates,
      total_need_mm: candidates.reduce((s, c) => s + c.need_mm, 0),
      gettable_mm: gettable(candidates),
      reroute_count: candidates.filter((c) => c.needs_reroute).length,
      run_needed_mm:
        (headroom.run_needed_mm ?? 0) +
        fromHeadroom.candidates.reduce((s, c) => s + (c.run_needed_mm ?? 0), 0),
      space_needed_mm2:
        (headroom.space_needed_mm2 ?? 0) +
        fromHeadroom.candidates.reduce((s, c) => s + (c.space_needed_mm2 ?? 0), 0),
    },
  }
}

/**
 * Build the briefing from an analysis, the room measurement when it has
 * arrived, and which interfaces the user ticked.
 */
export function buildBrief(
  analysis: Analysis,
  headroom: HeadroomResponse | null,
  selected?: readonly string[],
): Brief {
  const wanted = selected ? new Set(selected) : null
  const ddrCandidates = headroom?.candidates ?? analysis.candidates ?? []

  const missing: MissingGroup[] = []
  // The fly-by chain first: its gaps are pad to pad, and which span is missing
  // is the thing to act on.
  const byHop = new Map<string, string[]>()
  for (const m of analysis.routing.missing ?? []) {
    byHop.set(m.hop, [...(byHop.get(m.hop) ?? []), m.label])
  }
  for (const [hop, nets] of byHop) {
    missing.push({ key: `hop:${hop}`, where: hop, nets, hop: true })
  }
  // Then the nets of the DDR interface that are not joined at all, where the
  // chain says nothing about them.
  if ((analysis.routing.missing ?? []).length === 0 && analysis.routing.incomplete > 0) {
    for (const g of analysis.routing.gaps ?? []) {
      missing.push({ key: `gap:${g.islands.join('|')}`, where: g.islands.join(' | '), nets: g.nets })
    }
  }

  const groups: BriefGroup[] = []
  for (const g of analysis.groups ?? []) {
    const { nets, need, reachable, worst } = groupNeed(g, ddrCandidates)
    if (nets === 0) continue
    groups.push({
      key: `ddr:${g.name}`,
      name: g.name,
      netsOut: nets,
      members: g.members.length,
      need,
      reachable,
      worst: worst ?? undefined,
    })
  }

  let rows = legRows(ddrCandidates)
  let need = headroom?.total_need_mm ?? analysis.total_need_mm ?? 0
  let run = headroom?.run_needed_mm ?? analysis.run_needed_mm ?? 0
  let area = headroom?.space_needed_mm2 ?? analysis.space_needed_mm2 ?? 0
  let nets = ddrCandidates.length

  // Everything the DDR planner does not cover. An interface it does is left to
  // the plan, which measures it per leg and in more detail.
  for (const i of headroom?.interfaces ?? analysis.interfaces ?? []) {
    if (i.planner) continue
    if (wanted && !wanted.has(i.id)) continue
    if ((i.unrouted_nets?.length ?? 0) > 0) {
      missing.push({ key: `iface:${i.id}`, where: i.name, nets: i.unrouted_nets! })
    }
    const candidates = i.candidates ?? []
    for (const g of i.groups ?? []) {
      const mine = candidates.filter((c) => c.legs?.some((l) => l.group === g.name))
      if (mine.length === 0) continue
      const gNeed = mine.reduce((s, c) => s + c.need_mm, 0)
      const worst = mine.reduce((a, b) => (a.need_mm >= b.need_mm ? a : b))
      groups.push({
        key: `${i.id}:${g.name}`,
        name: g.name,
        source: i.name,
        netsOut: mine.length,
        members: g.members ?? mine.length,
        need: gNeed,
        reachable: gettable(mine),
        worst: { label: worst.label, need: worst.need_mm },
      })
    }
    rows = rows.concat(legRows(candidates))
    need += i.total_need_mm ?? 0
    nets += candidates.length
    for (const c of candidates) {
      run += c.run_needed_mm ?? 0
      area += c.space_needed_mm2 ?? 0
    }
  }
  rows.sort((a, b) => b.need_mm - a.need_mm)

  return {
    missing,
    missingCount: missing.reduce((s, m) => s + m.nets.length, 0),
    groups,
    rows,
    need,
    have: rows.reduce((s, r) => s + Math.min(r.need_mm, r.headroom_mm), 0),
    run,
    area,
    busSpare: headroom?.bus_spare_mm ?? analysis.bus_spare_mm ?? 0,
    nets,
  }
}

/**
 * The requirement, one row per span rather than one per net.
 *
 * Almost everything a reader is deciding about is per span: the length that
 * span is short, the room beside that span's copper, and -- for a net that
 * has to be re-routed -- the length to route that span to. A net summed across
 * its legs answers none of those, and reads as nonsense when it tries: A11 is
 * 16.888 mm long on U3->U4 and short 14.251 mm there and 2.641 mm on the next
 * leg, so "route it to 33.779 mm" is two requirements added to one length.
 */
export function legRows(candidates: MemberInfo[]): LegRow[] {
  const out: LegRow[] = []
  for (const c of candidates) {
    const legs = c.legs?.length
      ? c.legs
      : [
          {
            group: '',
            length_mm: c.length_mm,
            need_mm: c.need_mm,
            excess_mm: c.excess_mm,
            headroom_mm: c.headroom_mm,
            needs_reroute: c.needs_reroute,
            run_needed_mm: c.run_needed_mm,
            space_needed_mm2: c.space_needed_mm2,
          },
        ]
    for (const l of legs) {
      out.push({
        net: c.net,
        label: c.label,
        group: l.group,
        leg: l.leg,
        length_mm: l.length_mm,
        need_mm: l.need_mm,
        excess_mm: l.excess_mm ?? 0,
        target_mm: l.target_mm ?? l.length_mm + l.need_mm - (l.excess_mm ?? 0),
        headroom_mm: l.headroom_mm,
        needs_reroute: l.needs_reroute,
        run_needed_mm: l.run_needed_mm,
        space_needed_mm2: l.space_needed_mm2,
      })
    }
  }
  return out.sort((a, b) => b.need_mm - a.need_mm)
}

/** One span of one net, as the tables show it. */
export interface LegRow {
  net: string
  label: string
  group: string
  leg?: string
  length_mm: number
  need_mm: number
  /** How much too long it is: it needs routing shorter by this much. */
  excess_mm: number
  /** The length to route this span to: its reference. */
  target_mm: number
  headroom_mm: number
  needs_reroute: boolean
  run_needed_mm?: number
  space_needed_mm2?: number
}

/** The same three answers as `triage`, per span. */
export function triageLegs(rows: LegRow[]): { fits: LegRow[]; tight: LegRow[]; reroute: LegRow[] } {
  const fits: LegRow[] = []
  const tight: LegRow[] = []
  const reroute: LegRow[] = []
  for (const r of rows) {
    if (r.needs_reroute) reroute.push(r)
    else if (r.headroom_mm + 1e-6 >= r.need_mm) fits.push(r)
    else tight.push(r)
  }
  return { fits, tight, reroute }
}

/**
 * What one group needs, without counting another group's work as its own.
 *
 * A candidate stands for a whole net, and a fly-by address line belongs to
 * every leg of the chain: taking its whole requirement into each group made
 * U3->U4 and U4->U5 report the same figure -- the sum of both -- and a per
 * group table that added up to more than the board needed. So where a
 * candidate carries a per-leg breakdown, only the leg this group measured
 * counts towards it.
 */
export function groupNeed(
  g: GroupInfo,
  candidates: MemberInfo[],
): { nets: number; need: number; reachable: number; worst: { label: string; need: number } | null } {
  const mine = new Set(groupCandidates(g, candidates))
  let nets = 0
  let need = 0
  let reachable = 0
  let worst: { label: string; need: number } | null = null
  for (const c of candidates) {
    if (!mine.has(c.net)) continue
    const leg = c.legs?.find((l) => l.group === g.name)
    // A net with a breakdown that does not mention this group is short
    // somewhere else entirely and is not this group's business.
    if (c.legs?.length && !leg) continue
    const n = leg ? leg.need_mm : c.need_mm
    const room = leg ? leg.headroom_mm : c.headroom_mm
    nets += 1
    need += n
    reachable += Math.min(n, room)
    if (!worst || n > worst.need) worst = { label: c.label, need: n }
  }
  return { nets, need, reachable, worst }
}

/**
 * Split candidates by what kind of problem they are, once the room beside each
 * has been measured.
 *
 * The three answers are genuinely different things to do, and lumping them
 * together is what makes a tool feel like it is guessing. A net with room is
 * worth applying. A net short of room might be helped by opening some. A net
 * asking for more length than its route could ever carry is a reroute, and no
 * setting changes that.
 */
export function triage(candidates: MemberInfo[]): {
  fits: MemberInfo[]
  tight: MemberInfo[]
  reroute: MemberInfo[]
} {
  const fits: MemberInfo[] = []
  const tight: MemberInfo[] = []
  const reroute: MemberInfo[] = []
  for (const c of candidates) {
    if (c.needs_reroute) reroute.push(c)
    else if (c.headroom_mm + 1e-6 >= c.need_mm) fits.push(c)
    else tight.push(c)
  }
  return { fits, tight, reroute }
}

/** How much of the requirement the board can actually hold, net by net. */
/**
 * Whether the room available is enough, and what the shortfall is made of.
 *
 * The parts have to add up, and getting that wrong is easy: the first version
 * of this put a shortfall and a total need in the same sentence -- "40 mm is on
 * nets short of room, 192 mm is on nets needing a reroute" against a total
 * shortfall of 191 -- and a reader who tried to check it could not. So
 * everything here except `need` is a shortfall, and the invariant is that
 * have + reachable + unreachable comes to need exactly.
 *
 * The split between reachable and unreachable is the useful part. A net merely
 * short of room might be helped by drawing a bigger area. A net asking for more
 * length than its own route could carry cannot be: the limit is the track, not
 * the space, and offering "draw more" for those wastes an afternoon.
 */
export function areaVerdict(candidates: MemberInfo[]): {
  need: number
  have: number
  short: number
  reachable: number
  unreachable: number
  fits: number
  reroute: number
  enough: boolean
} {
  const need = candidates.reduce((s, c) => s + c.need_mm, 0)
  const have = gettable(candidates)
  const missing = (c: MemberInfo) => Math.max(0, c.need_mm - Math.min(c.need_mm, c.headroom_mm))
  const split = triage(candidates)
  const reachable = split.tight.reduce((s, c) => s + missing(c), 0)
  const unreachable = split.reroute.reduce((s, c) => s + missing(c), 0)
  return {
    need,
    have,
    short: Math.max(0, need - have),
    reachable,
    unreachable,
    fits: split.fits.length,
    reroute: split.reroute.length,
    enough: need - have <= 1e-6,
  }
}

export function gettable(candidates: MemberInfo[]): number {
  return candidates.reduce((sum, c) => sum + Math.min(c.need_mm, c.headroom_mm), 0)
}

/**
 * Split the candidates by how much length each one needs.
 *
 * This is a size split and nothing more. Whether a correction fits depends on
 * the space beside that particular track, and only the apply can find that
 * out -- on a bus routed at its minimum clearance even half a millimetre has
 * nowhere to go. So the groups are labelled by size rather than by a promise
 * about fit, and the small ones are ticked by default because they are the ones
 * worth trying first.
 */
export function partitionCandidates(
  candidates: MemberInfo[],
  smallMM = 3,
): { small: MemberInfo[]; large: MemberInfo[] } {
  const small: MemberInfo[] = []
  const large: MemberInfo[] = []
  for (const c of candidates) {
    ;(c.need_mm <= smallMM ? small : large).push(c)
  }
  return { small, large }
}

/** Total length the given nets would add. */
export function totalNeed(candidates: MemberInfo[], nets: Iterable<string>): number {
  const want = new Set(nets)
  return candidates.filter((c) => want.has(c.net)).reduce((sum, c) => sum + c.need_mm, 0)
}

/**
 * Things about the board the user should read before trusting a clearance
 * check, in the order they matter.
 */
export function caveats(a: Analysis): string[] {
  const out: string[] = []
  if (!a.board.has_project_file) {
    out.push(
      'No .kicad_pro was uploaded, so there are no net classes and every clearance fell back to the ' +
        'board minimum. Upload it for the clearances the design actually sets.',
    )
  }
  if (a.board.has_custom_dru) {
    const r = a.board.custom_rules
    if (!r) {
      out.push(
        'This board has a custom .kicad_dru that could not be read, so clearance is checked against ' +
          'the net classes at their strictest. Run KiCad’s own DRC on the result.',
      )
    } else if (r.skipped > 0) {
      out.push(
        `This board’s .kicad_dru has ${r.applied} rule${r.applied === 1 ? '' : 's'} this understands ` +
          `and ${r.skipped} it does not (${(r.skipped_rules ?? []).map((s) => s.name).join(', ')}). ` +
          'Where a rule is left alone the net classes at their strictest stand in for it, which can only ' +
          'be stricter than the board asks. KiCad is the authority on its own rule language: run its DRC ' +
          'on the result.',
      )
    } else {
      out.push(
        `This board’s .kicad_dru is read: ${r.applied} rule${r.applied === 1 ? '' : 's'} applied. They can ` +
          'only tighten what this tool draws to, never loosen it. KiCad is the authority on its own rule ' +
          'language: run its DRC on the result.',
      )
    }
  }
  if (a.routing.incomplete > 0) {
    out.push(
      `${a.routing.incomplete} of ${a.routing.complete + a.routing.incomplete} nets are not fully routed. ` +
        'They are measured over the legs that exist; their missing legs are not counted, and KiCad’s DRC ' +
        'does not report these gaps.',
    )
  }
  for (const note of a.interface.notes ?? []) out.push(note)
  return out
}

/** Field-by-field description of the parameters, for the form's help text. */
export const PARAM_HELP: Partial<Record<keyof Params, string>> = {
  clock_offset_percent:
    'Make address and command lines longer than the clock by this share of the clock length. 0 means equal.',
  data_to_strobe_mm: 'Allowed difference between a data line (DQ, DQM) and its strobe (DQS, the clock for that byte).',
  intra_pair_mm: 'Allowed difference between the two lines of a differential pair (P and N).',
  address_to_clock_mm:
    'Allowed difference between an address or command line and the clock (CLK), at each memory chip.',
  strobe_to_clock_mm:
    'Allowed difference between each strobe (DQS) and the clock (CLK) at the same memory chip. The controller corrects part of this at start-up (write levelling).',
  max_chip_delta_mm: 'Allowed difference between the average data line length of one memory chip and another.',
  data_to_strobe_ps: 'The same limit as a delay. If both are set, the stricter one is used.',
  intra_pair_ps: 'The same limit as a delay. If both are set, the stricter one is used.',
  address_to_clock_ps: 'The same limit as a delay. If both are set, the stricter one is used.',
  strobe_to_clock_ps: 'The same limit as a delay. If both are set, the stricter one is used.',
  include_control: 'Also match reset-type lines (RESET). Usually off, because they are not timed to the clock.',
  max_intra_pair_fix_mm:
    'For differential pairs outside DDR (USB, PCIe, and so on). DDR pairs are never lengthened on one line. Above this, reroute the pair.',
  package_lengths_mm: 'Length of the wiring inside the chip package, from the die to the ball.',
  open_clearance_mm:
    'Gap between traces away from components. Usually wider than the gap inside a BGA (ball grid array) escape. 0 uses the board rules everywhere.',
  open_margin_mm: 'Distance around a component where the board rules still apply.',
  max_amplitude_mm: 'Largest sideways size of a meander (a zigzag that adds length).',
  min_amplitude_mm: 'Smallest meander worth drawing.',
  meander_gap_widths: 'Gap between meander loops, in track widths. 3 is usual; closer loops couple and add less delay.',
  meander_chamfer_widths: 'Cut on meander corners, in track widths. Sharp corners disturb the signal.',
  min_run_mm: 'Shortest straight track that can take a meander.',
  pad_keepout_mm: 'Keep meanders this far from pads.',
}

/** Whether the controller's DDR pads have package lengths, for the warnings. */
export interface PackageStatus {
  /** "full": every DDR pad has one; "partial": some do; "none": none do. */
  state: 'full' | 'partial' | 'none'
  controller: string
  /** The controller's part value, when known. */
  value?: string
  withLength: number
  total: number
  /** Where the lengths came from, e.g. "STM32MP25xxAI table". */
  source: string
}

export function packageStatus(a: Analysis): PackageStatus | null {
  const controller = a.interface?.controller
  if (!controller || (a.groups?.length ?? 0) === 0) return null
  const pads = a.package_pads ?? []
  const withLength = pads.filter((p) => p.mm > 0).length
  const sources = new Set(pads.filter((p) => p.mm > 0).map((p) => p.source ?? ''))
  const names: string[] = []
  for (const src of sources) {
    if (src === 'footprint') names.push('footprint die lengths')
    else if (src === 'override') names.push('entered by hand')
    else if (src) names.push(`${src} table`)
  }
  return {
    state: withLength === 0 ? 'none' : withLength < pads.length ? 'partial' : 'full',
    controller,
    value: a.interface?.controller_value || undefined,
    withLength,
    total: pads.length,
    source: names.join(', '),
  }
}

/**
 * A length written out as the sum it is: track, then whatever else was counted.
 *
 * Only the parts that contribute are shown, so a net with no vias does not
 * carry a "+ 0 vias" a reader has to skip, and one that crosses two says so
 * where its length is. The pieces add up to the length exactly.
 */
export function lengthSum(p: LengthParts | undefined | null): string | null {
  if (!p) return null
  const terms = [`${mm(p.track_mm)} track`]
  if (p.vias > 0 && p.via_mm > 0) terms.push(`${mm(p.via_mm)} ${p.vias === 1 ? 'via' : `${p.vias} vias`}`)
  if (p.pad_mm > 0.0005) terms.push(`${mm(p.pad_mm)} pads`)
  if (p.package_mm > 0) terms.push(`${mm(p.package_mm)} package`)
  return terms.join(' + ')
}

/** Whether a length includes anything besides track: vias or package. */
export function countsMoreThanTrack(p: LengthParts | undefined | null): boolean {
  return Boolean(p && ((p.vias > 0 && p.via_mm > 0) || p.package_mm > 0))
}

/**
 * The limits a preset decides, and nothing else.
 *
 * Both units of each limit are applied, one of them zero: a guide states a
 * limit either as a length or as a delay, and leaving the other in place would
 * hold the board to the tighter of two numbers from different tables.
 */
export const PRESET_KEYS = [
  'data_to_strobe_mm',
  'data_to_strobe_ps',
  'intra_pair_mm',
  'intra_pair_ps',
  'address_to_clock_mm',
  'address_to_clock_ps',
  'strobe_to_clock_mm',
  'strobe_to_clock_ps',
  'max_chip_delta_mm',
  'clock_offset_percent',
] as const

/** The parameters with one preset's limits in force. */
export function applyPreset(params: Params, preset: Preset): Params {
  const next = { ...params }
  for (const k of PRESET_KEYS) next[k] = preset.params[k]
  return next
}

/**
 * Which preset a board's limits are those of, if any.
 *
 * Derived from the values rather than remembered, so a board can never show a
 * vendor's name beside numbers that are no longer that vendor's.
 */
export function matchingPreset(params: Params, presets: Preset[] | undefined): Preset | null {
  for (const preset of presets ?? []) {
    if (PRESET_KEYS.every((k) => Math.abs((params[k] ?? 0) - preset.params[k]) < 1e-9)) return preset
  }
  return null
}
