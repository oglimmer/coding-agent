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
const formError = ref<string | null>(null)
const submitting = ref(false)

async function addRepo() {
  submitting.value = true
  formError.value = null
  try {
    await api.createRepo(owner.value.trim(), name.value.trim(), baseBranch.value.trim() || 'main')
    owner.value = ''
    name.value = ''
    baseBranch.value = 'main'
    await reload()
  } catch (e) {
    formError.value = errMsg(e)
  } finally {
    submitting.value = false
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
      </form>
      <p v-if="formError" class="error-banner" style="margin-top: 0.75rem">{{ formError }}</p>
    </div>

    <p v-if="loading" class="muted">Loading repositories…</p>
    <p v-else-if="error" class="error-banner">{{ error }}</p>
    <p v-else-if="repos.length === 0" class="muted empty">
      No repositories configured yet.
      <span v-if="!auth.isAdmin">Ask an admin to add one.</span>
    </p>

    <ul v-else class="list">
      <li v-for="repo in repos" :key="repo.id" class="card item">
        <div class="info">
          <div class="repo-name">{{ repo.owner }}/{{ repo.name }}</div>
          <div class="muted small">base: {{ repo.baseBranch }}</div>
        </div>
        <div class="actions">
          <button v-if="auth.canWrite" class="btn btn-primary" @click="requestFeature(repo)">Request feature</button>
          <button v-if="auth.isAdmin" class="btn btn-danger" @click="removeRepo(repo)">Remove</button>
        </div>
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
.repo-name {
  font-weight: 600;
  font-size: 1.05rem;
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
