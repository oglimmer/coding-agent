<script setup lang="ts">
import { useConfirmDialog } from '@/composables/useConfirm'

const { state, respond } = useConfirmDialog()
</script>

<template>
  <Teleport to="body">
    <div v-if="state.open" class="overlay" @click.self="respond(false)">
      <div class="dialog card" role="dialog" aria-modal="true">
        <h3>{{ state.title }}</h3>
        <p class="muted">{{ state.message }}</p>
        <div class="actions">
          <button class="btn" @click="respond(false)">Cancel</button>
          <button class="btn" :class="state.danger ? 'btn-danger' : 'btn-primary'" @click="respond(true)">
            {{ state.confirmText }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 100;
  padding: 1rem;
}
.dialog {
  max-width: 420px;
  width: 100%;
  box-shadow: var(--shadow);
}
.dialog h3 {
  margin-top: 0;
}
.actions {
  display: flex;
  justify-content: flex-end;
  gap: 0.6rem;
  margin-top: 1.25rem;
}
</style>
