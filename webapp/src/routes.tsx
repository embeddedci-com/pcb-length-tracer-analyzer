/**
 * Route fragment for the length matcher.
 *
 * The paths here are relative to wherever the host mounts them, which is what
 * lets one fragment serve two shells: embeddedci-server renders it inside a
 * `<Route path="/tools/pcb-trace-length-analyzer/*">`, and the standalone harness wraps it in a
 * `<Route path="/tools/pcb-trace-length-analyzer">`.
 *
 * It deliberately does not carry its own prefix. A nested `<Routes>` matches
 * against the part of the URL its parent has not consumed, so a fragment that
 * repeated the prefix would silently match nothing under a host that had
 * already consumed it, and render a blank page. Which prefix the tool lives at
 * is the host's decision.
 */

import { Route } from 'react-router'
import { HomePage } from './pages/HomePage'
import { BoardPage } from './pages/BoardPage'
import type { AnalyzerApi } from './lib/analyzerApi'

export function analyzerRoutes(api: AnalyzerApi) {
  return (
    <>
      <Route index element={<HomePage api={api} />} />
      <Route path=":sessionId" element={<BoardPage api={api} />} />
    </>
  )
}
