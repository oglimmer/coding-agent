<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, errMsg } from '@/api'
import { useAsyncData } from '@/composables/useAsyncData'
import { useConfirm } from '@/composables/useConfirm'
import { useAuthStore } from '@/stores/auth'
import type { Repo } from '@/types'

const auth = useAuthStore()
const router = useRouter()
const { confirm } = useConfirm()
const { data: repos, loading, error, reload } = useAsyncData<Repo[]>(() => api.listRepos(), [])

const owner = ref('')
const name = ref('')
const baseBranch = ref('main')
const verifyCommand = ref('')
const testCommand = ref('')
const formError = ref<string | null>(null)
const submitting = ref(false)

async function addRepo() {
  submitting.value = true
  formError.value = null
  try {
    await api.createRepo(
      owner.value.trim(),
      name.value.trim(),
      baseBranch.value.trim() || 'main',
      verifyCommand.value.trim(),
      testCommand.value.trim(),
    )
    owner.value = ''
    name.value = ''
    baseBranch.value = 'main'
    verifyCommand.value = ''
    testCommand.value = ''
    await reload()
  } catch (e) {
    formError.value = errMsg(e)
  } finally {
    submitting.value = false
  }
}

const editingId = ref<string | null>(null)
const editOwner = ref('')
const editName = ref('')
const editBaseBranch = ref('')
const editVerifyCommand = ref('')
const editTestCommand = ref('')
const editError = ref<string | null>(null)
const editSaving = ref(false)

function startEdit(repo: Repo) {
  editingId.value = repo.id
  editOwner.value = repo.owner
  editName.value = repo.name
  editBaseBranch.value = repo.baseBranch
  editVerifyCommand.value = repo.verifyCommand ?? ''
  editTestCommand.value = repo.testCommand ?? ''
  editError.value = null
}

function cancelEdit() {
  editingId.value = null
  editError.value = null
}

async function saveEdit(repo: Repo) {
  editSaving.value = true
  editError.value = null
  try {
    await api.updateRepo(
      repo.id,
      editOwner.value.trim(),
      editName.value.trim(),
      editBaseBranch.value.trim() || 'main',
      editVerifyCommand.value.trim(),
      editTestCommand.value.trim(),
    )
    editingId.value = null
    await reload()
  } catch (e) {
    editError.value = errMsg(e)
  } finally {
    editSaving.value = false
  }
}

async function removeRepo(repo: Repo) {
  const ok = await confirm({
    title: 'Remove repository',
    message: `Remove ${repo.owner}/${repo.name}? Existing jobs are kept but you can no longer target it.`,
    confirmText: 'Remove',
    danger: true,
  })
  if (!ok) return
  try {
    await api.deleteRepo(repo.id)
    await reload()
  } catch (e) {
    formError.value = errMsg(e)
  }
}

function requestFeature(repo: Repo) {
  void router.push(`/new/${repo.id}`)
}
</script>

