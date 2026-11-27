import { describe, expect, test } from 'bun:test'
import { cleanCode, isIOS, joinTarget } from './pwa'

const origin = 'https://yui.example'

describe('joining from the installed app', () => {
  test("a scanned kiosk link gives its code (the QR token is dropped), on this site only", () => {
    expect(joinTarget('https://yui.example/play/kqxm?t=abc+/=', origin)).toBe('KQXM')
    expect(joinTarget('https://yui.example/play/KQXM/', origin)).toBe('KQXM')
    expect(joinTarget('kqxm', origin)).toBe('KQXM')
    // Not ours, or not a game: never followed.
    expect(joinTarget('https://evil.example/play/KQXM?t=x', origin)).toBeNull()
    expect(joinTarget('WIFI:T:WPA;S:Yui KQXM;P:secret;;', origin)).toBeNull()
    expect(joinTarget('https://yui.example/accounts/1', origin)).toBeNull()
    expect(joinTarget('https://yui.example/play/KQXM/join', origin)).toBeNull()
    expect(joinTarget('javascript:alert(1)', origin)).toBeNull()
    expect(joinTarget('https://yui.example/play/KQXM?t=%E0%A4%A', origin)).toBe('KQXM')
  })

  test('typed codes are four letters, any case or spacing', () => {
    expect(cleanCode(' kq xm ')).toBe('KQXM')
    expect(cleanCode('KQX')).toBeNull()
    expect(cleanCode('KQXM1')).toBe('KQXM')
    expect(cleanCode('KQXMA')).toBeNull()
  })

  test('iOS, including iPadOS reporting as a Mac', () => {
    expect(isIOS({ userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)', maxTouchPoints: 5 })).toBe(true)
    expect(isIOS({ userAgent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)', maxTouchPoints: 5 })).toBe(true)
    expect(isIOS({ userAgent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)', maxTouchPoints: 0 })).toBe(false)
    expect(isIOS({ userAgent: 'Mozilla/5.0 (Linux; Android 15; Pixel 7)', maxTouchPoints: 5 })).toBe(false)
  })
})
