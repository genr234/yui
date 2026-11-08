import { writable } from 'svelte/store'

/**
 * The kiosk's audio. Every peer carries an Opus track; the kiosk only writes
 * into the speaker phone's, so the element is silent everywhere else. The
 * kiosk applies the volume itself, so the element stays at full volume.
 *
 * iOS (and some Android browsers) refuse to start playback without a user
 * gesture: `blocked` is true until a play() succeeds, and the pad shows
 * "Tap to enable sound" while this phone is the speaker.
 */
export const speakerBlocked = writable(false)

let el: HTMLAudioElement | null = null

function element(): HTMLAudioElement {
  if (el) return el
  el = document.createElement('audio')
  el.autoplay = true
  el.setAttribute('playsinline', '')
  el.hidden = true
  document.body.appendChild(el)
  return el
}

export function attachSpeakerStream(stream: MediaStream) {
  const a = element()
  if (a.srcObject !== stream) a.srcObject = stream
  void playSpeaker()
}

/** Start playback; call it from a tap when `speakerBlocked` is true. */
export async function playSpeaker(): Promise<boolean> {
  const a = element()
  if (!a.srcObject) return false
  try {
    await a.play()
    speakerBlocked.set(false)
    return true
  } catch {
    speakerBlocked.set(true)
    return false
  }
}
