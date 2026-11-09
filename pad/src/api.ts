import type { IceConfig, Player } from '@yui/protocol'

export interface SessionInfo {
  code: string
  kiosk_name: string
  lobby_open: boolean
  player: Player | null
  colors_taken: string[]
  /** The phone's linked Yui account, when it has one (plan 10). */
  account_name?: string | null
}

export interface AccountState {
  account_name: string | null
  account_linked: boolean
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

type JoinResponse = { player: Player; session: { id: string; code: string } }

export const join = (code: string, name: string, color: string, qr_token: string | null, avatar: string | null = null, use_account?: boolean) =>
  request<JoinResponse>(`/play/${encodeURIComponent(code)}/join${simQuery}`, {
    method: 'POST',
    body: JSON.stringify({
      name,
      color,
      ...(avatar ? { avatar } : {}),
      ...(qr_token ? { qr_token } : {}),
      ...(use_account === undefined ? {} : { use_account }),
    }),
  })

/** Rejoins with only a new avatar (null removes it); name, colour and slot stay. */
export const updateAvatar = (code: string, avatar: string | null) =>
  request<JoinResponse>(`/play/${encodeURIComponent(code)}/join${simQuery}`, {
    method: 'POST',
    body: JSON.stringify({ avatar }),
  })

export const getIce = (code: string) => request<IceConfig>(`/play/${encodeURIComponent(code)}/ice${simQuery}`)

const playPath = (code: string, rest: string) => `/play/${encodeURIComponent(code)}/${rest}${simQuery}`
const json = (method: string, body?: unknown): RequestInit => ({ method, ...(body === undefined ? {} : { body: JSON.stringify(body) }) })

/** Redeems a 6-digit phone code and links this phone (and, once joined, this player) to the account. */
export const linkAccount = (code: string, link_code: string, label?: string) =>
  request<AccountState>(playPath(code, 'account'), json('POST', { link_code, ...(label ? { label } : {}) }))

/** Plays as the linked account this session (`use: true`) or as a guest. */
export const useAccount = (code: string, use: boolean) => request<AccountState>(playPath(code, 'account'), json('PATCH', { use }))

/** Forgets the link on this phone. */
export const forgetAccount = (code: string) => request<AccountState>(playPath(code, 'account'), json('DELETE'))

/** The player allows a game to keep a profile (answers 204). */
export const grantProfile = (code: string, app_id: string) =>
  request<null>(playPath(code, 'profile_grants'), json('POST', { app_id }))

export const revokeProfile = (code: string, app_id: string) =>
  request<null>(playPath(code, `profile_grants/${encodeURIComponent(app_id)}`), json('DELETE'))

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
    case 'invalid_code':
      return "That code didn't work. It may have expired or been used. Make a new one and try again."
    case 'not_linked':
      return 'This phone is not linked to a Yui account yet.'
    case 'not_joined':
      return 'Join the session first.'
    case 'validation':
    case 'validation_failed':
      return 'Check your name, colour and avatar and try again.'
    default:
      return 'Something went wrong. Please try again.'
  }
}
