import { describe, expect, it } from 'vitest'
import { age, diskTone, formatUptime, sortChecks, sortRepos, usedPercent, type RepoUsage } from './health'

const repo = (name: string, total: number, pushed_at: string | null = null): RepoUsage => ({
  name,
  archived: false,
  pushed_at,
  git_bytes: total,
  logs_bytes: 0,
  artifacts_bytes: 0,
  caches_bytes: 0,
  release_assets_bytes: 0,
  total_bytes: total,
})

describe('health helpers', () => {
  it('sorts checks worst first, stable within a status', () => {
    const got = sortChecks([
      { name: 'a', status: 'ok' as const },
      { name: 'b', status: 'warn' as const },
      { name: 'c', status: 'fail' as const },
      { name: 'd', status: 'warn' as const },
    ])
    expect(got.map((c) => c.name)).toEqual(['c', 'b', 'd', 'a'])
  })

  it('picks disk tones at 90% and 95%', () => {
    expect(diskTone(89.9)).toBe('ok')
    expect(diskTone(90)).toBe('warn')
    expect(diskTone(95)).toBe('fail')
    expect(usedPercent({ total_bytes: 200, used_bytes: 50 })).toBe(25)
    expect(usedPercent({ total_bytes: 0, used_bytes: 50 })).toBe(0)
  })

  it('formats uptimes', () => {
    expect(formatUptime(42)).toBe('42s')
    expect(formatUptime(125)).toBe('2m')
    expect(formatUptime(3 * 3600 + 4 * 60)).toBe('3h 4m')
    expect(formatUptime(2 * 86400 + 5 * 3600)).toBe('2d 5h')
    expect(age(null)).toBe('—')
    expect(age('2026-01-01T00:00:00Z', Date.parse('2026-01-01T00:01:30Z'))).toBe('1m')
  })

  it('sorts repos by column', () => {
    const items = [repo('b', 10, '2026-01-02T00:00:00Z'), repo('A', 30), repo('c', 20, '2026-01-03T00:00:00Z')]
    expect(sortRepos(items, 'total_bytes', true).map((r) => r.name)).toEqual(['A', 'c', 'b'])
    expect(sortRepos(items, 'name', false).map((r) => r.name)).toEqual(['A', 'b', 'c'])
    expect(sortRepos(items, 'pushed_at', true).map((r) => r.name)).toEqual(['c', 'b', 'A'])
  })
})
