import path from 'node:path'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The bundle the KiCad plugin serves into its own window.
//
// Same src/ as the site, wrapped in kicad/main.tsx instead of dev/main.tsx. It
// is served by the plugin's in-process scheme handler at kicad-engine://app/,
// so the page and its API share one origin and nothing goes over a socket.
//
//   npm run build:kicad   -> ../kicad-plugin/web
const emiSrc = path.resolve(import.meta.dirname, '../../emi-analyzer/webapp/src')

export default defineConfig({
  base: '/',
  plugins: [react()],
  resolve: {
    alias: { '@emi': emiSrc },
    dedupe: ['react', 'react-dom', 'react-router', '@mantine/core', '@mantine/hooks', '@tanstack/react-query'],
  },
  build: {
    // The bundle carries React, Mantine and the rest; their licences travel
    // with the plugin in this file.
    license: { fileName: 'THIRD-PARTY-LICENSES.md' },
    outDir: path.resolve(import.meta.dirname, '../kicad-plugin/web'),
    emptyOutDir: true,
    rollupOptions: { input: path.resolve(import.meta.dirname, 'kicad.html') },
  },
})
