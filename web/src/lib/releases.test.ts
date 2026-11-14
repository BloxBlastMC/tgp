import { describe, expect, it } from 'vitest'
import { archiveUrl, expiresText, releaseBadges, releaseTitle, tagError, usagePercent } from './releases'

describe('releases helpers', () => {
  it('titles', () => {
    expect(releaseTitle({ name: '', tag: 'v1.0' })).toBe('v1.0')
    expect(releaseTitle({ name: ' Big one ', tag: 'v1.0' })).toBe('Big one')
  })
  it('archive url encodes', () => {
    expect(archiveUrl('game', 'rel/v1', 'zip')).toBe('/api/v1/repos/game/archive?ref=rel%2Fv1&format=zip')
  })
  it('expiry text', () => {
    const now = Date.parse('2026-01-01T00:00:00Z')
    expect(expiresText('2025-12-31T00:00:00Z', now)).toBe('expired')
    expect(expiresText('2026-01-01T00:10:00Z', now)).toBe('expires in 10 min')
    expect(expiresText('2026-01-01T01:00:00Z', now)).toBe('expires in 1 hour')
    expect(expiresText('2026-01-02T05:00:00Z', now)).toBe('expires in 29 hours')
    expect(expiresText('2026-01-31T00:00:00Z', now)).toBe('expires in 30 days')
  })
  it('usage', () => {
    expect(usagePercent(5, 0)).toBeNull()
    expect(usagePercent(5, 10)).toBe(50)
    expect(usagePercent(50, 10)).toBe(100)
  })
  it('badges', () => {
    expect(releaseBadges({ draft: false, prerelease: false, latest: true })).toEqual(['Latest'])
    expect(releaseBadges({ draft: true, prerelease: true, latest: true })).toEqual(['Draft', 'Pre-release'])
  })
  it('tag names', () => {
    expect(tagError('v1.2.3')).toBeNull()
    expect(tagError('release/2026-01')).toBeNull()
    expect(tagError('')).toBe('Enter a tag')
    expect(tagError('a..b')).not.toBeNull()
    expect(tagError('has space')).not.toBeNull()
    expect(tagError('x.lock')).not.toBeNull()
    expect(tagError('-v1')).not.toBeNull()
  })
})
