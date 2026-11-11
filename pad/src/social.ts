import { EMOTES, type EmoteId, type Peer } from '@yui/protocol'

/** One reaction per 700 ms per player; the controller drops anything faster. */
export const REACT_COOLDOWN_MS = 700
/** One poke per 3 s per sender (and per target). */
export const POKE_COOLDOWN_MS = 3000
/** How long the "poked you" toast stays up. */
export const TOAST_MS = 2000

const POKES_KEY = 'yui_pad_allow_pokes'

/** Whether other players may poke this phone. Default on. */
export function loadPokes(): boolean {
  try {
    return localStorage.getItem(POKES_KEY) !== 'off'
  } catch {
    return true
  }
}

export function savePokes(on: boolean): void {
  try {
    localStorage.setItem(POKES_KEY, on ? 'on' : 'off')
  } catch {
    /* storage unavailable */
  }
}

export { EMOTES }

/** Words for screen readers. */
export const EMOTE_LABELS: Record<EmoteId, string> = {
  heart: 'Heart',
  hearts: 'Hearts',
  laugh: 'Laughing',
  faceHappy: 'Happy',
  faceSad: 'Sad',
  faceAngry: 'Angry',
  exclamation: 'Exclamation',
  exclamations: 'Two exclamations',
  question: 'Question',
  star: 'Star',
  stars: 'Stars',
  idea: 'Idea',
  music: 'Music',
  sleeps: 'Sleepy',
  anger: 'Fuming',
  heartBroken: 'Broken heart',
}

/** "Bo poked you"; falls back to "Someone" when the poker isn't in the roster. */
export function pokedText(peers: Peer[], from: string): string {
  const name = peers.find((p) => p.id === from)?.name
  return `${name || 'Someone'} poked you`
}
