import type { EmoteId } from '@yui/protocol'

// Vite-only (import.meta.glob), so the unit-tested helpers live in social.ts.
const emoteFiles = import.meta.glob('./assets/emotes/emote_*.png', { eager: true, query: '?url', import: 'default' }) as Record<string, string>

/** Kenney emote art by id (a data: or same-origin URL). */
export function emoteUrl(id: EmoteId): string {
  return emoteFiles[`./assets/emotes/emote_${id}.png`] ?? ''
}
