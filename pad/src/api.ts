import type { IceConfig, Player } from '@yui/protocol'

export interface SessionInfo {
  code: string
  kiosk_name: string
  lobby_open: boolean
  player: Player | null
  colors_taken: string[]
}

export class ApiError extends Error {
  constructor(public status: number, public code: string) {
    super(code)
  }
}

/** `/play/<CODE>` -> CODE (uppercased), or null when the path has no code. */
export function codeFromPath(pathname = location.pathname): string | null {
  const m = pathname.match(/^\/play\/([^/]+)/)
  return m ? decodeURIComponent(m[1]).toUpperCase() : null
}

export function qrTokenFromQuery(search = location.search): string | null {
  return new URLSearchParams(search).get('t')
}

/**
 * `?sim=1..4`: this pad runs inside the /sim page (development and test only).
 * Every request carries the same `sim` parameter so Rails keeps a separate
 * player cookie per simulated phone; motion comes from the mouse.
 */
export function simSlotFromQuery(search = typeof location === 'undefined' ? '' : location.search): number | null {
  const v = new URLSearchParams(search).get('sim')
  return v && /^[1-4]$/.test(v) ? Number(v) : null
}

const sim = simSlotFromQuery()
export const simQuery = sim ? `?sim=${sim}` : ''

export function cableUrl(loc: { protocol: string; host: string } = location): string {
  return `${loc.protocol === 'https:' ? 'wss:' : 'ws:'}//${loc.host}/cable${simQuery}`
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    ...init,
  })
  let body: any = null
  try {
    body = await res.json()
  } catch {
    /* empty or non-JSON body */
  }
  if (!res.ok) {
    const code = body?.error ?? (res.status === 404 ? 'not_found' : res.status === 429 ? 'rate_limited' : 'error')
    throw new ApiError(res.status, typeof code === 'string' ? code : 'validation')
  }
  return body as T
}

export const getInfo = (code: string) => request<SessionInfo>(`/play/${encodeURIComponent(code)}/info${simQuery}`)

export const join = (code: string, name: string, color: string, qr_token: string | null) =>
  request<{ player: Player; session: { id: string; code: string } }>(`/play/${encodeURIComponent(code)}/join${simQuery}`, {
    method: 'POST',
    body: JSON.stringify({ name, color, ...(qr_token ? { qr_token } : {}) }),
  })

export const getIce = (code: string) => request<IceConfig>(`/play/${encodeURIComponent(code)}/ice${simQuery}`)

export function friendlyError(e: unknown): string {
  const code = e instanceof ApiError ? e.code : 'error'
  switch (code) {
    case 'qr_required':
      return 'Scan the QR code on the kiosk to join.'
    case 'kicked':
      return 'You were removed from this session.'
    case 'not_found':
      return 'That session is no longer available.'
    case 'rate_limited':
      return 'Too many tries. Wait a moment and try again.'
    case 'validation':
      return 'Check your name and colour and try again.'
    default:
      return 'Something went wrong. Please try again.'
  }
}