<template>
  <div class="container">
    <div class="head">
      <h1>Repositories</h1>
      <p class="muted">Pick a repository to request a feature, or manage the configured list.</p>
    </div>

    <div v-if="auth.isAdmin" class="card add">
      <h3>Add a repository</h3>
      <form class="row" @submit.prevent="addRepo">
        <div class="f">
          <label class="label" for="o">Owner</label>
          <input id="o" v-model="owner" class="input" placeholder="oglimmer">
        </div>
        <div class="f">
          <label class="label" for="n">Name</label>
          <input id="n" v-model="name" class="input" placeholder="my-repo">
        </div>
        <div class="f narrow">
          <label class="label" for="b">Base branch</label>
          <input id="b" v-model="baseBranch" class="input" placeholder="main">
        </div>
        <button class="btn btn-primary" :disabled="submitting || !owner" type="submit">Add</button>
        <div class="f wide">
          <label class="label" for="v">Verify command <span class="muted">(optional)</span></label>
          <input
            id="v"
            v-model="verifyCommand"
            class="input"
            placeholder="npm run lint && npm run build && npm test"
          >
          <p class="hint">
            The build/lint/test command the agent must pass locally before opening a PR —
            use the same one your CI runs. Leave empty to let the agent detect one.
          </p>
        </div>
        <div class="f wide">
          <label class="label" for="t">Test command <span class="muted">(optional)</span></label>
          <input
            id="t"
            v-model="testCommand"
            class="input"
            placeholder="cd backend && go test ./..."
          >
          <p class="hint">
            A fast command run after every edit as the agent's convergence signal. Keep it
            quick (skip Docker/e2e). Leave empty to let the agent detect it from the repo's
            manifests.
          </p>
        </div>
      </form>
      <p v-if="formError" class="error-banner" style="margin-top: 0.75rem">{{ formError }}</p>

      <div class="reqs">
        <p class="reqs-title">Before adding a repo, make sure it's set up for the agent:</p>
        <ul>
          <li>
            Worker token has <strong>Contents</strong> + <strong>Pull requests</strong> write
            (add <strong>Workflows</strong> write if the agent edits <code>.github/workflows/</code>).
          </li>
          <li>
            Base branch exists and needs no <strong>human approving review</strong> — the agent
            merges via the API; the AI reviewer only comments.
          </li>
          <li><strong>Squash merging</strong> is enabled in the repo settings.</li>
          <li>
            The AI review workflow (<code>oglimmer/review-action</code>) is installed with an
            <code>OPENAI_API_KEY</code> secret — jobs wait for its review and time out without it.
          </li>
          <li>
            <strong>Verify command</strong> (above): your CI's build/lint/test command — the agent
            runs it plus any <code>.pre-commit-config.yaml</code> hooks locally and fixes failures
            before the PR opens.
          </li>
          <li>
            <strong>Test command</strong> (above): optional fast per-edit check; auto-detected from
            the repo's manifests if left empty.
          </li>
        </ul>
      </div>
    </div>

    <p v-if="loading" class="muted">Loading repositories…</p>
    <p v-else-if="error" class="error-banner">{{ error }}</p>
    <p v-else-if="repos.length === 0" class="muted empty">
      No repositories configured yet.
      <span v-if="!auth.isAdmin">Ask an admin to add one.</span>
    </p>

    <ul v-else class="list">
      <li v-for="repo in repos" :key="repo.id" class="card item">
        <template v-if="editingId === repo.id">
          <form class="edit" @submit.prevent="saveEdit(repo)">
            <div class="row">
              <div class="f">
                <label class="label">Owner</label>
                <input v-model="editOwner" class="input" placeholder="oglimmer">
              </div>
              <div class="f">
                <label class="label">Name</label>
                <input v-model="editName" class="input" placeholder="my-repo">
              </div>
              <div class="f narrow">
                <label class="label">Base branch</label>
                <input v-model="editBaseBranch" class="input" placeholder="main">
              </div>
              <div class="f wide">
                <label class="label">Verify command <span class="muted">(optional)</span></label>
                <input
                  v-model="editVerifyCommand"
                  class="input"
                  placeholder="npm run lint && npm run build && npm test"
                >
              </div>
              <div class="f wide">
                <label class="label">Test command <span class="muted">(optional)</span></label>
                <input
                  v-model="editTestCommand"
                  class="input"
                  placeholder="cd backend && go test ./..."
                >
              </div>
            </div>
            <p v-if="editError" class="error-banner" style="margin-top: 0.75rem">{{ editError }}</p>
            <div class="actions" style="margin-top: 0.75rem">
              <button class="btn btn-primary" :disabled="editSaving || !editOwner" type="submit">Save</button>
              <button class="btn" type="button" :disabled="editSaving" @click="cancelEdit">Cancel</button>
            </div>
          </form>
        </template>
        <template v-else>
          <div class="info">
            <div class="repo-name">{{ repo.owner }}/{{ repo.name }}</div>
            <div class="muted small">base: {{ repo.baseBranch }}</div>
            <div v-if="repo.verifyCommand" class="muted small verify">verify: <code>{{ repo.verifyCommand }}</code></div>
            <div v-if="repo.testCommand" class="muted small verify">test: <code>{{ repo.testCommand }}</code></div>
          </div>
          <div class="actions">
            <button v-if="auth.canWrite" class="btn btn-primary" @click="requestFeature(repo)">Request feature</button>
            <button v-if="auth.isAdmin" class="btn" @click="startEdit(repo)">Edit</button>
            <button v-if="auth.isAdmin" class="btn btn-danger" @click="removeRepo(repo)">Remove</button>
          </div>
        </template>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.head {
  margin-bottom: 1.25rem;
}
.head h1 {
  margin-bottom: 0.25rem;
}
.add {
  margin-bottom: 1.5rem;
}
.add h3 {
  margin-top: 0;
}
.reqs {
  margin-top: 1rem;
  padding-top: 0.85rem;
  border-top: 1px solid var(--border);
  font-size: 0.85rem;
  color: var(--text-muted);
}
.reqs-title {
  margin: 0 0 0.4rem;
  font-weight: 600;
}
.reqs ul {
  margin: 0;
  padding-left: 1.15rem;
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
}
.reqs code {
  font-size: 0.82em;
  padding: 0.05rem 0.3rem;
  border-radius: 4px;
  background: var(--bg);
}
.reqs strong {
  color: var(--text);
  font-weight: 600;
}
.row {
  display: flex;
  gap: 0.75rem;
  align-items: flex-end;
  flex-wrap: wrap;
}
.f {
  flex: 1;
  min-width: 140px;
}
.f.narrow {
  flex: 0 0 130px;
}
.f.wide {
  flex: 0 0 100%;
}
.hint {
  margin: 0.35rem 0 0;
  font-size: 0.8rem;
  color: var(--muted, #6b7280);
}
.list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}
.item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
}
.edit {
  flex: 1 1 100%;
}
.repo-name {
  font-weight: 600;
  font-size: 1.05rem;
}
.verify {
  margin-top: 0.15rem;
}
.verify code {
  font-size: 0.82em;
  padding: 0.05rem 0.3rem;
  border-radius: 4px;
  background: var(--bg);
}
.small {
  font-size: 0.82rem;
}
.actions {
  display: flex;
  gap: 0.5rem;
}
.empty {
  padding: 2rem;
  text-align: center;
}
</style>
