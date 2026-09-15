/**
 * Standalone harness.
 *
 * Wraps the same src/ the app will use in the minimum shell it needs, so the
 * pages can be worked on against cmd/pcb-trace-length-analyzer-server without
 * embeddedci-server in the way. It is not how the tool ships: production
 * renders this fragment inside the app's own layout and behind its own
 * authentication.
 */

import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { MantineProvider, AppShell, Container } from '@mantine/core'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Route, Routes, Navigate } from 'react-router'
import '@mantine/core/styles.css'

import { AnalyzerApi, analyzerRoutes } from '../src'

const api = new AnalyzerApi('/api')
const qc = new QueryClient({
  defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } },
})

const root = document.getElementById('root')
if (!root) throw new Error('no #root element')

createRoot(root).render(
  <StrictMode>
    <MantineProvider>
      <QueryClientProvider client={qc}>
        <BrowserRouter>
          <AppShell padding="md">
            <AppShell.Main>
              <Container size="lg">
                <Routes>
                  <Route path="/" element={<Navigate to="/tools/pcb-trace-length-analyzer" replace />} />
                  <Route path="/tools/pcb-trace-length-analyzer">{analyzerRoutes(api)}</Route>
                </Routes>
              </Container>
            </AppShell.Main>
          </AppShell>
        </BrowserRouter>
      </QueryClientProvider>
    </MantineProvider>
  </StrictMode>,
)
