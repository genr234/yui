import { Controller } from "@hotwired/stimulus"

// Submits the form it is on when a field inside it changes (e.g. an import checkbox).
export default class extends Controller {
  submit() {
    this.element.requestSubmit()
  }
}
