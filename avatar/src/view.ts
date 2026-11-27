// Shows any avatar inside a host element and keeps a creature's eyes alive.
// Every surface (pad, overlay, app pages) renders avatars through this.
import { drawCreature, eyeMoves } from './draw'
import { parseAvatar, type Avatar, type Creature } from './format'
import type { View } from './layout'
import { atRest, joltPupil, makePupil, restPupil, stepPupil, type Pupil } from './physics'

export interface AvatarProps {
  /** The player's avatar string, or null. */
  avatar: string | null | undefined
  /** The player's colour as a CSS colour. */
  color?: string
  /** `face` (default) for chips, `full` for the whole creature. */
  view?: View
  /** Shown when there is no avatar, e.g. the player's initial. */
  fallback?: string
  /** Keep the eyes still: for rows of small previews where movement would distract. */
  still?: boolean
}

export interface AvatarHandle {
  update(props: AvatarProps): void
  /** Pushes the pupils, in eye units per second. */
  jolt(vx: number, vy: number): void
  destroy(): void
}

const DEFAULT_COLOR = '#9fb1c4'

// ---- shared state: atlas, gravity, one animation loop ----------------------

type AtlasSource = { atlasUrl: string } | { atlasData: Blob }

let atlasKey: string | Blob | null = null
let atlas: CanvasImageSource | null = null
let atlasLoading = false
let atlasReady = false

/**
 * Tells the renderer where to get the sprite atlas (src/atlas.png). Call once
 * at startup. `atlasData` hands it the bytes themselves, which no page's
 * Content-Security-Policy can block: the kiosk overlay runs inside other
 * people's pages and uses that.
 */
export function configureAvatars(options: AtlasSource): void {
  const key = 'atlasData' in options ? options.atlasData : options.atlasUrl
  if (key === atlasKey) return
  atlasKey = key
  atlas = null
  atlasLoading = false
  atlasReady = false
}

function loadAtlas(): void {
  if (atlas || atlasLoading || !atlasKey) return
  atlasLoading = true
  const key = atlasKey
  loadImage(key).then(
    (image) => {
      if (key !== atlasKey) return
      atlas = image
      atlasReady = true
      for (const view of views) view.wake()
    },
    () => {
      if (key === atlasKey) atlasLoading = false
    },
  )
}

/** The bytes of a base64 `data:` URL, or null for anything else. */
export function dataUrlBlob(url: string): Blob | null {
  const match = /^data:([^;,]+);base64,(.*)$/s.exec(url)
  if (!match) return null
  try {
    const binary = atob(match[2])
    const bytes = new Uint8Array(binary.length)
    for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
    return new Blob([bytes], { type: match[1] })
  } catch {
    return null
  }
}

/**
 * Decodes an image. Bytes and `data:` URLs go through createImageBitmap,
 * which a page's Content-Security-Policy does not govern (an <img> would be
 * held to its img-src). Anything else loads as a normal image.
 */
function loadImage(source: string | Blob): Promise<CanvasImageSource> {
  const blob = typeof source === 'string' ? dataUrlBlob(source) : source
  if (blob && typeof createImageBitmap === 'function') return createImageBitmap(blob)
  return new Promise((resolve, reject) => {
    const img = new Image()
    img.decoding = 'async'
    img.onload = () => resolve(img)
    img.onerror = reject
    img.src = typeof source === 'string' ? source : URL.createObjectURL(source)
  })
}

/** Draws a selfie into a round canvas, cropped to fill it like object-fit: cover. */
function selfieCanvas(url: string): HTMLCanvasElement {
  const canvas = document.createElement('canvas')
  canvas.style.cssText = 'display:block;width:100%;height:100%;border-radius:50%'
  loadImage(url).then(
    (image) => {
      const { width: w, height: h } = image as { width: number; height: number }
      const side = Math.min(w, h)
      if (!side) return
      canvas.width = side
      canvas.height = side
      canvas.getContext('2d')?.drawImage(image, (w - side) / 2, (h - side) / 2, side, side, 0, 0, side, side)
    },
    () => {},
  )
  return canvas
}

let gravityX = 0
let gravityY = 1

/**
 * Sets which way is down for every avatar on the page, in g (+x right, +y
 * down). A phone feeds its tilt in here; a TV leaves it at (0, 1).
 */
export function setAvatarGravity(x: number, y: number): void {
  const moved = Math.abs(x - gravityX) + Math.abs(y - gravityY) > 0.03
  gravityX = x
  gravityY = y
  if (moved) for (const view of views) view.wake()
}

/** Pushes every avatar's pupils at once (a shake, a big moment in a game). */
export function joltAvatars(vx: number, vy: number): void {
  for (const view of views) view.jolt(vx, vy)
}

const reducedMotion = () => typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches

class CreatureView {
  readonly canvas = document.createElement('canvas')
  private ctx = this.canvas.getContext('2d')
  private pupils: Pupil[] = []
  private photo: CanvasImageSource | null = null
  private photoSrc: string | null = null
  private awake = false
  private idle: ReturnType<typeof setTimeout> | null = null
  private pixels = 0

  constructor(
    private creature: Creature,
    private color: string,
    private view: View,
    private still: boolean,
  ) {
    this.canvas.style.cssText = 'display:block;width:100%;height:100%'
    this.set(creature, color, view, still)
    this.scheduleIdle()
  }

