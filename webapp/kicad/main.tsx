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

import { StrictMode, useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { Alert, Anchor, AppShell, Button, Container, MantineProvider, Stack, Text } from '@mantine/core'
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

const query = new URLSearchParams(window.location.search)
const session = query.get('session') ?? ''
// The plugin's version, passed by the window, for the footer.
const version = query.get('v') ?? ''
const BASE = '/tools/pcb-trace-length-analyzer'

/**
 * Which build this is: the plugin's version, and the engine's build when it
 * says something the version does not (a development build names its commit).
 */
function Version() {
  const [engine, setEngine] = useState('')
  useEffect(() => {
    void tunnelled()('/api/health')
      .then((r) => r.json())
      .then((h: { version?: string }) => setEngine(h.version ?? ''))
      .catch(() => setEngine(''))
  }, [])
  const build = engine && engine !== version ? ` (engine ${engine})` : ''
  return (
    <>
      Version {version || 'unknown'}
      {build}
    </>
  )
}

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
                  <Text size="xs" c="dimmed" ta="center" py="lg">
                    PCB Trace Length Analyzer · <Version /> · plugin by{' '}
                    {/* Opened in the system browser: the plugin's page does not navigate away. */}
                    <Anchor size="xs" href="https://embeddedci.com/tools/pcb-trace-length-analyzer">
                      embeddedci.com
                    </Anchor>
                  </Text>
                </Container>
              </AppShell.Main>
            </AppShell>
          </MemoryRouter>
        </HostProvider>
      </QueryClientProvider>
    </MantineProvider>
  </StrictMode>,
)
