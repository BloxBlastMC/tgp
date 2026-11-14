import { describe, expect, it } from 'vitest'
import { AVATAR_HUES, avatarHue, initials } from './avatar'

describe('avatar', () => {
  it('picks a stable hue in range', () => {
    for (const k of ['mia', 'tom', 'owen', 'a', '']) {
      const h = avatarHue(k)
      expect(h).toBeGreaterThanOrEqual(1)
      expect(h).toBeLessThanOrEqual(AVATAR_HUES)
      expect(avatarHue(k)).toBe(h)
    }
    expect(new Set(['mia', 'tom', 'owen', 'alice', 'bob', 'carol'].map(avatarHue)).size).toBeGreaterThan(2)
  })
  it('takes first and last initials', () => {
    expect(initials('Mia Chen')).toBe('MC')
    expect(initials('tom')).toBe('T')
    expect(initials('Anna Maria Lopez')).toBe('AL')
    expect(initials('  ')).toBe('?')
  })
})
