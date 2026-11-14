import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { ArrowUpDown, Database, MoreHorizontal, SearchX, Trash2 } from 'lucide-react'
import { api, errorMessage, unwrap, type CacheEntry } from '../../api/client'
import { cachesQuery, repoQuery, type CacheFilters } from '../../api/queries'
import { EmptyState, SubHeader } from '../../components/Page'
import { Alert, Button, ErrorState, FilterMenu, IconButton, List, Menu, MenuItem, Meter, Pagination, SearchInput, SkeletonRows, StatCard, Time, useConfirm, useToast } from '../../components/ui'
import { canWrite as canWritePerm } from '../../lib/permissions'
import { formatBytes, refLabel } from '../../lib/ci'
import { usagePercent } from '../../lib/releases'
import { useRepoName } from '../repos/RepoLayout'

const PER_PAGE = 50
const sorts = ['last_used', 'created', 'size'] as const

export function validateCaches(s: Record<string, unknown>): CacheFilters {
  const out: CacheFilters = {}
  if (typeof s.key === 'string' && s.key.trim()) out.key = s.key
  if (typeof s.sort === 'string' && (sorts as readonly string[]).includes(s.sort) && s.sort !== 'last_used') out.sort = s.sort as CacheFilters['sort']
  const p = Number(s.page)
  if (Number.isInteger(p) && p > 1) out.page = p
  return out
}

export function CachesPage() {
  const repo = useRepoName()
  const r = useQuery(repoQuery(repo)).data!
  const canWrite = canWritePerm(r.permission)
  const search = useSearch({ strict: false }) as CacheFilters
  const navigate = useNavigate()
  const confirm = useConfirm()
  const toast = useToast()
  const set = (s: Partial<CacheFilters>) => void navigate({ to: '/$repo/ci/caches', params: { repo }, search: { ...search, page: undefined, ...s }, replace: true })
  const q = useQuery({ ...cachesQuery(repo, search), placeholderData: (prev) => prev })
  const qc = useQueryClient()
  const invalidate = () => void qc.invalidateQueries({ queryKey: ['repo', repo, 'caches'] })
  const delOne = useMutation({
    mutationFn: (id: number) => unwrap(api.DELETE('/repos/{repo}/caches/{id}', { params: { path: { repo, id } } })),
    onSuccess: () => {
      toast.success('Cache deleted')
      invalidate()
    },
  })
  const delKey = useMutation({
    mutationFn: (k?: string) => unwrap(api.DELETE('/repos/{repo}/caches', { params: { path: { repo }, query: k ? { key: k } : {} } })),
    onSuccess: (_, k) => {
      toast.success(k ? `Deleted caches for ${k}` : 'Deleted all caches')
      invalidate()
    },
  })
  const err = delOne.error ?? delKey.error
  const pct = q.data ? usagePercent(q.data.total_bytes, q.data.quota_bytes) : null

  return (
    <div className="mx-auto flex max-w-5xl flex-col px-6 py-5">
      <SubHeader
        back={{ to: '/$repo/ci', params: { repo }, label: 'CI' }}
        title="Caches"
        description={
          <>
            Saved by the <code className="font-mono">cache</code> step. Runs read caches from their own branch first, then the base and default branches.
          </>
        }
        actions={
          canWrite &&
          (q.data?.total ?? 0) > 0 && (
            <Button
              variant="danger"
              icon={<Trash2 size={13} />}
              loading={delKey.isPending && delKey.variables === undefined}
              onClick={async () => {
                const ok = await confirm({ title: `Delete all ${q.data!.total} caches?`, description: `Every cache of ${repo} is removed. The next runs rebuild them.`, confirmLabel: 'Delete all caches' })
                if (ok) delKey.mutate(undefined)
              }}
            >
              Delete all
            </Button>
          )
        }
      />
      {q.data && (
        <div className="mb-4 grid gap-3 sm:grid-cols-3">
          <StatCard label="Caches" value={q.data.total.toLocaleString()} icon={<Database size={15} />} />
          <StatCard label="Storage used" value={formatBytes(q.data.total_bytes)} hint={q.data.quota_bytes > 0 ? `of ${formatBytes(q.data.quota_bytes)} quota` : 'No quota'} />
          <div className="flex flex-col justify-center rounded-lg border border-border bg-surface-1 px-4 py-3 shadow-card">
            <div className="mb-2 flex items-center justify-between text-sm text-fg-muted">
              <span>Quota</span>
              <span className="tabular-nums">{pct == null ? '—' : `${Math.round(pct)}%`}</span>
            </div>
            <Meter value={pct ?? 0} label="Cache quota used" />
          </div>
        </div>
      )}
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <SearchInput value={search.key ?? ''} onChange={(k) => set({ key: k.trim() || undefined })} debounce={300} placeholder="Filter by key prefix…" label="Filter by key prefix" shortcut />
        <FilterMenu
          label="Sort"
          icon={<ArrowUpDown size={12} />}
          value={search.sort ?? 'last_used'}
          onChange={(v) => set({ sort: v === 'last_used' ? undefined : v })}
          options={[
            { value: 'last_used', label: 'Recently used' },
            { value: 'created', label: 'Newest' },
            { value: 'size', label: 'Largest' },
          ]}
        />
      </div>
      {err && <Alert className="mb-3">{errorMessage(err)}</Alert>}
      {q.isPending ? (
        <List>
          <SkeletonRows rows={4} />
        </List>
      ) : q.isError ? (
        <ErrorState error={q.error} className="p-0" />
      ) : q.data.caches.length === 0 ? (
        <EmptyState size="inline" icon={search.key ? <SearchX size={18} /> : <Database size={18} />} title={search.key ? 'No matching caches' : 'No caches yet'}>
          {search.key ? `No cache key starts with "${search.key}".` : 'Workflows that use the cache step will store their caches here.'}
        </EmptyState>
      ) : (
        <>
          <List aria-label="Caches">
            {q.data.caches.map((c) => (
              <CacheRow
                key={c.id}
                c={c}
                canWrite={canWrite}
                busy={delOne.isPending || delKey.isPending}
                onDelete={async () => {
                  if (await confirm({ title: 'Delete this cache?', description: <code className="font-mono">{c.key}</code>, confirmLabel: 'Delete cache' })) delOne.mutate(c.id)
                }}
                onDeleteKey={async () => {
                  if (await confirm({ title: 'Delete every cache with this key?', description: <code className="font-mono">{c.key}</code>, confirmLabel: 'Delete caches' })) delKey.mutate(c.key)
                }}
                onFilter={() => set({ key: c.key })}
              />
            ))}
          </List>
          <Pagination page={search.page ?? 1} total={q.data.total} perPage={PER_PAGE} onPage={(p) => set({ page: p > 1 ? p : undefined })} className="px-0" />
        </>
      )}
    </div>
  )
}

