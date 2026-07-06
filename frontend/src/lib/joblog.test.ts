import { describe, expect, it } from 'vitest'
import { parseSteps } from './joblog'

describe('parseSteps', () => {
  it('extracts === banner === lines and drops the markers', () => {
    const log = [
      '=== coding-agent worker: cloning oglimmer/example ===',
      'Cloning into ...',
      'remote: Enumerating objects',
      '=== scoping the task (deepseek-chat) ===',
      '{"files": [...]}',
      '=== opening pull request ===',
    ].join('\n')
    expect(parseSteps(log)).toEqual([
      'coding-agent worker: cloning oglimmer/example',
      'scoping the task (deepseek-chat)',
      'opening pull request',
    ])
  })

  it('ignores non-banner output', () => {
    expect(parseSteps('just some aider output\nrunning tests\n=== not a banner')).toEqual([])
  })

  it('collapses consecutive repeats of a step, keeping the latest detail', () => {
    const log = [
      '=== waiting for review (checks 0/2, verdict=none, reviewed=no) ===',
      '=== waiting for review (checks 1/2, verdict=none, reviewed=no) ===',
      '=== waiting for review (checks 2/2, verdict=none, reviewed=yes) ===',
      '=== review ready: verdict=none ===',
    ].join('\n')
    expect(parseSteps(log)).toEqual([
      'waiting for review (checks 2/2, verdict=none, reviewed=yes)',
      'review ready: verdict=none',
    ])
  })

  it('keeps interleaved iterations distinct', () => {
    const log = [
      '=== findings to address (round 1/3) ===',
      '=== waiting for review (checks 1/1) ===',
      '=== findings to address (round 2/3) ===',
    ].join('\n')
    expect(parseSteps(log)).toEqual([
      'findings to address (round 1/3)',
      'waiting for review (checks 1/1)',
      'findings to address (round 2/3)',
    ])
  })

  it('returns an empty list for empty input', () => {
    expect(parseSteps('')).toEqual([])
  })
})
