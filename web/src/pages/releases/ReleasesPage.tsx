import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams, useSearch } from '@tanstack/react-router'
import { ArrowLeft, Download, FileArchive, FileCode2, GitCommit, Package, Pencil, Plus, Rocket, Tag, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { api, ApiRequestError, errorMessage, unwrap, type Release } from '../../api/client'
import { releaseByTagQuery, releasesQuery, repoQuery } from '../../api/queries'
import { EmptyState, SubHeader } from '../../components/Page'
import { Alert, Avatar, Badge, Button, ButtonLink, Checkbox, Dialog, ErrorState, IconButton, Pagination, PageSkeleton, Time, useToast } from '../../components/ui'
import { formatBytes, formatDate } from '../../lib/format'
import { canWrite } from '../../lib/permissions'
import { archiveUrl, assetUrl, releaseBadges, releaseTitle } from '../../lib/releases'
import { useRepoName } from '../repos/RepoLayout'

export function canWriteRepo(perm?: string): boolean {
  return canWrite(perm)
}

export function validateReleases(s: Record<string, unknown>): { page?: number } {
  const p = Number(s.page)
  return Number.isInteger(p) && p > 1 ? { page: p } : {}
}

const badgeTone = { Draft: 'warning', 'Pre-release': 'accent', Latest: 'success' } as const

export function ReleasesPage() {
  const repo = useRepoName()
  const r = useQuery(repoQuery(repo)).data!
  const search = useSearch({ strict: false }) as { page?: number }
  const page = search.page ?? 1
  const navigate = useNavigate()
  const q = useQuery({ ...releasesQuery(repo, page), placeholderData: (prev) => prev })
  const canWrite = canWriteRepo(r.permission)
  return (
    <div className="mx-auto flex max-w-5xl flex-col px-6 py-5">
      <SubHeader
        title="Releases"
        description="Tagged versions with release notes and downloadable files."
        actions={
          canWrite && (
            <ButtonLink to="/$repo/releases/new" params={{ repo }} variant="primary" icon={<Plus size={14} />}>
              New release
            </ButtonLink>
          )
        }
      />
      {q.isPending ? (
        <PageSkeleton rows={3} />
      ) : q.isError ? (
        <ErrorState error={q.error} className="p-0" />
      ) : q.data.releases.length === 0 ? (
        <EmptyState icon={<Package size={20} />} title="No releases yet">
          {canWrite ? 'Create a release to publish a tagged version with notes and downloadable files.' : 'Releases of this repository will show up here.'}
        </EmptyState>
      ) : (
        <>
          <ol className="flex flex-col">
            {q.data.releases.map((rel) => (
              <li key={rel.id} className="grid gap-x-8 gap-y-2 border-b border-border py-6 first:pt-0 last:border-0 md:grid-cols-[160px_minmax(0,1fr)]">
                <div className="flex flex-row flex-wrap items-center gap-x-3 gap-y-1 text-sm text-fg-muted md:flex-col md:items-start md:pt-3">
                  <span className="font-medium text-fg">{formatDate(rel.published_at ?? rel.created_at)}</span>
                  <Link to="/$repo/releases/tag/$" params={{ repo, _splat: rel.tag }} className="inline-flex items-center gap-1 font-mono hover:text-accent">
                    <Tag size={11} /> {rel.tag}
                  </Link>
                  {rel.sha && (
                    <Link to="/$repo/commit/$sha" params={{ repo, sha: rel.sha }} className="inline-flex items-center gap-1 font-mono text-fg-faint hover:text-accent">
                      <GitCommit size={11} /> {rel.sha.slice(0, 7)}
                    </Link>
                  )}
                </div>
                <ReleaseCard repo={repo} release={rel} canWrite={canWrite} linkTitle />
              </li>
            ))}
          </ol>
          <Pagination page={page} total={q.data.total} perPage={10} onPage={(p) => void navigate({ to: '/$repo/releases', params: { repo }, search: p > 1 ? { page: p } : {} })} prevLabel="Newer" nextLabel="Older" className="px-0" />
        </>
      )}
    </div>
  )
}

export function ReleaseDetailPage() {
  const repo = useRepoName()
  const r = useQuery(repoQuery(repo)).data!
  const { _splat: tag = '' } = useParams({ strict: false }) as { _splat?: string }
  const q = useQuery(releaseByTagQuery(repo, tag))
  return (
    <div className="mx-auto flex max-w-5xl flex-col px-6 py-5">
      <Link to="/$repo/releases" params={{ repo }} search={{}} className="mb-4 inline-flex items-center gap-1 self-start text-sm text-fg-muted hover:text-fg">
        <ArrowLeft size={13} /> All releases
      </Link>
      {q.isPending ? (
        <PageSkeleton rows={2} />
      ) : q.isError ? (
        q.error instanceof ApiRequestError && q.error.status === 404 ? (
          <EmptyState icon={<Package size={20} />} title="Release not found">
            There's no release for tag {tag} in {repo}.
          </EmptyState>
        ) : (
          <Alert>{errorMessage(q.error)}</Alert>
        )
      ) : (
        <ReleaseCard repo={repo} release={q.data} canWrite={canWriteRepo(r.permission)} />
      )}
    </div>
  )
}

function ReleaseCard({ repo, release: rel, canWrite, linkTitle }: { repo: string; release: Release; canWrite: boolean; linkTitle?: boolean }) {
  const [deleting, setDeleting] = useState(false)
  const qc = useQueryClient()
  const toast = useToast()
  const publish = useMutation({
    mutationFn: () => unwrap(api.PATCH('/repos/{repo}/releases/{id}', { params: { path: { repo, id: rel.id } }, body: { draft: false } })),
    onSuccess: () => {
      toast.success(`Published ${releaseTitle(rel)}`)
      void qc.invalidateQueries({ queryKey: ['repo', repo, 'releases'] })
    },
  })
  const title = releaseTitle(rel)
  const badges = releaseBadges(rel)
  return (
    <article
      className={`overflow-hidden rounded-lg border bg-surface-1 shadow-card ${badges.includes('Latest') ? 'border-success/40' : rel.draft ? 'border-dashed border-border-strong' : 'border-border'}`}
      aria-label={`Release ${title}`}
    >
      <header className="flex items-start gap-3 px-5 pt-4 pb-3">
        <span className={`mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-lg ${badges.includes('Latest') ? 'bg-success/12 text-success' : 'bg-bg-hover text-fg-muted'}`}>
          <Package size={16} />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            {linkTitle ? (
              <Link to="/$repo/releases/tag/$" params={{ repo, _splat: rel.tag }} className="truncate text-xl font-semibold tracking-tight hover:text-accent">
                {title}
              </Link>
            ) : (
              <h1 className="truncate text-xl font-semibold tracking-tight">{title}</h1>
            )}
            {badges.map((b) => (
              <Badge key={b} tone={badgeTone[b]}>
                {b}
              </Badge>
            ))}
          </div>
          <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-fg-muted">
            {!linkTitle && (
              <span className="inline-flex items-center gap-1 font-mono">
                <Tag size={11} />
                {rel.tag}
              </span>
            )}
            {!rel.sha && <span title="The tag is created when the release is published">tag created on publish from {rel.target}</span>}
            {rel.author && (
              <span className="inline-flex items-center gap-1.5">
                <Avatar user={rel.author} size={16} /> {rel.author.display_name}
              </span>
            )}
            <span>{rel.published_at ? <>published <Time iso={rel.published_at} /></> : <>created <Time iso={rel.created_at} /></>}</span>
          </div>
        </div>
        {canWrite && (
          <div className="flex shrink-0 gap-1.5">
            {rel.draft && (
              <Button variant="primary" icon={<Rocket size={13} />} loading={publish.isPending} onClick={() => publish.mutate()}>
                Publish
              </Button>
            )}
            <ButtonLink to="/$repo/releases/edit/$id" params={{ repo, id: String(rel.id) }} aria-label="Edit release" icon={<Pencil size={13} />}>
              Edit
            </ButtonLink>
            <IconButton label="Delete release" variant="secondary" className="hover:border-danger/50 hover:text-danger" onClick={() => setDeleting(true)}>
              <Trash2 size={13} />
            </IconButton>
          </div>
        )}
      </header>
      {publish.isError && (
        <div className="px-5 pb-3">
          <Alert>{errorMessage(publish.error)}</Alert>
        </div>
      )}
      <div className="px-5 pb-4">
        {rel.body_html ? <div className="markdown" dangerouslySetInnerHTML={{ __html: rel.body_html }} /> : <p className="text-fg-faint italic">No release notes.</p>}
      </div>
      <Assets repo={repo} release={rel} />
      <DeleteReleaseDialog repo={repo} release={rel} open={deleting} onClose={() => setDeleting(false)} />
    </article>
  )
}

function Assets({ repo, release: rel }: { repo: string; release: Release }) {
  const n = rel.assets.length + (rel.sha ? 2 : 0)
  return (
    <section className="border-t border-border bg-bg-subtle px-5 py-3" aria-label="Assets">
      <h3 className="mb-2 text-xs font-semibold tracking-wide text-fg-faint uppercase">Assets {n > 0 && <span className="font-normal">{n}</span>}</h3>
      <ul className="divide-y divide-border overflow-hidden rounded-md border border-border bg-surface-1">
        {rel.assets.map((a) => (
          <li key={a.id} className="flex h-9 items-center gap-2.5 px-3 text-base">
            <FileArchive size={14} className="shrink-0 text-fg-muted" />
            <a href={assetUrl(repo, rel.id, a.id)} download={a.name} className="min-w-0 flex-1 truncate text-accent hover:underline" title={a.sha256 ? `sha256 ${a.sha256}` : undefined}>
              {a.label || a.name}
            </a>
            <span className="shrink-0 text-sm text-fg-faint tabular-nums">{formatBytes(a.size_bytes)}</span>
            <span className="inline-flex w-16 shrink-0 items-center justify-end gap-1 text-sm text-fg-faint tabular-nums" title="Downloads">
              <Download size={11} />
              {a.download_count}
            </span>
          </li>
        ))}
        {rel.sha &&
          (['zip', 'tar.gz'] as const).map((f) => (
            <li key={f} className="flex h-9 items-center gap-2.5 px-3 text-base">
              <FileCode2 size={14} className="shrink-0 text-fg-faint" />
              <a href={archiveUrl(repo, rel.tag, f)} className="min-w-0 flex-1 truncate text-fg-muted hover:text-fg hover:underline">
                Source code ({f})
              </a>
            </li>
          ))}
        {n === 0 && <li className="px-2.5 py-2 text-sm text-fg-faint">No files yet.</li>}
      </ul>
    </section>
  )
}

function DeleteReleaseDialog({ repo, release: rel, open, onClose }: { repo: string; release: Release; open: boolean; onClose: () => void }) {
  const [deleteTag, setDeleteTag] = useState(false)
  const qc = useQueryClient()
  const navigate = useNavigate()
  const toast = useToast()
  const del = useMutation({
    mutationFn: () => unwrap(api.DELETE('/repos/{repo}/releases/{id}', { params: { path: { repo, id: rel.id }, query: { delete_tag: deleteTag } } })),
    onSuccess: () => {
      onClose()
      toast.success('Release deleted')
      qc.removeQueries({ queryKey: ['repo', repo, 'releases', 'tag', rel.tag] })
      void qc.invalidateQueries({ queryKey: ['repo', repo, 'releases'] })
      if (deleteTag) void qc.invalidateQueries({ queryKey: ['repo', repo, 'tags'] })
      void navigate({ to: '/$repo/releases', params: { repo }, search: {} })
    },
  })
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={`Delete ${releaseTitle(rel)}?`}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="danger-solid" loading={del.isPending} onClick={() => del.mutate()}>
            Delete release
          </Button>
        </>
      }
    >
      <p className="text-fg-muted">The release and its {rel.assets.length === 1 ? 'file' : `${rel.assets.length} files`} are deleted. This can't be undone.</p>
      {rel.sha && <Checkbox checked={deleteTag} onChange={setDeleteTag} label={`Also delete the tag ${rel.tag}`} hint="Otherwise the tag stays in the repository." />}
      {del.isError && <Alert>{errorMessage(del.error)}</Alert>}
    </Dialog>
  )
}
