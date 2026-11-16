import { useInfiniteQuery } from '@tanstack/react-query'
import { ChevronDown, ChevronRight, Filter, History } from 'lucide-react'
import { Fragment, useState } from 'react'
import { SubHeader } from '../../components/Page'
import { api, unwrap } from '../../api/client'
import { auditActionLabel, auditActionTone } from '../../lib/labels'
import { Badge, Button, cx, DescriptionList, Dot, EmptyState, ErrorState, FilterBar, FilterMenu, SearchInput, SkeletonRows, Table, TD, TH, THead, Time, Tooltip, TR } from '../../components/ui'

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


export function AdminAuditPage() {
  const [action, setAction] = useState('')
  const [actor, setActor] = useState('')
  const [open, setOpen] = useState<number | null>(null)
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
    placeholderData: (prev) => prev,
  })
  const entries = q.data?.pages.flatMap((p) => p.entries) ?? []

  return (
    <div className="flex flex-col">
      <div className="px-6 pt-6">
        <SubHeader
          title="Audit log"
          as="h2"
          description={
            <>
              Security-relevant events: sign-ins, user and token changes, organization settings.{' '}
              <span className="inline-flex flex-wrap items-center gap-x-3 gap-y-1 align-middle text-sm">
                <span className="inline-flex items-center gap-1.5">
                  <Dot tone="danger" /> deletions and failures
                </span>
                <span className="inline-flex items-center gap-1.5">
                  <Dot tone="warning" /> admin changes
                </span>
              </span>
            </>
          }
          className="mb-4"
        />
      </div>
      <FilterBar className="border-t px-6" end={entries.length > 0 && <span className="text-sm text-fg-faint tabular-nums">{entries.length} shown</span>}>
        <FilterMenu label="Action" icon={<Filter size={12} />} value={action} onChange={setAction} options={actionGroups.map(([value, label]) => ({ value, label }))} />
        <SearchInput value={actor} onChange={(v) => setActor(v.trim())} debounce={300} placeholder="Actor username…" label="Filter by actor" className="max-w-56" />
      </FilterBar>
      {q.isPending ? (
        <SkeletonRows rows={8} />
      ) : q.isError ? (
        <ErrorState error={q.error} />
      ) : entries.length === 0 ? (
        <EmptyState icon={<History size={20} />} title="No matching entries">
          Try a different action or actor.
        </EmptyState>
      ) : (
        <Table aria-label="Audit log">
          <THead>
            <tr>
              <TH className="w-8">
                <span className="sr-only">Expand</span>
              </TH>
              <TH>When</TH>
              <TH>Actor</TH>
              <TH>Action</TH>
              <TH>Target</TH>
              <TH>IP</TH>
            </tr>
          </THead>
          <tbody>
            {entries.map((e) => {
              const hasDetails = Object.keys(e.details).length > 0
              const isOpen = open === e.id
              return (
                <Fragment key={e.id}>
                  <TR className={cx(hasDetails && 'cursor-pointer', isOpen && 'bg-bg-hover/60')} onClick={() => hasDetails && setOpen(isOpen ? null : e.id)}>
                    <TD className="w-8 pr-0">
                      {hasDetails ? (
                        <Tooltip content={isOpen ? 'Hide details' : 'Show details'} describe={false}>
                          <button type="button" aria-expanded={isOpen} aria-label={isOpen ? 'Hide details' : 'Show details'} className="flex size-5 items-center justify-center rounded text-fg-faint hover:text-fg">
                            {isOpen ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
                          </button>
                        </Tooltip>
                      ) : (
                        <span className="block size-5" aria-hidden />
                      )}
                    </TD>
                    <TD className="whitespace-nowrap text-fg-muted">
                      <Time iso={e.created_at} />
                    </TD>
                    <TD>{e.actor || <span className="text-fg-faint">anonymous</span>}</TD>
                    <TD>
                      <div className="flex flex-col items-start gap-0.5">
                        <Badge tone={auditActionTone(e.action)}>{auditActionLabel(e.action)}</Badge>
                        <span className="font-mono text-2xs text-fg-faint">{e.action}</span>
                      </div>
                    </TD>
                    <TD>{e.target}</TD>
                    <TD className="font-mono text-sm text-fg-faint">{e.ip}</TD>
                  </TR>
                  {isOpen && (
                    <tr className="border-b border-border bg-bg-subtle">
                      <td />
                      <td colSpan={5} className="px-3 py-3">
                        <DescriptionList
                          className="text-sm"
                          items={Object.entries(e.details).map(([k, v]) => ({
                            key: k,
                            term: <span className="font-mono">{k}</span>,
                            detail: <code className="font-mono break-all">{typeof v === 'string' ? v : JSON.stringify(v)}</code>,
                          }))}
                        />
                      </td>
                    </tr>
                  )}
                </Fragment>
              )
            })}
          </tbody>
        </Table>
      )}
      {q.hasNextPage && (
        <div className="flex justify-center p-4">
          <Button onClick={() => void q.fetchNextPage()} loading={q.isFetchingNextPage}>
            Load older entries
          </Button>
        </div>
      )}
    </div>
  )
}
