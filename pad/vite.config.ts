import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

// Preprocessing lives in svelte.config.js so svelte-check uses the same setup.
export default defineConfig({
  base: '/pad/',
  plugins: [svelte()],
  server: {
    proxy: {
      '/play': 'http://localhost:3000',
      '/sim': 'http://localhost:3000',
      '/cable': { target: 'http://localhost:3000', ws: true },
    },
  },
  build: {
    outDir: '../server/public/pad',
    emptyOutDir: true,
    // The modulepreload polyfill is an inline script. /play is served with a
    // nonce script-src and no 'unsafe-inline', so that polyfill would not run.
    // The pad's browsers support <link rel="modulepreload"> natively.
    modulePreload: { polyfill: false },
  },
})
