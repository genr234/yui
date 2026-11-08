import type { Player } from '@yui/protocol'

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

export const getInfo = (code: string) => request<SessionInfo>(`/play/${encodeURIComponent(code)}/info`)

export const join = (code: string, name: string, color: string, qr_token: string | null) =>
  request<{ player: Player; session: { id: string; code: string } }>(`/play/${encodeURIComponent(code)}/join`, {
    method: 'POST',
    body: JSON.stringify({ name, color, ...(qr_token ? { qr_token } : {}) }),
  })

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
