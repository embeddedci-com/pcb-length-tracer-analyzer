import { describe, expect, it, vi } from 'vitest'
import userEvent from '@testing-library/user-event'
import { RouteResult } from './RouteResult'
import { renderUI, screen } from '../testRender'
import type { RouteResponse } from '../lib/analyzerApi'

// The part of the tool most likely to disappoint, so the thing being checked
// is that it disappoints honestly: every refusal visible with its reason,
// every claim of success matched by a download, and no cheerful summary
// standing in for either.

function result(over: Partial<RouteResponse> = {}): RouteResponse {
  return {
    requested: 2,
    connected: 0,
    added_mm: 0,
    vias: 0,
    changed: false,
    hops: [
      {
        net: '/ddr4/DDR_CKE',
        label: 'CKE',
        from: 'U4.K2',
        to: 'U5.K2',
        routed: false,
        reason: 'no way through: the corridor between these pads is full of copper',
      },
    ],
    notes: ['nothing was written'],
    ...over,
  }
}

describe('RouteResult', () => {
  it('shows every refusal with the reason it failed', () => {
    renderUI(<RouteResult result={result()} onDownload={vi.fn()} />)
    expect(screen.getByText('0 of 2')).toBeInTheDocument()
    expect(screen.getByText(/no way through/)).toBeInTheDocument()
    expect(screen.getByText('nothing was written.')).toBeInTheDocument()
  })

  // A run that changed nothing must not offer a download: there is nothing
  // behind it, and "check it in KiCad" would send somebody looking for a file
  // that does not exist.
  it('offers nothing to download when nothing was written', () => {
    renderUI(<RouteResult result={result()} onDownload={vi.fn()} />)
    expect(screen.queryByRole('button', { name: 'Download it' })).not.toBeInTheDocument()
    expect(screen.queryByText('Made')).not.toBeInTheDocument()
  })

  it('lists what it made, and says the board has changed under you', async () => {
    const onDownload = vi.fn()
    renderUI(
      <RouteResult
        onDownload={onDownload}
        result={result({
          connected: 1,
          added_mm: 37.896,
          vias: 1,
          changed: true,
          hops: [
            {
              net: '/ddr4/DDR_A11',
              label: 'A11',
              from: 'U5.T2',
              to: 'R31.1',
              routed: true,
              length_mm: 37.896,
              vias: 1,
            },
          ],
          notes: ['the board in this session is now the routed one'],
        })}
      />,
    )
    expect(screen.getByText('Made')).toBeInTheDocument()
    expect(screen.getByText('37.896 mm')).toBeInTheDocument()
    expect(screen.getByText('U5.T2')).toBeInTheDocument()
    expect(screen.getByText('Check it before you trust it')).toBeInTheDocument()
    // A button that fetches with the host's credentials, not a bare link that
    // would carry none.
    await userEvent.click(screen.getByRole('button', { name: 'Download it' }))
    expect(onDownload).toHaveBeenCalled()
  })

  it('can be dismissed once it has been read', async () => {
    const onDismiss = vi.fn()
    renderUI(<RouteResult result={result()} onDownload={vi.fn()} onDismiss={onDismiss} />)
    await userEvent.click(screen.getByRole('button', { name: 'Dismiss' }))
    expect(onDismiss).toHaveBeenCalled()
  })
})
