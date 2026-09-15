/**
 * The page's side of the KiCad plugin.
 *
 * The plugin serves this page from its own URL scheme and answers two kinds of
 * request on it: /api/... goes to the analyzer engine, exactly as it would on
 * the site, and /kicad/... is answered by the plugin itself, which holds the
 * connection to pcbnew. Both are ordinary fetches to the page's own origin, so
 * nothing here opens a socket or needs a bridge object injected into the page.
 */

import type { BoardHost } from '../src/lib/host'
import { tunnelled } from './fetch'

const fetchWithStatus = tunnelled()

async function call<T>(path: string, body: unknown): Promise<T> {
  const res = await fetchWithStatus(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body ?? {}),
  })
  let data: unknown = null
  try {
    data = await res.json()
  } catch {
    // An empty body is fine for a call that returns nothing.
  }
  if (!res.ok) {
    const msg = (data as { error?: string } | null)?.error
    throw new Error(msg || `${path} failed with status ${res.status}`)
  }
  return data as T
}

/** The URL that shows a session. */
export function sessionURL(id: string): string {
  return `/index.html?session=${encodeURIComponent(id)}`
}

export const kicadHost: BoardHost = {
  name: 'KiCad',
  async selectNets(nets) {
    await call('/kicad/select', { nets })
  },
  // No applyToBoard yet: writing to a live board is switched off until it has
  // been tested in KiCad. The plugin refuses /kicad/apply as well, so this is
  // not the only thing keeping it off.
  async rescan() {
    const { session } = await call<{ session: string }>('/kicad/rescan', {})
    window.location.assign(sessionURL(session))
  },
}
