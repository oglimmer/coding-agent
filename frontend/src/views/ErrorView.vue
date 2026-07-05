<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ code?: string }>()

const messages: Record<string, string> = {
  '403': "You don't have permission to view this page.",
  '404': 'That page could not be found.',
  '500': 'Something went wrong on our side. Please try again.',
}

const code = computed(() => props.code ?? '404')
const message = computed(() => messages[code.value] ?? 'An unexpected error occurred.')
</script>

<template>
  <div class="wrap">
    <div class="card box">
      <div class="code">{{ code }}</div>
      <p>{{ message }}</p>
      <RouterLink to="/repos" class="btn btn-primary">Back to app</RouterLink>
    </div>
  </div>
</template>

<style scoped>
.wrap {
  min-height: 70vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 1.5rem;
}
.box {
  text-align: center;
  max-width: 380px;
}
.code {
  font-size: 3rem;
  font-weight: 800;
  color: var(--accent);
}
.box .btn {
  margin-top: 1rem;
}
</style>
