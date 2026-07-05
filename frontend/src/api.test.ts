import { describe, expect, it } from 'vitest'
import { ApiError, errMsg, errStatus, isJwtExpired } from './api'

function makeJwt(exp?: number): string {
  const header = btoa(JSON.stringify({ alg: 'HS256' }))
  const payload = btoa(JSON.stringify(exp ? { exp } : {}))
  return `${header}.${payload}.sig`
}

describe('isJwtExpired', () => {
  it('returns true for malformed tokens', () => {
    expect(isJwtExpired('not-a-jwt')).toBe(true)
    expect(isJwtExpired('a.b')).toBe(true)
  })

  it('returns true for an expired token', () => {
    expect(isJwtExpired(makeJwt(Math.floor(Date.now() / 1000) - 60))).toBe(true)
  })

  it('returns false for a valid future token', () => {
    expect(isJwtExpired(makeJwt(Math.floor(Date.now() / 1000) + 3600))).toBe(false)
  })

  it('returns false when no exp claim present', () => {
    expect(isJwtExpired(makeJwt())).toBe(false)
  })
})

describe('errMsg / errStatus', () => {
  it('reads ApiError message and status', () => {
    const e = new ApiError(404, 'not found')
    expect(errMsg(e)).toBe('not found')
    expect(errStatus(e)).toBe(404)
  })

  it('reads plain Error message', () => {
    expect(errMsg(new Error('boom'))).toBe('boom')
    expect(errStatus(new Error('boom'))).toBeUndefined()
  })

  it('falls back for unknown values', () => {
    expect(errMsg('weird', 'fallback')).toBe('fallback')
  })
})
