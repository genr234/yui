import { expect, test } from 'bun:test'
import { existsSync, readFileSync } from 'node:fs'
import { join } from 'node:path'

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

test('pad source shell has no inline scripts', () => {
  assertExternalScriptsOnly(readFileSync(join(padRoot, 'index.html'), 'utf8'), 'pad/index.html')
})

test('vite build has no inline scripts', () => {
  const built = join(padRoot, '..', 'server', 'public', 'pad', 'index.html')
  expect(existsSync(built), 'run just build-pad before this test').toBe(true)
  const html = readFileSync(built, 'utf8')
  assertExternalScriptsOnly(html, 'public/pad/index.html')
  expect(html).toContain('src="/pad/')
})
