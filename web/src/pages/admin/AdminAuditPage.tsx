import { useInfiniteQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { api, errorMessage, unwrap } from '../../api/client'
import { Alert, Badge, Button, Input, Select, Spinner, Time } from '../../components/ui'

const actionGroups = [
  ['', 'All actions'],
  ['auth.', 'Sign-ins'],
  ['auth.login_failed', 'Failed sign-ins'],
  ['user.', 'User management'],
  ['account.', 'Account security'],
  ['token.', 'Access tokens'],
  ['org.', 'Organization'],
  ['setup.', 'Setup'],
] as const

function tone(action: string) {
  if (action.endsWith('failed')) return 'danger' as const
  if (action.startsWith('user.') || action.startsWith('org.') || action.startsWith('setup.')) return 'warning' as const
  return 'neutral' as const
}

export function AdminAuditPage() {
  const [action, setAction] = useState('')
  const [actor, setActor] = useState('')
  const q = useInfiniteQuery({
    queryKey: ['admin', 'audit', action, actor],
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET('/admin/audit-log', {
          params: { query: { limit: 50, ...(pageParam ? { before: pageParam } : {}), ...(action ? { action } : {}), ...(actor ? { actor } : {}) } },
        }),
      ),
    initialPageParam: 0,
    getNextPageParam: (last) => last.next_before ?? undefined,
  })
  const entries = q.data?.pages.flatMap((p) => p.entries) ?? []

  return (
    <div className="flex flex-col">
      <div className="flex items-center gap-2 border-b border-border px-4 py-2">
        <Select value={action} onChange={(e) => setAction(e.target.value)} className="h-7 w-48" aria-label="Filter by action">
          {actionGroups.map(([v, l]) => (
            <option key={v} value={v}>
              {l}
            </option>
          ))}
        </Select>
        <Input value={actor} onChange={(e) => setActor(e.target.value.trim())} placeholder="Actor username" className="h-7 w-48" aria-label="Filter by actor" />
      </div>
      {q.isPending ? (
        <Spinner />
      ) : q.isError ? (
        <div className="p-4">
          <Alert>{errorMessage(q.error)}</Alert>
        </div>
      ) : entries.length === 0 ? (
        <p className="p-8 text-center text-fg-muted">No matching entries.</p>
      ) : (
        <table className="w-full text-left">
          <thead className="text-xs tracking-wide text-fg-faint uppercase">
            <tr className="border-b border-border">
              <th className="px-4 py-1.5 font-medium">When</th>
              <th className="px-2 py-1.5 font-medium">Actor</th>
              <th className="px-2 py-1.5 font-medium">Action</th>
              <th className="px-2 py-1.5 font-medium">Target</th>
              <th className="px-2 py-1.5 font-medium">Details</th>
              <th className="px-4 py-1.5 font-medium">IP</th>
            </tr>
          </thead>
          <tbody>
            {entries.map((e) => (
              <tr key={e.id} className="h-9 border-b border-border align-middle hover:bg-bg-subtle">
                <td className="px-4 whitespace-nowrap text-fg-muted">
                  <Time iso={e.created_at} />
                </td>
                <td className="px-2 font-mono text-sm">{e.actor || <span className="text-fg-faint">anonymous</span>}</td>
                <td className="px-2">
                  <Badge tone={tone(e.action)}>{e.action}</Badge>
                </td>
                <td className="px-2">{e.target}</td>
                <td className="max-w-xs truncate px-2 font-mono text-xs text-fg-faint" title={JSON.stringify(e.details)}>
                  {Object.keys(e.details).length ? JSON.stringify(e.details) : ''}
                </td>
                <td className="px-4 font-mono text-sm text-fg-faint">{e.ip}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {q.hasNextPage && (
        <div className="flex justify-center p-3">
          <Button onClick={() => void q.fetchNextPage()} loading={q.isFetchingNextPage}>
            Load more
          </Button>
        </div>
      )}
    </div>
  )
}
