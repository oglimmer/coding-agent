<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { api, errMsg } from '@/api'
import { useAsyncData } from '@/composables/useAsyncData'
import type { ClientConfig, Engine, EngineModels, Job, Repo } from '@/types'

const props = defineProps<{ repoId?: string }>()
const router = useRouter()

const { data: repos, loading, error } = useAsyncData<Repo[]>(() => api.listRepos(), [])
const { data: config } = useAsyncData<ClientConfig | null>(() => api.clientConfig(), null)
const selectedRepo = ref(props.repoId ?? '')
const feature = ref('')

// Which worker implements the request. Both are DeepSeek-backed; they differ in
// the coding engine driving the change.
const ENGINES: { value: Engine; label: string; hint: string }[] = [
  { value: 'aider', label: 'aider', hint: 'aider in architect mode on DeepSeek (default)' },
  { value: 'claude-code', label: 'Claude Code', hint: 'Claude Code CLI on a DeepSeek backend' },
]
const engine = ref<Engine>('aider')
// Per-job coding model(s). aider exposes an architect + editor split; claude-code
// drives a single model (editorModel is ignored server-side for it). Both default
// to the deployment default the backend reports for the selected engine.
const model = ref('')
const editorModel = ref('')
const submitting = ref(false)
const formError = ref<string | null>(null)
const rejected = ref<Job | null>(null)

// The model catalog for the currently selected engine (allowlist + defaults).
const engineModels = computed<EngineModels | null>(() => config.value?.engines[engine.value] ?? null)

// Whenever the engine changes or the catalog loads, snap the model selections to
// that engine's defaults (its ids differ per engine, so a carried-over value
// would be invalid).
watch(
  [engine, engineModels],
  () => {
    const em = engineModels.value
    if (!em) return
    model.value = em.defaultModel
    editorModel.value = em.defaultEditorModel ?? ''
  },
  { immediate: true },
)

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
    const job = await api.createJob(
      selectedRepo.value,
      feature.value.trim(),
      engine.value,
      model.value,
      editorModel.value,
    )
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

    <details class="explainer">
      <summary>
        <span class="explainer-title">How does the agent build this?</span>
        <span class="explainer-chevron" aria-hidden="true">▸</span>
      </summary>
      <div class="explainer-body">
        <p class="muted explainer-intro">
          Every request runs the same end-to-end pipeline — the only difference is the
          <strong>coding engine</strong> that writes the change. Both engines are DeepSeek-backed by
          default.
        </p>

        <ol class="flow">
          <li><strong>Safety check.</strong> Your description is screened before any code runs; unsafe or out-of-scope requests are rejected.</li>
          <li><strong>Clone.</strong> A fresh, isolated checkout of the repository on a new branch.</li>
          <li><strong>Implement (with tests).</strong> The coding engine writes the feature and its tests — see the two engines below.</li>
          <li><strong>Self-review gate.</strong> The diff is critiqued and corrected before it ever leaves the sandbox.</li>
          <li><strong>Verify gate.</strong> The repo's real build/lint/test command must pass — the same one CI runs.</li>
          <li><strong>Open a PR</strong> and wait for the repository's GitHub Action review.</li>
          <li><strong>Address the review.</strong> The engine fixes any findings and re-requests review, looping until it's clean.</li>
          <li><strong>Squash-merge</strong> the PR.</li>
        </ol>

        <div class="engine-explains">
          <div class="engine-explain">
            <h3>aider <span class="muted tag">architect mode</span></h3>
            <p class="muted">
              A cheap model first <strong>scopes</strong> the task — picking the relevant files and
              detecting the test command. Then aider runs a two-model split: an
              <strong>architect model</strong> reasons about <em>what</em> to change and an
              <strong>editor model</strong> turns that plan into precise edits. It re-runs the repo's
              fast test suite after each edit (<code>--auto-test</code>) and keeps going until the
              change is green.
            </p>
          </div>
          <div class="engine-explain">
            <h3>Claude Code <span class="muted tag">agentic</span></h3>
            <p class="muted">
              A single autonomous agent drives the whole change. Claude Code
              <strong>explores the repo, edits files, and runs tests itself</strong>, deciding what to
              read and run as it goes — so there's no separate scoping step, no architect/editor split,
              and no test-command detection. One model, one loop.
            </p>
          </div>
        </div>
      </div>
    </details>

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

      <template v-if="engineModels">
        <div class="models">
          <div class="model-field">
            <label class="label" for="model">
              {{ engine === 'aider' ? 'Architect model' : 'Model' }}
            </label>
            <select id="model" v-model="model" class="select">
              <option v-for="m in engineModels.models" :key="m" :value="m">{{ m }}</option>
            </select>
          </div>
          <div v-if="engine === 'aider'" class="model-field">
            <label class="label" for="editor-model">Editor model</label>
            <select id="editor-model" v-model="editorModel" class="select">
              <option v-for="m in engineModels.models" :key="m" :value="m">{{ m }}</option>
            </select>
          </div>
        </div>
        <p class="model-hint muted">
          {{
            engine === 'aider'
              ? 'The architect model plans the change; the editor model writes the diff.'
              : 'The model Claude Code drives the change with.'
          }}
        </p>
      </template>

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
.models {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0.6rem;
  margin-top: 0.75rem;
}
.model-field {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}
.model-field .label {
  margin: 0;
}
.model-hint {
  font-size: 0.78rem;
  margin-top: 0.35rem;
}
@media (max-width: 520px) {
  .engines,
  .models {
    grid-template-columns: 1fr;
  }
}
.reject {
  margin-top: 1rem;
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
}

/* --- process explainer --- */
.explainer {
  margin: 1rem 0 1.5rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--surface);
}
.explainer > summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  padding: 0.85rem 1.1rem;
  cursor: pointer;
  list-style: none;
  user-select: none;
}
.explainer > summary::-webkit-details-marker {
  display: none;
}
.explainer > summary:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
  border-radius: var(--radius);
}
.explainer-title {
  font-weight: 600;
}
.explainer-chevron {
  color: var(--text-muted);
  transition: transform 0.15s ease;
}
.explainer[open] .explainer-chevron {
  transform: rotate(90deg);
}
.explainer-body {
  padding: 0 1.1rem 1.1rem;
  border-top: 1px solid var(--border);
}
.explainer-intro {
  margin-top: 1rem;
  font-size: 0.88rem;
}
.flow {
  margin: 0.5rem 0 1.25rem;
  padding-left: 1.25rem;
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
  font-size: 0.88rem;
}
.flow li {
  color: var(--text-muted);
}
.flow li strong {
  color: var(--text);
}
.engine-explains {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0.75rem;
}
.engine-explain {
  padding: 0.85rem;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--bg-elevated);
}
.engine-explain h3 {
  margin: 0 0 0.4rem;
  font-size: 0.95rem;
}
.engine-explain .tag {
  font-size: 0.72rem;
  font-weight: 500;
  text-transform: uppercase;
  letter-spacing: 0.03em;
}
.engine-explain p {
  margin: 0;
  font-size: 0.83rem;
}
.engine-explain code {
  font-size: 0.9em;
  padding: 0.05rem 0.3rem;
  border-radius: 4px;
  background: var(--surface-hover);
}
@media (max-width: 520px) {
  .engine-explains {
    grid-template-columns: 1fr;
  }
}
</style>
