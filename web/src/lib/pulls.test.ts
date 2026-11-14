import { describe, expect, it } from 'vitest'
import type { Comment, PullEvent } from '../api/client'
import { anchorThreads, canBypass, commentAnchor, eventText, groupThreads, lineKey, parseCompare } from './pulls'

const user = { id: 1, username: 'tom', display_name: 'Tom', role: 'tester' as const }
let id = 0
function c(over: Partial<Comment>): Comment {
  id++
  return { id, author: user, body: 'x', body_html: '<p>x</p>', created_at: '', updated_at: '', edited: false, ...over }
}

describe('groupThreads', () => {
  it('groups replies under their inline root and skips conversation comments', () => {
    const root = c({ path: 'a.go', line: 3, side: 'new' })
    const other = c({ path: 'b.go', line: 1, side: 'old' })
    const plain = c({})
    const r1 = c({ reply_to: root.id, path: 'a.go', line: 3 })
    const orphan = c({ reply_to: 999 })
    const threads = groupThreads([root, other, plain, r1, orphan])
    expect(threads.map((t) => t.root.id)).toEqual([root.id, other.id])
    expect(threads[0]!.replies.map((r) => r.id)).toEqual([r1.id])
    expect(threads[1]!.replies).toEqual([])
  })
})

describe('anchorThreads', () => {
  it('keys threads by side, line and path and leaves out outdated ones', () => {
    const a = c({ path: 'a.go', line: 3, side: 'new' })
    const b = c({ path: 'a.go', line: 3, side: 'new' })
    const old = c({ path: 'a.go', line: 2, side: 'old' })
    const stale = c({ path: 'a.go', line: 4, side: 'new', outdated: true })
    const m = anchorThreads(groupThreads([a, b, old, stale]))
    expect(m.get(lineKey('a.go', 'new', 3))?.length).toBe(2)
    expect(m.get(lineKey('a.go', 'old', 2))?.length).toBe(1)
    expect(m.has(lineKey('a.go', 'new', 4))).toBe(false)
  })
})

describe('commentAnchor', () => {
  it('uses the old side for deletions and the new side otherwise', () => {
    expect(commentAnchor({ type: 'del', old_line: 5 })).toEqual({ side: 'old', line: 5 })
    expect(commentAnchor({ type: 'add', new_line: 7 })).toEqual({ side: 'new', line: 7 })
    expect(commentAnchor({ type: 'context', old_line: 4, new_line: 6 })).toEqual({ side: 'new', line: 6 })
    expect(commentAnchor({ type: 'add' })).toBeNull()
  })
})

describe('parseCompare', () => {
  it('splits base...head, decoding slashes in branch names', () => {
    expect(parseCompare('main...feat')).toEqual({ base: 'main', head: 'feat' })
    expect(parseCompare('main...feat%2Fnew-ui')).toEqual({ base: 'main', head: 'feat/new-ui' })
    expect(parseCompare('release/1.0...fix/x')).toEqual({ base: 'release/1.0', head: 'fix/x' })
    expect(parseCompare('feat')).toEqual({ head: 'feat' })
    expect(parseCompare('...feat')).toEqual({ base: undefined, head: 'feat' })
    expect(parseCompare(undefined)).toEqual({})
  })
})

describe('eventText', () => {
  const ev = (type: PullEvent['type'], data: Record<string, unknown>): PullEvent => ({ id: 1, type, data, created_at: '' })
  it('describes pushes and force pushes', () => {
    expect(eventText(ev('pushed', { commits: 1, forced: false }))).toBe('pushed 1 commit')
    expect(eventText(ev('pushed', { commits: 3, forced: false }))).toBe('pushed 3 commits')
    expect(eventText(ev('pushed', { forced: true, before: 'aaaaaaaaaa', after: 'bbbbbbbbbb' }))).toBe('force-pushed the head from aaaaaaa to bbbbbbb')
  })
  it('describes merges', () => {
    expect(eventText(ev('merged', { method: 'squash', commit: '1234567890' }))).toBe('merged this (squash) as 1234567')
    expect(eventText(ev('merged', { method: 'manual', commit: '1234567890' }))).toContain('by pushing 1234567')
  })
})

describe('canBypass', () => {
  it('needs at least one blocker, all bypassable', () => {
    expect(canBypass({ blockers: [] })).toBe(false)
    expect(canBypass({ blockers: [{ code: 'approvals', message: '', bypassable: true }] })).toBe(true)
    expect(
      canBypass({
        blockers: [
          { code: 'approvals', message: '', bypassable: true },
          { code: 'conflict', message: '', bypassable: false },
        ],
      }),
    ).toBe(false)
  })
})
