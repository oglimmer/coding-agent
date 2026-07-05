// Theme registry + persistence. Keep the id list in sync with the inline boot
// script in index.html.
export type ThemeId = 'dark' | 'light'

const STORAGE_KEY = 'theme'

export const THEMES: ThemeId[] = ['dark', 'light']

export function getStoredTheme(): ThemeId {
  const t = localStorage.getItem(STORAGE_KEY)
  if (t === 'light' || t === 'dark') return t
  return window.matchMedia?.('(prefers-color-scheme: light)').matches ? 'light' : 'dark'
}

export function applyTheme(theme: ThemeId): void {
  document.documentElement.setAttribute('data-theme', theme)
  localStorage.setItem(STORAGE_KEY, theme)
}

export function toggleTheme(): ThemeId {
  const next: ThemeId = getStoredTheme() === 'dark' ? 'light' : 'dark'
  applyTheme(next)
  return next
}
