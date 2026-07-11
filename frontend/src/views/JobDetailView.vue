<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { api, errMsg } from '@/api'
import { useAsyncData } from '@/composables/useAsyncData'
import { useAutoReload } from '@/composables/useAutoReload'
import { useConfirm } from '@/composables/useConfirm'
import { relativeTime } from '@/lib/format'
import StatusBadge from '@/components/StatusBadge.vue'
import JobLog from '@/components/JobLog.vue'
import type { Job } from '@/types'

const props = defineProps<{ id: string }>()
const router = useRouter()
const { confirm } = useConfirm()
const { data: job, loading, error, reload } = useAsyncData<Job | null>(() => api.getJob(props.id), null)

const inFlight = computed(() => job.value?.status === 'running' || job.value?.status === 'checking')

// Provenance/config snapshot captured when the job was created, for diagnosis.
const metaRows = computed(() => {
  const m = job.value?.metadata
  if (!m) return []
  const rows: { label: string; value: string }[] = []
  const push = (label: string, value: unknown) => {
    if (value !== undefined && value !== null && value !== '') rows.push({ label, value: String(value) })
  }
  push('Platform', [m.platformVersion, m.platformCommit].filter(Boolean).join(' @ '))
  push('Engine', m.engine)
  push('Worker image', m.workerImage)
  push('Model', m.model)
  push('Editor model', m.editorModel)
  push('Review rounds', m.reviewMaxRounds)
  push('Agent timeout', m.aiderTimeoutSec ? `${m.aiderTimeoutSec}s` : undefined)
  push('Base branch', m.baseBranch)
  push('Verify command', m.verifyCommand)
  push('Test command', m.testCommand)
  return rows
})
const canRetry = computed(() => {
  const s = job.value?.status
  return s === 'failed' || s === 'rejected'
})
const actionError = ref<string | null>(null)
const busy = ref(false)

// Worker pod logs. A pod exists only once the job leaves 'checking', and lingers
// (TTL) for a while after it finishes, so we show logs for running and terminal
// jobs alike.
const logs = ref('')
const hadPod = computed(() => {
  const s = job.value?.status
  return s === 'running' || s === 'success' || s === 'failed'
})

async function loadLogs() {
  if (!job.value || !hadPod.value) return
  try {
    const res = await api.getJobLogs(job.value.id)
    logs.value = res.logs
  } catch {
    // Transient (pod starting, brief API hiccup) — keep the last good logs.
  }
}

// Refresh logs when the job first resolves and on every status transition (so
// the terminal snapshot is captured once the agent finishes).
watch(
  () => (job.value ? `${job.value.id}:${job.value.status}` : ''),
  () => void loadLogs(),
)

useAutoReload(() => {
  if (inFlight.value) void reload()
}, 5000)

// While the agent works, stream logs a little faster than the status poll.
useAutoReload(() => {
  if (inFlight.value) void loadLogs()
}, 3000)

async function retry() {
  if (!job.value) return
  busy.value = true
  actionError.value = null
  try {
    const fresh = await api.createJob(
      job.value.repoId,
      job.value.feature,
      job.value.engine,
      job.value.model ?? '',
      job.value.editorModel ?? '',
    )
    await router.push(`/jobs/${fresh.id}`)
  } catch (e) {
    actionError.value = errMsg(e)
  } finally {
    busy.value = false
  }
}

async function remove() {
  if (!job.value) return
  const ok = await confirm({
    title: 'Delete job',
    message: 'Delete this job record? If it is still running, the worker Job is cancelled.',
    confirmText: 'Delete',
    danger: true,
  })
  if (!ok) return
  busy.value = true
  actionError.value = null
  try {
    await api.deleteJob(job.value.id)
    await router.push('/jobs')
  } catch (e) {
    actionError.value = errMsg(e)
    busy.value = false
  }
}
</script>

<template>
  <div class="container detail">
    <RouterLink to="/jobs" class="back muted">← All jobs</RouterLink>

    <p v-if="loading && !job" class="muted">Loading…</p>
    <p v-else-if="error" class="error-banner">{{ error }}</p>

    <div v-else-if="job" class="card">
      <div class="top">
        <h1 class="mono">{{ job.repoName }}</h1>
        <StatusBadge :status="job.status" />
      </div>

      <p class="feature">{{ job.feature }}</p>

      <dl class="meta">
        <div><dt>Requested by</dt><dd>{{ job.userName }}</dd></div>
        <div><dt>Created</dt><dd>{{ relativeTime(job.createdAt) }}</dd></div>
        <div><dt>Updated</dt><dd>{{ relativeTime(job.updatedAt) }}</dd></div>
        <div v-if="job.branch"><dt>Branch</dt><dd class="mono">{{ job.branch }}</dd></div>
      </dl>

      <div v-if="inFlight" class="note muted">
        The agent is working. This page refreshes automatically.
      </div>

      <div v-if="job.reason" class="reason" :class="{ bad: job.status === 'failed' || job.status === 'rejected' }">
        {{ job.reason }}
      </div>

      <details v-if="metaRows.length" class="provenance">
        <summary>Run metadata</summary>
        <dl class="meta">
          <div v-for="row in metaRows" :key="row.label">
            <dt>{{ row.label }}</dt>
            <dd class="mono">{{ row.value }}</dd>
          </div>
        </dl>
      </details>

      <JobLog v-if="hadPod" :text="logs" :live="inFlight" />

      <p v-if="actionError" class="error-banner" style="margin-top: 1rem">{{ actionError }}</p>

      <div class="actions">
        <a v-if="job.prUrl" :href="job.prUrl" target="_blank" rel="noopener" class="btn btn-primary">
          View pull request ↗
        </a>
        <button v-if="canRetry" class="btn" :disabled="busy" @click="retry">Retry</button>
        <button class="btn btn-danger" :disabled="busy" @click="remove">
          {{ inFlight ? 'Cancel & delete' : 'Delete' }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* Wider than the default container so the worker log — whose lines arrive
   pre-wrapped at ~72 columns — has room to render without re-wrapping. */
.detail {
  max-width: 1040px;
}
.back {
  display: inline-block;
  margin-bottom: 1rem;
}
.top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
}
.top h1 {
  margin: 0;
  font-size: 1.2rem;
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}
.feature {
  font-size: 1.05rem;
  margin: 1rem 0 1.5rem;
}
.meta {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 0.75rem 1.5rem;
  margin: 0;
}
.meta dt {
  font-size: 0.75rem;
  text-transform: uppercase;
  letter-spacing: 0.03em;
  color: var(--text-muted);
}
.meta dd {
  margin: 0.1rem 0 0;
}
.note {
  margin-top: 1.5rem;
  padding: 0.75rem;
  border: 1px dashed var(--border);
  border-radius: var(--radius-sm);
  text-align: center;
}
.reason {
  margin-top: 1.5rem;
  padding: 0.75rem 1rem;
  border-radius: var(--radius-sm);
  background: var(--bg-elevated);
  border: 1px solid var(--border);
}
.reason.bad {
  color: var(--danger);
  border-color: rgb(var(--danger-rgb) / 0.4);
  background: rgb(var(--danger-rgb) / 0.1);
}
.provenance {
  margin-top: 1.5rem;
}
.provenance summary {
  cursor: pointer;
  color: var(--text-muted, var(--muted));
  font-size: 0.9rem;
}
.provenance .meta {
  margin-top: 0.75rem;
}
.actions {
  display: flex;
  gap: 0.6rem;
  flex-wrap: wrap;
  margin-top: 1.5rem;
}
</style>
