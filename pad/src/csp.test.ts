import { expect, test } from 'bun:test'
import { mkdtemp, readFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { build } from 'vite'

const padRoot = join(import.meta.dir, '..')

function assertExternalScriptsOnly(html: string, label: string) {
  const tags = [...html.matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script>/gi)]
  expect(tags.length, label).toBeGreaterThan(0)
  for (const [, attrs, body] of tags) {
    expect(attrs, label).toMatch(/\ssrc\s*=\s*["'][^"']+["']/i)
    expect(attrs, label).not.toMatch(/javascript:/i)
    expect(body.trim(), `${label} inline body`).toBe('')
  }
  expect(html, label).not.toMatch(/\son\w+\s*=/i)
}

test('pad source shell has no inline scripts', async () => {
  assertExternalScriptsOnly(await readFile(join(padRoot, 'index.html'), 'utf8'), 'pad/index.html')
})

// Builds into a temp directory so this does not depend on the gitignored
// server/public/pad output, and so `bun test` is enough on a clean checkout
// (after `bun install`). Vite's modulepreload polyfill is an inline script;
// vite.config.ts turns it off, and this asserts the built HTML stays clean.
test('vite build has no inline scripts', async () => {
  const outDir = await mkdtemp(join(tmpdir(), 'pad-csp-'))
  try {
    await build({
      root: padRoot,
      configFile: join(padRoot, 'vite.config.ts'),
      build: { outDir, emptyOutDir: true },
      logLevel: 'silent',
    })
    const html = await readFile(join(outDir, 'index.html'), 'utf8')
    assertExternalScriptsOnly(html, 'vite build index.html')
    expect(html).toContain('src="/pad/')
  } finally {
    await rm(outDir, { recursive: true, force: true })
  }
})
