import createClient from 'openapi-fetch'
import type { components, paths } from './schema.gen'

export type Schemas = components['schemas']
export type ApiError = Schemas['Error']['error']

/** Typed client for /api/v1, generated from api/openapi.yaml. */
export const api = createClient<paths>({
  baseUrl: '/api/v1',
  credentials: 'same-origin',
})

/** Thrown by `unwrap` when the API returns an error response. */
export class ApiRequestError extends Error {
  readonly code: string
  readonly status: number
  readonly details?: Record<string, unknown>

  constructor(status: number, err: ApiError) {
    super(err.message)
    this.code = err.code
    this.status = status
    this.details = err.details
  }
}

/** Unwraps an openapi-fetch result, throwing ApiRequestError on failure. */
export async function unwrap<T>(
  p: Promise<{ data?: T; error?: unknown; response: Response }>,
): Promise<T> {
  const { data, error, response } = await p
  if (error !== undefined || data === undefined) {
    const e = (error as { error?: ApiError } | undefined)?.error
    throw new ApiRequestError(response.status, e ?? { code: 'unknown', message: response.statusText })
  }
  return data
}
