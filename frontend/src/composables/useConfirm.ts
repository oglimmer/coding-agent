import { reactive, readonly } from 'vue'

interface ConfirmOptions {
  title: string
  message: string
  confirmText?: string
  danger?: boolean
}

interface ConfirmState extends ConfirmOptions {
  open: boolean
}

const state = reactive<ConfirmState>({
  open: false,
  title: '',
  message: '',
  confirmText: 'Confirm',
  danger: false,
})

let resolver: ((ok: boolean) => void) | null = null

// useConfirm is for callers: returns a promise resolving to the user's choice.
export function useConfirm() {
  function confirm(options: ConfirmOptions): Promise<boolean> {
    state.title = options.title
    state.message = options.message
    state.confirmText = options.confirmText ?? 'Confirm'
    state.danger = options.danger ?? false
    state.open = true
    return new Promise((resolve) => {
      resolver = resolve
    })
  }
  return { confirm }
}

// useConfirmDialog is for the single <ConfirmDialog> instance in App.vue.
export function useConfirmDialog() {
  function respond(ok: boolean) {
    state.open = false
    resolver?.(ok)
    resolver = null
  }
  return { state: readonly(state), respond }
}
