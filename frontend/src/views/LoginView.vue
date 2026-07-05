<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, errMsg } from '@/api'
import { useAuthStore } from '@/stores/auth'
import type { AuthConfig } from '@/types'

const auth = useAuthStore()
const route = useRoute()
const router = useRouter()

const config = ref<AuthConfig | null>(null)
const loading = ref(true)
const error = ref<string | null>(null)
const submitting = ref(false)

const username = ref('')
const password = ref('')

onMounted(async () => {
  if (typeof route.query.error === 'string') {
    error.value = `Login failed: ${route.query.error}`
  }
  try {
    config.value = await api.authConfig()
  } catch (e) {
    error.value = errMsg(e)
  } finally {
    loading.value = false
  }
})

function redirectTarget(): string {
  const r = route.query.redirect
  return typeof r === 'string' ? r : '/repos'
}

function startOidc() {
  window.location.href = '/api/auth/oidc/start'
}

async function devLogin() {
  submitting.value = true
  error.value = null
  try {
    await auth.devLogin(username.value, password.value)
    await router.push(redirectTarget())
  } catch (e) {
    error.value = errMsg(e)
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div class="wrap">
    <div class="card login">
      <h1>⚙️ Coding Agent</h1>
      <p class="muted">Sign in to request autonomous feature implementations.</p>

      <div v-if="error" class="error-banner">{{ error }}</div>

      <p v-if="loading" class="muted">Loading…</p>

      <template v-else-if="config">
        <button
          v-if="config.oidcEnabled"
          class="btn btn-primary full"
          @click="startOidc"
        >
          Sign in with SSO
        </button>

        <form v-if="config.mode === 'password'" class="dev" @submit.prevent="devLogin">
          <p class="or muted">Developer login</p>
          <label class="label" for="u">Username</label>
          <input id="u" v-model="username" class="input" autocomplete="username">
          <label class="label" for="p">Password</label>
          <input id="p" v-model="password" type="password" class="input" autocomplete="current-password">
          <button class="btn btn-primary full" :disabled="submitting || !username" type="submit">
            {{ submitting ? 'Signing in…' : 'Sign in' }}
          </button>
        </form>

        <p v-if="!config.oidcEnabled && config.mode !== 'password'" class="muted">
          No login method is configured. Set up OIDC or enable the developer login.
        </p>
      </template>
    </div>
  </div>
</template>

<style scoped>
.wrap {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 1.5rem;
}
.login {
  max-width: 380px;
  width: 100%;
  box-shadow: var(--shadow);
}
.login h1 {
  margin: 0 0 0.25rem;
}
.full {
  width: 100%;
  margin-top: 1rem;
}
.dev {
  margin-top: 1.25rem;
}
.dev .label {
  margin-top: 0.75rem;
}
.or {
  text-align: center;
  font-size: 0.8rem;
  margin: 1.25rem 0 0.5rem;
}
</style>
