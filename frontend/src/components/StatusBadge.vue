<script setup lang="ts">
import { computed } from 'vue'
import { statusLabel, statusTone } from '@/lib/format'

const props = defineProps<{ status: string }>()

const label = computed(() => statusLabel(props.status))
const tone = computed(() => statusTone(props.status))
</script>

<template>
  <span class="badge" :class="`tone-${tone}`">
    <span class="dot" />
    {{ label }}
  </span>
</template>

<style scoped>
.badge {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  padding: 0.15rem 0.6rem;
  border-radius: 999px;
  font-size: 0.78rem;
  font-weight: 600;
  border: 1px solid var(--border);
}
.dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: currentColor;
}
.tone-success {
  color: var(--success);
  background: rgb(var(--success-rgb) / 0.12);
  border-color: rgb(var(--success-rgb) / 0.35);
}
.tone-danger {
  color: var(--danger);
  background: rgb(var(--danger-rgb) / 0.12);
  border-color: rgb(var(--danger-rgb) / 0.35);
}
.tone-accent {
  color: var(--accent);
  background: rgb(var(--accent-rgb) / 0.12);
  border-color: rgb(var(--accent-rgb) / 0.35);
}
.tone-warning {
  color: var(--warning);
  background: rgb(var(--warning-rgb) / 0.12);
  border-color: rgb(var(--warning-rgb) / 0.35);
}
.tone-muted {
  color: var(--text-muted);
}
</style>
