/**
 * The board, drawn.
 *
 * Everything else this app shows is a number in a table, and a number in a
 * table cannot answer the question a layout engineer asks first: where is this
 * trace, and what did you do to it. A meander is obvious on screen and
 * invisible in a diff, so the before-and-after toggle here is how the tool
 * shows its work.
 *
 * The renderer and the two components around it come from the EMI Analyzer's
 * frontend, linked in by path. Nothing about a PCB viewer is specific to
 * length matching, and that one is already written, tested against real boards
 * and fast on dense ones.
 */

import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Card,
  Group,
  Loader,
  SegmentedControl,
  Select,
  Stack,
  Switch,
  Table,
  Text,
} from '@mantine/core'
import { LengthInput } from './LengthInput'
import type { BoardRenderer } from '@emi/lib/BoardRenderer'
import { BoardCanvas } from '@emi/components/BoardCanvas'
import { LayerRail } from '@emi/components/LayerRail'
import type { Area, AnalyzerApi, PreviewOf } from '../lib/analyzerApi'
import { centre, netBounds, zoomFor } from '../lib/netBounds'

export interface BoardPreviewProps {
  api: AnalyzerApi
  sessionId: string

  /**
   * Nets worth offering, in the order the tables show them. The label is the
   * name people say -- the tool's tables call it DQ23, the board file calls it
   * /ddr4/DDR_DQ23, and the picker has to match the tables.
   */
  nets: { net: string; label: string }[]

  /** Nets the apply actually changed, marked on the result view. */
  changed?: string[]

  /** Whether an apply has produced a result to look at. */
  hasResult: boolean

  /**
   * The regions copper may be added in. When onAreasChange is given, the user
   * can drag new ones out on the board.
   */
  areas?: Area[]
  onAreasChange?: (next: Area[]) => void

  /** Fills the areas in from where the tool would have put them. */
  onSuggest?: () => void
  suggesting?: boolean

  height?: number
}

