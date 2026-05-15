import { Controller } from "@hotwired/stimulus"

export default class extends Controller {
  static values = {
    mode: String,
    optionsUrl: String
  }

  async submit(event) {
    const credentialField = this.element.querySelector("[name='public_key_credential']")
    if (credentialField?.value) return

    event.preventDefault()
    this.setMessage("Waiting for your passkey...")

    try {
      const options = await this.fetchOptions()
      const credential = this.modeValue === "register"
        ? await navigator.credentials.create({ publicKey: this.decodeOptions(options) })
        : await navigator.credentials.get({ publicKey: this.decodeOptions(options) })

      credentialField.value = JSON.stringify(this.encodeCredential(credential))
      this.element.requestSubmit()
    } catch (error) {
      this.setMessage(error.message || "Passkey failed. Try again.")
    }
  }

  async fetchOptions() {
    const body = new FormData(this.element)
    const response = await fetch(this.optionsUrlValue, {
      method: "POST",
      headers: {
        "Accept": "application/json",
        "X-CSRF-Token": document.querySelector("meta[name='csrf-token']")?.content
      },
      body
    })

    const payload = await response.json()
    if (!response.ok) throw new Error(payload.error || "Could not start passkey ceremony.")

    return payload
  }

  decodeOptions(options) {
    const decoded = { ...options }
    decoded.challenge = this.base64urlToBuffer(options.challenge)

    if (decoded.user?.id) decoded.user.id = this.base64urlToBuffer(decoded.user.id)
    if (decoded.excludeCredentials) {
      decoded.excludeCredentials = decoded.excludeCredentials.map((credential) => ({
        ...credential,
        id: this.base64urlToBuffer(credential.id)
      }))
    }
    if (decoded.allowCredentials) {
      decoded.allowCredentials = decoded.allowCredentials.map((credential) => ({
        ...credential,
        id: this.base64urlToBuffer(credential.id)
      }))
    }

    return decoded
  }

  encodeCredential(credential) {
    const response = credential.response
    const encoded = {
      id: credential.id,
      type: credential.type,
      rawId: this.bufferToBase64url(credential.rawId),
      response: {}
    }

    if (response.clientDataJSON) {
      encoded.response.clientDataJSON = this.bufferToBase64url(response.clientDataJSON)
    }
    if (response.attestationObject) {
      encoded.response.attestationObject = this.bufferToBase64url(response.attestationObject)
    }
    if (response.authenticatorData) {
      encoded.response.authenticatorData = this.bufferToBase64url(response.authenticatorData)
    }
    if (response.signature) {
      encoded.response.signature = this.bufferToBase64url(response.signature)
    }
    if (response.userHandle) {
      encoded.response.userHandle = this.bufferToBase64url(response.userHandle)
    }

    return encoded
  }

  base64urlToBuffer(value) {
    const padding = "=".repeat((4 - value.length % 4) % 4)
    const base64 = (value + padding).replace(/-/g, "+").replace(/_/g, "/")
    const binary = atob(base64)
    const bytes = new Uint8Array(binary.length)
    for (let index = 0; index < binary.length; index += 1) {
      bytes[index] = binary.charCodeAt(index)
    }
    return bytes.buffer
  }

  bufferToBase64url(buffer) {
    const bytes = new Uint8Array(buffer)
    let binary = ""
    bytes.forEach((byte) => { binary += String.fromCharCode(byte) })
    return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "")
  }

  setMessage(message) {
    const target = this.element.querySelector("[data-passkey-message]")
    if (target) target.textContent = message
  }
}
