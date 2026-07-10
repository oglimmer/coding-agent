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
  testCommand?: string
  addedBy?: string
  createdAt: string
}

export type JobStatus = 'checking' | 'rejected' | 'running' | 'success' | 'failed'

// Which coding-agent worker implements a request. 'aider' is the default worker
// (aider + DeepSeek); 'claude-code' runs Claude Code against a DeepSeek backend.
export type Engine = 'aider' | 'claude-code'

export interface JobMetadata {
  platformCommit?: string
  platformVersion?: string
  engine?: Engine
  workerImage?: string
  model?: string
  editorModel?: string
  reviewMaxRounds?: number
  aiderTimeoutSec?: number
  deepseekBaseURL?: string
  baseBranch?: string
  verifyCommand?: string
  testCommand?: string
}

export interface Job {
  id: string
  repoId: string
  repoName: string
  userId: string
  userName: string
  feature: string
  status: JobStatus
  engine: Engine
  branch?: string
  prUrl?: string
  reason?: string
  metadata?: JobMetadata
  createdAt: string
  updatedAt: string
}

export interface AuthConfig {
  mode: 'oidc' | 'password'
  oidcEnabled: boolean
}