export function BoardPreview({
  api,
  sessionId,
  nets,
  changed = [],
  hasResult,
  areas = [],
  onAreasChange,
  onSuggest,
  suggesting = false,
  height = 520,
}: BoardPreviewProps) {
  // Drawing is a mode rather than a modifier key: marking out where the tool
  // may put copper is a deliberate step, not an expert shortcut, and while it
  // is on, a drag has to mean that rather than panning.
  const [drawing, setDrawing] = useState(false)
  const [pending, setPending] = useState<[number, number, number, number] | null>(null)
  // The renderer's view, mirrored here so the areas can be drawn over the
  // canvas. Polled on an animation frame and only stored when it changes, so
  // a board nobody is touching costs nothing -- a drag re-renders a handful of
  // divs, which is what this trades for being able to see what you drew.
  const [view, setView] = useState<{ tx: number; ty: number; scale: number } | null>(null)
  useEffect(() => {
    let raf = 0
    const tick = () => {
      const v = rendererRef.current?.getView()
      if (v) {
        setView((prev) =>
          prev && prev.tx === v.tx && prev.ty === v.ty && prev.scale === v.scale
            ? prev
            : { tx: v.tx, ty: v.ty, scale: v.scale },
        )
      }
      raf = requestAnimationFrame(tick)
    }
    raf = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(raf)
  }, [])
  const [of, setOf] = useState<PreviewOf>(hasResult ? 'result' : 'board')
  const [zones, setZones] = useState(false)
  const [highlight, setHighlight] = useState<string | null>(null)
  const [visibility, setVisibility] = useState<Record<string, boolean>>({})
  const [cursor, setCursor] = useState<{ x: number; y: number } | null>(null)
  const [focus, setFocus] = useState<{ x: number; y: number; zoom?: number } | null>(null)
  const boxRef = useRef<HTMLDivElement | null>(null)
  const rendererRef = useRef<BoardRenderer | null>(null)
  // Where the user was looking. A new board -- the other side of the
  // before-and-after, or the pours being switched on -- is framed from scratch
  // by the canvas, which is right the first time and wrong every time after:
  // the whole point of flipping between the two boards is to compare the same
  // square millimetre, and being thrown back to the whole board each time makes
  // that impossible. So the view is carried across.
  const viewRef = useRef<{ tx: number; ty: number; scale: number } | null>(null)

  // A result appearing is worth switching to: the user just applied something
  // and the point is to see it.
  useEffect(() => {
    if (hasResult) setOf('result')
  }, [hasResult])

  const preview = useQuery({
    queryKey: ['analyzer', 'preview', sessionId, of, zones],
    queryFn: () => api.preview(sessionId, of, { zones }),
    staleTime: Infinity,
  })

  const doc = preview.data?.doc ?? null
  const geometry = preview.data?.geometry ?? null

  // Runs after the canvas has loaded the new board and framed it, because a
  // child's effects run before its parent's.
  useEffect(() => {
    const renderer = rendererRef.current
    const view = viewRef.current
    if (!renderer || !doc || !view) return
    renderer.setView(view)
    renderer.render()
  }, [doc, geometry])

  const labels = useMemo(() => new Map(nets.map((n) => [n.net, n.label])), [nets])

  // Where the changed nets are, so the result view says at a glance where the
  // work happened rather than leaving the user to hunt for it.
  const markers = useMemo(() => {
    if (!doc || !geometry || of !== 'result') return []
    return changed
      .map((net) => {
        const b = netBounds(doc, geometry, net)
        if (!b) return null
        return { ...centre(b), label: labels.get(net) ?? net }
      })
      .filter((m): m is { x: number; y: number; label: string } => m !== null)
  }, [doc, geometry, of, changed, labels])

  // Picking a net frames it. A net is long and thin, so this is a zoom to its
  // extent with room around it: what the user wants to see is the trace and
  // what it runs beside.
  //
  // Only when the net actually changes, though. Re-framing whenever the board
  // does would undo the view being carried across, and a user who has zoomed
  // past the whole net to look at one meander has said where they want to be
  // more recently than the picker did.
  const framed = useRef<string | null>(null)
  useEffect(() => {
    if (!doc || !geometry) return
    if (framed.current === highlight) return
    framed.current = highlight
    if (!highlight) {
      setFocus(null)
      return
    }
    const b = netBounds(doc, geometry, highlight)
    if (!b) {
      setFocus(null)
      return
    }
    const box = boxRef.current?.getBoundingClientRect()
    viewRef.current = null
    setFocus({
      ...centre(b),
      zoom: zoomFor(b, { width: box?.width ?? 800, height: box?.height ?? height }),
    })
  }, [doc, geometry, highlight, height])

  const options = useMemo(() => {
    const seen = new Set<string>()
    const out: { value: string; label: string }[] = []
    for (const n of nets) {
      if (seen.has(n.net)) continue
      seen.add(n.net)
      out.push({ value: n.net, label: n.label })
    }
    return out
  }, [nets])

  return (
    <Card withBorder padding="md">
      <Stack gap="sm">
        <Group justify="space-between" align="flex-end">
          <Group gap="sm" align="flex-end">
            <SegmentedControl
              size="xs"
              value={of}
              onChange={(v) => {
                viewRef.current = rendererRef.current?.getView() ?? null
                setOf(v as PreviewOf)
              }}
              data={[
                { value: 'board', label: 'Before' },
                { value: 'result', label: 'After', disabled: !hasResult },
              ]}
            />
            <Select
              size="xs"
              w={220}
              placeholder="Find a net"
              searchable
              clearable
              nothingFoundMessage="No net by that name"
              data={options}
              value={highlight}
              onChange={setHighlight}
            />
            <Switch
              size="xs"
              label="Copper pours"
              checked={zones}
              onChange={(e) => {
                viewRef.current = rendererRef.current?.getView() ?? null
                setZones(e.currentTarget.checked)
              }}
            />
            {onAreasChange && (
              <Button
                size="xs"
                variant={drawing ? 'filled' : 'light'}
                onClick={() => setDrawing((d) => !d)}
              >
                {drawing ? 'Done drawing' : 'Draw an area'}
              </Button>
            )}
            {onSuggest && (
              <Button size="xs" variant="light" loading={suggesting} onClick={onSuggest}>
                Suggest areas
              </Button>
            )}
          </Group>
          <Group gap="xs">
            {cursor ? (
              <Text size="xs" c="dimmed" ff="monospace">
                {cursor.x.toFixed(2)}, {cursor.y.toFixed(2)} mm
              </Text>
            ) : null}
            {doc ? (
              <Badge size="sm" variant="light">
                {doc.board.width_mm.toFixed(1)} x {doc.board.height_mm.toFixed(1)} mm ·{' '}
                {(doc.geometry.vertex_count / 3).toLocaleString()} triangles
              </Badge>
            ) : null}
          </Group>
        </Group>

        {preview.error ? (
          <Alert color="red" variant="light" title="Could not draw the board">
            {(preview.error as Error).message}
          </Alert>
        ) : null}

        <Group align="flex-start" gap="md" wrap="nowrap">
          {doc ? (
            <div style={{ flex: 'none', width: 150 }}>
              <LayerRail
                doc={doc}
                visibility={visibility}
                onToggle={(layer, visible) =>
                  setVisibility((prev) => ({ ...prev, [layer]: visible }))
                }
              />
            </div>
          ) : null}
          <div
            ref={boxRef}
            style={{
              flex: 1,
              minWidth: 0,
              height,
              position: 'relative',
              borderRadius: 4,
              overflow: 'hidden',
              background: '#0b0d10',
            }}
          >
            {preview.isFetching && !doc ? (
              <Group justify="center" align="center" h="100%">
                <Loader size="sm" />
              </Group>
            ) : null}
            {doc && geometry && view ? <AreaOverlay areas={areas} view={view} /> : null}
            {doc && geometry ? (
              <BoardCanvas
                doc={doc}
                geometry={geometry}
                layerVisibility={visibility}
                highlightNet={highlight}
                markers={markers}
                focus={focus}
                mode={drawing ? 'roi' : 'pan'}
                roi={pending}
                onRoiChange={(r) => {
                  setPending(r)
                  if (!onAreasChange) return
                  const [minX, minY, maxX, maxY] = r
                  // A click is not an area. Half a millimetre is below
                  // anything worth meandering in and is what a stray press
                  // produces.
                  if (Math.abs(maxX - minX) < 0.5 || Math.abs(maxY - minY) < 0.5) return
                  onAreasChange([...areas, { min_x: minX, min_y: minY, max_x: maxX, max_y: maxY }])
                  setPending(null)
                }}
                onCursorMove={setCursor}
                onReady={(r) => {
                  rendererRef.current = r
                }}
                style={{ width: '100%', height: '100%', display: 'block' }}
              />
            ) : null}
          </div>
        </Group>

        {/* Drag to pan, scroll to zoom: worth saying once, because a canvas
            offers no other clue that it is interactive. */}
        <Text size="xs" c="dimmed">
          {drawing ? 'Drag out a region copper may be added in. ' : 'Drag to pan, scroll to zoom. '}
          {of === 'result' && markers.length > 0
            ? `Crosses mark the ${markers.length} net${markers.length === 1 ? '' : 's'} that changed.`
            : 'Pick a net to highlight it and zoom to it.'}
        </Text>

        {onAreasChange && areas.length > 0 ? (
          <div>
            <Text size="sm" fw={500} mb={4}>
              Copper may be added here, and nowhere else
            </Text>
            <Table.ScrollContainer minWidth={380}>
              <Table verticalSpacing={4} fz="xs">
                <Table.Thead>
                  <Table.Tr>
                    <Table.Th>Region</Table.Th>
                    <Table.Th>Least space between traces</Table.Th>
                    <Table.Th />
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {areas.map((a, i) => (
                    <Table.Tr key={`${a.min_x},${a.min_y},${a.max_x},${a.max_y}`}>
                      <Table.Td>
                        <Text size="xs" ff="monospace">
                          {Math.abs(a.max_x - a.min_x).toFixed(1)} &times;{' '}
                          {Math.abs(a.max_y - a.min_y).toFixed(1)} mm at (
                          {Math.min(a.min_x, a.max_x).toFixed(1)},{' '}
                          {Math.min(a.min_y, a.max_y).toFixed(1)})
                        </Text>
                      </Table.Td>
                      <Table.Td w={190}>
                        {/* The spacing belongs to the area, not to the board: in
                            here you know what you want, where a global setting
                            has to guess what "away from the components" means. */}
                        <LengthInput
                          size="xs"
                          placeholder="board's own"
                          step={0.05}
                          min={0}
                          max={5}
                          value={a.min_clearance_mm ?? ''}
                          onChange={(v) => {
                            const n = typeof v === 'number' ? v : Number.parseFloat(String(v))
                            onAreasChange(
                              areas.map((x, k) =>
                                k === i
                                  ? { ...x, min_clearance_mm: Number.isFinite(n) && n > 0 ? n : undefined }
                                  : x,
                              ),
                            )
                          }}
                        />
                      </Table.Td>
                      <Table.Td w={40}>
                        <ActionIcon
                          size="xs"
                          variant="subtle"
                          color="red"
                          aria-label="Remove this area"
                          onClick={() => onAreasChange(areas.filter((_, k) => k !== i))}
                        >
                          &times;
                        </ActionIcon>
                      </Table.Td>
                    </Table.Tr>
                  ))}
                </Table.Tbody>
              </Table>
            </Table.ScrollContainer>
          </div>
        ) : null}

        {doc && doc.warnings.length > 0 ? (
          <Alert color="yellow" variant="light" title="About this drawing">
            <Stack gap={2}>
              {doc.warnings.map((w) => (
                <Text key={w} size="xs">
                  {w}
                </Text>
              ))}
            </Stack>
          </Alert>
        ) : null}
      </Stack>
    </Card>
  )
}

