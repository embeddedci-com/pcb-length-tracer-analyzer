/**
 * Typed client for the length matcher's control plane.
 *
 * The shapes here mirror server/analysis.go and server/api.go exactly. They are
 * hand-written rather than generated so that the field comments can say what a
 * number means -- which for this tool is most of the difficulty, because
 * "length" has several defensible definitions and the wrong one silently
 * produces a board that does not work.
 */

import type { BoardDoc } from '@emi/lib/boardTypes'

export type { BoardDoc }

export interface Params {
  net_prefix: string
  controller: string
  /** How much longer the address and command group runs than the clock, as a percentage of the clock. */
  clock_offset_percent: number
  data_to_strobe_mm: number
  intra_pair_mm: number
  address_to_clock_mm: number
  /** Each byte lane's DQS pair against the clock at its device. */
  strobe_to_clock_mm?: number
  /** Most one device's byte lanes may differ from another's. */
  max_chip_delta_mm?: number
  data_to_strobe_ps: number
  intra_pair_ps: number
  address_to_clock_ps: number
  include_control: boolean
  max_intra_pair_fix_mm: number
  /** Package length overrides for the controller's pads, in mm, keyed by full net name. */
  package_lengths_mm?: Record<string, number>
  /** The controller's package length table, chosen by hand: a part name or "none". Empty recognises it from the footprint. */
  package_part?: string
  /**
   * A tolerance of its own for a group, in mm, keyed by the group's name as the
   * report shows it: "byte lane 0", "address/command U3->U4", or
   * "<interface> / <group>" for anything that is not DDR.
   */
  group_tolerance_mm?: Record<string, number>
  /** Clearance held to away from the components, overriding the board's own rules there. 0 uses them. */
  /**
   * The regions you have allowed copper to be added in, in the coordinates the
   * board preview draws: origin at the bottom-left of the board, Y up.
   *
   * Empty means anywhere. Once areas are given, nothing is added outside them —
   * not a meander, not a bus being spread.
   */
  meander_areas?: Area[]
  /** What you have said the board's interfaces actually are. */
  interfaces?: InterfaceOverride[]
  open_clearance_mm: number
  /** How far outside a component's pads still counts as being around it. */
  open_margin_mm: number
  max_amplitude_mm: number
  min_amplitude_mm: number
  meander_gap_widths: number
  meander_chamfer_widths: number
  min_run_mm: number
  pad_keepout_mm: number
}

export interface BoardInfo {
  filename: string
  copper_layers: string[]
  stackup_mm: number
  footprints: number
  pads: number
  tracks: number
  vias: number
  net_classes: string[]
  has_custom_dru: boolean
  /** What became of that .kicad_dru: how many rules are applied, and which were left alone. */
  custom_rules?: CustomRulesInfo
  via_length_counted: boolean
  /** What one via through the whole board adds when via height is counted. */
  via_barrel_mm?: number
  /** False when no .kicad_pro was uploaded, in which case every clearance fell back to the board minimum. */
  has_project_file: boolean
}

/** What the board's own .kicad_dru amounted to. */
/**
 * Where every figure the tool works to came from.
 *
 * KiCad tells you why a track is the width it is; a tool that applies its own
 * numbers on top of the board's owes the same. Three sources, not
 * interchangeable: the board's setup and net classes, the custom rules in the
 * .kicad_dru, and this tool's own settings -- which are the only ones you can
 * change here.
 */
export interface DesignRules {
  min_clearance_mm: number
  min_track_width_mm: number
  edge_clearance_mm: number
  classes?: NetClassInfo[]
  custom?: CustomRuleInfo[]
  /** What the nets being matched actually work to, and why. */
  effective?: EffectiveRule[]
}

export interface NetClassInfo {
  name: string
  clearance_mm: number
  track_width_mm: number
  diff_pair_width_mm?: number
  diff_pair_gap_mm?: number
  via_diameter_mm?: number
  via_drill_mm?: number
  /** How many of the nets being matched fall in this class. */
  nets?: number
}

