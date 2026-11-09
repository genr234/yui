import { describe, expect, test } from 'bun:test'
import { askText, enqueueAsk, isCompleteCode, normaliseCode } from './account'
import { ApiError, friendlyError } from './api'

describe('normaliseCode', () => {
  test('keeps digits only and caps at six', () => {
    expect(normaliseCode('123 456')).toBe('123456')
    expect(normaliseCode('12-34-56-78')).toBe('123456')
    expect(normaliseCode('ab12')).toBe('12')
    expect(normaliseCode('')).toBe('')
  })
  test('isCompleteCode needs exactly six digits', () => {
    expect(isCompleteCode('123456')).toBe(true)
    expect(isCompleteCode('12345')).toBe(false)
    expect(isCompleteCode('1234567')).toBe(false)
    expect(isCompleteCode('12345a')).toBe(false)
  })
})

describe('enqueueAsk', () => {
  const a = { app_id: 'a', app_name: 'A' }
  const b = { app_id: 'b', app_name: 'B' }
  test('keeps order and drops duplicates by app id', () => {
    expect(enqueueAsk(enqueueAsk([], a), b)).toEqual([a, b])
    expect(enqueueAsk([a, b], { app_id: 'a', app_name: 'Again' })).toEqual([a, b])
  })
  test('does not mutate the queue', () => {
    const q = [a]
    enqueueAsk(q, b)
    expect(q).toEqual([a])
  })
})

describe('askText', () => {
  test('names the game and the account', () => {
    expect(askText({ app_id: 'a', app_name: 'Racer' }, 'Sam')).toBe('Racer wants to save your progress to Sam.')
    expect(askText({ app_id: 'a', app_name: ' ' }, null)).toBe('A game wants to save your progress to your Yui account.')
  })
})

describe('friendlyError for accounts', () => {
  test('explains a bad or used code', () => {
    expect(friendlyError(new ApiError(404, 'invalid_code'))).toContain('code')
    expect(friendlyError(new ApiError(409, 'not_linked'))).toContain('account')
    expect(friendlyError(new ApiError(429, 'rate_limited'))).toContain('Too many')
  })
})
