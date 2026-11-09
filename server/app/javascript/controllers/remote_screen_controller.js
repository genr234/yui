import { Controller } from "@hotwired/stimulus"

// Live view of a kiosk's Chrome page. Asks the kiosk for a screenshot, waits
// for the result, shows it and asks again; clicks, wheel, keys and text are
// sent as kiosk.input commands. Stops while the tab is hidden or paused.
export default class extends Controller {
  static targets = ["image", "status", "text", "pause"]
  static values = { screenshotUrl: String, inputUrl: String, commandUrl: String }

  connect() {
    this.running = true
    this.paused = false
    this.visibility = () => { if (!document.hidden) this.loop() }
    document.addEventListener("visibilitychange", this.visibility)
    this.loop()
  }

  disconnect() {
    this.running = false
    document.removeEventListener("visibilitychange", this.visibility)
  }

  toggle() {
    this.paused = !this.paused
    this.pauseTarget.textContent = this.paused ? "Resume" : "Pause"
    if (!this.paused) this.loop()
    else this.say("Paused")
  }

  async loop() {
    if (this.looping) return
    this.looping = true
    let failures = 0
    try {
      while (this.running && !this.paused && !document.hidden) {
        try {
          const started = performance.now()
          const result = await this.run(this.screenshotUrlValue, { max_side: Math.min(1600, Math.round(this.element.clientWidth * (window.devicePixelRatio || 1))) })
          this.imageTarget.src = result.image
          this.size = { width: result.width, height: result.height }
          this.say(`Live · ${Math.round(performance.now() - started)} ms`)
          failures = 0
        } catch (error) {
          failures += 1
          this.say(error.message)
          await this.sleep(Math.min(10000, 1000 * failures))
        }
        await this.sleep(this.nudged ? 0 : 400)
        this.nudged = false
      }
    } finally {
      this.looping = false
    }
  }

  tap(event) {
    const box = this.imageTarget.getBoundingClientRect()
    if (!box.width) return
    this.send({ action_type: "tap", x: (event.clientX - box.left) / box.width, y: (event.clientY - box.top) / box.height })
  }

  scroll(event) {
    event.preventDefault()
    const box = this.imageTarget.getBoundingClientRect()
    clearTimeout(this.wheelTimer)
    this.wheel = (this.wheel || 0) + event.deltaY
    this.wheelTimer = setTimeout(() => {
      const dy = this.wheel
      this.wheel = 0
      this.send({ action_type: "scroll", dy, x: (event.clientX - box.left) / box.width, y: (event.clientY - box.top) / box.height })
    }, 120)
  }

  key(event) {
    this.send({ action_type: "key", key: event.currentTarget.dataset.key })
  }

  type(event) {
    event.preventDefault()
    const text = this.textTarget.value
    if (!text) return
    this.textTarget.value = ""
    this.send({ action_type: "text", text })
  }

  async send(params) {
    try {
      await this.run(this.inputUrlValue, params)
      this.nudged = true
    } catch (error) {
      this.say(error.message)
    }
  }

  // Queue a command, then poll until the kiosk answers.
  async run(url, params) {
    const response = await fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-CSRF-Token": this.csrfToken, Accept: "application/json" },
      body: JSON.stringify(params),
    })
    if (response.redirected || response.status === 401) throw new Error("Confirm your passkey again to keep controlling the kiosk (reload the page).")
    if (!response.ok) throw new Error(`The server said ${response.status}`)
    const { id } = await response.json()
    const deadline = Date.now() + 15000
    while (Date.now() < deadline) {
      await this.sleep(150)
      const poll = await fetch(this.commandUrlValue.replace("ID", encodeURIComponent(id)), { headers: { Accept: "application/json" } })
      if (!poll.ok) continue
      const command = await poll.json()
      if (command.status === "succeeded") return command.result
      if (command.status === "failed") throw new Error(command.error || "The kiosk couldn't do that")
      if (!command.online) throw new Error("The kiosk is offline")
    }
    throw new Error("The kiosk didn't answer in time")
  }

  say(text) {
    this.statusTarget.textContent = text
  }

  sleep(ms) {
    return new Promise((resolve) => setTimeout(resolve, ms))
  }

  get csrfToken() {
    return document.querySelector("meta[name='csrf-token']")?.content || ""
  }
}
