import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore } from './auth'

function makeJwt(exp: number): string {
  return `${btoa(JSON.stringify({ alg: 'HS256' }))}.${btoa(JSON.stringify({ exp }))}.sig`
}

describe('useAuthStore', () => {
  beforeEach(() => {
    localStorage.clear()
    setActivePinia(createPinia())
  })

  it('starts unauthenticated', () => {
    const auth = useAuthStore()
    expect(auth.isAuthenticated).toBe(false)
    expect(auth.isAdmin).toBe(false)
  })

  it('sets and persists a session', () => {
    const auth = useAuthStore()
    const token = makeJwt(Math.floor(Date.now() / 1000) + 3600)
    auth.setSession(token, { id: '1', email: 'a@b.c', name: 'A', isAdmin: true, createdAt: '' })
    expect(auth.isAuthenticated).toBe(true)
    expect(auth.isAdmin).toBe(true)
    expect(localStorage.getItem('token')).toBe(token)
  })

  it('discards an expired token on load', () => {
    localStorage.setItem('token', makeJwt(Math.floor(Date.now() / 1000) - 60))
    const auth = useAuthStore()
    expect(auth.isAuthenticated).toBe(false)
    expect(localStorage.getItem('token')).toBeNull()
  })

  it('clears the session', () => {
    const auth = useAuthStore()
    auth.setSession(makeJwt(Math.floor(Date.now() / 1000) + 3600), null)
    auth.clear()
    expect(auth.isAuthenticated).toBe(false)
    expect(localStorage.getItem('token')).toBeNull()
  })
})
