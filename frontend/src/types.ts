export type Role = 'viewer' | 'user' | 'admin'

export interface User {
  id: string
  email: string
  name: string
  role: Role
  createdAt: string
}

export interface Repo {
  id: string
  owner: string
  name: string
  baseBranch: string
  verifyCommand?: string
  addedBy?: string
  createdAt: string
}

export type JobStatus = 'checking' | 'rejected' | 'running' | 'success' | 'failed'

export interface Job {
  id: string
  repoId: string
  repoName: string
  userId: string
  userName: string
  feature: string
  status: JobStatus
  branch?: string
  prUrl?: string
  reason?: string
  createdAt: string
  updatedAt: string
}

export interface AuthConfig {
  mode: 'oidc' | 'password'
  oidcEnabled: boolean
}
