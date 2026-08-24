import { defineConfig } from 'vite'
import { svelte, vitePreprocess } from '@sveltejs/vite-plugin-svelte'
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = fileURLToPath(new URL('.', import.meta.url))

export default defineConfig(({ mode }) => ({
  base: mode === 'demo' ? './' : '/',
  plugins: [svelte({ preprocess: vitePreprocess() })],
  server: {
    cors: true,
    host: '127.0.0.1',
  },
  optimizeDeps: {
    exclude: ['lucide-svelte', '@lucide/svelte'],
  },
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
    },
  },
  build: {
    outDir: mode === 'demo' ? 'demo-dist' : '../controller/static',
    emptyOutDir: mode === 'demo',
    ...(mode === 'demo'
      ? {}
      : {
          rollupOptions: {
            input: resolve(__dirname, 'src/main.ts'),
            output: {
              format: 'iife',
              name: 'YuiPlatform',
              entryFileNames: 'platform.js',
              inlineDynamicImports: true,
            },
          },
        }),
  },
}))