export interface CustomRuleInfo {
  name: string
  condition?: string
  clearance_mm?: number
  applied: boolean
  why?: string
}

export interface EffectiveRule {
  what: string
  value_mm: number
  /** What set it: a net class, a rule, or your own setting. */
  source: string
  note?: string
}

/** A requirement across groups, and whether the board meets it. */
export interface CheckInfo {
  /** "strobe-to-clock" or "chip-delta". */
  kind: string
  name: string
  /** Signed (strobe minus clock) for strobe-to-clock; a magnitude for chip-delta. */
  value_mm: number
  limit_mm: number
  ok: boolean
  /** The arithmetic behind the figure. */
  detail: string
}

export interface CustomRulesInfo {
  applied: number
  skipped: number
  skipped_rules?: { name: string; why: string }[]
}

export interface InterfaceInfo {
  net_prefix: string
  controller: string
  /** The controller footprint's value: the part number a package table is found by. */
  controller_value?: string
  devices: string[]
  width_bits: number
  lanes: number
  nets_found: number
  unclassified?: string[]
  notes?: string[]
}

export interface RoutingGap {
  /** How the net's copper is split, e.g. ["U3+U4", "U5", "termination"]. */
  islands: string[]
  nets: string[]
}

/** One differential pair, measured. */
export interface PairSkewInfo {
  name: string
  p: string
  n: string
  skew_mm: number
  limit_mm: number
  routed: boolean
  in_tolerance: boolean
}

/** One group of nets measured against its reference. */
export interface GroupSkewInfo {
  name: string
  reference: string
  reference_mm: number
  spread_mm: number
  limit_mm: number
  out_of_tolerance: number
  unroutable: number
  /** The length every member is brought to, and what the group needs to get there. */
  target_mm?: number
  need_mm?: number
  /** How many nets it has. */
  members?: number
  /** What the group is and where its limit comes from. */
  why?: string
}

/**
 * One interface found on the board.
 *
 * Detection is by net name, which is a reading and not a proof — nothing in the
 * geometry says a pair is PCIe rather than SATA. So each one carries the
 * evidence it was recognised by, and disagreeing with the tool is a matter of
 * unticking a box.
 */
export interface DetectedInterface {
  id: string
  kind: string
  name: string
  nets: number
  routed: number
  pairs: number
  evidence: string
  /** The analyser that handles this one in detail, where there is one. */
  planner?: string
  intra_pair_mm?: number
  summary: string
  actionable: boolean
  /** Nets with no complete route: nothing can be matched over copper that is not there. */
  unroutable: number
  /** Which ones they are, by the name the tables use. */
  unrouted_nets?: string[]
  /** The nets of this interface that need length, in the same shape the DDR plan produces. */
  candidates?: MemberInfo[]
  total_need_mm?: number
  /** Every pad-to-pad connection its nets have not got, for the router. */
  missing?: MissingConnection[]
  pair_skew?: PairSkewInfo[]
  groups?: GroupSkewInfo[]
  geometry: GeometryInfo
  impedance: ImpedanceInfo
  /** True when you said what this is, rather than the tool reading it from the names. */
  assigned?: boolean
}

/** An interface's physical shape: measured where the board has it, asked for where it does not. */
export interface GeometryInfo {
  width_mm: number
  gap_mm: number
  layer?: string
  /** What came off the board rather than from you — offered for confirmation. */
  measured?: string[]
  needs_width: boolean
  needs_gap: boolean
  widths?: { width_mm: number; length_mm: number }[]
  note?: string
}

/**
 * What the geometry comes out at, against what the family usually asks for.
 *
 * An estimate from closed-form models and the stackup in the board file. A
 * board that has to hold its impedance to a few percent needs the fabricator's
 * stackup and a field solver; this catches the blunter problem of a width
 * nowhere near what the interface wants.
 */