/**
 * The allowed areas, drawn over the board.
 *
 * Drawing a region you cannot then see is not a feature -- you put down three
 * of them and the board looks exactly as it did. The canvas itself will draw
 * one rectangle, which is the wrong number, so these go over the top in HTML.
 *
 * The maths is the renderer's own, in reverse: it puts board millimetres on
 * screen as (mm + translate) * scale from the bottom-left, and CSS counts down
 * from the top, so the vertical flip happens here.
 */
function AreaOverlay({
  areas,
  view,
}: {
  areas: Area[]
  view: { tx: number; ty: number; scale: number }
}) {
  if (areas.length === 0) return null
  // The canvas is drawn at device pixels; the element is laid out in CSS
  // pixels, and the ratio between them is what the renderer scaled by.
  const dpr = typeof window === 'undefined' ? 1 : window.devicePixelRatio || 1
  return (
    <div style={{ position: 'absolute', inset: 0, pointerEvents: 'none', zIndex: 2 }}>
      {areas.map((a, i) => {
        const minX = Math.min(a.min_x, a.max_x)
        const maxX = Math.max(a.min_x, a.max_x)
        const minY = Math.min(a.min_y, a.max_y)
        const maxY = Math.max(a.min_y, a.max_y)
        const left = ((minX + view.tx) * view.scale) / dpr
        const right = ((maxX + view.tx) * view.scale) / dpr
        // Board Y grows upward, CSS downward, so the top edge is the larger Y.
        const top = ((maxY + view.ty) * view.scale) / dpr
        const bottom = ((minY + view.ty) * view.scale) / dpr
        return (
          <div
            key={`${a.min_x},${a.min_y},${a.max_x},${a.max_y}`}
            style={{
              position: 'absolute',
              left,
              width: Math.max(1, right - left),
              bottom,
              height: Math.max(1, top - bottom),
              border: '1px solid rgba(120, 200, 255, 0.9)',
              background: 'rgba(120, 200, 255, 0.10)',
              borderRadius: 2,
            }}
          >
            <span
              style={{
                position: 'absolute',
                top: 2,
                left: 4,
                fontSize: 10,
                color: 'rgba(190, 230, 255, 0.95)',
                fontFamily: 'ui-monospace, monospace',
              }}
            >
              {i + 1}
            </span>
          </div>
        )
      })}
    </div>
  )
}
