import App from './App.svelte'

const target = document.createElement('div')
target.id = 'kiosk-platform-root'
document.body.appendChild(target)

new App({
  target,
})

