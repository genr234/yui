// Entry for app pages: served by the kiosk at `_yui/avatar.js` on every app
// origin, next to its atlas. Loading it defines <yui-avatar>.
import { defineAvatarElement } from './element'
import { configureAvatars } from './view'

export * from './index'

configureAvatars({ atlasUrl: new URL('./avatar-atlas.png', import.meta.url).href })
defineAvatarElement()
