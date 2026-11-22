import { writable, type Readable } from 'svelte/store'
import { ClusterDriver, type ClusterTarget } from './drivers'
import type { ControlSink } from './sink'

/**
 * The DOM side of a face-button cluster (a layout `grid` with `cluster: true`).
 * The grid takes every finger; its buttons only register where they are and
 * show `held`. The geometry is measured on each touch, so rotating the phone
 * or a new layout needs nothing special.
 */
export class ClusterHandle {
  private driver: ClusterDriver
  private buttons = new Map<string, { el: HTMLElement; bits: number }>()
  private heldStore = writable<Set<string>>(new Set())

  constructor(sink: ControlSink, id: string) {
    this.driver = new ClusterDriver(sink, id)
  }

  get held(): Readable<Set<string>> {
    return this.heldStore
  }

  /** A button joins the cluster; the returned function takes it out. */
  register(id: string, el: HTMLElement, bits: number): () => void {
    this.buttons.set(id, { el, bits })
    return () => {
      if (this.buttons.get(id)?.el === el) this.buttons.delete(id)
    }
  }

  down = (e: PointerEvent) => {
    e.preventDefault()
    try {
      ;(e.currentTarget as Element).setPointerCapture(e.pointerId)
    } catch {
      /* pointer already gone */
    }
    this.driver.setTargets(this.measure())
    this.driver.down(e.pointerId, e.clientX, e.clientY)
    this.sync()
  }

  move = (e: PointerEvent) => {
    this.driver.move(e.pointerId, e.clientX, e.clientY)
    this.sync()
  }

  up = (e: PointerEvent) => {
    this.driver.up(e.pointerId, e.type !== 'pointerup')
    this.sync()
  }

  reset() {
    this.driver.reset()
    this.sync()
  }

  private measure(): ClusterTarget[] {
    const out: ClusterTarget[] = []
    for (const [id, b] of this.buttons) {
      const r = b.el.getBoundingClientRect()
      if (!r.width || !r.height) continue
      out.push({ id, bits: b.bits, x: r.left + r.width / 2, y: r.top + r.height / 2, r: Math.min(r.width, r.height) / 2 })
    }
    return out
  }

  private sync() {
    this.heldStore.set(this.driver.held)
  }
}
