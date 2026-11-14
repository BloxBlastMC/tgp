import { queryOptions } from '@tanstack/react-query'
import { api, unwrap } from './client'

export const metaQuery = queryOptions({
  queryKey: ['meta'],
  queryFn: () => unwrap(api.GET('/meta')),
  staleTime: 60_000,
})
