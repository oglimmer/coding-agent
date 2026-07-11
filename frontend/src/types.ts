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
  autoMerge?: boolean
  reviewMaxRounds?: number
  aiderTimeoutSec?: number
  deepseekBaseURL?: string
  baseBranch?: string
  verifyCommand?: string
  testCommand?: string
}

// Per-engine model catalog returned by GET /api/config, used to render the
// New Job form's model dropdowns. defaultEditorModel is only present for aider.
export interface EngineModels {
  models: string[]
  defaultModel: string
  defaultEditorModel?: string
}

export interface ClientConfig {
  engines: Record<Engine, EngineModels>
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
  model?: string
  editorModel?: string
  autoMerge: boolean
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
