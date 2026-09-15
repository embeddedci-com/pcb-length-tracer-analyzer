import { describe, expect, it, vi } from 'vitest'
import userEvent from '@testing-library/user-event'
import type { BoardDoc } from '@emi/lib/boardTypes'
import { BoardPreview } from './BoardPreview'
import { renderUI, screen, waitFor, within } from '../testRender'
import type { Area, AnalyzerApi } from '../lib/analyzerApi'

// The board view, minus the board.
//
// The renderer is WebGL2 and jsdom has no GPU, so the canvas is replaced by a
// stub that offers the same callbacks as buttons. What is left is everything
// around it that has nothing to do with drawing: the list of areas, the
// spacing that belongs to each one, the delete, and the rule that a click is
// not an area. Those are the parts a user can lose work to, and none of them
// need a pixel.

/** A renderer that knows only where it is looking. */
const fakeRenderer = { getView: () => ({ tx: 0, ty: 0, scale: 10 }), setView: () => {}, render: () => {} }

vi.mock('@emi/components/BoardCanvas', () => ({
  BoardCanvas: ({
    mode,
    onRoiChange,
    onReady,
  }: {
    mode?: string
    onRoiChange?: (r: [number, number, number, number]) => void
    onReady?: (r: unknown) => void
  }) => (
    <div data-testid="canvas" data-mode={mode}>
      <button type="button" onClick={() => onReady?.(fakeRenderer)}>
        canvas ready
      </button>
      <button type="button" onClick={() => onRoiChange?.([10, 20, 30, 45])}>
        drag out a region
      </button>
      <button type="button" onClick={() => onRoiChange?.([10, 20, 10.2, 20.3])}>
        click without dragging
      </button>
    </div>
  ),
}))

vi.mock('@emi/components/LayerRail', () => ({ LayerRail: () => <div data-testid="layers" /> }))

const doc = {
  board: { width_mm: 100, height_mm: 80, thickness_mm: 1.6, outline: [] },
  layers: [],
  nets: [],
  vias: [],
  pads: [],
  geometry: { file: 'geometry.bin', dtype: 'float32', components: 2, primitive: 'triangles', vertex_count: 300, byte_length: 2400, groups: [] },
  warnings: [],
} as unknown as BoardDoc

function apiStub(over: Partial<AnalyzerApi> = {}): AnalyzerApi {
  return {
    preview: vi.fn().mockResolvedValue({ doc, geometry: new ArrayBuffer(2400) }),
    ...over,
  } as unknown as AnalyzerApi
}

const area = (over: Partial<Area> = {}): Area => ({
  min_x: 10,
  min_y: 20,
  max_x: 30,
  max_y: 45,
  ...over,
})

function preview(props: Partial<Parameters<typeof BoardPreview>[0]> = {}) {
  return renderUI(
    <BoardPreview
      api={apiStub()}
      sessionId="s1"
      nets={[{ net: '/ddr4/DDR_DQ0', label: 'DQ0' }]}
      hasResult={false}
      {...props}
    />,
  )
}

