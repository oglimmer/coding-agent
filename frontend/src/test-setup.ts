// In-memory localStorage shim for Vitest (the app uses bare localStorage).
class LocalStorageMock {
  private store = new Map<string, string>()
  getItem(key: string): string | null {
    return this.store.has(key) ? (this.store.get(key) as string) : null
  }
  setItem(key: string, value: string): void {
    this.store.set(key, String(value))
  }
  removeItem(key: string): void {
    this.store.delete(key)
  }
  clear(): void {
    this.store.clear()
  }
}

const mock = new LocalStorageMock()
Object.defineProperty(globalThis, 'localStorage', { value: mock, writable: true })
if (typeof window !== 'undefined') {
  Object.defineProperty(window, 'localStorage', { value: mock, writable: true })
}
