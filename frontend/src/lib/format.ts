// Pure display helpers.

export function relativeTime(iso: string, now: number = Date.now()): string {
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return ''
  const diff = Math.round((now - then) / 1000)
  if (diff < 60) return 'just now'
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`
  return `${Math.floor(diff / 86400)}d ago`
}

const STATUS_LABELS: Record<string, string> = {
  checking: 'Checking',
  rejected: 'Rejected',
  running: 'Running',
  success: 'Merged',
  failed: 'Failed',
}

export function statusLabel(status: string): string {
  return STATUS_LABELS[status] ?? status
}

export function statusTone(status: string): 'accent' | 'success' | 'danger' | 'warning' | 'muted' {
  switch (status) {
    case 'success':
      return 'success'
    case 'failed':
    case 'rejected':
      return 'danger'
    case 'running':
      return 'accent'
    case 'checking':
      return 'warning'
    default:
      return 'muted'
  }
}