describe('BoardPreview areas', () => {
  it('lists each area by its size and where it is', async () => {
    preview({ areas: [area()], onAreasChange: vi.fn() })
    expect(await screen.findByText(/20\.0 × 25\.0 mm at \(10\.0, 20\.0\)/)).toBeInTheDocument()
  })

  it('takes an area back out when its × is pressed', async () => {
    const onAreasChange = vi.fn()
    preview({ areas: [area(), area({ min_x: 50, max_x: 60 })], onAreasChange })

    const remove = await screen.findAllByLabelText('Remove this area')
    await userEvent.click(remove[0])
    expect(onAreasChange).toHaveBeenCalledWith([area({ min_x: 50, max_x: 60 })])
  })

  // The spacing is per area on purpose: inside a region you know what you
  // want, where one global figure has to guess what "away from the
  // components" means. So it has to land on the row it was typed into.
  it('attaches a spacing to the area it was typed into, and no other', async () => {
    const onAreasChange = vi.fn()
    const a = area()
    const b = area({ min_x: 50, max_x: 60 })
    preview({ areas: [a, b], onAreasChange })

    const second = (await screen.findByText(/at \(50\.0/)).closest('tr')!
    await userEvent.type(within(second).getByRole('textbox'), '0.25')

    const last = onAreasChange.mock.calls.at(-1)![0] as Area[]
    expect(last[0].min_clearance_mm).toBeUndefined()
    expect(last[1].min_clearance_mm).toBeCloseTo(0.25, 9)
  })

  it('clears the spacing back to the board’s own when it is emptied', async () => {
    const onAreasChange = vi.fn()
    preview({ areas: [area({ min_clearance_mm: 0.25 })], onAreasChange })

    // Scoped to the row: the net finder above the board is a textbox too.
    const row = (await screen.findByText(/at \(10\.0/)).closest('tr')!
    await userEvent.clear(within(row).getByRole('textbox'))
    const last = onAreasChange.mock.calls.at(-1)![0] as Area[]
    expect(last[0].min_clearance_mm).toBeUndefined()
  })

  it('shows no area table at all until one is drawn', async () => {
    preview({ areas: [], onAreasChange: vi.fn() })
    await screen.findByTestId('canvas')
    expect(screen.queryByText(/Copper may be added here/)).not.toBeInTheDocument()
  })
})

describe('BoardPreview drawing', () => {
  it('turns dragging into drawing, and says so', async () => {
    preview({ areas: [], onAreasChange: vi.fn() })
    const canvas = await screen.findByTestId('canvas')
    expect(canvas).toHaveAttribute('data-mode', 'pan')
    expect(screen.getByText(/Drag to pan, scroll to zoom/)).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Draw an area' }))
    expect(screen.getByTestId('canvas')).toHaveAttribute('data-mode', 'roi')
    expect(screen.getByText(/Drag out a region copper may be added in/)).toBeInTheDocument()
    // And back, so the board can be panned again without a reload.
    expect(screen.getByRole('button', { name: 'Done drawing' })).toBeInTheDocument()
  })

  it('adds what was dragged out to the areas already there', async () => {
    const onAreasChange = vi.fn()
    preview({ areas: [area({ min_x: 50, max_x: 60 })], onAreasChange })

    await userEvent.click(await screen.findByRole('button', { name: 'drag out a region' }))
    expect(onAreasChange).toHaveBeenCalledWith([
      area({ min_x: 50, max_x: 60 }),
      { min_x: 10, min_y: 20, max_x: 30, max_y: 45 },
    ])
  })

  // A stray press on the board should not silently confine the tuner to a
  // region a fraction of a millimetre across.
  it('ignores a press too small to be a region', async () => {
    const onAreasChange = vi.fn()
    preview({ areas: [], onAreasChange })

    await userEvent.click(await screen.findByRole('button', { name: 'click without dragging' }))
    expect(onAreasChange).not.toHaveBeenCalled()
  })

  it('offers drawing and suggesting only when the caller can accept them', async () => {
    preview({ areas: [] })
    await screen.findByTestId('canvas')
    expect(screen.queryByRole('button', { name: 'Draw an area' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Suggest areas' })).not.toBeInTheDocument()
  })

  it('asks for suggestions when asked to', async () => {
    const onSuggest = vi.fn()
    preview({ areas: [], onAreasChange: vi.fn(), onSuggest })
    await userEvent.click(await screen.findByRole('button', { name: 'Suggest areas' }))
    expect(onSuggest).toHaveBeenCalled()
  })
})

describe('BoardPreview overlay', () => {
  // Drawing a region you cannot then see is not a feature: you put three down
  // and the board looks exactly as it did.
  it('draws a numbered box for each area once the view is known', async () => {
    preview({ areas: [area(), area({ min_x: 50, max_x: 60 })], onAreasChange: vi.fn() })

    await userEvent.click(await screen.findByRole('button', { name: 'canvas ready' }))
    // The renderer's view arrives on an animation frame rather than a render.
    await waitFor(() => expect(screen.getByText('1')).toBeInTheDocument())
    expect(screen.getByText('2')).toBeInTheDocument()

    // Board millimetres, scaled and flipped: y counts up from the bottom.
    const box = screen.getByText('1').parentElement!
    expect(box.style.left).toBe('100px')
    expect(box.style.width).toBe('200px')
    expect(box.style.bottom).toBe('200px')
    expect(box.style.height).toBe('250px')
  })
})

describe('BoardPreview when the board will not draw', () => {
  it('says so rather than showing an empty frame', async () => {
    const api = apiStub({
      preview: vi.fn().mockRejectedValue(new Error('geometry.bin arrived 0 bytes long')),
    } as unknown as Partial<AnalyzerApi>)
    renderUI(<BoardPreview api={api} sessionId="s1" nets={[]} hasResult={false} />)

    expect(await screen.findByText('Could not draw the board')).toBeInTheDocument()
    expect(screen.getByText(/geometry.bin arrived 0 bytes long/)).toBeInTheDocument()
  })

  it('offers the After view only once there is a result', async () => {
    const { unmount } = preview({ hasResult: false })
    await screen.findByTestId('canvas')
    expect(screen.getByRole('radio', { name: 'After' })).toBeDisabled()
    unmount()

    preview({ hasResult: true })
    await screen.findByTestId('canvas')
    expect(screen.getByRole('radio', { name: 'After' })).toBeEnabled()
  })
})
