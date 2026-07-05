<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { errMsg } from '@/api'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const router = useRouter()
const error = ref<string | null>(null)

onMounted(async () => {
  // The backend redirects here with the session token in the URL fragment.
  const params = new URLSearchParams(window.location.hash.replace(/^#/, ''))
  const token = params.get('token')
  if (!token) {
    error.value = 'No session token returned from the identity provider.'
    return
  }
  auth.setSession(token, null)
  try {
    await auth.ensureFreshUser()
    await router.replace('/repos')
  } catch (e) {
    auth.clear()
    error.value = errMsg(e)
  }
})
</script>

<template>
  <div class="wrap">
    <div v-if="error" class="card">
      <div class="error-banner">{{ error }}</div>
      <RouterLink to="/login" class="btn" style="margin-top: 1rem">Back to login</RouterLink>
    </div>
    <p v-else class="muted">Completing sign-in…</p>
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
.card {
  max-width: 380px;
}
</style>
