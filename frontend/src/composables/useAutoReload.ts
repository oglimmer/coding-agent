import { onScopeDispose } from 'vue'

// useAutoReload calls fn every intervalMs until the scope is disposed.
export function useAutoReload(fn: () => void, intervalMs = 5000): void {
  const id = setInterval(fn, intervalMs)
  onScopeDispose(() => clearInterval(id))
}
