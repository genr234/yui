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
  },
})
