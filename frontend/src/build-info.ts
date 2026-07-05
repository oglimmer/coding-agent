// Vite-injected build metadata (see vite.config.ts `define`).
declare const __APP_VERSION__: string
declare const __GIT_COMMIT__: string
declare const __BUILD_TIME__: string

export interface BuildInfo {
  version: string
  commit: string
  time: string
}

export function useBuildInfo(): BuildInfo {
  return {
    version: __APP_VERSION__,
    commit: __GIT_COMMIT__,
    time: __BUILD_TIME__,
  }
}
