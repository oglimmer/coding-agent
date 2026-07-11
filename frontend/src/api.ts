// Hand-rolled fetch wrapper. Backend errors are JSON `{ "error": "..." }`.
import type { AuthConfig, ClientConfig, Engine, Job, Repo, Role, User } from './types'

const TOKEN_KEY = 'token'

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

export function errMsg(e: unknown, fallback = 'Something went wrong'): string {
  if (e instanceof ApiError) return e.message
  if (e instanceof Error) return e.message
  return fallback
}

export function errStatus(e: unknown): number | undefined {
  return e instanceof ApiError ? e.status : undefined
}

interface JwtPayload {
  exp?: number
}

export function isJwtExpired(token: string): boolean {
  const parts = token.split('.')
  if (parts.length !== 3) return true
  try {
    const payload = JSON.parse(atob(parts[1])) as JwtPayload
    if (!payload.exp) return false
    return payload.exp * 1000 <= Date.now()
  } catch {
    return true
  }
}

function authHeaders(): Record<string, string> {
  const token = localStorage.getItem(TOKEN_KEY)
  if (!token) return {}
  if (isJwtExpired(token)) {
    throw new ApiError(401, 'Session expired')
  }
  return { Authorization: `Bearer ${token}` }
}

async function request<T>(method: string, path: string, body?: unknown, auth = true): Promise<T> {
  const headers: Record<string, string> = {}
  if (auth) Object.assign(headers, authHeaders())
  let payload: BodyInit | undefined
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json'
    payload = JSON.stringify(body)
  }

  const res = await fetch(`/api${path}`, { method, headers, body: payload })
  if (res.status === 204) return undefined as T

  const text = await res.text()
  let data: unknown = undefined
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = undefined
    }
  }

  if (!res.ok) {
    const msg =
      data && typeof data === 'object' && 'error' in data
        ? String((data as { error: unknown }).error)
        : `Request failed (${res.status})`
    throw new ApiError(res.status, msg)
  }
  return data as T
}

export const api = {
  authConfig: () => request<AuthConfig>('GET', '/auth/config', undefined, false),
  devLogin: (username: string, password: string) =>
    request<{ token: string; user: User }>('POST', '/auth/login', { username, password }, false),
  me: () => request<User>('GET', '/me'),
  clientConfig: () => request<ClientConfig>('GET', '/config'),

  listRepos: () => request<Repo[]>('GET', '/repos'),
  createRepo: (owner: string, name: string, baseBranch: string, verifyCommand: string, testCommand: string) =>
    request<Repo>('POST', '/repos', { owner, name, baseBranch, verifyCommand, testCommand }),
  updateRepo: (id: string, owner: string, name: string, baseBranch: string, verifyCommand: string, testCommand: string) =>
    request<Repo>('PUT', `/repos/${id}`, { owner, name, baseBranch, verifyCommand, testCommand }),
  deleteRepo: (id: string) => request<void>('DELETE', `/repos/${id}`),

  listJobs: (all = false) => request<Job[]>('GET', `/jobs${all ? '?all=true' : ''}`),
  getJob: (id: string) => request<Job>('GET', `/jobs/${id}`),
  getJobLogs: (id: string) =>
    request<{ logs: string; unavailable?: boolean }>('GET', `/jobs/${id}/logs`),
  createJob: (
    repoId: string,
    feature: string,
    engine: Engine,
    model: string,
    editorModel: string,
    autoMerge: boolean,
  ) => request<Job>('POST', '/jobs', { repoId, feature, engine, model, editorModel, autoMerge }),
  deleteJob: (id: string) => request<void>('DELETE', `/jobs/${id}`),

  listUsers: () => request<User[]>('GET', '/admin/users'),
  setUserRole: (id: string, role: Role) => request<User>('PUT', `/admin/users/${id}/role`, { role }),
}
