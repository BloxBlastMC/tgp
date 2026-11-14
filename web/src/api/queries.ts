import { queryOptions } from '@tanstack/react-query'
import { api, ApiRequestError, unwrap, type Session } from './client'

export const metaQuery = queryOptions({
  queryKey: ['meta'],
  queryFn: () => unwrap(api.GET('/meta')),
  staleTime: 60_000,
})

/** The current session, or null when not logged in. */
export const sessionQuery = queryOptions({
  queryKey: ['session'],
  queryFn: async (): Promise<Session | null> => {
    try {
      return await unwrap(api.GET('/auth/session'))
    } catch (e) {
      if (e instanceof ApiRequestError && (e.status === 401 || e.code === 'setup_required')) return null
      throw e
    }
  },
  staleTime: 30_000,
})

export const orgQuery = queryOptions({
  queryKey: ['org'],
  queryFn: () => unwrap(api.GET('/org')),
})

export const adminUsersQuery = queryOptions({
  queryKey: ['admin', 'users'],
  queryFn: () => unwrap(api.GET('/admin/users')),
})

export const tokensQuery = queryOptions({
  queryKey: ['account', 'tokens'],
  queryFn: () => unwrap(api.GET('/account/tokens')),
})