export interface ImpedanceInfo {
  single_ended_ohms?: number
  diff_ohms?: number
  target_single_ended_ohms?: number
  target_diff_ohms?: number
  /** False when there was no width to work from: everything else here is then meaningless. */
  computed: boolean
  microstrip: boolean
  in_range: boolean
  layer?: string
  /** True when nothing is routed, so the layer these figures assume is a guess. */
  layer_assumed?: boolean
  note?: string
  target_note?: string
}

/** One kind of interface the tool can describe, for saying what your nets are. */
export interface FamilyInfo {
  kind: string
  label: string
  summary: string
  /** The signal names a board usually gives each, so you can recognise your own. */
  tx?: string[]
  rx?: string[]
  common?: string[]
  single_ended_ohms?: number
  diff_ohms?: number
  ohms_note?: string
  intra_pair_mm?: number
  group_mm?: number
  differential?: boolean
}

/** What you have said an interface actually is. */
export interface InterfaceOverride {
  id: string
  kind?: string
  reference?: string
  width_mm?: number
  gap_mm?: number
  ignore?: boolean
}

/** A rectangle on the board, in the preview's coordinates. */
export interface Area {
  min_x: number
  min_y: number
  max_x: number
  max_y: number
  /**
   * The least space between different nets' copper inside this area.
   *
   * It lives on the area because that is where you know it: "keep 0.25 mm apart
   * in here" needs nothing guessed, where a global setting has to work out for
   * itself what "away from the components" means. It only ever tightens.
   */
  min_clearance_mm?: number
  label?: string
}

/** A region the tool would draw, and what it would be worth. */
export interface SuggestedArea extends Area {
  gain_mm: number
  nets: number
}

/** What the tool proposes, for you to adjust rather than invent. */
export interface SuggestResponse {
  areas: SuggestedArea[]
  total_need_mm: number
  /** What these are and are not. */
  note: string
}

export interface RoutingInfo {
  complete: number
  incomplete: number
  gaps?: RoutingGap[]
  /**
   * Every connection the fly-by chain has not got, pad to pad. The work list:
   * a length cannot be matched over copper that is not there, and KiCad
   * reports none of these as unconnected because each net has copper on it.
   */
  missing?: MissingConnection[]
  /** The fly-by chain the address, command, control and clock nets run in. */
  chain?: ChainInfo
}

/** One pad-to-pad join the board has not got. */
export interface MissingConnection {
  net: string
  label: string
  from: string
  to: string
  /** The span it belongs to, as "U4 -> U5". */
  hop: string
}

/** One span of the fly-by chain. */
export interface HopInfo {
  from: string
  to: string
  /** How many of the chain's nets have copper joining these two. */
  nets: number
  of: number
  routed: boolean
}

/**
 * The fly-by chain as the board has it.
 *
 * DDR3 and DDR4 address, command, control and clock have to be fly-by: through
 * each device in turn and terminated at the end. A board missing a hop has not
 * routed that bus, however finished the copper on it looks — and KiCad reports
 * no unconnected item for any of those nets, because each one is a net with
 * copper on it. Which is why this is stated rather than left to be inferred
 * from a list of islands.
 */
export interface ChainInfo {
  /** The controller followed by the devices, outward. */
  order: string[]
  hops: HopInfo[]
  /** How the order was established: a conclusion, not a reading. */
  order_from?: string
  note?: string
  complete: boolean
}

/**
 * A measured length taken apart. The four lengths add up to the length they
 * belong to, so the report can show what it counted.
 */
export interface LengthParts {
  /** Centreline length of the tracks and arcs on the route. */
  track_mm: number
  /** Via barrels crossed: each via's height through the stack-up. Zero when the board does not count via height. */
  via_mm: number
  /** How many vias the route crosses. */
  vias: number
  /** From each end pad's centre to where the track meets the pad. */
  pad_mm: number
  /** The wiring inside the chip package at each end: the pad's die length. */
  package_mm: number
}

