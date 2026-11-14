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

// --- issues, labels, milestones ---

export type IssueFilters = {
  state?: 'open' | 'closed' | 'all'
  labels?: string
  milestone?: string
  assignee?: string
  author?: string
  q?: string
  sort?: 'updated' | 'created' | 'comments'
  page?: number
  per_page?: number
}

export const issuesQuery = (repo: string, f: IssueFilters) =>
  queryOptions({
    queryKey: ['repo', repo, 'issues', 'list', f],
    queryFn: () => unwrap(api.GET('/repos/{repo}/issues', { params: { path: { repo }, query: { per_page: 50, ...f } } })),
  })

export type MyIssuesFilter = 'assigned' | 'created' | 'all'

export const myIssuesQuery = (filter: MyIssuesFilter, state: 'open' | 'closed' | 'all' = 'open') =>
  queryOptions({
    queryKey: ['issues', filter, state],
    queryFn: () => unwrap(api.GET('/issues', { params: { query: { filter, state, per_page: 100 } } })),
  })

const issueKey = (repo: string, n: number) => ['repo', repo, 'issue', n] as const
export const issueKeyPrefix = issueKey

export const issueQuery = (repo: string, n: number) =>
  queryOptions({
    queryKey: [...issueKey(repo, n), 'detail'],
    queryFn: () => unwrap(api.GET('/repos/{repo}/issues/{number}', { params: { path: { repo, number: n } } })),
    retry: false,
  })

export const issueTimelineQuery = (repo: string, n: number) =>
  queryOptions({
    queryKey: [...issueKey(repo, n), 'timeline'],
    queryFn: () => unwrap(api.GET('/repos/{repo}/issues/{number}/timeline', { params: { path: { repo, number: n } } })),
  })

export const labelsQuery = (repo: string) =>
  queryOptions({
    queryKey: ['repo', repo, 'labels'],
    queryFn: () => unwrap(api.GET('/repos/{repo}/labels', { params: { path: { repo } } })),
  })

export const milestonesQuery = (repo: string, state: 'open' | 'closed' | 'all' = 'open') =>
  queryOptions({
    queryKey: ['repo', repo, 'milestones', state],
    queryFn: () => unwrap(api.GET('/repos/{repo}/milestones', { params: { path: { repo }, query: { state } } })),
  })

// --- CI ---

export type RunFilters = {
  workflow?: string
  branch?: string
  status?: string
  event?: 'push' | 'pull_request' | 'workflow_dispatch'
  actor?: string
  page?: number
  per_page?: number
}

type RunStatusParam = NonNullable<NonNullable<import('./schema.gen').operations['listRuns']['parameters']['query']>['status']>

export const runsQuery = (repo: string, f: RunFilters) =>
  queryOptions({
    queryKey: ['repo', repo, 'runs', 'list', f],
    queryFn: () =>
      unwrap(
        api.GET('/repos/{repo}/runs', {
          params: { path: { repo }, query: { per_page: 30, ...f, status: f.status as RunStatusParam | undefined } },
        }),
      ),
  })

export const allRunsQuery = (f: { status?: string; actor?: string; page?: number }) =>
  queryOptions({
    queryKey: ['runs', f],
    queryFn: () => unwrap(api.GET('/runs', { params: { query: { per_page: 50, ...f, status: f.status as RunStatusParam | undefined } } })),
  })

export const runQuery = (repo: string, n: number) =>
  queryOptions({
    queryKey: ['repo', repo, 'run', n],
    queryFn: () => unwrap(api.GET('/repos/{repo}/runs/{number}', { params: { path: { repo, number: n } } })),
    retry: false,
  })

export const workflowsQuery = (repo: string, ref?: string) =>
  queryOptions({
    queryKey: ['repo', repo, 'workflows', ref ?? ''],
    queryFn: () => unwrap(api.GET('/repos/{repo}/workflows', { params: { path: { repo }, query: { ref } } })),
  })

export const repoSecretsQuery = (repo: string) =>
  queryOptions({
    queryKey: ['repo', repo, 'secrets'],
    queryFn: () => unwrap(api.GET('/repos/{repo}/secrets', { params: { path: { repo } } })),
  })

export const orgSecretsQuery = queryOptions({
  queryKey: ['admin', 'secrets'],
  queryFn: () => unwrap(api.GET('/admin/secrets')),
})

export const runnersQuery = queryOptions({
  queryKey: ['admin', 'runners'],
  queryFn: () => unwrap(api.GET('/admin/runners')),
  refetchInterval: 10_000,
})

export const ciSettingsQuery = queryOptions({
  queryKey: ['admin', 'ci', 'settings'],
  queryFn: () => unwrap(api.GET('/admin/ci/settings')),
})