  set(creature: Creature, color: string, view: View, still: boolean): void {
    this.creature = creature
    this.color = color
    this.view = view
    this.still = still
    while (this.pupils.length < creature.eyes.length) {
      const p = makePupil(this.pupils.length)
      restPupil(p, gravityX, gravityY)
      this.pupils.push(p)
    }
    this.pupils.length = creature.eyes.length
    if (creature.photo !== this.photoSrc) {
      this.photoSrc = creature.photo
      this.photo = null
      if (creature.photo) {
        const src = creature.photo
        loadImage(src).then(
          (image) => {
            if (this.photoSrc === src) {
              this.photo = image
              this.wake()
            }
          },
          () => {},
        )
      }
    }
    this.wake()
  }

  /** Matches the canvas to its box. Returns false while it has no size. */
  private fit(): boolean {
    const box = this.canvas.getBoundingClientRect()
    const side = Math.min(box.width, box.height)
    if (side < 1) return false
    const pixels = Math.round(side * Math.min(3, window.devicePixelRatio || 1))
    if (pixels !== this.pixels) {
      this.pixels = pixels
      this.canvas.width = pixels
      this.canvas.height = pixels
    }
    return true
  }

  wake(): void {
    if (this.awake) return
    this.awake = true
    startLoop()
  }

  jolt(vx: number, vy: number): void {
    if (this.still || reducedMotion() || !eyeMoves(this.creature.eyeStyle)) return
    for (const p of this.pupils) joltPupil(p, vx, vy)
    this.wake()
  }

  /** One frame. Returns false when the view can stop until something wakes it. */
  frame(dt: number): boolean {
    if (!this.awake) return false
    if (!this.ctx || !atlasReady || !atlas || !this.canvas.isConnected) {
      this.awake = false
      return false
    }
    if (!this.fit()) return true // not laid out yet; try again next frame
    const still = this.still || reducedMotion() || !eyeMoves(this.creature.eyeStyle)
    let moving = false
    for (const p of this.pupils) {
      if (still) restPupil(p, this.still ? 0 : gravityX, this.still ? 1 : gravityY)
      else {
        stepPupil(p, dt, gravityX, gravityY)
        if (!atRest(p, gravityX, gravityY)) moving = true
      }
    }
    this.ctx.clearRect(0, 0, this.pixels, this.pixels)
    drawCreature(this.ctx, this.creature, {
      atlas,
      color: this.color,
      view: this.view,
      size: this.pixels,
      pupils: this.pupils,
      photo: this.photo,
    })
    this.awake = moving
    return moving
  }

  /** Now and then a tiny nudge, so a resting creature still feels alive. */
  private scheduleIdle(): void {
    this.idle = setTimeout(
      () => {
        if (this.canvas.isConnected && !document.hidden) {
          const angle = Math.random() * Math.PI * 2
          const push = 1.2 + Math.random() * 1.6
          this.jolt(Math.cos(angle) * push, -Math.abs(Math.sin(angle)) * push)
        }
        this.scheduleIdle()
      },
      6000 + Math.random() * 9000,
    )
  }

  destroy(): void {
    if (this.idle) clearTimeout(this.idle)
    this.awake = false
  }
}

const views = new Set<CreatureView>()
let frameId = 0
let lastTime = 0

function startLoop(): void {
  if (frameId) return
  lastTime = 0
  frameId = requestAnimationFrame(tick)
}

function tick(now: number): void {
  const dt = lastTime ? Math.min(0.05, (now - lastTime) / 1000) : 1 / 60
  lastTime = now
  let busy = false
  for (const view of views) if (view.frame(dt)) busy = true
  frameId = busy ? requestAnimationFrame(tick) : 0
}

// ---- mounting ---------------------------------------------------------------

/**
 * Renders an avatar into `host`, replacing its content. The host decides the
 * size: give it a width and height (a square works best).
 */
export function mountAvatar(host: HTMLElement, props: AvatarProps): AvatarHandle {
  let creatureView: CreatureView | null = null
  let shown = ''
  let resize: ResizeObserver | null = null

  function clear(): void {
    if (creatureView) {
      views.delete(creatureView)
      creatureView.destroy()
      creatureView = null
    }
    host.replaceChildren()
  }

  function render(next: AvatarProps): void {
    const parsed: Avatar = parseAvatar(next.avatar)
    const color = next.color || DEFAULT_COLOR
    const view = next.view ?? 'face'
    host.dataset.avatarKind = parsed.kind

    if (parsed.kind === 'creature') {
      loadAtlas()
      if (creatureView) creatureView.set(parsed.creature, color, view, next.still === true)
      else {
        clear()
        creatureView = new CreatureView(parsed.creature, color, view, next.still === true)
        views.add(creatureView)
        host.append(creatureView.canvas)
        creatureView.wake()
      }
      shown = ''
      return
    }

    const key = `${parsed.kind}|${parsed.kind === 'image' ? parsed.url : parsed.kind === 'emoji' ? parsed.text : (next.fallback ?? '')}`
    if (key === shown && !creatureView) return
    clear()
    shown = key
    if (parsed.kind === 'image') {
      host.append(selfieCanvas(parsed.url))
    } else {
      // Text only, never HTML.
      const text = parsed.kind === 'emoji' ? parsed.text : (next.fallback ?? '')
      if (text) {
        const span = document.createElement('span')
        span.textContent = text
        host.append(span)
      }
    }
  }

  render(props)
  if (typeof ResizeObserver === 'function') {
    resize = new ResizeObserver(() => creatureView?.wake())
    resize.observe(host)
  }

  return {
    update: render,
    jolt(vx, vy) {
      creatureView?.jolt(vx, vy)
    },
    destroy() {
      resize?.disconnect()
      clear()
    },
  }
}