export interface MemberInfo {
  net: string
  label: string
  role: string
  routed: boolean
  length_mm: number
  delay_ps: number
  /** Signed distance from the group target; negative means short. */
  deviation_mm: number
  /** length_mm taken apart: track, vias, pad entry, package. */
  parts?: LengthParts
  /** How much has to be added. Never negative: a meander cannot shorten a track. */
  need_mm: number
  need_ps: number
  in_tolerance: boolean
  from?: string
  to?: string
  /**
   * The room beside this route. Zero until measured.
   *
   * As a candidate -- one entry standing for a whole net -- it is the room
   * that can actually be used: each leg's room capped at what that leg needs,
   * summed. Room beside one leg cannot be lent to another.
   */
  headroom_mm: number
  /** True when the net asks for more length than any meander could supply, whatever room were opened, or is longer than its reference. */
  needs_reroute: boolean
  /** How far past the tolerance band it is on the long side: how much shorter it has to be routed. */
  excess_mm?: number
  /** A half of the group's own reference pair: its offset is against the pair's mean, and it is not tuned. */
  reference?: boolean
  /** The whole net's length in KiCad, when this member is only one leg of it. */
  net_total_mm?: number
  /** The whole net's length once every leg of it is matched. */
  net_aim_mm?: number
  /**
   * Roughly what adding need_mm would take: the straight track to fold the
   * meander into, and the board that meander would then occupy. Best case --
   * it assumes the full amplitude the rules allow -- and zero until the room
   * is measured.
   */
  run_needed_mm?: number
  space_needed_mm2?: number
  /**
   * Set when a net is short on more than one span of the fly-by chain, which
   * is normal for address and command lines: the same net is measured
   * controller-to-device and device-to-device, each against the clock over
   * that span, each needing its own copper. need_mm above is their sum.
   */
  legs?: CandidateLeg[]
}

/**
 * One span a net is short over: the group that measured it, and everything
 * about the requirement belonging to that span alone.
 *
 * length_mm is the length of the span, not of the net -- which is why the
 * breakdown exists. A net short on two legs has two current lengths and two
 * targets, and adding a two-leg requirement to a one-leg length gives a figure
 * that means nothing.
 */
export interface CandidateLeg {
  group: string
  leg?: string
  length_mm: number
  parts?: LengthParts
  need_mm: number
  /** How much too long this leg is for its target: nothing to add, it needs routing shorter. */
  excess_mm?: number
  /** The length this leg is matched to: its group's reference. */
  target_mm?: number
  /** The room beside this leg's copper, uncapped. */
  headroom_mm: number
  needs_reroute: boolean
  run_needed_mm?: number
  space_needed_mm2?: number
}

export interface GroupInfo {
  name: string
  kind: string
  lane: number
  leg?: string
  reference: string
  /** The reference length: the mean of its halves when it is a pair. */
  reference_length_mm: number
  /** The halves the reference length is the mean of, with their own lengths. */
  reference_members?: { net: string; label: string; length_mm: number; parts?: LengthParts }[]
  /** What every member is brought to. */
  target_mm: number
  /** What the reference asked for, before the target was raised to clear the longest member. */
  target_from_reference_mm: number
  tolerance_mm: number
  spread_mm: number
  total_need_mm: number
  out_of_tolerance: number
  members: MemberInfo[]
  notes?: string[]
}

export interface PairSkew {
  pair: string
  skew_mm: number
  limit_mm: number
  ok: boolean
}

/** Pads of one footprint that carry a length inside the package. */
export interface PackageLength {
  ref: string
  part: string
  source: string
  /** Pads given a length from a built-in vendor table. */
  pads: number
  /** Pads whose footprint already sets a die length. */
  from_board: number
}

