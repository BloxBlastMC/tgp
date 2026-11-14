import { describe, expect, it } from 'vitest'
import { ApiRequestError, unwrap } from './client'

describe('unwrap', () => {
  it('returns data on success', async () => {
    const data = await unwrap(Promise.resolve({ data: { ok: true }, response: new Response() }))
    expect(data).toEqual({ ok: true })
  })

  it('throws ApiRequestError with the server error code', async () => {
    const p = unwrap(
      Promise.resolve({
        error: { error: { code: 'not_found', message: 'nope' } },
        response: new Response(null, { status: 404 }),
      }),
    )
    await expect(p).rejects.toBeInstanceOf(ApiRequestError)
    await expect(p).rejects.toMatchObject({ code: 'not_found', status: 404, message: 'nope' })
  })
})
