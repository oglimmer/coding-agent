<script setup lang="ts">
import { ref } from 'vue'
import { api, errMsg } from '@/api'
import { useAsyncData } from '@/composables/useAsyncData'
import { useConfirm } from '@/composables/useConfirm'
import { useAuthStore } from '@/stores/auth'
import { relativeTime } from '@/lib/format'
import type { User } from '@/types'

const auth = useAuthStore()
const { confirm } = useConfirm()
const { data: users, loading, error, reload } = useAsyncData<User[]>(() => api.listUsers(), [])
const actionError = ref<string | null>(null)

async function grant(u: User) {
  actionError.value = null
  try {
    await api.grantAdmin(u.id)
    await reload()
  } catch (e) {
    actionError.value = errMsg(e)
  }
}

async function revoke(u: User) {
  const ok = await confirm({
    title: 'Revoke admin',
    message: `Remove admin rights from ${u.name}?`,
    confirmText: 'Revoke',
    danger: true,
  })
  if (!ok) return
  actionError.value = null
  try {
    await api.revokeAdmin(u.id)
    await reload()
  } catch (e) {
    actionError.value = errMsg(e)
  }
}
</script>

<template>
  <div class="container">
    <h1>Admin · Users</h1>
    <p class="muted">Grant or revoke admin rights. Admins manage repositories and other admins.</p>

    <p v-if="actionError" class="error-banner">{{ actionError }}</p>
    <p v-if="loading" class="muted">Loading users…</p>
    <p v-else-if="error" class="error-banner">{{ error }}</p>

    <div v-else class="card table-wrap">
      <table>
        <thead>
          <tr>
            <th>Name</th>
            <th>Email</th>
            <th>Role</th>
            <th>Joined</th>
            <th />
          </tr>
        </thead>
        <tbody>
          <tr v-for="u in users" :key="u.id">
            <td>
              {{ u.name }}
              <span v-if="u.id === auth.user?.id" class="muted">(you)</span>
            </td>
            <td class="muted">{{ u.email }}</td>
            <td>
              <span :class="u.isAdmin ? 'role admin' : 'role'">{{ u.isAdmin ? 'Admin' : 'User' }}</span>
            </td>
            <td class="muted nowrap">{{ relativeTime(u.createdAt) }}</td>
            <td class="nowrap">
              <button v-if="!u.isAdmin" class="btn" @click="grant(u)">Make admin</button>
              <button v-else class="btn btn-danger" @click="revoke(u)">Revoke admin</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style scoped>
.table-wrap {
  padding: 0;
  overflow-x: auto;
  margin-top: 1.25rem;
}
.nowrap {
  white-space: nowrap;
}
.role {
  font-size: 0.8rem;
  padding: 0.1rem 0.5rem;
  border-radius: 999px;
  border: 1px solid var(--border);
  color: var(--text-muted);
}
.role.admin {
  color: var(--accent);
  border-color: rgb(var(--accent-rgb) / 0.4);
  background: rgb(var(--accent-rgb) / 0.12);
}
</style>