/** A layer rule some DDR nets do not follow. For information only. Mirrors server LayerFinding. */
export interface LayerFinding {
  /** "data-top" or "address-bottom". */
  rule: string
  /** What the routing guide expects, in words. */
  expect: string
  nets: { net: string; label: string; by_layer_mm: Record<string, number>; share: number }[]
}

/** The package length on one of the controller's DDR pads. Mirrors pkglen.PadLength. */
export interface PackagePad {
  net: string
  /** "U3.M19" */
  pad: string
  /** The ball name from the pin function, "DDR_A5". */
  ball?: string
  /** From the footprint or the part's table. */
  default_mm: number
  /** In force: the default unless overridden. */
  mm: number
  /** "footprint", a part name, "override", or empty when nothing set it. */
  source?: string
}

export interface Analysis {
  package_pads?: PackagePad[]
  layers?: LayerFinding[]
  package_lengths?: PackageLength[]
  board: BoardInfo
  interface: InterfaceInfo
  routing: RoutingInfo
  params: Params
  groups: GroupInfo[]
  pair_skew?: PairSkew[]
  /** Everything on the board this tool recognises, DDR included. */
  interfaces?: DetectedInterface[]
  /** Things to know before trusting the rest: that there is no DDR here, say. */
  notes?: string[]
  /** Every figure the tool works to, and what set it. */
  rules?: DesignRules
  /** Requirements across groups: each lane's strobe against the clock, one device's lanes against another's. */
  checks?: CheckInfo[]
  skipped?: Record<string, string>
  /** The nets that would change, worst first. This is the list the user ticks. */
  candidates: MemberInfo[]
  total_need_mm: number
  /** Each net's room capped at what it needs, summed. Zero until headroom is measured. */
  gettable_mm: number
  reroute_count: number
  bus_spare_mm: number
  /**
   * What the whole requirement would take up: the straight track to fold the
   * meanders into, and the board area they would occupy. Zero until the room
   * is measured.
   *
   * A total, not a place to look -- the length has to go beside the net that
   * needs it. It is here because "336.944 mm to add" is a figure nobody can
   * picture and "about 90 mm² of clear board, in the right places" is one
   * somebody can decide about.
   */
  run_needed_mm?: number
  space_needed_mm2?: number
}

/**
 * How much of what is needed the board can actually hold.
 *
 * A request of its own because it is the slow half: where the report measures
 * lengths, this probes the design rules along every candidate track. It is what
 * separates "no room here" from "this needs rerouting", which is the difference
 * between a shortfall worth working around and one that is not.
 */
export interface HeadroomResponse {
  candidates: MemberInfo[]
  /** Every other interface, with its room measured the same way. */
  interfaces?: DetectedInterface[]
  total_need_mm: number
  gettable_mm: number
  reroute_count: number
  bus_spare_mm: number
  /**
   * What the whole requirement would take up: the straight track to fold the
   * meanders into, and the board area they would occupy. Zero until the room
   * is measured.
   *
   * A total, not a place to look -- the length has to go beside the net that
   * needs it. It is here because "336.944 mm to add" is a figure nobody can
   * picture and "about 90 mm² of clear board, in the right places" is one
   * somebody can decide about.
   */
  run_needed_mm?: number
  space_needed_mm2?: number
}

/**
 * What the router made of the connections the fly-by chain is missing.
 *
 * Creating copper is a different kind of change from lengthening it -- a
 * meander cannot alter what a board does electrically and a new trace can --
 * so it is a request of its own, and the front end asks before making it.
 */
export interface RouteResponse {
  requested: number
  connected: number
  added_mm: number
  vias: number
  hops?: RouteHopResult[]
  /** True when copper was written, which is also when the session's board was replaced by the routed one. */
  changed: boolean
  after?: Analysis
  session?: Session
  notes?: string[]
}

