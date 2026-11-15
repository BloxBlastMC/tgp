import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { GitBranch, GitPullRequest, SearchX, Shield, Tag, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { api, errorMessage, unwrap, type Schemas } from '../../api/client'
import { branchesQuery, pullsQuery, repoQuery, tagsQuery } from '../../api/queries'
import { Alert, Badge, ButtonLink, EmptyState, ErrorState, IconButton, List, SearchInput, SegmentedControl, SkeletonRows, Time, useConfirm, useToast } from '../../components/ui'
import { canWrite as canWritePerm } from '../../lib/permissions'
import { compareSplat } from '../../lib/pulls'
import { Container } from '../../components/Page'
import { useRepoName } from './RepoLayout'

type Ref = Schemas['Ref']

function RefRow({ r, repo, kind, extra }: { r: Ref; repo: string; kind: 'branch' | 'tag'; extra?: React.ReactNode }) {
  return (
    <li className="flex items-center gap-3 border-b border-border px-4 py-2.5 transition-colors last:border-0 hover:bg-bg-hover">
      {kind === 'tag' ? <Tag size={14} className="shrink-0 text-fg-faint" /> : <GitBranch size={14} className="shrink-0 text-fg-faint" />}
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <Link to="/$repo" params={{ repo }} search={{ ref: r.name }} className="font-mono text-base font-medium hover:text-accent">
            {r.name}
          </Link>
          {r.is_default && <Badge tone="accent">default</Badge>}
          {r.protected && (
            <Badge tone="warning" icon={<Shield size={10} />}>
              protected
            </Badge>
          )}
        </div>
        <div className="mt-0.5 flex min-w-0 items-center gap-1.5 text-sm text-fg-muted">
          <Link to="/$repo/commit/$sha" params={{ repo, sha: r.commit }} className="min-w-0 truncate hover:text-accent">
            {r.subject}
          </Link>
          <span className="shrink-0 text-fg-faint">
            · {r.author} · <Time iso={r.date} />
          </span>
        </div>
      </div>
      {extra}
    </li>
  )
}

export function BranchesPage() {
  const name = useRepoName()
  const repo = useQuery(repoQuery(name)).data!
  const branches = useQuery(branchesQuery(name))
  const tags = useQuery(tagsQuery(name))
  const open = useQuery(pullsQuery(name, { state: 'open' }))
  const qc = useQueryClient()
  const confirm = useConfirm()
  const toast = useToast()
  const [tab, setTab] = useState<'branches' | 'tags'>(() => (window.location.hash === '#tags' ? 'tags' : 'branches'))
  const [filter, setFilter] = useState('')
  const canWrite = canWritePerm(repo.permission) && !repo.archived
  const del = useMutation({
    mutationFn: (r: Ref) => unwrap(api.POST('/repos/{repo}/branches/delete', { params: { path: { repo: name } }, body: { name: r.name, sha: r.commit } })),
    onSuccess: (_, r) => {
      toast.success(`Deleted ${r.name}`)
      void qc.invalidateQueries({ queryKey: branchesQuery(name).queryKey })
      void qc.invalidateQueries({ queryKey: ['repo', name, 'pull'] })
    },
  })
  const q = tab === 'branches' ? branches : tags
  const shown = useMemo(() => {
    const f = filter.trim().toLowerCase()
    const list = (q.data ?? []).filter((r) => !f || r.name.toLowerCase().includes(f))
    // Default branch first, then most recently updated.
    return [...list].sort((a, b) => Number(!!b.is_default) - Number(!!a.is_default) || b.date.localeCompare(a.date))
  }, [q.data, filter])
  const prFor = (branch: string) => open.data?.pulls.find((p) => p.head_branch === branch)

  const extra = (r: Ref) => {
    if (r.is_default || tab === 'tags') return null
    const pr = prFor(r.name)
    return (
      <span className="flex shrink-0 items-center gap-1.5">
        {pr ? (
          <Link
            to="/$repo/pulls/$number"
            params={{ repo: name, number: String(pr.number) }}
            className="inline-flex items-center gap-1 rounded-md border border-success/30 bg-success/10 px-2 py-0.5 text-sm text-success hover:border-success/60"
          >
            <GitPullRequest size={12} /> #{pr.number}
          </Link>
        ) : (
          canWrite && (
            <ButtonLink
              to="/$repo/compare/$"
              params={{ repo: name, _splat: compareSplat(repo.default_branch, r.name) }}
              size="sm"
              variant="ghost"
              icon={<GitPullRequest size={12} />}
            >
              New PR
            </ButtonLink>
          )
        )}
        {canWrite && !pr && !r.protected && (
          <IconButton
            label={`Delete branch ${r.name}`}
            size="sm"
            className="hover:bg-danger/10 hover:text-danger"
            loading={del.isPending && del.variables?.name === r.name}
            onClick={async () => {
              if (await confirm({ title: `Delete branch ${r.name}?`, description: 'The commits stay reachable only if another branch or tag contains them.', confirmLabel: 'Delete branch' }))
                del.mutate(r)
            }}
          >
            <Trash2 size={13} />
          </IconButton>
        )}
      </span>
    )
  }

  return (
    <Container width="wide" className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <SegmentedControl
          label="Show"
          value={tab}
          onChange={(t) => {
            setTab(t)
            setFilter('')
          }}
          options={[
            { value: 'branches', label: 'Branches', icon: <GitBranch size={12} />, count: branches.data?.length },
            { value: 'tags', label: 'Tags', icon: <Tag size={12} />, count: tags.data?.length },
          ]}
        />
        <SearchInput key={tab} value={filter} onChange={setFilter} label={tab === 'branches' ? 'Filter branches' : 'Filter tags'} placeholder={tab === 'branches' ? 'Filter branches…' : 'Filter tags…'} shortcut />
      </div>
      {del.isError && <Alert>{errorMessage(del.error)}</Alert>}
      {q.isError ? (
        <ErrorState error={q.error} className="p-0" />
      ) : q.isPending ? (
        <List>
          <SkeletonRows rows={4} />
        </List>
      ) : shown.length === 0 ? (
        filter ? (
          <EmptyState size="inline" icon={<SearchX size={18} />} title={`No ${tab} match “${filter}”`} />
        ) : (
          <EmptyState size="inline" icon={tab === 'tags' ? <Tag size={18} /> : <GitBranch size={18} />} title={tab === 'tags' ? 'No tags yet' : 'No branches yet'}>
            {tab === 'tags' ? 'Tags appear here when you push them or publish a release.' : 'Push a branch to see it here.'}
          </EmptyState>
        )
      ) : (
        <List aria-label={tab === 'branches' ? 'Branches' : 'Tags'}>
          {shown.map((r) => (
            <RefRow key={r.name} r={r} repo={repo.name} kind={tab === 'tags' ? 'tag' : 'branch'} extra={extra(r)} />
          ))}
        </List>
      )}
    </Container>
  )
}
