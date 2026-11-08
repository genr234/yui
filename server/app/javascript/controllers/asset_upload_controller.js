import { Controller } from "@hotwired/stimulus"
import { Upload } from "tus-js-client"

const CHUNK_SIZE = 50 * 1024 * 1024
const RETRY_DELAYS = [ 0, 1000, 3000, 5000, 10000, 20000, 30000, 60000 ]

// Resumable asset upload over tus. tus-js-client remembers each upload in localStorage
// (keyed by file name, size, type and mtime), so after a reload or a dropped connection
// choosing the same file again continues where the server left off.
export default class extends Controller {
  static targets = [ "file", "kind", "title", "runtimeFields", "runtime", "version", "submit", "pause", "progress", "message" ]
  static values = { endpoint: String }

  connect() {
    this.toggleRuntime()
  }

  disconnect() {
    this.upload?.abort()
  }

  toggleRuntime() {
    this.runtimeFieldsTarget.hidden = this.kindTarget.value !== "runtime"
  }

  async start(event) {
    event.preventDefault()
    const file = this.fileTarget.files[0]
    if (!file) {
      this.say("Choose a file first.")
      return
    }

    const upload = new Upload(file, {
      endpoint: this.endpointValue,
      chunkSize: CHUNK_SIZE,
      retryDelays: RETRY_DELAYS,
      metadata: this.metadataFor(file),
      headers: { "X-CSRF-Token": this.csrfToken },
      removeFingerprintOnSuccess: true,
      onUploadUrlAvailable: () => this.changed(),
      onProgress: (sent, total) => this.showProgress(sent, total),
      onChunkComplete: () => this.changed(),
      onSuccess: () => this.finished(file),
      onError: (error) => this.failed(error)
    })
    this.upload = upload
    this.busy(true)

    const previous = await upload.findPreviousUploads()
    if (previous.length > 0) {
      upload.resumeFromPreviousUpload(previous[0])
      this.say(`Resuming ${file.name}…`)
    } else {
      this.say(`Uploading ${file.name}…`)
    }
    upload.start()
  }

  pause() {
    if (!this.upload) return

    this.upload.abort()
    this.upload = null
    this.busy(false)
    this.say("Paused. Choose the same file and press Upload to continue.")
    this.changed()
  }

  metadataFor(file) {
    const metadata = {
      filename: file.name,
      filetype: file.type || "application/octet-stream",
      kind: this.kindTarget.value
    }
    const title = this.titleTarget.value.trim()
    if (title) metadata.title = title
    if (this.kindTarget.value === "runtime") {
      if (this.runtimeTarget.value.trim()) metadata.runtime = this.runtimeTarget.value.trim()
      if (this.versionTarget.value.trim()) metadata.version = this.versionTarget.value.trim()
    }
    return metadata
  }

  showProgress(sent, total) {
    this.progressTarget.hidden = false
    this.progressTarget.max = total || 1
    this.progressTarget.value = sent
    const percent = total ? Math.floor((sent / total) * 100) : 100
    this.say(`${formatBytes(sent)} / ${formatBytes(total)} (${percent}%)`)
  }

  finished(file) {
    this.upload = null
    this.busy(false)
    this.element.reset()
    this.toggleRuntime()
    this.progressTarget.hidden = true
    this.say(`${file.name} uploaded. Processing…`)
    this.changed()
  }

  failed(error) {
    this.upload = null
    this.busy(false)
    const status = error?.originalResponse?.getStatus?.()
    const body = error?.originalResponse?.getBody?.()
    this.say(`Upload stopped${status ? ` (${status})` : ""}: ${body || error.message}. Choose the same file to resume.`)
    this.changed()
  }

  busy(active) {
    this.submitTarget.disabled = active
    this.pauseTarget.hidden = !active
    this.fileTarget.disabled = active
  }

  say(text) {
    this.messageTarget.textContent = text
  }

  changed() {
    this.dispatch("changed")
  }

  get csrfToken() {
    return document.querySelector("meta[name='csrf-token']")?.content || ""
  }
}

function formatBytes(bytes) {
  const units = [ "B", "KB", "MB", "GB", "TB" ]
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  return `${unit === 0 ? value : value.toFixed(1)} ${units[unit]}`
}