/** What happened to one connection. */
export interface RouteHopResult {
  net: string
  label: string
  from: string
  to: string
  routed: boolean
  length_mm?: number
  vias?: number
  /** How many paths were tried. More than one means the grid proposed something the clearance check refused. */
  attempts?: number
  /** Why it did not route. */
  reason?: string
}

/** What the router made of the missing hops, kept on the session. */
export interface RouteRecord {
  at: string
  requested: number
  connected: number
  added_mm: number
  vias: number
  result_filename: string
  result_bytes: number
}

export interface ApplyRecord {
  at: string
  nets: string[]
  added_mm: number
  shortfall_mm: number
  nets_met: number
  nets_short: number
  result_filename: string
  result_bytes: number
}

export interface Session {
  id: string
  user_id: string
  organization_id: string
  filename: string
  board_bytes: number
  created_at: string
  expires_at: string
  params: Params
  has_project_file: boolean
  applied?: ApplyRecord
  /**
   * Set once the router has run and written copper. When it is, the board in
   * this session is the routed one: everything measured since is measured over
   * the new copper.
   */
  routed?: RouteRecord
}

export interface SessionResponse {
  session: Session
  analysis: Analysis
}

export interface NetResult {
  net: string
  label: string
  /** The span this row is about, when the net was short on more than one. */
  leg?: string
  requested_mm: number
  added_mm: number
  shortfall_mm: number
  meanders: number
  notes?: string[]
}

export interface ApplyResponse {
  session: Session
  results: NetResult[]
  /** The board re-measured from the edited copper, not the tuner's own bookkeeping. */
  after: Analysis
  added_mm: number
  shortfall_mm: number
  nets_met: number
  nets_short: number
  /** False when nothing could be fitted, in which case there is no board to download. */
  changed: boolean
  /** What to run so KiCad checks the result. */
  verify_command: string
}

export interface Defaults {
  params: Params
  max_upload_bytes: number
  session_ttl_seconds: number
  /** The interface families the tool knows, with the names each usually carries. */
  families?: FamilyInfo[]
  /** Parts with a package length table. */
  package_parts?: string[]
}

/** One net's length against what it is matched to. Mirrors server/kicad.go. */
export interface NetStatus {
  net: string
  label: string
  /** "DDR", or the interface's name. */
  interface: string
  group: string
  leg?: string
  /** A differential pair matched only to its other half. */
  pair?: boolean
  /** The net the group is matched to: reported, not tuned. */
  reference?: boolean
  routed: boolean
  length_mm: number
  parts?: LengthParts
  target_mm: number
  tolerance_mm: number
  /** Signed: negative is short. */
  deviation_mm: number
  in_tolerance: boolean
  need_mm: number
  excess_mm?: number
}

export interface UnmatchedNet {
  net: string
  label: string
  exists: boolean
  /** KiCad's own figure: every piece of copper on the net. */
  length_mm: number
  complete: boolean
}

export interface NetsResponse {
  nets: NetStatus[]
  unmatched?: UnmatchedNet[]
}

/** One piece of copper, in KiCad's internal units (nanometres). */
export interface ChangeItem {
  uuid?: string
  kind: 'segment' | 'arc' | 'via'
  net: string
  layer?: string
  start_nm: [number, number]
  end_nm: [number, number]
  mid_nm?: [number, number]
  width_nm?: number
  size_nm?: number
  drill_nm?: number
  layer_top?: string
  layer_bottom?: string
}

/** The last apply, as edits to the board that was uploaded. */
export interface ChangesResponse {
  remove: ChangeItem[]
  add: ChangeItem[]
  nets: string[]
  message: string
}

/** ApiError carries the status so a caller can tell "not signed in" from "bad board". */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }

  /** True when the session is gone: expired, deleted, or never the caller's. */
  get isMissing(): boolean {
    return this.status === 404
  }

  get isUnauthorized(): boolean {
    return this.status === 401
  }

  get isTooLarge(): boolean {
    return this.status === 413
  }
}

