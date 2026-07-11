import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { api, isJwtExpired } from '@/api'
import type { User } from '@/types'

const TOKEN_KEY = 'token'
const USER_KEY = 'user'

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(loadToken())
  const user = ref<User | null>(loadUser())
  let freshUserPromise: Promise<User> | null = null

  const isAuthenticated = computed(() => token.value !== null)
  const isAdmin = computed(() => user.value?.role === 'admin')
  // Only admins may mutate — submit feature requests, delete jobs, manage repos.
  // Users have full read visibility but are otherwise read-only.
  const canWrite = computed(() => user.value?.role === 'admin')
  // Viewers have no access yet — they are held on the pending screen until an
  // admin promotes them.
  const isPending = computed(() => user.value?.role === 'viewer')

  function loadToken(): string | null {
    const t = localStorage.getItem(TOKEN_KEY)
    if (t && isJwtExpired(t)) {
      localStorage.removeItem(TOKEN_KEY)
      localStorage.removeItem(USER_KEY)
      return null
    }
    return t
  }

  function loadUser(): User | null {
    const raw = localStorage.getItem(USER_KEY)
    if (!raw) return null
    try {
      return JSON.parse(raw) as User
    } catch {
      return null
    }
  }

  function setSession(newToken: string, newUser: User | null) {
    token.value = newToken
    localStorage.setItem(TOKEN_KEY, newToken)
    if (newUser) {
      user.value = newUser
      localStorage.setItem(USER_KEY, JSON.stringify(newUser))
    }
  }

  function clear() {
    token.value = null
    user.value = null
    freshUserPromise = null
    localStorage.removeItem(TOKEN_KEY)
    localStorage.removeItem(USER_KEY)
  }

  async function devLogin(username: string, password: string) {
    const res = await api.devLogin(username, password)
    setSession(res.token, res.user)
  }

  // ensureFreshUser resolves the current profile from the server, caching the
  // in-flight promise so concurrent guards share one request.
  async function ensureFreshUser(): Promise<User> {
    if (!token.value) throw new Error('not authenticated')
    if (!freshUserPromise) {
      freshUserPromise = api
        .me()
        .then((u) => {
          user.value = u
          localStorage.setItem(USER_KEY, JSON.stringify(u))
          return u
        })
        .finally(() => {
          freshUserPromise = null
        })
    }
    return freshUserPromise
  }

  return {
    token,
    user,
    isAuthenticated,
    isAdmin,
    canWrite,
    isPending,
    setSession,
    clear,
    devLogin,
    ensureFreshUser,
  }
})
