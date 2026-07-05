import { ref, type Ref } from 'vue'
import { errMsg } from '@/api'

export interface AsyncData<T> {
  data: Ref<T>
  loading: Ref<boolean>
  error: Ref<string | null>
  reload: () => Promise<void>
}

// useAsyncData wraps a fetcher with loading/error state and a reload trigger.
export function useAsyncData<T>(fetcher: () => Promise<T>, initial: T): AsyncData<T> {
  const data = ref(initial) as Ref<T>
  const loading = ref(true)
  const error = ref<string | null>(null)

  async function reload() {
    loading.value = true
    error.value = null
    try {
      data.value = await fetcher()
    } catch (e) {
      error.value = errMsg(e)
    } finally {
      loading.value = false
    }
  }

  void reload()
  return { data, loading, error, reload }
}