export interface UploadInput {
  board: File
  /** The .kicad_pro. Strongly encouraged: without it there are no net classes. */
  project?: File | null
  /** The .kicad_dru, so the report can say that custom rules exist. */
  rules?: File | null
  netPrefix?: string
  controller?: string
}

/** AnalyzerApi talks to the control plane mounted at `base`. */
export class AnalyzerApi {
  /**
   * `headers` is how a host authenticates: embeddedci-server keeps its session
   * as a bearer token rather than a cookie, so every request -- the JSON, the
   * geometry, the board download -- has to carry it, and a plain link cannot.
   * The standalone harness passes nothing.
   */
  constructor(
    private readonly base = '/api',
    private readonly baseFetch: typeof fetch = (...a) => fetch(...a),
    private readonly headers: () => Record<string, string> = () => ({}),
  ) {}

  private fetchImpl: typeof fetch = (input, init) => {
    const merged = new Headers(init?.headers)
    for (const [k, v] of Object.entries(this.headers())) merged.set(k, v)
    return this.baseFetch(input, { credentials: 'same-origin', ...init, headers: merged })
  }

  private async request<T>(path: string, init?: RequestInit): Promise<T> {
    const res = await this.fetchImpl(this.base + path, {
      credentials: 'same-origin',
      ...init,
    })
    if (!res.ok) {
      throw new ApiError(res.status, await errorMessage(res))
    }
    if (res.status === 204) {
      return undefined as T
    }
    return (await res.json()) as T
  }

  defaults(): Promise<Defaults> {
    return this.request<Defaults>('/pcb-trace-length-analyzer/defaults')
  }

  listSessions(): Promise<{ sessions: Session[] }> {
    return this.request<{ sessions: Session[] }>('/pcb-trace-length-analyzer/sessions')
  }

  getSession(id: string): Promise<SessionResponse> {
    return this.request<SessionResponse>(`/pcb-trace-length-analyzer/sessions/${encodeURIComponent(id)}`)
  }

  deleteSession(id: string): Promise<void> {
    return this.request<void>(`/pcb-trace-length-analyzer/sessions/${encodeURIComponent(id)}`, { method: 'DELETE' })
  }

  upload(input: UploadInput): Promise<SessionResponse> {
    const form = new FormData()
    form.set('board', input.board, input.board.name)
    if (input.project) form.set('project', input.project, input.project.name)
    if (input.rules) form.set('rules', input.rules, input.rules.name)
    if (input.netPrefix) form.set('net_prefix', input.netPrefix)
    if (input.controller) form.set('controller', input.controller)
    return this.request<SessionResponse>('/pcb-trace-length-analyzer/sessions', { method: 'POST', body: form })
  }

  headroom(id: string): Promise<HeadroomResponse> {
    return this.request<HeadroomResponse>(`/pcb-trace-length-analyzer/sessions/${encodeURIComponent(id)}/headroom`)
  }

