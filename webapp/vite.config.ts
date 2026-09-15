import path from 'node:path'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The board viewer comes from the EMI Analyzer's frontend, which lives in its
// own repository next to this one and is linked in by path rather than copied.
// Same arrangement embeddedci-server already uses for it, and the same alias
// name, so this source folder drops in there unchanged.
//
// It adds no dependency of its own -- the renderer is framework-free WebGL2 and
// the two components around it use Mantine, which this app already has.
const emiSrc = path.resolve(import.meta.dirname, '../../emi-analyzer/webapp/src')

// Two consumers, one config.
//
//   npm run dev    -- the local harness on :5175, proxying /api to the standalone
//                     control plane from cmd/pcb-trace-length-analyzer-server.
//   npm run build  -- a bundle for looking at the tool on its own.
//
// `base` has to match the path the app is served under, because Vite bakes it into the
// asset URLs in index.html. Served at a sub-path with base "/", every bundle request goes
// to /assets/..., which on the real site is embeddedci-server's own asset route: the app
// loads a blank page and the network tab shows nothing but 200s.
//
// This is not how the finished integration ships. That build happens inside
// embeddedci-server's own webapp, from this same src/.
export default defineConfig({
  base: process.env.AUTOROUTE_BASE || '/',
  plugins: [react()],
  resolve: {
    alias: { '@emi': emiSrc },
    // The EMI source resolves its bare imports against its own node_modules,
    // which holds a second copy of React and Mantine at slightly different
    // versions. Two Reacts breaks hooks and two Mantines gives a provider in
    // one instance and a consumer looking at the other -- at runtime, on a page
    // that built and tested perfectly well. embeddedci-server guards this with
    // a resolver plugin; here the dedupe list is enough, because this harness
    // is only ever used by hand.
    dedupe: ['react', 'react-dom', 'react-router', '@mantine/core', '@mantine/hooks', '@tanstack/react-query'],
  },
  server: {
    // Whichever port is free; the standalone control plane reflects back any
    // loopback origin rather than requiring one to be agreed in advance.
    port: Number(process.env.PORT) || 5175,
    proxy: { '/api': 'http://localhost:8091' },
    fs: { allow: ['..', emiSrc] },
  },
  test: {
    // jsdom rather than node, so the components can be rendered rather than
    // only their arithmetic tested. What that buys is the wiring: a checkbox
    // that does not reach its handler, a figure shown from the wrong field, a
    // panel that renders nothing when the data is still loading -- none of
    // which a pure function can be wrong about.
    environment: 'jsdom',
    setupFiles: ['./src/testSetup.ts'],
    include: ['src/**/*.test.ts', 'src/**/*.test.tsx', 'kicad/**/*.test.ts'],
  },
  appType: 'spa',
})
