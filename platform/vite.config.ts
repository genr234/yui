import { defineConfig } from 'vite'
import { svelte, vitePreprocess } from '@sveltejs/vite-plugin-svelte'
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { yuiAppWeb } from './vite-app-web'

const __dirname = fileURLToPath(new URL('.', import.meta.url))

export default defineConfig(({ mode }) => ({
  base: mode === 'demo' ? './' : '/',
  plugins: [
    svelte({ preprocess: vitePreprocess() }),
    yuiAppWeb({
      appsRoot: resolve(__dirname, '../apps'),
      embedJs: resolve(__dirname, '../controller/internal/webapps/embed.js'),
      demoRoot: __dirname,
    }),
  ],
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
    // The overlay hands these to the page as bytes, so they must be inlined
    // into platform.js: a load from the controller could be blocked by the
    // host page's Content-Security-Policy.
    assetsInlineLimit: (file: string) =>
      /[\\/]avatar[\\/]src[\\/]atlas\.png$|[\\/]AvenirNext\.woff2$/.test(file) ? true : undefined,
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
