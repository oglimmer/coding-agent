import { describe, expect, it } from 'vitest'
import { relativeTime, statusLabel, statusTone } from './format'

describe('relativeTime', () => {
  const now = new Date('2026-07-05T12:00:00Z').getTime()
  it('handles just now', () => {
    expect(relativeTime('2026-07-05T11:59:30Z', now)).toBe('just now')
  })
  it('handles minutes', () => {
    expect(relativeTime('2026-07-05T11:30:00Z', now)).toBe('30m ago')
  })
  it('handles hours', () => {
    expect(relativeTime('2026-07-05T09:00:00Z', now)).toBe('3h ago')
  })
  it('handles days', () => {
    expect(relativeTime('2026-07-03T12:00:00Z', now)).toBe('2d ago')
  })
  it('returns empty for invalid dates', () => {
    expect(relativeTime('nope', now)).toBe('')
  })
})

describe('statusLabel / statusTone', () => {
  it('maps known statuses', () => {
    expect(statusLabel('success')).toBe('Merged')
    expect(statusTone('success')).toBe('success')
    expect(statusTone('failed')).toBe('danger')
    expect(statusTone('rejected')).toBe('danger')
    expect(statusTone('running')).toBe('accent')
    expect(statusTone('checking')).toBe('warning')
  })
  it('passes through unknown status', () => {
    expect(statusLabel('weird')).toBe('weird')
    expect(statusTone('weird')).toBe('muted')
  })
})
