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

// --- repositories ---

export const reposQuery = queryOptions({
  queryKey: ['repos'],
  queryFn: () => unwrap(api.GET('/repos')),
})

export const repoQuery = (repo: string) =>
  queryOptions({
    queryKey: ['repo', repo],
    queryFn: () => unwrap(api.GET('/repos/{repo}', { params: { path: { repo } } })),
  })

export const branchesQuery = (repo: string) =>
  queryOptions({
    queryKey: ['repo', repo, 'branches'],
    queryFn: () => unwrap(api.GET('/repos/{repo}/branches', { params: { path: { repo } } })),
  })

export const tagsQuery = (repo: string) =>
  queryOptions({
    queryKey: ['repo', repo, 'tags'],
    queryFn: () => unwrap(api.GET('/repos/{repo}/tags', { params: { path: { repo } } })),
  })

type RefPath = { ref?: string; path?: string }

export const treeQuery = (repo: string, { ref, path }: RefPath) =>
  queryOptions({
    queryKey: ['repo', repo, 'tree', ref ?? '', path ?? ''],
    queryFn: () => unwrap(api.GET('/repos/{repo}/tree', { params: { path: { repo }, query: { ref, path } } })),
  })

export const blobQuery = (repo: string, { ref, path }: RefPath & { path: string }) =>
  queryOptions({
    queryKey: ['repo', repo, 'blob', ref ?? '', path],
    queryFn: () => unwrap(api.GET('/repos/{repo}/blob', { params: { path: { repo }, query: { ref, path } } })),
  })

export const readmeQuery = (repo: string, { ref, path }: RefPath) =>
  queryOptions({
    queryKey: ['repo', repo, 'readme', ref ?? '', path ?? ''],
    queryFn: async () => {
      try {
        return await unwrap(api.GET('/repos/{repo}/readme', { params: { path: { repo }, query: { ref, path } } }))
      } catch (e) {
        if (e instanceof ApiRequestError && e.status === 404) return null
        throw e
      }
    },
  })

export const commitsQuery = (repo: string, { ref, path, page }: RefPath & { page: number }) =>
  queryOptions({
    queryKey: ['repo', repo, 'commits', ref ?? '', path ?? '', page],
    queryFn: () =>
      unwrap(api.GET('/repos/{repo}/commits', { params: { path: { repo }, query: { ref, path, page, per_page: 50 } } })),
  })

export const commitQuery = (repo: string, sha: string) =>
  queryOptions({
    queryKey: ['repo', repo, 'commit', sha],
    queryFn: () => unwrap(api.GET('/repos/{repo}/commits/{sha}', { params: { path: { repo, sha } } })),
    staleTime: Infinity,
  })

export const blameQuery = (repo: string, { ref, path }: RefPath & { path: string }) =>
  queryOptions({
    queryKey: ['repo', repo, 'blame', ref ?? '', path],
    queryFn: () => unwrap(api.GET('/repos/{repo}/blame', { params: { path: { repo }, query: { ref, path } } })),
  })

export const repoAccessQuery = (repo: string) =>
  queryOptions({
    queryKey: ['repo', repo, 'access'],
    queryFn: () => unwrap(api.GET('/repos/{repo}/access', { params: { path: { repo } } })),
  })

export const userReposQuery = (username: string) =>
  queryOptions({
    queryKey: ['admin', 'users', username, 'repos'],
    queryFn: () => unwrap(api.GET('/admin/users/{username}/repos', { params: { path: { username } } })),
  })

// --- pull requests ---

export type PullState = 'open' | 'closed' | 'merged' | 'all'

export const usersQuery = queryOptions({
  queryKey: ['users'],
  queryFn: () => unwrap(api.GET('/users')),
  staleTime: 60_000,
})

export const pullsQuery = (repo: string, f: { state: PullState; q?: string; page?: number }) =>
  queryOptions({
    queryKey: ['repo', repo, 'pulls', 'list', f.state, f.q ?? '', f.page ?? 1],
    queryFn: () =>
      unwrap(
        api.GET('/repos/{repo}/pulls', {
          params: { path: { repo }, query: { state: f.state, q: f.q || undefined, page: f.page, per_page: 50 } },
        }),
      ),
  })

export type MyPullsFilter = 'review_requested' | 'authored' | 'all'

export const myPullsQuery = (filter: MyPullsFilter, state: PullState = 'open') =>
  queryOptions({
    queryKey: ['pulls', filter, state],
    queryFn: () => unwrap(api.GET('/pulls', { params: { query: { filter, state, per_page: 100 } } })),
  })

const pullKey = (repo: string, n: number) => ['repo', repo, 'pull', n] as const

export const pullQuery = (repo: string, n: number) =>
  queryOptions({
    queryKey: [...pullKey(repo, n), 'detail'],
    queryFn: () => unwrap(api.GET('/repos/{repo}/pulls/{number}', { params: { path: { repo, number: n } } })),
  })

export const pullTimelineQuery = (repo: string, n: number) =>
  queryOptions({
    queryKey: [...pullKey(repo, n), 'timeline'],
    queryFn: () => unwrap(api.GET('/repos/{repo}/pulls/{number}/timeline', { params: { path: { repo, number: n } } })),
  })

export const pullCommentsQuery = (repo: string, n: number) =>
  queryOptions({
    queryKey: [...pullKey(repo, n), 'comments'],
    queryFn: () => unwrap(api.GET('/repos/{repo}/pulls/{number}/comments', { params: { path: { repo, number: n } } })),
  })

export const pullCommitsQuery = (repo: string, n: number) =>
  queryOptions({
    queryKey: [...pullKey(repo, n), 'commits'],
    queryFn: () => unwrap(api.GET('/repos/{repo}/pulls/{number}/commits', { params: { path: { repo, number: n } } })),
  })

export const pullFilesQuery = (repo: string, n: number) =>
  queryOptions({
    queryKey: [...pullKey(repo, n), 'files'],
    queryFn: () => unwrap(api.GET('/repos/{repo}/pulls/{number}/files', { params: { path: { repo, number: n } } })),
  })

/** Everything about one pull request (detail, timeline, comments, diff). */
export const pullKeyPrefix = pullKey

export const compareQuery = (repo: string, base: string, head: string) =>
  queryOptions({
    queryKey: ['repo', repo, 'compare', base, head],
    queryFn: () => unwrap(api.GET('/repos/{repo}/compare', { params: { path: { repo }, query: { base, head } } })),
    retry: false,
  })

export const protectionsQuery = (repo: string) =>
  queryOptions({
    queryKey: ['repo', repo, 'protections'],
    queryFn: () => unwrap(api.GET('/repos/{repo}/protections', { params: { path: { repo } } })),
  })
