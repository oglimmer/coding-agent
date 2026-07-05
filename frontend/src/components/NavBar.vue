<script setup lang="ts">
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { toggleTheme } from '@/theme'

const auth = useAuthStore()
const router = useRouter()

function logout() {
  auth.clear()
  void router.push('/login')
}
</script>

<template>
  <header class="nav">
    <div class="nav-inner">
      <RouterLink to="/repos" class="brand">⚙️ Coding Agent</RouterLink>
      <nav class="links">
        <RouterLink to="/repos">Repositories</RouterLink>
        <RouterLink to="/new">New request</RouterLink>
        <RouterLink to="/jobs">Jobs</RouterLink>
        <RouterLink v-if="auth.isAdmin" to="/admin">Admin</RouterLink>
      </nav>
      <div class="right">
        <button class="icon-btn" title="Toggle theme" @click="toggleTheme">🌓</button>
        <span v-if="auth.user" class="who">{{ auth.user.name }}</span>
        <button class="btn" @click="logout">Sign out</button>
      </div>
    </div>
  </header>
</template>

<style scoped>
.nav {
  background: var(--bg-elevated);
  border-bottom: 1px solid var(--border);
  position: sticky;
  top: 0;
  z-index: 10;
}
.nav-inner {
  max-width: 1100px;
  margin: 0 auto;
  padding: 0.7rem 1.5rem;
  display: flex;
  align-items: center;
  gap: 1.5rem;
}
.brand {
  font-weight: 700;
  color: var(--text);
}
.brand:hover {
  text-decoration: none;
}
.links {
  display: flex;
  gap: 1rem;
  flex: 1;
}
.links a {
  color: var(--text-muted);
  font-weight: 500;
  padding: 0.25rem 0;
  border-bottom: 2px solid transparent;
}
.links a:hover {
  color: var(--text);
  text-decoration: none;
}
.links a.router-link-active {
  color: var(--text);
  border-bottom-color: var(--accent);
}
.right {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}
.who {
  color: var(--text-muted);
  font-size: 0.9rem;
}
.icon-btn {
  background: none;
  border: none;
  cursor: pointer;
  font-size: 1.1rem;
}
@media (max-width: 640px) {
  .who {
    display: none;
  }
  .nav-inner {
    gap: 0.75rem;
    padding: 0.7rem 1rem;
  }
}
</style>
