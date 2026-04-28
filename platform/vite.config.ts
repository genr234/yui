import { defineConfig } from 'vite'
import { svelte, vitePreprocess } from '@sveltejs/vite-plugin-svelte'
import { resolve } from 'node:path'

export default defineConfig({
  plugins: [svelte({ preprocess: vitePreprocess() })],
  server: {
    cors: true,
    host: '127.0.0.1',
  },
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
    },
  },
  build: {
    outDir: '../controller/static',
    emptyOutDir: false,
    rollupOptions: {
      input: resolve(__dirname, 'src/main.ts'),
      output: {
        format: 'iife',
        name: 'YuiPlatform',
        entryFileNames: 'platform.js',
        inlineDynamicImports: true,
      },
    },
  },
})
