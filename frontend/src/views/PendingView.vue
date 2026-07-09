<script setup lang="ts">
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const router = useRouter()

function logout() {
  auth.clear()
  void router.push('/login')
}
</script>

<template>
  <div class="wrap">
    <div class="card box">
      <div class="icon">⏳</div>
      <h1>Access pending</h1>
      <p class="muted">
        Your account doesn't have access yet. Please wait for an administrator to
        grant you permissions.
      </p>
      <p v-if="auth.user" class="who muted">
        Signed in as <strong>{{ auth.user.email }}</strong>
      </p>
      <button class="btn" @click="logout">Sign out</button>
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
.box {
  text-align: center;
  max-width: 400px;
  width: 100%;
  box-shadow: var(--shadow);
}
.icon {
  font-size: 2.5rem;
}
.box h1 {
  margin: 0.5rem 0 0.5rem;
}
.who {
  margin-top: 0.75rem;
  font-size: 0.9rem;
}
.box .btn {
  margin-top: 1.25rem;
}
</style>
