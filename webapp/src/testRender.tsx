/**
 * Rendering a component the way the app does.
 *
 * Mantine's components read their theme from a provider, and one rendered
 * without it throws on mount. Wrapping here rather than in every test keeps the
 * tests about the component instead of about its scaffolding.
 */

import { useState } from 'react'
import type { ReactElement, ReactNode } from 'react'
import { MantineProvider } from '@mantine/core'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render } from '@testing-library/react'

function Providers({ children }: { children: ReactNode }) {
  // A client per render, so nothing a test fetched is still cached in the
  // next one, and no retries: a test that fails should fail at once rather
  // than three seconds later.
  const [client] = useState(
    () => new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } }),
  )
  // env="test" turns off Mantine's transitions. Without it a dropdown opened by
  // a click is still mid-transition -- display:none -- when the assertion runs,
  // and jsdom never advances the animation that would reveal it.
  return (
    <QueryClientProvider client={client}>
      <MantineProvider env="test">{children}</MantineProvider>
    </QueryClientProvider>
  )
}

export function renderUI(ui: ReactElement) {
  return render(ui, { wrapper: Providers })
}

export * from '@testing-library/react'
