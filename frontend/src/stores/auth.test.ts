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
    expect(auth.canWrite).toBe(false)
  })

  it('treats a viewer as read-only and pending', () => {
    const auth = useAuthStore()
    const token = makeJwt(Math.floor(Date.now() / 1000) + 3600)
    auth.setSession(token, { id: '2', email: 'v@b.c', name: 'V', role: 'viewer', createdAt: '' })
    expect(auth.isAdmin).toBe(false)
    expect(auth.canWrite).toBe(false)
    expect(auth.isPending).toBe(true)
  })

  it('treats a user as read-only (not a writer, admin, or pending)', () => {
    const auth = useAuthStore()
    const token = makeJwt(Math.floor(Date.now() / 1000) + 3600)
    auth.setSession(token, { id: '3', email: 'u@b.c', name: 'U', role: 'user', createdAt: '' })
    expect(auth.isAdmin).toBe(false)
    expect(auth.canWrite).toBe(false)
    expect(auth.isPending).toBe(false)
  })

  it('sets and persists a session', () => {
    const auth = useAuthStore()
    const token = makeJwt(Math.floor(Date.now() / 1000) + 3600)
    auth.setSession(token, { id: '1', email: 'a@b.c', name: 'A', role: 'admin', createdAt: '' })
    expect(auth.isAuthenticated).toBe(true)
    expect(auth.isAdmin).toBe(true)
    expect(auth.canWrite).toBe(true)
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
