export const COLOR_HEX: Record<string, string> = {
  blue: '#4d8dff',
  red: '#ff5470',
  green: '#3ddc84',
  yellow: '#ffd23f',
  purple: '#a77bff',
  orange: '#ff9340',
  pink: '#ff7ac8',
  teal: '#2ad4c4',
}

export const COLOR_NAMES = Object.keys(COLOR_HEX)

export const colorHex = (name: string | undefined) => COLOR_HEX[name ?? ''] ?? '#8a8fb5'
