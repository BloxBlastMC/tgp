import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { ArrowLeft, Database, Search, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { api, errorMessage, unwrap, type CacheEntry } from '../../api/client'
import { cachesQuery, repoQuery, type CacheFilters } from '../../api/queries'
import { Pager } from '../../components/ci/common'
import { EmptyState } from '../../components/Page'
import { Alert, Button, cx, Input, Select, Spinner, Time } from '../../components/ui'
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
  const canWrite = r.permission === 'write' || r.permission === 'admin'
  const search = useSearch({ strict: false }) as CacheFilters
  const navigate = useNavigate()
  const set = (s: Partial<CacheFilters>) => void navigate({ to: '/$repo/ci/caches', params: { repo }, search: { ...search, page: undefined, ...s }, replace: true })
  const [key, setKey] = useState(search.key ?? '')
  useEffect(() => setKey(search.key ?? ''), [search.key])
  const q = useQuery(cachesQuery(repo, search))
  const qc = useQueryClient()
  const invalidate = () => void qc.invalidateQueries({ queryKey: ['repo', repo, 'caches'] })
  const delOne = useMutation({
    mutationFn: (id: number) => unwrap(api.DELETE('/repos/{repo}/caches/{id}', { params: { path: { repo, id } } })),
    onSuccess: invalidate,
  })
  const delKey = useMutation({
    mutationFn: (k?: string) => unwrap(api.DELETE('/repos/{repo}/caches', { params: { path: { repo }, query: k ? { key: k } : {} } })),
    onSuccess: invalidate,
  })
  const err = delOne.error ?? delKey.error

  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-3 p-4">
      <div>
        <Link to="/$repo/ci" params={{ repo }} search={{}} className="inline-flex items-center gap-1 text-[12px] text-fg-muted hover:text-fg">
          <ArrowLeft size={12} /> CI
        </Link>
        <div className="mt-1 flex items-center justify-between gap-3">
          <h2 className="text-[15px] font-semibold">Caches</h2>
          {canWrite && (q.data?.total ?? 0) > 0 && (
            <Button variant="danger" loading={delKey.isPending && delKey.variables === undefined} onClick={() => window.confirm(`Delete all ${q.data!.total} caches of ${repo}?`) && delKey.mutate(undefined)}>
              <Trash2 size={13} /> Delete all
            </Button>
          )}
        </div>
        <p className="mt-0.5 text-[12px] text-fg-muted">
          Saved by the <code className="font-mono">cache</code> step. Runs read caches from their own branch first, then the base and default branches.
        </p>
      </div>
      {q.data && <Usage used={q.data.total_bytes} quota={q.data.quota_bytes} count={q.data.total} />}
      <div className="flex items-center gap-2">
        <form
          className="relative min-w-48 flex-1"
          onSubmit={(e) => {
            e.preventDefault()
            set({ key: key.trim() || undefined })
          }}
        >
          <Search size={13} className="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-fg-faint" />
          <Input value={key} onChange={(e) => setKey(e.target.value)} placeholder="Filter by key prefix" aria-label="Filter by key prefix" className="h-7 pl-7 font-mono text-[12px]" />
        </form>
        <div className="w-40 shrink-0">
          <Select aria-label="Sort" value={search.sort ?? 'last_used'} onChange={(e) => set({ sort: e.target.value === 'last_used' ? undefined : (e.target.value as CacheFilters['sort']) })} className="h-7 text-[12px]">
            <option value="last_used">Recently used</option>
            <option value="created">Newest</option>
            <option value="size">Largest</option>
          </Select>
        </div>
      </div>
      {err && <Alert>{errorMessage(err)}</Alert>}
      {q.isPending ? (
        <Spinner />
      ) : q.isError ? (
        <Alert>{errorMessage(q.error)}</Alert>
      ) : q.data.caches.length === 0 ? (
        <EmptyState title={search.key ? 'No matching caches' : 'No caches yet'}>
          {search.key ? `No cache key starts with "${search.key}".` : 'Workflows that use the cache step will store their caches here.'}
        </EmptyState>
      ) : (
        <>
          <ul className="divide-y divide-border rounded-md border border-border" aria-label="Caches">
            {q.data.caches.map((c) => (
              <CacheRow
                key={c.id}
                c={c}
                canWrite={canWrite}
                busy={delOne.isPending || delKey.isPending}
                onDelete={() => window.confirm(`Delete cache ${c.key}?`) && delOne.mutate(c.id)}
                onDeleteKey={() => window.confirm(`Delete every cache with key ${c.key}?`) && delKey.mutate(c.key)}
                onFilter={() => set({ key: c.key })}
              />
            ))}
          </ul>
          <Pager page={search.page ?? 1} total={q.data.total} perPage={PER_PAGE} onPage={(p) => set({ page: p > 1 ? p : undefined })} />
        </>
      )}
    </div>
  )
}

function Usage({ used, quota, count }: { used: number; quota: number; count: number }) {
  const pct = usagePercent(used, quota)
  return (
    <div className="rounded-md border border-border bg-bg-subtle px-3 py-2.5">
      <div className="flex items-center justify-between text-[12px]">
        <span className="inline-flex items-center gap-1.5 text-fg-muted">
          <Database size={12} /> {count} {count === 1 ? 'cache' : 'caches'}
        </span>
        <span className="tabular-nums">
          {formatBytes(used)}
          {quota > 0 && <span className="text-fg-faint"> of {formatBytes(quota)}</span>}
        </span>
      </div>
      {pct != null && (
        <div className="mt-2 h-1.5 overflow-hidden rounded bg-bg-hover" role="meter" aria-label="Cache quota used" aria-valuenow={Math.round(pct)} aria-valuemin={0} aria-valuemax={100}>
          <div className={cx('h-full', pct > 90 ? 'bg-warning' : 'bg-accent')} style={{ width: `${pct}%` }} />
        </div>
      )}
    </div>
  )
}

function CacheRow({ c, canWrite, busy, onDelete, onDeleteKey, onFilter }: { c: CacheEntry; canWrite: boolean; busy: boolean; onDelete: () => void; onDeleteKey: () => void; onFilter: () => void }) {
  return (
    <li className="group flex items-center gap-3 px-3 py-2">
      <div className="min-w-0 flex-1">
        <button type="button" onClick={onFilter} className="block max-w-full truncate text-left font-mono text-[13px] hover:underline" title={`Show caches with key ${c.key}`}>
          {c.key}
        </button>
        <div className="mt-0.5 flex flex-wrap gap-x-3 text-[12px] text-fg-faint">
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
      <span className="shrink-0 text-[12px] tabular-nums text-fg-muted">{formatBytes(c.size_bytes)}</span>
      {canWrite && (
        <div className="flex shrink-0 gap-1">
          <button type="button" disabled={busy} onClick={onDeleteKey} className="hidden rounded px-1.5 py-0.5 text-[12px] text-fg-muted group-hover:block hover:bg-bg-hover hover:text-danger" title="Delete every cache with this key">
            Delete key
          </button>
          <button type="button" disabled={busy} onClick={onDelete} aria-label={`Delete cache ${c.key}`} className="rounded p-1 text-fg-muted hover:bg-bg-hover hover:text-danger">
            <Trash2 size={13} />
          </button>
        </div>
      )}
    </li>
  )
}
