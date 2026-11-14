import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams } from '@tanstack/react-router'
import { ArrowLeft, FileArchive, Upload, X } from 'lucide-react'
import { useEffect, useId, useRef, useState } from 'react'
import { api, errorMessage, unwrap, type Release } from '../../api/client'
import { branchesQuery, repoQuery, tagsQuery } from '../../api/queries'
import { EmptyState } from '../../components/Page'
import { MarkdownEditor } from '../../components/pulls/common'
import { Alert, Button, Checkbox, Field, Input, Select, Spinner } from '../../components/ui'
import { formatBytes } from '../../lib/ci'
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
  if (q.isPending) return <Spinner />
  if (q.isError) return <div className="p-4"><Alert>{errorMessage(q.error)}</Alert></div>
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
  const listId = useId()
  const fileRef = useRef<HTMLInputElement>(null)
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
      if (ok) void navigate({ to: '/$repo/releases/tag/$', params: { repo, _splat: rel.tag } })
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

  if (!canWriteRepo(r.permission)) return <EmptyState title="Can't edit releases">You need write access to {repo}.</EmptyState>

  const addFiles = (list: FileList | null) => {
    if (!list) return
    const add = Array.from(list).filter((f) => !files.some((p) => p.file.name === f.name))
    setFiles((fs) => [...fs, ...add.map((file) => ({ file, progress: 0 }))])
  }
  const assets = release?.assets ?? []
  const pending = save.isPending || busy
  const submit = (draft: boolean) => !invalid && save.mutate(draft)

  return (
    <div className="mx-auto flex max-w-4xl flex-col gap-4 p-4">
      <div>
        <Link to="/$repo/releases" params={{ repo }} search={{}} className="inline-flex items-center gap-1 text-sm text-fg-muted hover:text-fg">
          <ArrowLeft size={12} /> Releases
        </Link>
        <h2 className="mt-1 text-lg font-semibold">{release ? `Edit ${releaseTitle(release)}` : 'New release'}</h2>
      </div>
      <form
        aria-label={release ? 'Edit release' : 'New release'}
        className="flex flex-col gap-4 rounded-md border border-border p-4"
        onSubmit={(e) => {
          e.preventDefault()
          submit(release ? release.draft : false)
        }}
      >
        <div className="grid gap-3 md:grid-cols-2">
          <Field
            label="Tag"
            hint={
              tag.trim() && !invalid
                ? existingTag
                  ? `Existing tag at ${existingTag.commit.slice(0, 7)}`
                  : tagLocked
                    ? 'The tag is renamed in the release only'
                    : 'New tag — created from the target when the release is published'
                : undefined
            }
          >
            {(id) => (
              <>
                <Input id={id} value={tag} onChange={(e) => setTag(e.target.value)} placeholder="v1.0.0" list={listId} autoFocus={!release} className="font-mono" />
                <datalist id={listId}>
                  {(tags.data ?? []).map((t) => (
                    <option key={t.name} value={t.name} />
                  ))}
                </datalist>
              </>
            )}
          </Field>
          {!existingTag && (
            <Field label="Target" hint="Branch the new tag points at">
              {(id) => (
                <Select id={id} value={target} onChange={(e) => setTarget(e.target.value)}>
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
        {tag.trim() && invalid && <p className="-mt-2 text-sm text-danger">{invalid}</p>}
        <Field label="Title">{(id) => <Input id={id} value={name} onChange={(e) => setName(e.target.value)} placeholder={tag.trim() || 'Release title'} maxLength={200} />}</Field>
        <MarkdownEditor value={body} onChange={setBody} repo={repo} label="Release notes" placeholder="What changed in this release?" minRows={10} onSubmit={() => submit(release ? release.draft : false)} />
        <Checkbox checked={prerelease} onChange={setPrerelease} label="This is a pre-release" hint="Pre-releases are never marked Latest." />

        <section aria-label="Release files" className="flex flex-col gap-2">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-medium text-fg-muted">Files</h3>
            <Button onClick={() => fileRef.current?.click()}>
              <Upload size={13} /> Attach files
            </Button>
            <input ref={fileRef} type="file" multiple hidden aria-label="Attach files" onChange={(e) => (addFiles(e.target.files), (e.target.value = ''))} />
          </div>
          <div
            className="flex min-h-14 flex-col justify-center rounded border border-dashed border-border-strong bg-bg-subtle"
            onDragOver={(e) => e.preventDefault()}
            onDrop={(e) => {
              e.preventDefault()
              addFiles(e.dataTransfer.files)
            }}
          >
            {assets.length === 0 && files.length === 0 && <p className="py-3 text-center text-sm text-fg-faint">Drop files here or attach them. They're uploaded when you save.</p>}
            <ul className="divide-y divide-border">
              {assets.map((a) => (
                <li key={a.id} className="flex h-8 items-center gap-2 px-2.5 text-base">
                  <FileArchive size={13} className="text-fg-muted" />
                  <a href={assetUrl(repo, release!.id, a.id)} className="min-w-0 flex-1 truncate hover:underline">
                    {a.name}
                  </a>
                  <span className="text-sm text-fg-faint tabular-nums">{formatBytes(a.size_bytes)}</span>
                  <button
                    type="button"
                    aria-label={`Delete ${a.name}`}
                    className="rounded p-1 text-fg-muted hover:bg-bg-hover hover:text-danger disabled:opacity-40"
                    disabled={delAsset.isPending}
                    onClick={() => window.confirm(`Delete ${a.name}?`) && delAsset.mutate(a.id)}
                  >
                    <X size={13} />
                  </button>
                </li>
              ))}
              {files.map((p, i) => (
                <li key={p.file.name} className="flex flex-col gap-1 px-2.5 py-1.5 text-base">
                  <div className="flex items-center gap-2">
                    <Upload size={13} className="text-accent" />
                    <span className="min-w-0 flex-1 truncate">{p.file.name}</span>
                    <span className="text-sm text-fg-faint tabular-nums">{p.progress >= 1 && !p.error ? 'uploaded' : p.progress > 0 ? `${Math.round(p.progress * 100)}%` : formatBytes(p.file.size)}</span>
                    {!pending && (
                      <button type="button" aria-label={`Remove ${p.file.name}`} className="rounded p-1 text-fg-muted hover:bg-bg-hover hover:text-fg" onClick={() => setFiles((fs) => fs.filter((_, j) => j !== i))}>
                        <X size={13} />
                      </button>
                    )}
                  </div>
                  {p.progress > 0 && p.progress < 1 && (
                    <div className="h-1 overflow-hidden rounded bg-bg-hover">
                      <div className="h-full bg-accent transition-[width]" style={{ width: `${p.progress * 100}%` }} />
                    </div>
                  )}
                  {p.error && <p className="text-sm text-danger">{p.error}</p>}
                </li>
              ))}
            </ul>
          </div>
        </section>

        {save.isError && <Alert>{errorMessage(save.error)}</Alert>}
        {delAsset.isError && <Alert>{errorMessage(delAsset.error)}</Alert>}
        {uploadError && <Alert>{uploadError}</Alert>}
        <div className="flex justify-end gap-2">
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
            <Button variant="primary" loading={pending && save.variables === false} disabled={!!invalid || pending} onClick={() => submit(false)}>
              Publish release
            </Button>
          )}
        </div>
      </form>
    </div>
  )
}
