<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { parseSteps } from '@/lib/joblog'

const props = defineProps<{ text: string; live: boolean }>()

const mode = ref<'full' | 'steps'>('full')
const steps = computed(() => parseSteps(props.text))
const hasText = computed(() => props.text.trim().length > 0)

const scroller = ref<HTMLElement | null>(null)
// Follow the tail while output streams, but stop if the user scrolls up to read
// back — re-enabled once they return near the bottom.
const stick = ref(true)

function onScroll() {
  const el = scroller.value
  if (!el) return
  stick.value = el.scrollHeight - el.scrollTop - el.clientHeight < 40
}

async function scrollToBottom() {
  await nextTick()
  const el = scroller.value
  if (el) el.scrollTop = el.scrollHeight
}

watch(
  () => props.text,
  () => {
    if (mode.value === 'full' && stick.value) void scrollToBottom()
  },
)
watch(mode, (m) => {
  if (m === 'full') void scrollToBottom()
})
</script>

<template>
  <section class="log">
    <header class="log-head">
      <div class="title">
        <span v-if="live" class="live-dot" aria-hidden="true" />
        <h2>Progress</h2>
      </div>
      <div class="toggle" role="tablist" aria-label="Log view">
        <button
          type="button"
          role="tab"
          :aria-selected="mode === 'full'"
          :class="{ active: mode === 'full' }"
          @click="mode = 'full'"
        >
          Full log
        </button>
        <button
          type="button"
          role="tab"
          :aria-selected="mode === 'steps'"
          :class="{ active: mode === 'steps' }"
          @click="mode = 'steps'"
        >
          Key steps
        </button>
      </div>
    </header>

    <p v-if="!hasText" class="empty muted">
      {{ live ? 'Waiting for the worker to start…' : 'No worker logs available.' }}
    </p>

    <pre v-else-if="mode === 'full'" ref="scroller" class="full" @scroll="onScroll">{{ text }}</pre>

    <ol v-else class="steps">
      <li v-for="(step, i) in steps" :key="i" :class="{ current: live && i === steps.length - 1 }">
        <span class="marker" aria-hidden="true" />
        <span class="step-text">{{ step }}</span>
      </li>
    </ol>
  </section>
</template>

<style scoped>
.log {
  margin-top: 1.5rem;
}
.log-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  margin-bottom: 0.6rem;
}
.title {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.title h2 {
  margin: 0;
  font-size: 0.95rem;
}
.live-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--accent);
  box-shadow: 0 0 0 0 rgb(var(--accent-rgb) / 0.6);
  animation: pulse 1.6s ease-out infinite;
}
@keyframes pulse {
  0% {
    box-shadow: 0 0 0 0 rgb(var(--accent-rgb) / 0.5);
  }
  70% {
    box-shadow: 0 0 0 7px rgb(var(--accent-rgb) / 0);
  }
  100% {
    box-shadow: 0 0 0 0 rgb(var(--accent-rgb) / 0);
  }
}
.toggle {
  display: inline-flex;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  overflow: hidden;
}
.toggle button {
  padding: 0.3rem 0.7rem;
  border: 0;
  background: var(--surface);
  color: var(--text-muted);
  font-size: 0.8rem;
  font-weight: 600;
  cursor: pointer;
}
.toggle button + button {
  border-left: 1px solid var(--border);
}
.toggle button.active {
  background: var(--accent);
  color: var(--accent-contrast);
}
.empty {
  padding: 1rem;
  border: 1px dashed var(--border);
  border-radius: var(--radius-sm);
  text-align: center;
}
.full {
  margin: 0;
  max-height: 460px;
  overflow: auto;
  padding: 0.85rem 1rem;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.8rem;
  line-height: 1.5;
  /* The worker streams pre-wrapped output; honour its own line breaks and let
     genuinely long lines scroll horizontally rather than re-wrapping them. */
  white-space: pre;
  color: var(--text);
}
.steps {
  list-style: none;
  margin: 0;
  padding: 0.25rem 0 0.25rem 0;
  max-height: 460px;
  overflow: auto;
}
.steps li {
  position: relative;
  display: flex;
  gap: 0.7rem;
  padding: 0.35rem 0 0.35rem 0.2rem;
}
.steps .marker {
  flex: none;
  margin-top: 0.4rem;
  width: 9px;
  height: 9px;
  border-radius: 50%;
  background: var(--success);
}
.steps li.current .marker {
  background: var(--accent);
  animation: pulse 1.6s ease-out infinite;
}
/* Connecting line between markers. */
.steps li::before {
  content: '';
  position: absolute;
  left: calc(0.2rem + 4px);
  top: 1.1rem;
  bottom: -0.35rem;
  width: 1px;
  background: var(--border);
}
.steps li:last-child::before {
  display: none;
}
.step-text {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.82rem;
  line-height: 1.5;
  overflow-wrap: anywhere;
}
.steps li.current .step-text {
  color: var(--text);
  font-weight: 600;
}
</style>
