import { describe, expect, it } from 'vitest'
import { computePosition } from './position'

const vp = { width: 1000, height: 800 }
const anchor = { top: 100, left: 100, width: 80, height: 28 }

describe('computePosition', () => {
  it('places below, aligned to the start', () => {
    expect(computePosition(anchor, { width: 200, height: 100 }, 'bottom-start', vp)).toEqual({ top: 132, left: 100, side: 'bottom' })
  })
  it('aligns to the end', () => {
    expect(computePosition(anchor, { width: 200, height: 100 }, 'bottom-end', vp).left).toBe(8)
  })
  it('flips to the top when there is no room below', () => {
    const low = { ...anchor, top: 740 }
    expect(computePosition(low, { width: 200, height: 100 }, 'bottom-start', vp)).toMatchObject({ top: 636, side: 'top' })
  })
  it('clamps into the viewport', () => {
    const right = { ...anchor, left: 950 }
    expect(computePosition(right, { width: 200, height: 100 }, 'bottom-start', vp).left).toBe(792)
  })
})
