import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useParams } from '@tanstack/react-router'
import { ChevronDown, FileArchive, Lock, Rocket, Upload, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import { api, errorMessage, unwrap, type Release } from '../../api/client'
import { branchesQuery, repoQuery, tagsQuery } from '../../api/queries'
import { EmptyState, SubHeader, Container } from '../../components/Page'
import { MarkdownEditor } from '../../components/pulls/common'
import { Alert, Button, Card, Checkbox, cx, ErrorState, Field, FileDrop, IconButton, Input, Menu, MenuItem, MenuLabel, Meter, PageSkeleton, Select, useConfirm, useToast } from '../../components/ui'
import { formatBytes } from '../../lib/format'
import { assetUrl, releaseTitle, tagError, uploadAsset } from '../../lib/releases'
import { useRepoName } from '../repos/RepoLayout'
import { canWriteRepo } from './ReleasesPage'

export function NewReleasePage() {
  return <ReleaseForm />
}

export function EditReleasePage() {
  const repo = useRepoName()
  const { id } = useParams({ strict: false }) as { id: string }
  const q = useQuery({
    queryKey: ['repo', repo, 'releases', 'id', Number(id)],
    queryFn: () => unwrap(api.GET('/repos/{repo}/releases/{id}', { params: { path: { repo, id: Number(id) } } })),
    retry: false,
  })
  if (q.isPending) return <PageSkeleton rows={3} />
  if (q.isError) return <ErrorState error={q.error} />
  return <ReleaseForm key={q.data.id} release={q.data} />
}

type Pending = { file: File; progress: number; error?: string }

function ReleaseForm({ release: initial }: { release?: Release }) {
  // A new release that was created but whose files failed to upload is
  // edited from then on, so saving again doesn't create it twice.
  const [created, setCreated] = useState<Release>()
  const release = initial ?? created
  const repo = useRepoName()
  const r = useQuery(repoQuery(repo)).data!
  const tags = useQuery(tagsQuery(repo))
  const branches = useQuery(branchesQuery(repo))
  const navigate = useNavigate()
  const qc = useQueryClient()
  const confirm = useConfirm()
  const toast = useToast()
  const [tag, setTag] = useState(release?.tag ?? '')
  const [target, setTarget] = useState(release?.target || r.default_branch)
  const [name, setName] = useState(release?.name ?? '')
  const [body, setBody] = useState(release?.body ?? '')
  const [prerelease, setPrerelease] = useState(release?.prerelease ?? false)
  const [files, setFiles] = useState<Pending[]>([])
  const [uploadError, setUploadError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (!release && !target && r.default_branch) setTarget(r.default_branch)
  }, [release, target, r.default_branch])

  const existingTag = (tags.data ?? []).find((t) => t.name === tag.trim())
  const tagLocked = !!release?.sha // a published release's tag already exists; renaming is allowed but rare
  const invalid = tagError(tag)

  const invalidate = () => void qc.invalidateQueries({ queryKey: ['repo', repo, 'releases'] })

  const uploadAll = async (rel: Release): Promise<boolean> => {
    let ok = true
    for (let i = 0; i < files.length; i++) {
      const p = files[i]!
      if (p.progress >= 1 && !p.error) continue
      try {
        await uploadAsset(repo, rel.id, p.file, (f) => setFiles((fs) => fs.map((x, j) => (j === i ? { ...x, progress: f, error: undefined } : x))))
        setFiles((fs) => fs.map((x, j) => (j === i ? { ...x, progress: 1 } : x)))
      } catch (e) {
        ok = false
        setFiles((fs) => fs.map((x, j) => (j === i ? { ...x, error: errorMessage(e) } : x)))
      }
    }
    return ok
  }

  const save = useMutation({
    mutationFn: async (draft: boolean) => {
      const fields = { tag: tag.trim(), name, body, prerelease, ...(existingTag ? {} : { target }) }
      const rel = release
        ? await unwrap(api.PATCH('/repos/{repo}/releases/{id}', { params: { path: { repo, id: release.id } }, body: { ...fields, draft } }))
        : await unwrap(api.POST('/repos/{repo}/releases', { params: { path: { repo } }, body: { ...fields, draft } }))
      return rel
    },
    onSuccess: async (rel) => {
      setUploadError(null)
      invalidate()
      void qc.invalidateQueries({ queryKey: ['repo', repo, 'tags'] })
      setBusy(true)
      const ok = await uploadAll(rel)
      setBusy(false)
      invalidate()
      if (ok) {
        toast.success(rel.draft ? 'Draft saved' : `Published ${releaseTitle(rel)}`)
        void navigate({ to: '/$repo/releases/tag/$', params: { repo, _splat: rel.tag } })
      }
      else {
        setUploadError('Some files failed to upload. The release was saved; fix the problem and save again to retry.')
        setCreated(rel)
      }
    },
  })

  const delAsset = useMutation({
    mutationFn: (assetId: number) => unwrap(api.DELETE('/repos/{repo}/releases/{id}/assets/{asset_id}', { params: { path: { repo, id: release!.id, asset_id: assetId } } })),
    onSuccess: () => invalidate(),
  })

  if (!canWriteRepo(r.permission))
    return (
      <EmptyState icon={<Lock size={20} />} title="Can't edit releases">
        You need write access to {repo}.
      </EmptyState>
    )

  const addFiles = (list: File[]) => {
    const add = list.filter((f) => !files.some((p) => p.file.name === f.name))
    setFiles((fs) => [...fs, ...add.map((file) => ({ file, progress: 0 }))])
  }
  const assets = release?.assets ?? []
  const pending = save.isPending || busy
  const submit = (draft: boolean) => !invalid && save.mutate(draft)

  return (
    <Container width="default" className="flex flex-col">
      <SubHeader back={{ to: '/$repo/releases', params: { repo }, search: {}, label: 'Releases' }} title={release ? `Edit ${releaseTitle(release)}` : 'New release'} />
      <form
        aria-label={release ? 'Edit release' : 'New release'}
        onSubmit={(e) => {
          e.preventDefault()
          submit(release ? release.draft : false)
        }}
      >
        <Card>
          <div className="flex flex-col gap-5">
            <div className="grid gap-4 md:grid-cols-2">
              <Field
                label="Tag"
                error={tag.trim() && invalid ? invalid : undefined}
                hint={
                  tag.trim() && !invalid
                    ? existingTag
                      ? `Existing tag at ${existingTag.commit.slice(0, 7)}`
                      : tagLocked
                        ? 'The tag is renamed in the release only'
                        : 'New tag — created from the target when the release is published'
                    : 'Pick an existing tag or type a new one, like v1.2.0.'
                }
              >
                {(id) => (
                  <div className="flex gap-2">
                    <Input id={id} value={tag} onChange={(e) => setTag(e.target.value)} placeholder="v1.0.0" autoFocus={!release} className="font-mono" aria-invalid={!!(tag.trim() && invalid) || undefined} />
                    {(tags.data?.length ?? 0) > 0 && (
                      <Menu
                        placement="bottom-end"
                        label="Existing versions"
                        className="max-h-72 w-56 overflow-y-auto"
                        trigger={(t) => (
                          <Button {...t} aria-label="Pick an existing version" className="h-8" iconRight={<ChevronDown size={12} className="text-fg-faint" />}>
                            Existing
                          </Button>
                        )}
                      >
                        <MenuLabel>Existing versions</MenuLabel>
                        {(tags.data ?? []).map((t) => (
                          <MenuItem key={t.name} onSelect={() => setTag(t.name)}>
                            <span className={cx('font-mono', t.name === tag.trim() && 'font-semibold')}>{t.name}</span>
                          </MenuItem>
                        ))}
                      </Menu>
                    )}
                  </div>
                )}
              </Field>
              {!existingTag && (
                <Field label="Target" hint="Branch the new tag points at">
                  {(id) => (
                    <Select id={id} value={target} onChange={(e) => setTarget(e.target.value)} className="font-mono">
                      {(branches.data ?? [{ name: r.default_branch }]).map((b) => (
                        <option key={b.name} value={b.name}>
                          {b.name}
                        </option>
                      ))}
                      {target && !(branches.data ?? []).some((b) => b.name === target) && <option value={target}>{target}</option>}
                    </Select>
                  )}
                </Field>
              )}
            </div>
            <Field label="Title">{(id) => <Input id={id} value={name} onChange={(e) => setName(e.target.value)} placeholder={tag.trim() || 'Release title'} maxLength={200} size="lg" />}</Field>
            <MarkdownEditor value={body} onChange={setBody} repo={repo} label="Release notes" placeholder="What changed in this release?" minRows={10} onSubmit={() => submit(release ? release.draft : false)} />
            <Checkbox checked={prerelease} onChange={setPrerelease} label="This is a pre-release" hint="Pre-releases are never marked Latest." />

            <section aria-label="Release files" className="flex flex-col gap-2">
              <h3 className="text-sm font-medium">Files</h3>
              <FileDrop multiple label="Attach files" onFiles={addFiles} disabled={pending} hint="They're uploaded when you save." />
              {(assets.length > 0 || files.length > 0) && (
                <ul className="divide-y divide-border overflow-hidden rounded-md border border-border">
                  {assets.map((a) => (
                    <li key={a.id} className="flex h-10 items-center gap-2.5 px-3 text-base">
                      <FileArchive size={14} className="text-fg-muted" />
                      <a href={assetUrl(repo, release!.id, a.id)} className="min-w-0 flex-1 truncate hover:underline">
                        {a.name}
                      </a>
                      <span className="text-sm text-fg-faint tabular-nums">{formatBytes(a.size_bytes)}</span>
                      <IconButton
                        label={`Delete ${a.name}`}
                        size="sm"
                        className="hover:bg-danger/10 hover:text-danger"
                        disabled={delAsset.isPending}
                        onClick={async () => {
                          if (await confirm({ title: `Delete ${a.name}?`, description: 'The file is removed from the release right away.', confirmLabel: 'Delete file' })) delAsset.mutate(a.id)
                        }}
                      >
                        <X size={13} />
                      </IconButton>
                    </li>
                  ))}
                  {files.map((p, i) => (
                    <li key={p.file.name} className="flex flex-col gap-1.5 px-3 py-2 text-base">
                      <div className="flex items-center gap-2.5">
                        <Upload size={14} className="text-accent" />
                        <span className="min-w-0 flex-1 truncate">{p.file.name}</span>
                        <span className="text-sm text-fg-faint tabular-nums">{p.progress >= 1 && !p.error ? 'uploaded' : p.progress > 0 ? `${Math.round(p.progress * 100)}%` : formatBytes(p.file.size)}</span>
                        {!pending && (
                          <IconButton label={`Remove ${p.file.name}`} size="sm" onClick={() => setFiles((fs) => fs.filter((_, j) => j !== i))}>
                            <X size={13} />
                          </IconButton>
                        )}
                      </div>
                      {p.progress > 0 && p.progress < 1 && <Meter value={p.progress * 100} label={`Uploading ${p.file.name}`} tone="accent" size="sm" />}
                      {p.error && <p className="text-sm text-danger">{p.error}</p>}
                    </li>
                  ))}
                </ul>
              )}
            </section>

            {save.isError && <Alert>{errorMessage(save.error)}</Alert>}
            {delAsset.isError && <Alert>{errorMessage(delAsset.error)}</Alert>}
            {uploadError && <Alert>{uploadError}</Alert>}
          </div>
        </Card>
        {/* Stays in view while the notes and files scroll. */}
        <div className="sticky bottom-4 z-10 mt-4 flex items-center justify-end gap-2 rounded-lg border border-border bg-surface-2 px-4 py-2.5 shadow-popover">
          <span className="mr-auto text-sm text-fg-faint">{release && !release.draft ? 'Changes apply right away.' : 'Drafts are only visible to people with write access.'}</span>
          {(!release || release.draft) && (
            <Button loading={pending && save.variables === true} disabled={!!invalid || pending} onClick={() => submit(true)}>
              {release ? 'Save draft' : 'Save as draft'}
            </Button>
          )}
          {release && !release.draft ? (
            <Button type="submit" variant="primary" loading={pending} disabled={!!invalid}>
              Save release
            </Button>
          ) : (
            <Button variant="primary" icon={<Rocket size={13} />} loading={pending && save.variables === false} disabled={!!invalid || pending} onClick={() => submit(false)}>
              Publish release
            </Button>
          )}
        </div>
      </form>
    </Container>
  )
}
