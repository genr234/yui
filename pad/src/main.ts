import './styles.css'
import App from './App.svelte'
import { installGestureGuards } from './platform/lockdown'

installGestureGuards()

const app = new App({ target: document.getElementById('app')! })

// The pad lives at /play/<CODE>, but the worker is served from /pad/sw.js. Widening the scope
// needs `Service-Worker-Allowed: /play/` on that response (Rails); without it we silently skip.
if ('serviceWorker' in navigator && import.meta.env.PROD) {
  navigator.serviceWorker.register('/pad/sw.js', { scope: '/play/' }).catch(() => {})
}

export default app
