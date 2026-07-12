<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { toggleTheme } from '@/theme'

const auth = useAuthStore()
const route = useRoute()
const router = useRouter()

// On mobile the links + controls collapse behind a hamburger. Close the panel on
// any navigation so a tapped link doesn't leave the menu hanging open.
const menuOpen = ref(false)
watch(() => route.fullPath, () => (menuOpen.value = false))

function logout() {
  menuOpen.value = false
  auth.clear()
  void router.push('/login')
}
</script>

<template>
  <header class="nav">
    <div class="nav-inner">
      <RouterLink to="/repos" class="brand" @click="menuOpen = false">⚙️ Coding Agent</RouterLink>

      <button
        class="hamburger"
        :aria-expanded="menuOpen"
        aria-label="Toggle navigation menu"
        @click="menuOpen = !menuOpen"
      >
        <span class="bars" :class="{ open: menuOpen }" />
      </button>

      <div class="menu" :class="{ open: menuOpen }">
        <nav class="links">
          <RouterLink to="/repos">Repositories</RouterLink>
          <RouterLink v-if="auth.canWrite" to="/new">New request</RouterLink>
          <RouterLink to="/jobs">Jobs</RouterLink>
          <RouterLink v-if="auth.isAdmin" to="/admin">Admin</RouterLink>
        </nav>
        <div class="right">
          <button class="icon-btn" title="Toggle theme" @click="toggleTheme">🌓</button>
          <span v-if="auth.user" class="who">{{ auth.user.name }}</span>
          <button class="btn" @click="logout">Sign out</button>
        </div>
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
  z-index: 20;
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
  white-space: nowrap;
}
.brand:hover {
  text-decoration: none;
}
.menu {
  display: flex;
  align-items: center;
  gap: 1.5rem;
  flex: 1;
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
  white-space: nowrap;
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
  line-height: 1;
}

/* Hamburger toggle — hidden until the row can no longer hold every link. */
.hamburger {
  display: none;
  position: relative;
  width: 40px;
  height: 40px;
  margin-left: auto;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  cursor: pointer;
}
.bars,
.bars::before,
.bars::after {
  position: absolute;
  left: 50%;
  width: 18px;
  height: 2px;
  border-radius: 2px;
  background: var(--text);
  transition: transform 0.2s ease, background 0.2s ease;
}
.bars {
  top: 50%;
  transform: translate(-50%, -50%);
}
.bars::before,
.bars::after {
  content: '';
  transform: translateX(-50%);
}
.bars::before {
  top: -6px;
}
.bars::after {
  top: 6px;
}
.bars.open {
  background: transparent;
}
.bars.open::before {
  top: 0;
  transform: translateX(-50%) rotate(45deg);
}
.bars.open::after {
  top: 0;
  transform: translateX(-50%) rotate(-45deg);
}

@media (max-width: 640px) {
  .nav-inner {
    gap: 0.75rem;
    padding: 0.6rem 1rem;
  }
  .brand {
    flex: 1;
  }
  .hamburger {
    display: block;
  }
  /* Collapse links + controls into a dropdown panel spanning the full bar. */
  .menu {
    position: absolute;
    top: 100%;
    left: 0;
    right: 0;
    flex-direction: column;
    align-items: stretch;
    gap: 0;
    background: var(--bg-elevated);
    border-bottom: 1px solid var(--border);
    box-shadow: var(--shadow);
    padding: 0.4rem 0;
    display: none;
  }
  .menu.open {
    display: flex;
  }
  .links {
    flex-direction: column;
    gap: 0;
  }
  .links a {
    padding: 0.75rem 1rem;
    border-bottom: none;
    border-left: 3px solid transparent;
  }
  .links a.router-link-active {
    border-bottom: none;
    border-left-color: var(--accent);
    background: var(--surface);
  }
  .right {
    padding: 0.75rem 1rem 0.5rem;
    margin-top: 0.4rem;
    border-top: 1px solid var(--border);
  }
  .who {
    order: -1;
    flex: 1;
  }
}
</style>
