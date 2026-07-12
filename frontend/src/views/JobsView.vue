<script setup lang="ts">
import { computed, ref, watch } from 'vue'
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

// Client-side pagination. The list endpoint returns lightweight rows (no logs)
// capped at 100, so slicing in the browser is cheap and keeps the page short
// enough to avoid a whole-page scrollbar.
const pageSize = ref(10)
const page = ref(1)
const totalPages = computed(() => Math.max(1, Math.ceil(jobs.value.length / pageSize.value)))
const pagedJobs = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return jobs.value.slice(start, start + pageSize.value)
})
// Keep the current page in range when the list shrinks (reload, delete) or the
// page size grows past what's left.
watch([() => jobs.value.length, pageSize], () => {
  if (page.value > totalPages.value) page.value = totalPages.value
})

watch(showAll, () => {
  page.value = 1
  void reload()
})

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

    <template v-else>
      <ul class="card job-list">
        <li v-for="job in pagedJobs" :key="job.id" class="job-item">
          <div class="job-line">
            <span class="mono repo">{{ job.repoName }}</span>
            <span class="job-meta">
              <span v-if="showAll" class="muted by">{{ job.userName }}</span>
              <StatusBadge :status="job.status" />
              <span class="muted nowrap">{{ relativeTime(job.updatedAt) }}</span>
              <span class="job-actions nowrap">
                <a v-if="job.prUrl" :href="job.prUrl" target="_blank" rel="noopener">PR ↗</a>
                <RouterLink :to="`/jobs/${job.id}`">Details</RouterLink>
                <button v-if="auth.canWrite" class="link-btn" title="Delete job" @click="remove(job)">Delete</button>
              </span>
            </span>
          </div>
          <div class="job-line job-line-sub">
            <span class="feat">{{ job.feature }}</span>
          </div>
        </li>
      </ul>

      <div v-if="totalPages > 1 || pageSize !== 10" class="pager">
        <span class="muted">{{ jobs.length }} job{{ jobs.length === 1 ? '' : 's' }}</span>
        <div v-if="totalPages > 1" class="pg-nav">
          <button class="btn pg-btn" :disabled="page <= 1" @click="page--">‹ Prev</button>
          <span class="muted">Page {{ page }} / {{ totalPages }}</span>
          <button class="btn pg-btn" :disabled="page >= totalPages" @click="page++">Next ›</button>
        </div>
        <label class="per-page muted">
          Rows
          <select v-model.number="pageSize" class="select pg-select">
            <option :value="10">10</option>
            <option :value="20">20</option>
            <option :value="50">50</option>
          </select>
        </label>
      </div>
    </template>
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
/* Two-line list rows: each job spans a header line (repo · status · time) and a
   detail line (feature · actions). Everything flexes to the container width, so
   there is no fixed-width table and no horizontal scrollbar. */
.job-list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.job-item {
  padding: 0.85rem 1.1rem;
  border-bottom: 1px solid var(--border);
}
.job-item:last-child {
  border-bottom: none;
}
.job-line {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  min-width: 0;
}
.job-line-sub {
  margin-top: 0.35rem;
}
.repo {
  font-weight: 600;
  color: var(--text);
}
.job-meta {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  flex-shrink: 0;
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.85rem;
  white-space: nowrap;
}
.feat {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-muted);
}
.job-actions {
  flex-shrink: 0;
}
.job-actions a,
.job-actions .router-link-active {
  margin-right: 0.75rem;
}
.nowrap {
  white-space: nowrap;
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
.pager {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 1rem;
  margin-top: 1rem;
  font-size: 0.9rem;
}
.pg-nav {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}
.pg-btn {
  padding: 0.35rem 0.7rem;
}
.per-page {
  display: flex;
  align-items: center;
  gap: 0.4rem;
}
.pg-select {
  width: auto;
  padding: 0.3rem 0.5rem;
}

@media (max-width: 640px) {
  .head {
    flex-wrap: wrap;
    gap: 0.5rem;
  }
  /* The repo name keeps the first line; status · time · actions drop to a second
     line and wrap among themselves rather than blowing out the row width. */
  .job-line {
    flex-wrap: wrap;
    gap: 0.5rem;
  }
  .repo {
    flex: 1 1 100%;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .job-meta {
    width: 100%;
    justify-content: flex-start;
    flex-wrap: wrap;
    gap: 0.4rem 0.75rem;
  }
  .job-actions {
    margin-left: auto;
  }
}
</style>
