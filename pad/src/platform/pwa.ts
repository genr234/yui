import { writable, type Readable } from 'svelte/store'

/**
 * Installing the pad as an app (PWA). Installed, it opens at /play/ full
 * screen with no browser bars, and a phone held like a controller is not
 * one stray swipe away from the address bar or a tab switch.
 */

interface InstallPromptEvent extends Event {
  prompt(): Promise<void>
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>
}

export type InstallState =
  /** Already running as an installed app. */
  | 'installed'
  /** Android/desktop Chrome offered an install prompt we can show. */
  | 'prompt'
  /** iOS Safari: no prompt exists; the player adds it from the Share menu. */
  | 'ios'
  /** Nothing to offer (unsupported browser, or the prompt was used). */
  | 'none'

export function isStandalone(win: Window = window): boolean {
  const nav = win.navigator as Navigator & { standalone?: boolean }
  return (
    nav.standalone === true ||
    ['standalone', 'fullscreen', 'minimal-ui'].some((m) => win.matchMedia?.(`(display-mode: ${m})`).matches)
  )
}

/** iPhone/iPad Safari, including iPadOS that reports itself as a Mac. */
export function isIOS(nav: Pick<Navigator, 'userAgent' | 'maxTouchPoints'> = navigator): boolean {
  return /iPad|iPhone|iPod/.test(nav.userAgent) || (/Macintosh/.test(nav.userAgent) && nav.maxTouchPoints > 1)
}

let deferred: InstallPromptEvent | null = null
const state = writable<InstallState>('none')
export const installState: Readable<InstallState> = { subscribe: state.subscribe }

/** Call once at startup, before the browser fires `beforeinstallprompt`. */
export function watchInstall(win: Window = window) {
  if (isStandalone(win)) return state.set('installed')
  if (isIOS(win.navigator)) state.set('ios')
  win.addEventListener('beforeinstallprompt', (e) => {
    e.preventDefault() // keep it for our own button
    deferred = e as InstallPromptEvent
    state.set('prompt')
  })
  win.addEventListener('appinstalled', () => {
    deferred = null
    state.set('installed')
  })
}

/** Shows the browser's install dialog. Returns true when the player accepted. */
export async function promptInstall(): Promise<boolean> {
  const e = deferred
  if (!e) return false
  deferred = null
  state.set('none')
  await e.prompt()
  return (await e.userChoice).outcome === 'accepted'
}

// ------------------------------------------------------------------ joining

const LAST_CODE_KEY = 'yui_pad_last_code'
const CODE = /^[A-Za-z]{4}$/

export function rememberCode(code: string) {
  try {
    localStorage.setItem(LAST_CODE_KEY, code.toUpperCase())
  } catch {
    /* storage unavailable */
  }
}

export function lastCode(): string | null {
  try {
    const v = localStorage.getItem(LAST_CODE_KEY)
    return v && CODE.test(v) ? v : null
  } catch {
    return null
  }
}

/** Four letters, uppercased, or null. */
export function cleanCode(text: string): string | null {
  const v = text.replace(/[^A-Za-z]/g, '').toUpperCase()
  return CODE.test(v) ? v : null
}

/**
 * The code a scanned QR code (or pasted text) points at: a same-origin
 * /play/<CODE> link (its `?t=` is dropped: the code alone is enough to join)
 * or a bare code. Anything else (another site, a Wi-Fi code) is null.
 */
export function joinTarget(text: string, origin: string = location.origin): string | null {
  const t = text.trim()
  if (CODE.test(t)) return t.toUpperCase()
  let url: URL
  try {
    url = new URL(t)
  } catch {
    return null
  }
  if (url.origin !== origin) return null
  const m = url.pathname.match(/^\/play\/([A-Za-z]{4})\/?$/)
  return m ? m[1].toUpperCase() : null
}

/** Forgets the remembered code (its game is over), so Rejoin stops offering it. */
export function forgetCode(code: string) {
  try {
    if (localStorage.getItem(LAST_CODE_KEY) === code.toUpperCase()) localStorage.removeItem(LAST_CODE_KEY)
  } catch {
    /* storage unavailable */
  }
}

/** The browser can read QR codes from the camera itself (Android Chrome; not iOS Safari). */
export function canScan(win: Window = window): boolean {
  return 'BarcodeDetector' in win && !!win.navigator.mediaDevices?.getUserMedia
}