function CacheRow({ c, canWrite, busy, onDelete, onDeleteKey, onFilter }: { c: CacheEntry; canWrite: boolean; busy: boolean; onDelete: () => void; onDeleteKey: () => void; onFilter: () => void }) {
  return (
    <li className="flex items-center gap-3 border-b border-border px-4 py-2.5 transition-colors last:border-0 hover:bg-bg-hover/60">
      <Database size={14} className="shrink-0 text-fg-faint" />
      <div className="min-w-0 flex-1">
        <button type="button" onClick={onFilter} className="block max-w-full truncate text-left font-mono text-base hover:text-accent hover:underline" title={`Show caches with key ${c.key}`}>
          {c.key}
        </button>
        <div className="mt-0.5 flex flex-wrap gap-x-3 text-sm text-fg-faint">
          <span className="font-mono" title={c.ref}>
            {refLabel(c.ref)}
          </span>
          <span>
            used <Time iso={c.last_used_at} />
          </span>
          <span>
            saved <Time iso={c.created_at} />
          </span>
          <span className="font-mono" title="Hash of the cached paths">
            v {c.version.slice(0, 8)}
          </span>
        </div>
      </div>
      <span className="shrink-0 text-sm text-fg-muted tabular-nums">{formatBytes(c.size_bytes)}</span>
      {canWrite && (
        <Menu
          placement="bottom-end"
          label="Cache actions"
          trigger={(t) => (
            <IconButton {...t} label={`Actions for ${c.key}`} size="sm" disabled={busy}>
              <MoreHorizontal size={14} />
            </IconButton>
          )}
        >
          <MenuItem icon={<Trash2 size={13} />} tone="danger" onSelect={onDelete}>
            Delete this cache
          </MenuItem>
          <MenuItem icon={<Trash2 size={13} />} tone="danger" onSelect={onDeleteKey}>
            Delete every cache with this key
          </MenuItem>
        </Menu>
      )}
    </li>
  )
}
