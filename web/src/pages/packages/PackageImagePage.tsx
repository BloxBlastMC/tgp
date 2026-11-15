import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useSearch } from '@tanstack/react-router'
import { Boxes, Layers, MoreHorizontal, Tag, Trash2, X } from 'lucide-react'
import { api, errorMessage, unwrap, type PackageVersion } from '../../api/client'
import { packagesQuery, packageVersionsQuery, repoQuery } from '../../api/queries'
import { EmptyState, SubHeader } from '../../components/Page'
import { Alert, Badge, CopyButton, ErrorState, IconButton, List, Menu, MenuItem, SecretValue, SkeletonRows, Time, useConfirm, useToast } from '../../components/ui'
import { formatBytes } from '../../lib/ci'
import { canWrite as canWritePerm } from '../../lib/permissions'
import { useRepoName } from '../repos/RepoLayout'
import { registryHost } from './PackagesPage'

export function validatePackageImage(s: Record<string, unknown>): { name?: string } {
  return typeof s.name === 'string' && s.name ? { name: s.name } : {}
}

export function PackageImagePage() {
  const repo = useRepoName()
  const r = useQuery(repoQuery(repo)).data!
  const canWrite = canWritePerm(r.permission) && !r.archived
  const { name = '' } = useSearch({ strict: false }) as { name?: string }
  const list = useQuery(packagesQuery(repo))
  const q = useQuery(packageVersionsQuery(repo, name))
  const qc = useQueryClient()
  const toast = useToast()
  const confirm = useConfirm()
  const invalidate = () => void qc.invalidateQueries({ queryKey: ['repo', repo, 'packages'] })
  const delTag = useMutation({
    mutationFn: (tag: string) => unwrap(api.DELETE('/repos/{repo}/packages/tags', { params: { path: { repo }, query: { name, tag } } })),
    onSuccess: (_, tag) => {
      toast.success(`Tag ${tag} deleted`)
      invalidate()
    },
  })
  const delVersion = useMutation({
    mutationFn: (digest: string) => unwrap(api.DELETE('/repos/{repo}/packages/versions', { params: { path: { repo }, query: { name, digest } } })),
    onSuccess: () => {
      toast.success('Version deleted')
      invalidate()
    },
  })
  const host = registryHost(list.data?.registry ?? '')
  const image = list.data ? `${host}/${list.data.namespace}/${repo.toLowerCase()}${name ? `/${name}` : ''}` : ''
  const latest = q.data?.versions.find((v) => v.tags.length > 0)?.tags[0]
  const err = delTag.error ?? delVersion.error
  return (
    <div className="mx-auto flex max-w-5xl flex-col px-6 py-5">
      <SubHeader back={{ to: '/$repo/packages', params: { repo }, label: 'Packages' }} title={<span className="font-mono">{name || repo.toLowerCase()}</span>} />
      {image && (
        <div className="mb-4 flex flex-col gap-1.5">
          <span className="text-sm text-fg-muted">Pull</span>
          <SecretValue value={`docker pull ${image}${latest ? `:${latest}` : ''}`} label="docker pull command" />
        </div>
      )}
      {err && <Alert className="mb-3">{errorMessage(err)}</Alert>}
      {q.isPending ? (
        <List>
          <SkeletonRows rows={4} />
        </List>
      ) : q.isError ? (
        <ErrorState error={q.error} className="p-0" />
      ) : q.data.versions.length === 0 ? (
        <EmptyState size="inline" icon={<Boxes size={18} />} title="No versions">
          Every version of this image was deleted.
        </EmptyState>
      ) : (
        <List aria-label="Versions">
          {q.data.versions.map((v) => (
            <VersionRow
              key={v.digest}
              v={v}
              image={image}
              canWrite={canWrite}
              busy={delTag.isPending || delVersion.isPending}
              onDeleteTag={async (tag) => {
                if (await confirm({ title: `Delete tag ${tag}?`, description: 'The version stays; untagged versions are removed after the retention period.', confirmLabel: 'Delete tag' }))
                  delTag.mutate(tag)
              }}
              onDelete={async () => {
                if (
                  await confirm({
                    title: 'Delete this version?',
                    description: (
                      <>
                        <code className="font-mono break-all">{v.digest}</code>
                        {v.tags.length > 0 && <> and its tags {v.tags.join(', ')}</>} will be deleted.
                      </>
                    ),
                    confirmLabel: 'Delete version',
                  })
                )
                  delVersion.mutate(v.digest)
              }}
            />
          ))}
        </List>
      )}
    </div>
  )
}

function VersionRow({ v, image, canWrite, busy, onDeleteTag, onDelete }: { v: PackageVersion; image: string; canWrite: boolean; busy: boolean; onDeleteTag: (tag: string) => void; onDelete: () => void }) {
  const short = v.digest.replace('sha256:', '').slice(0, 12)
  return (
    <li className="flex items-start gap-3 border-b border-border px-4 py-2.5 transition-colors last:border-0 hover:bg-bg-hover/60">
      <Layers size={14} className="mt-1 shrink-0 text-fg-faint" />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-1.5">
          {v.tags.length === 0 && <span className="text-sm text-fg-faint italic">untagged</span>}
          {v.tags.map((t) => (
            <Badge key={t} tone="accent" icon={<Tag size={10} />} className="font-mono">
              {t}
              {canWrite && (
                <button type="button" aria-label={`Delete tag ${t}`} title={`Delete tag ${t}`} disabled={busy} onClick={() => onDeleteTag(t)} className="-mr-0.5 ml-0.5 rounded hover:text-danger">
                  <X size={10} />
                </button>
              )}
            </Badge>
          ))}
          <Badge>{v.kind === 'index' ? 'multi-platform' : v.kind === 'artifact' ? (v.artifact_type ?? 'artifact') : 'image'}</Badge>
          {v.platforms.map((p) => (
            <Badge key={p} className="font-mono">
              {p}
            </Badge>
          ))}
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-x-3 text-sm text-fg-faint">
          <span className="inline-flex items-center gap-0.5 font-mono" title={v.digest}>
            {short}
            <CopyButton value={`${image}@${v.digest}`} label="Copy image reference with digest" />
          </span>
          <span>
            pushed <Time iso={v.created_at} />
            {v.pushed_by && <> by {v.pushed_by.username}</>}
            {v.pushed_by_job != null && <> from CI</>}
          </span>
        </div>
      </div>
      <span className="mt-0.5 shrink-0 text-sm text-fg-muted tabular-nums">{formatBytes(v.size_bytes)}</span>
      {canWrite && (
        <Menu
          placement="bottom-end"
          label="Version actions"
          trigger={(t) => (
            <IconButton {...t} label={`Actions for ${short}`} size="sm" disabled={busy}>
              <MoreHorizontal size={14} />
            </IconButton>
          )}
        >
          <MenuItem icon={<Trash2 size={13} />} tone="danger" onSelect={onDelete}>
            Delete version and its tags
          </MenuItem>
        </Menu>
      )}
    </li>
  )
}
