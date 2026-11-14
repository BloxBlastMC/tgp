import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams, useSearch } from '@tanstack/react-router'
import { ArrowLeft, Download, FileArchive, GitCommit, Package, Pencil, Plus, Tag, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { api, ApiRequestError, errorMessage, unwrap, type Release } from '../../api/client'
import { releaseByTagQuery, releasesQuery, repoQuery } from '../../api/queries'
import { Pager } from '../../components/ci/common'
import { EmptyState } from '../../components/Page'
import { Alert, Badge, Button, Checkbox, Dialog, Spinner, Time } from '../../components/ui'
import { formatBytes } from '../../lib/ci'
import { archiveUrl, assetUrl, releaseBadges, releaseTitle } from '../../lib/releases'
import { useRepoName } from '../repos/RepoLayout'

export function canWriteRepo(perm?: string): boolean {
  return perm === 'write' || perm === 'admin'
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
  const q = useQuery(releasesQuery(repo, page))
  const canWrite = canWriteRepo(r.permission)
  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-3 p-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">Releases</h2>
        {canWrite && (
          <Link to="/$repo/releases/new" params={{ repo }}>
            <Button variant="primary">
              <Plus size={13} /> New release
            </Button>
          </Link>
        )}
      </div>
      {q.isPending ? (
        <Spinner />
      ) : q.isError ? (
        <Alert>{errorMessage(q.error)}</Alert>
      ) : q.data.releases.length === 0 ? (
        <EmptyState title="No releases yet">
          {canWrite ? 'Create a release to publish a tagged version with notes and downloadable files.' : 'Releases of this repository will show up here.'}
        </EmptyState>
      ) : (
        <>
          {q.data.releases.map((rel) => (
            <ReleaseCard key={rel.id} repo={repo} release={rel} canWrite={canWrite} linkTitle />
          ))}
          <Pager page={page} total={q.data.total} perPage={10} onPage={(p) => void navigate({ to: '/$repo/releases', params: { repo }, search: p > 1 ? { page: p } : {} })} />
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
    <div className="mx-auto flex max-w-5xl flex-col gap-3 p-4">
      <Link to="/$repo/releases" params={{ repo }} search={{}} className="inline-flex items-center gap-1 text-sm text-fg-muted hover:text-fg">
        <ArrowLeft size={12} /> Releases
      </Link>
      {q.isPending ? (
        <Spinner />
      ) : q.isError ? (
        q.error instanceof ApiRequestError && q.error.status === 404 ? (
          <EmptyState title="Release not found">There's no release for tag {tag} in {repo}.</EmptyState>
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
  const publish = useMutation({
    mutationFn: () => unwrap(api.PATCH('/repos/{repo}/releases/{id}', { params: { path: { repo, id: rel.id } }, body: { draft: false } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['repo', repo, 'releases'] }),
  })
  const title = releaseTitle(rel)
  return (
    <article className="rounded-md border border-border bg-bg-subtle" aria-label={`Release ${title}`}>
      <header className="flex items-start gap-3 border-b border-border px-4 py-3">
        <Package size={18} className="mt-0.5 shrink-0 text-fg-muted" />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            {linkTitle ? (
              <Link to="/$repo/releases/tag/$" params={{ repo, _splat: rel.tag }} className="truncate text-lg font-semibold hover:underline">
                {title}
              </Link>
            ) : (
              <h1 className="truncate text-lg font-semibold">{title}</h1>
            )}
            {releaseBadges(rel).map((b) => (
              <Badge key={b} tone={badgeTone[b]}>
                {b}
              </Badge>
            ))}
          </div>
          <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-fg-muted">
            <span className="inline-flex items-center gap-1 font-mono">
              <Tag size={11} />
              {rel.tag}
            </span>
            {rel.sha ? (
              <Link to="/$repo/commit/$sha" params={{ repo, sha: rel.sha }} className="inline-flex items-center gap-1 font-mono hover:text-fg">
                <GitCommit size={11} />
                {rel.sha.slice(0, 7)}
              </Link>
            ) : (
              <span title="The tag is created when the release is published">tag created on publish from {rel.target}</span>
            )}
            {rel.author && <span>by {rel.author.display_name}</span>}
            <span>{rel.published_at ? <>published <Time iso={rel.published_at} /></> : <>created <Time iso={rel.created_at} /></>}</span>
          </div>
        </div>
        {canWrite && (
          <div className="flex shrink-0 gap-2">
            {rel.draft && (
              <Button variant="primary" loading={publish.isPending} onClick={() => publish.mutate()}>
                Publish
              </Button>
            )}
            <Link to="/$repo/releases/edit/$id" params={{ repo, id: String(rel.id) }}>
              <Button aria-label="Edit release">
                <Pencil size={13} /> Edit
              </Button>
            </Link>
            <Button variant="danger" aria-label="Delete release" onClick={() => setDeleting(true)}>
              <Trash2 size={13} />
            </Button>
          </div>
        )}
      </header>
      {publish.isError && (
        <div className="px-4 pt-3">
          <Alert>{errorMessage(publish.error)}</Alert>
        </div>
      )}
      <div className="px-4 py-3">
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
    <section className="border-t border-border px-4 py-3" aria-label="Assets">
      <h3 className="mb-1.5 text-xs font-semibold tracking-wide text-fg-faint uppercase">Assets {n > 0 && <span className="font-normal">{n}</span>}</h3>
      <ul className="divide-y divide-border rounded border border-border bg-bg">
        {rel.assets.map((a) => (
          <li key={a.id} className="flex h-8 items-center gap-2 px-2.5 text-base">
            <FileArchive size={13} className="shrink-0 text-fg-muted" />
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
            <li key={f} className="flex h-8 items-center gap-2 px-2.5 text-base">
              <FileArchive size={13} className="shrink-0 text-fg-faint" />
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
  const del = useMutation({
    mutationFn: () => unwrap(api.DELETE('/repos/{repo}/releases/{id}', { params: { path: { repo, id: rel.id }, query: { delete_tag: deleteTag } } })),
    onSuccess: () => {
      onClose()
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
          <Button variant="danger" loading={del.isPending} onClick={() => del.mutate()}>
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
