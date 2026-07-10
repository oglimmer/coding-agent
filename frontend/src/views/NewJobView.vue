<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { api, errMsg } from '@/api'
import { useAsyncData } from '@/composables/useAsyncData'
import type { Engine, Job, Repo } from '@/types'

const props = defineProps<{ repoId?: string }>()
const router = useRouter()

const { data: repos, loading, error } = useAsyncData<Repo[]>(() => api.listRepos(), [])
const selectedRepo = ref(props.repoId ?? '')
const feature = ref('')

// Which worker implements the request. Both are DeepSeek-backed; they differ in
// the coding engine driving the change.
const ENGINES: { value: Engine; label: string; hint: string }[] = [
  { value: 'aider', label: 'aider', hint: 'aider in architect mode on DeepSeek (default)' },
  { value: 'claude-code', label: 'Claude Code', hint: 'Claude Code CLI on a DeepSeek backend' },
]
const engine = ref<Engine>('aider')
const submitting = ref(false)
const formError = ref<string | null>(null)
const rejected = ref<Job | null>(null)

watch(repos, (list) => {
  if (!selectedRepo.value && list.length > 0) selectedRepo.value = list[0].id
})

const MIN = 20

async function submit() {
  formError.value = null
  rejected.value = null
  if (feature.value.trim().length < MIN) {
    formError.value = `Please describe the feature in at least ${MIN} characters.`
    return
  }
  submitting.value = true
  try {
    const job = await api.createJob(selectedRepo.value, feature.value.trim(), engine.value)
    if (job.status === 'rejected') {
      rejected.value = job
      return
    }
    await router.push(`/jobs/${job.id}`)
  } catch (e) {
    formError.value = errMsg(e)
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div class="container narrow">
    <h1>Request a feature</h1>
    <p class="muted">
      Describe what you want built. The request is safety-checked, then an agent implements it
      <strong>with tests</strong>, opens a PR, addresses the review, and merges it.
    </p>

    <p v-if="loading" class="muted">Loading repositories…</p>
    <p v-else-if="error" class="error-banner">{{ error }}</p>
    <p v-else-if="repos.length === 0" class="muted">No repositories are configured yet.</p>

    <form v-else class="card" @submit.prevent="submit">
      <label class="label" for="repo">Repository</label>
      <select id="repo" v-model="selectedRepo" class="select">
        <option v-for="r in repos" :key="r.id" :value="r.id">{{ r.owner }}/{{ r.name }}</option>
      </select>

      <label class="label" style="margin-top: 1rem">Coding engine</label>
      <div class="engines">
        <label v-for="e in ENGINES" :key="e.value" class="engine" :class="{ active: engine === e.value }">
          <input v-model="engine" type="radio" name="engine" :value="e.value">
          <span class="engine-name">{{ e.label }}</span>
          <span class="engine-hint muted">{{ e.hint }}</span>
        </label>
      </div>

      <label class="label" for="feat" style="margin-top: 1rem">Feature description</label>
      <textarea
        id="feat"
        v-model="feature"
        class="textarea"
        placeholder="e.g. Add a /health endpoint that returns build version and uptime as JSON."
      />
      <div class="counter muted">{{ feature.trim().length }} characters (min {{ MIN }})</div>

      <div v-if="rejected" class="error-banner reject">
        <strong>Request rejected by the safety check.</strong>
        <div>{{ rejected.reason }}</div>
      </div>
      <p v-if="formError" class="error-banner">{{ formError }}</p>

      <button
        class="btn btn-primary submit"
        :disabled="submitting || !selectedRepo || feature.trim().length < MIN"
        type="submit"
      >
        {{ submitting ? 'Submitting…' : 'Submit request' }}
      </button>
    </form>
  </div>
</template>

<style scoped>
.narrow {
  max-width: 680px;
}
.counter {
  font-size: 0.8rem;
  margin-top: 0.35rem;
}
.submit {
  margin-top: 1.25rem;
}
.engines {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0.6rem;
  margin-top: 0.4rem;
}
.engine {
  display: flex;
  flex-direction: column;
  gap: 0.15rem;
  padding: 0.6rem 0.75rem;
  border: 1px solid var(--border, #d0d0d0);
  border-radius: 8px;
  cursor: pointer;
}
.engine.active {
  border-color: var(--accent, #3b82f6);
  box-shadow: 0 0 0 1px var(--accent, #3b82f6) inset;
}
.engine input {
  position: absolute;
  opacity: 0;
}
.engine-name {
  font-weight: 600;
}
.engine-hint {
  font-size: 0.78rem;
}
@media (max-width: 520px) {
  .engines {
    grid-template-columns: 1fr;
  }
}
.reject {
  margin-top: 1rem;
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
}
</style>
