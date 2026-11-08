// Player colours. Must match platform/src/sdk/pad.ts `playerSwatch` so a
// phone's colour is the same as its chip on the kiosk overlay.
export const COLOR_HEX: Record<string, string> = {
  blue: '#2f76dc',
  red: '#d84a4a',
  green: '#1f9d68',
  yellow: '#e2a60f',
  purple: '#6558d8',
  orange: '#e5761f',
  pink: '#d64f95',
  teal: '#0f9f8f',
}

export const COLOR_NAMES = Object.keys(COLOR_HEX)

export const colorHex = (name: string | undefined) => COLOR_HEX[name ?? ''] ?? '#9fb1c4'
