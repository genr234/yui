// <yui-avatar avatar="…" color="#2f76dc" view="face" fallback="A">: the same
// renderer as a custom element, for pages without a framework.
import { mountAvatar, type AvatarHandle, type AvatarProps } from './view'

export function defineAvatarElement(tag = 'yui-avatar'): void {
  if (typeof customElements === 'undefined' || customElements.get(tag)) return

  class YuiAvatar extends HTMLElement {
    static observedAttributes = ['avatar', 'color', 'view', 'fallback']
    private handle: AvatarHandle | null = null

    private props(): AvatarProps {
      return {
        avatar: this.getAttribute('avatar'),
        color: this.getAttribute('color') ?? undefined,
        view: this.getAttribute('view') === 'full' ? 'full' : 'face',
        fallback: this.getAttribute('fallback') ?? undefined,
      }
    }

    connectedCallback(): void {
      if (!this.style.display) this.style.display = 'inline-grid'
      this.style.placeItems = 'center'
      this.handle = mountAvatar(this, this.props())
    }

    disconnectedCallback(): void {
      this.handle?.destroy()
      this.handle = null
    }

    attributeChangedCallback(): void {
      this.handle?.update(this.props())
    }

    /** Pushes the creature's pupils, in eye units per second. */
    jolt(vx: number, vy: number): void {
      this.handle?.jolt(vx, vy)
    }
  }

  customElements.define(tag, YuiAvatar)
}
