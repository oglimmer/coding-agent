<script setup lang="ts">
import { ref } from 'vue'
import { api, errMsg } from '@/api'
import { useAsyncData } from '@/composables/useAsyncData'
import { useConfirm } from '@/composables/useConfirm'
import { useAuthStore } from '@/stores/auth'
import { relativeTime } from '@/lib/format'
import type { Role, User } from '@/types'

const auth = useAuthStore()
const { confirm } = useConfirm()
const { data: users, loading, error, reload } = useAsyncData<User[]>(() => api.listUsers(), [])
const actionError = ref<string | null>(null)
const busyId = ref<string | null>(null)

const ROLES: { value: Role; label: string }[] = [
  { value: 'viewer', label: 'Viewer' },
  { value: 'user', label: 'User' },
  { value: 'admin', label: 'Admin' },
]

async function changeRole(u: User, role: Role) {
  if (role === u.role) return

  if (u.role === 'admin' && role !== 'admin') {
    const ok = await confirm({
      title: 'Demote admin',
      message: `Remove admin rights from ${u.name}?`,
      confirmText: 'Demote',
      danger: true,
    })
    if (!ok) {
      await reload() // reset the select back to the current value
      return
    }
  }

  actionError.value = null
  busyId.value = u.id
  try {
    await api.setUserRole(u.id, role)
    await reload()
  } catch (e) {
    actionError.value = errMsg(e)
    await reload()
  } finally {
    busyId.value = null
  }
}
</script>

<template>
  <div class="container">
    <h1>Admin · Users</h1>
    <p class="muted">
      New accounts start as <strong>viewers</strong> (read-only). Promote to <strong>user</strong> to let them
      submit feature requests, or <strong>admin</strong> to also manage repositories and other users.
    </p>

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
              <select
                class="role-select"
                :value="u.role"
                :disabled="busyId === u.id"
                @change="changeRole(u, ($event.target as HTMLSelectElement).value as Role)"
              >
                <option v-for="r in ROLES" :key="r.value" :value="r.value">{{ r.label }}</option>
              </select>
            </td>
            <td class="muted nowrap">{{ relativeTime(u.createdAt) }}</td>
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
.role-select {
  padding: 0.25rem 0.5rem;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg);
  color: var(--text);
  font-size: 0.85rem;
}
</style>
