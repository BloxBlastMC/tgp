import { describe, expect, it } from 'vitest'
import { filterItems, matchRank } from './filter'

describe('filterItems', () => {
  const items = ['feature/scores', 'main', 'release/1.0', 'maintenance', 'docs-main']
  it('returns everything for an empty query', () => {
    expect(filterItems(items, '  ', (s) => s)).toEqual(items)
  })
  it('ranks exact, prefix, word-start, then substring', () => {
    expect(filterItems(items, 'main', (s) => s)).toEqual(['main', 'maintenance', 'docs-main'])
    expect(filterItems(items, 'sco', (s) => s)).toEqual(['feature/scores'])
    expect(filterItems(items, 'ease', (s) => s)).toEqual(['release/1.0'])
  })
  it('is case-insensitive', () => {
    expect(matchRank('Tom Smith', 'tom')).toBe(1)
    expect(matchRank('Tom Smith', 'smi')).toBe(2)
    expect(matchRank('Tom Smith', 'xyz')).toBe(-1)
  })
})
