import { describe, expect, it } from 'vitest'
import { errorText } from './upload'

describe('errorText', () => {
  it('reads the API error message', () => {
    expect(errorText('{"error":{"code":"too_large","message":"logo is too large"}}', 'Bad Request')).toBe('logo is too large')
  })
  it('falls back for other bodies', () => {
    expect(errorText('<html>', 'Bad Gateway')).toBe('Bad Gateway')
    expect(errorText('{}', '')).toBe('Upload failed')
  })
})
