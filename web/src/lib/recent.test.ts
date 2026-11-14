import { describe, expect, it } from 'vitest'
import { mergeVisit, pickRecent } from './recent'

describe('recent repositories', () => {
  it('moves a visit to the front without duplicates', () => {
    expect(mergeVisit(['a', 'b', 'c'], 'b')).toEqual(['b', 'a', 'c'])
    expect(mergeVisit(['a', 'B'], 'b')).toEqual(['b', 'a'])
    expect(mergeVisit(['a', 'b', 'c'], 'd', 3)).toEqual(['d', 'a', 'b'])
  })
  it('shows visited repos that still exist, then the most recently pushed', () => {
    const repos = [
      { name: 'game', pushed_at: '2026-11-01T00:00:00Z' },
      { name: 'lobby', pushed_at: '2026-11-10T00:00:00Z' },
      { name: 'web', pushed_at: null },
      { name: 'docs', pushed_at: '2026-11-05T00:00:00Z' },
    ]
    expect(pickRecent(['web', 'gone', 'game'], repos, 3).map((r) => r.name)).toEqual(['web', 'game', 'lobby'])
    expect(pickRecent([], repos, 2).map((r) => r.name)).toEqual(['lobby', 'docs'])
  })
})
