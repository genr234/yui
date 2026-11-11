/*
 * Kenney Mobile Controls art (CC0, style F + glow highlights, see
 * assets/kenney/LICENSE.md), inlined so it can be recoloured with CSS.
 *
 * Each SVG is imported as text and its flat fills are swapped for classes
 * (.k-rim, .k-face, .k-mark, .k-edge, .k-shine, .k-hl, .k-hl-line, styled in
 * styles.css from the --k-* custom properties). Inline SVG needs no CSP
 * allowance, renders crisply at any size and recolours the same way on iOS
 * Safari and Android Chrome. `crop` gives a viewBox slice, which Art.svelte
 * uses to stretch a button between fixed-size corners without distorting them.
 */
const sources = import.meta.glob('../assets/kenney/controls/*.svg', { eager: true, query: '?raw', import: 'default' }) as Record<
  string,
  string
>
const glyphUrls = import.meta.glob('../assets/kenney/controls/glyph_*.svg', { eager: true, query: '?url', import: 'default' }) as Record<
  string,
  string
>

const raw: Record<string, string> = {}
for (const [path, src] of Object.entries(sources)) raw[path.replace(/^.*\/|\.svg$/g, '')] = src

export type Box = [x: number, y: number, w: number, h: number]

/** Natural size of an art file, in its own units. */
export function artSize(name: string): [number, number] {
  const m = raw[name]?.match(/width="(\d+)" height="(\d+)"/)
  return m ? [Number(m[1]), Number(m[2])] : [64, 64]
}

const CLASSES: [RegExp, string][] = [
  [/fill="url\(#[^)]*\)"/, 'k-hl'],
  [/stroke="#FFE272"/, 'k-hl-line'],
  [/stroke="#CEDDE0"/, 'k-edge'],
  [/stroke="#FFFFFF"/, 'k-shine'],
  [/fill="#F2F6F7"/, 'k-rim'],
  [/fill="#DCE2E4"/, 'k-face'],
  [/fill="#FFFFFF"/, 'k-mark'],
]

const cache = new Map<string, string>()

/** Inline SVG markup for `name`, optionally cropped to `crop` (art units). */
export function svg(name: string, crop?: Box, par = 'xMidYMid meet'): string {
  const key = `${name}|${crop?.join(',') ?? ''}|${par}`
  const hit = cache.get(key)
  if (hit) return hit
  const src = raw[name]
  if (!src) return ''
  const [w, h] = artSize(name)
  const body = (src.match(/<g>([\s\S]*)<\/g>/)?.[1] ?? '').replace(/<path\b([^>]*?)\/?>/g, (_, attrs: string) => {
    for (const [re, cls] of CLASSES) {
      if (re.test(attrs)) return `<path class="${cls}" ${attrs.replace(re, '')}/>`
    }
    return `<path ${attrs}/>`
  })
  const [x, y, cw, ch] = crop ?? [0, 0, w, h]
  const out = `<svg class="k" viewBox="${x} ${y} ${cw} ${ch}" preserveAspectRatio="${par}" focusable="false" aria-hidden="true">${body}</svg>`
  cache.set(key, out)
  return out
}

/** Which button glyph a label maps to ("A", "b", "L"...), if any. */
export function glyphFor(label: string): string | null {
  const key = `../assets/kenney/controls/glyph_${label.trim().toLowerCase()}.svg`
  return /^[abxylr]$/i.test(label.trim()) ? (glyphUrls[key] ?? null) : null
}
