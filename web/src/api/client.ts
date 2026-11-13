import createClient, { type Middleware } from 'openapi-fetch'
import type { components, paths } from './schema.gen'

export type Schemas = components['schemas']
export type ApiError = Schemas['Error']['error']
export type User = Schemas['User']
export type Session = Schemas['Session']

function readCookie(name: string): string {
  const m = document.cookie.match(new RegExp('(?:^|; )' + name + '=([^;]*)'))
  return m ? decodeURIComponent(m[1] ?? '') : ''
}

/** Echo the CSRF cookie on unsafe requests (double-submit protection). */
const csrf: Middleware = {
  onRequest({ request }) {
    if (!['GET', 'HEAD', 'OPTIONS'].includes(request.method)) {
      request.headers.set('X-CSRF-Token', readCookie('tgp_csrf'))
    }
    return request
  },
}

/** Typed client for /api/v1, generated from api/openapi.yaml. */
export const api = createClient<paths>({
  baseUrl: '/api/v1',
  credentials: 'same-origin',
})
api.use(csrf)

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
  if (error !== undefined || (data === undefined && response.status !== 204)) {
    const e = (error as { error?: ApiError } | undefined)?.error
    throw new ApiRequestError(response.status, e ?? { code: 'unknown', message: response.statusText || 'Request failed' })
  }
  return data as T
}

/** Makes sure the CSRF cookie exists before the first unsafe request. */
export async function ensureCsrf(): Promise<void> {
  if (!readCookie('tgp_csrf')) await api.GET('/meta')
}

export function errorMessage(e: unknown): string {
  if (e instanceof Error) return e.message
  return String(e)
}

export type Repo = Schemas['Repo']
export type Diff = Schemas['Diff']
export type DiffFile = Schemas['DiffFile']
export type Commit = Schemas['Commit']
export type PullRequest = Schemas['PullRequest']
export type PullSummary = Schemas['PullSummary']
export type Comment = Schemas['Comment']
export type Review = Schemas['Review']
export type TimelineItem = Schemas['TimelineItem']
export type PullEvent = Schemas['PullEvent']
export type UserSummary = Schemas['UserSummary']
export type BranchProtection = Schemas['BranchProtection']