  /**
   * Make the copper the fly-by chain is missing.
   *
   * The one call here that creates rather than edits, and the one the front end
   * asks about first: it replaces the board this session works on, and what it
   * writes is a grid search's idea of where a bus should run rather than a
   * person's.
   */
  route(id: string, nets?: string[]): Promise<RouteResponse> {
    return this.request<RouteResponse>(`/pcb-trace-length-analyzer/sessions/${encodeURIComponent(id)}/route`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(nets && nets.length > 0 ? { nets } : {}),
    })
  }

  /**
   * Where the tool would put the areas, so you adjust rather than invent.
   *
   * Measured with no areas in force: the question is what the board offers, not
   * what is allowed under whatever has been drawn so far.
   */
  suggestedAreas(id: string): Promise<SuggestResponse> {
    return this.request<SuggestResponse>(
      `/pcb-trace-length-analyzer/sessions/${encodeURIComponent(id)}/suggested-areas`,
    )
  }

  plan(id: string, params: Params): Promise<SessionResponse> {
    return this.request<SessionResponse>(`/pcb-trace-length-analyzer/sessions/${encodeURIComponent(id)}/plan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ params }),
    })
  }

  apply(id: string, opts: { params?: Params; nets?: string[]; groups?: string[] }): Promise<ApplyResponse> {
    return this.request<ApplyResponse>(`/pcb-trace-length-analyzer/sessions/${encodeURIComponent(id)}/apply`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(opts),
    })
  }

  /**
   * Nets by name -- the full name KiCad uses, or the label the report shows --
   * or, with `attention`, every net out of tolerance, worst first.
   */
  nets(id: string, opts: { names?: string[]; attention?: boolean } = {}): Promise<NetsResponse> {
    const q = new URLSearchParams()
    for (const n of opts.names ?? []) q.append('name', n)
    if (opts.attention) q.set('attention', '1')
    const qs = q.toString()
    return this.request<NetsResponse>(
      `/pcb-trace-length-analyzer/sessions/${encodeURIComponent(id)}/nets${qs ? `?${qs}` : ''}`,
    )
  }

  /** The last apply as edits: tracks to remove by uuid, tracks to add. */
  changes(id: string): Promise<ChangesResponse> {
    return this.request<ChangesResponse>(`/pcb-trace-length-analyzer/sessions/${encodeURIComponent(id)}/changes`)
  }

  /** The URL the tuned board is served from. */
  downloadUrl(id: string): string {
    return `${this.base}/pcb-trace-length-analyzer/sessions/${encodeURIComponent(id)}/download`
  }

  /**
   * The tuned or routed board, fetched with the same credentials as everything
   * else. A plain link would carry no bearer token, so a signed-in user would
   * be asking as nobody and get somebody else's -- that is, no -- board.
   */
  async downloadResult(id: string): Promise<{ filename: string; blob: Blob }> {
    const res = await this.fetchImpl(this.downloadUrl(id))
    if (!res.ok) {
      throw new ApiError(res.status, await errorMessage(res))
    }
    const disposition = res.headers.get('Content-Disposition') ?? ''
    const filename = /filename="([^"]+)"/.exec(disposition)?.[1] ?? 'board.kicad_pcb'
    return { filename, blob: await res.blob() }
  }

  /**
   * The board as geometry a browser can draw: a description and a buffer of
   * triangles, fetched together because the viewer needs both to show anything.
   *
   * `of` picks the board that was uploaded or the result of applying the plan.
   * The pours are left out by default: they are most of a board's copper by
   * area, they sit under the traces this tool changes, and drawing them doubles
   * the download. The document says when they are missing.
   */
  async preview(
    id: string,
    of: PreviewOf = 'board',
    opts: { zones?: boolean } = {},
  ): Promise<{ doc: BoardDoc; geometry: ArrayBuffer }> {
    const q = new URLSearchParams({ of })
    if (opts.zones) q.set('zones', '1')
    const at = `/pcb-trace-length-analyzer/sessions/${encodeURIComponent(id)}/preview`
    const doc = await this.request<BoardDoc>(`${at}/board.json?${q}`)
    const res = await this.fetchImpl(`${this.base}${at}/geometry.bin?${q}`, {
      credentials: 'same-origin',
    })
    if (!res.ok) {
      throw new ApiError(res.status, await errorMessage(res))
    }
    const geometry = await res.arrayBuffer()
    if (geometry.byteLength !== doc.geometry.byte_length) {
      throw new Error(
        `the board's geometry arrived ${geometry.byteLength} bytes long, its index says ${doc.geometry.byte_length}`,
      )
    }
    return { doc, geometry }
  }
}

/** Which board a preview is of. */
export type PreviewOf = 'board' | 'result'

async function errorMessage(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: string }
    if (body.error) return body.error
  } catch {
    // Not JSON; fall through to the status text, which is better than nothing.
  }
  return res.statusText || `request failed with status ${res.status}`
}
