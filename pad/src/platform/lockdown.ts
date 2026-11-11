/**
 * Keeps the browser's own page gestures out of the way. The pad is a controller,
 * not a document: pinch and double-tap must never zoom it, and while a session
 * is open the page must never scroll, bounce or resize under the player's thumbs.
 *
 * - iOS Safari ignores `user-scalable=no`, so pinch is stopped through its
 *   `gesture*` events, and double-tap zoom by `touch-action: manipulation` on
 *   <html> (styles.css) plus a `dblclick` guard.
 * - Android Chrome honours the viewport meta, but a two-finger move outside a
 *   control is still cancelled here in case the page is ever zoomed.
 * - `lockPage()` pins the document while the pad is up (html.pad-locked).
 */

const stop = (e: Event) => e.preventDefault()

function multiTouch(e: TouchEvent) {
  if (e.touches.length > 1) e.preventDefault()
}

let installed = false

/** Called once at startup. */
export function installGestureGuards(doc: Document = document) {
  if (installed) return
  installed = true
  const opts = { passive: false } as AddEventListenerOptions
  for (const name of ['gesturestart', 'gesturechange', 'gestureend']) doc.addEventListener(name, stop, opts)
  doc.addEventListener('dblclick', stop, opts)
  doc.addEventListener('touchmove', multiTouch, opts)
}

/** Pins the page while the pad is mounted; returns the unlock. */
export function lockPage(doc: Document = document): () => void {
  const root = doc.documentElement
  root.classList.add('pad-locked')
  window.scrollTo(0, 0)
  return () => root.classList.remove('pad-locked')
}
