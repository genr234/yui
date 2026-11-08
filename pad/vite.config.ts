import { defineConfig } from 'vite'
import { svelte, vitePreprocess } from '@sveltejs/vite-plugin-svelte'

export default defineConfig({
  base: '/pad/',
  plugins: [svelte({ preprocess: vitePreprocess() })],
  server: {
    proxy: {
      '/play': 'http://localhost:3000',
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
