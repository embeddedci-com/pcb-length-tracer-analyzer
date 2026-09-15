/**
 * The analyzer inside KiCad.
 *
 * The same pages as the site, in the plugin's window. The differences are all
 * in this file: the API lives at the page's own origin (the plugin's scheme
 * handler), the board is never uploaded by hand -- the plugin reads it out of
 * pcbnew and names the session in the URL -- and a host is provided, which is
 * what turns on selecting nets and applying to the open board.
 *
 * A memory router rather than a browser one: the page is loaded fresh for each
 * session, and there is no address bar to keep in step.
 */

import { StrictMode, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { Alert, AppShell, Button, Container, MantineProvider, Stack, Text } from '@mantine/core'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router'
import '@mantine/core/styles.css'

import { AnalyzerApi, BoardPage, HostProvider } from '../src'
import { kicadHost } from './kicadHost'
import { tunnelled } from './fetch'

const api = new AnalyzerApi('/api', tunnelled())
const qc = new QueryClient({
  defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } },
})

const session = new URLSearchParams(window.location.search).get('session') ?? ''
const BASE = '/tools/pcb-trace-length-analyzer'

/** Shown when there is no session to show, or it has gone. */
function NoBoard() {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  return (
    <Stack gap="md" p="xl">
      <Text>No board has been read yet.</Text>
      <Button
        w="fit-content"
        loading={busy}
        onClick={() => {
          setBusy(true)
          setError(null)
          kicadHost
            .rescan()
            .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
            .finally(() => setBusy(false))
        }}
      >
        Read the board from KiCad
      </Button>
      {error && (
        <Alert color="red" variant="light" title="Could not read the board">
          {error}
        </Alert>
      )}
    </Stack>
  )
}

const root = document.getElementById('root')
if (!root) throw new Error('no #root element')

createRoot(root).render(
  <StrictMode>
    <MantineProvider>
      <QueryClientProvider client={qc}>
        <HostProvider host={kicadHost}>
          <MemoryRouter initialEntries={[session ? `${BASE}/${session}` : BASE]}>
            <AppShell padding="md">
              <AppShell.Main>
                <Container size="lg">
                  <Routes>
                    <Route path={BASE}>
                      <Route index element={<NoBoard />} />
                      <Route path=":sessionId" element={<BoardPage api={api} />} />
                    </Route>
                    <Route path="*" element={<NoBoard />} />
                  </Routes>
                </Container>
              </AppShell.Main>
            </AppShell>
          </MemoryRouter>
        </HostProvider>
      </QueryClientProvider>
    </MantineProvider>
  </StrictMode>,
)
