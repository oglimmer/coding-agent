<script setup lang="ts">
import { ref, watch } from 'vue'
import { api, errMsg } from '@/api'
import { useAsyncData } from '@/composables/useAsyncData'
import { useAutoReload } from '@/composables/useAutoReload'
import { useConfirm } from '@/composables/useConfirm'
import { useAuthStore } from '@/stores/auth'
import { relativeTime } from '@/lib/format'
import StatusBadge from '@/components/StatusBadge.vue'
import type { Job } from '@/types'

const auth = useAuthStore()
const { confirm } = useConfirm()
const showAll = ref(false)
const { data: jobs, loading, error, reload } = useAsyncData<Job[]>(() => api.listJobs(showAll.value), [])
const actionError = ref<string | null>(null)

watch(showAll, () => void reload())

// Poll while any job is still in flight.
useAutoReload(() => {
  if (jobs.value.some((j) => j.status === 'running' || j.status === 'checking')) void reload()
}, 5000)

async function remove(job: Job) {
  const ok = await confirm({
    title: 'Delete job',
    message: `Delete the job for "${job.feature.slice(0, 60)}"? If it is running, the worker is cancelled.`,
    confirmText: 'Delete',
    danger: true,
  })
  if (!ok) return
  actionError.value = null
  try {
    await api.deleteJob(job.id)
    await reload()
  } catch (e) {
    actionError.value = errMsg(e)
  }
}
</script>

<template>
  <div class="container">
    <div class="head">
      <h1>Jobs</h1>
      <label v-if="auth.isAdmin" class="all">
        <input v-model="showAll" type="checkbox"> Show everyone's jobs
      </label>
    </div>

    <p v-if="actionError" class="error-banner">{{ actionError }}</p>
    <p v-if="loading && jobs.length === 0" class="muted">Loading jobs…</p>
    <p v-else-if="error" class="error-banner">{{ error }}</p>
    <p v-else-if="jobs.length === 0" class="muted empty">
      No jobs yet. <RouterLink to="/new">Request a feature</RouterLink> to get started.
    </p>

    <div v-else class="card table-wrap">
      <table>
        <thead>
          <tr>
            <th>Repository</th>
            <th>Feature</th>
            <th v-if="showAll">By</th>
            <th>Status</th>
            <th>Updated</th>
            <th />
          </tr>
        </thead>
        <tbody>
          <tr v-for="job in jobs" :key="job.id">
            <td class="mono">{{ job.repoName }}</td>
            <td class="feat">{{ job.feature }}</td>
            <td v-if="showAll" class="muted">{{ job.userName }}</td>
            <td><StatusBadge :status="job.status" /></td>
            <td class="muted nowrap">{{ relativeTime(job.updatedAt) }}</td>
            <td class="nowrap">
              <a v-if="job.prUrl" :href="job.prUrl" target="_blank" rel="noopener">PR ↗</a>
              <RouterLink :to="`/jobs/${job.id}`">Details</RouterLink>
              <button class="link-btn" title="Delete job" @click="remove(job)">Delete</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style scoped>
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 1.25rem;
}
.all {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  font-size: 0.9rem;
  color: var(--text-muted);
}
.table-wrap {
  padding: 0;
  overflow-x: auto;
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.85rem;
  white-space: nowrap;
}
.feat {
  max-width: 320px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.nowrap {
  white-space: nowrap;
}
td a,
td .router-link-active {
  margin-right: 0.75rem;
}
.link-btn {
  background: none;
  border: none;
  padding: 0;
  cursor: pointer;
  color: var(--danger);
  font: inherit;
}
.link-btn:hover {
  text-decoration: underline;
}
.empty {
  padding: 2rem;
  text-align: center;
}
</style>
