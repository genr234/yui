import App from './App.svelte'
import css from './overlay.css?inline'

declare global {
  interface Window {
    __YUI_APP__?: {
      destroy: () => void
    }
    __YUI_BRIDGE_URL?: string
    __YUI_PLATFORM_HTTP?: string
  }
}

const hostId = 'yui-platform-host'

function mount() {
  if (document.getElementById(hostId)) {
    return
  }

  const host = document.createElement('div')
  host.id = hostId
  host.style.position = 'fixed'
  host.style.inset = '0'
  host.style.zIndex = '2147483647'
  host.style.pointerEvents = 'none'

  const shadow = host.attachShadow({ mode: 'open' })
  const style = document.createElement('style')
  style.textContent = css
  const target = document.createElement('div')
  target.id = 'yui-shadow-root'
  shadow.append(style, target)
  document.documentElement.appendChild(host)

  const app = new App({ target })
  window.__YUI_APP__ = {
    destroy: () => {
      app.$destroy()
      host.remove()
    },
  }
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', mount, { once: true })
} else {
  mount()
}
