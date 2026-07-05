<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import NavBar from '@/components/NavBar.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import { useBuildInfo } from '@/build-info'

const route = useRoute()
const showChrome = computed(() => !route.meta.hideChrome)
const build = useBuildInfo()
</script>

<template>
  <NavBar v-if="showChrome" />
  <main>
    <RouterView />
  </main>
  <footer v-if="showChrome" class="foot">
    <span class="muted">Coding Agent · v{{ build.version }} · {{ build.commit.slice(0, 7) }}</span>
  </footer>
  <ConfirmDialog />
</template>

<style scoped>
.foot {
  max-width: 1100px;
  margin: 2rem auto 1rem;
  padding: 1rem 1.5rem;
  border-top: 1px solid var(--border);
  font-size: 0.82rem;
}
</style>
