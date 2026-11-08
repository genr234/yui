import { Controller } from "@hotwired/stimulus"

// Reloads the asset list Turbo Frame every few seconds while the server says something
// is still moving (upload, processing, kiosk sync), and right away when an upload in
// this tab makes progress. Browsers don't use ActionCable here; it's kiosk-only.
export default class extends Controller {
  static values = { url: String, interval: { type: Number, default: 5000 } }

  connect() {
    this.schedule()
  }

  disconnect() {
    clearTimeout(this.timer)
  }

  refresh() {
    const frame = this.frame
    if (!frame) return

    clearTimeout(this.timer)
    if (frame.getAttribute("src")) {
      frame.reload()
    } else {
      frame.src = this.urlValue
    }
  }

  // turbo:frame-load
  loaded() {
    this.schedule()
  }

  schedule() {
    clearTimeout(this.timer)
    if (this.active) this.timer = setTimeout(() => this.refresh(), this.intervalValue)
  }

  get frame() {
    return this.element.querySelector("turbo-frame#asset_library")
  }

  get active() {
    return this.frame?.querySelector("[data-asset-library-active='true']") != null
  }
}
