import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { Download, Eye, Code2, History, UserSquare } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { api, errorMessage, unwrap } from '../../api/client'
import { blobQuery, repoQuery } from '../../api/queries'
import { Breadcrumbs } from '../../components/code/Breadcrumbs'
import { CodeView } from '../../components/code/CodeView'
import { RefPicker } from '../../components/code/RefPicker'
import { Alert, Button, Spinner } from '../../components/ui'
import { formatBytes } from './CodePage'
import { useRepoName } from './RepoLayout'
import type { RefPathSearch } from './search'

const imageExt = /\.(png|jpe?g|gif|webp|svg)$/i
const markdownExt = /\.(md|markdown)$/i

export function rawUrl(repo: string, ref: string, path: string) {
  return `/api/v1/repos/${encodeURIComponent(repo)}/raw?${new URLSearchParams({ ref, path })}`
}

export function BlobPage() {
  const name = useRepoName()
  const search = useSearch({ strict: false }) as RefPathSearch
  const navigate = useNavigate()
  const repo = useQuery(repoQuery(name)).data!
  const ref = search.ref ?? repo.default_branch
  const path = search.path ?? ''
  const blob = useQuery({ ...blobQuery(name, { ref, path }), enabled: !!path })
  const isMarkdown = markdownExt.test(path)
  const [rendered, setRendered] = useState(true)
  const md = useQuery({
    queryKey: ['markdown', name, ref, path, blob.data?.sha],
    queryFn: () => unwrap(api.POST('/markdown', { body: { text: blob.data!.content, repo: name, ref } })),
    enabled: isMarkdown && rendered && blob.isSuccess && !blob.data.binary,
  })
  const lineCount = useMemo(() => (blob.data && !blob.data.binary ? blob.data.content.split('\n').length - (blob.data.content.endsWith('\n') ? 1 : 0) : 0), [blob.data])
  useEffect(() => setRendered(true), [path])

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-3 p-4">
      <div className="flex flex-wrap items-center gap-3">
        <RefPicker repo={name} current={ref} onSelect={(r) => void navigate({ to: '/$repo/blob', params: { repo: name }, search: { ref: r, path } })} />
        <Breadcrumbs repo={repo.name} gitRef={ref} path={path} />
      </div>
      {blob.isPending ? (
        <Spinner />
      ) : blob.isError ? (
        <Alert>{errorMessage(blob.error)}</Alert>
      ) : (
        <div className="overflow-hidden rounded-md border border-border">
          <div className="flex items-center gap-2 border-b border-border bg-bg-subtle px-3 py-1.5 text-[12px] text-fg-muted">
            {!blob.data.binary && (
              <span>
                {lineCount} lines ·{' '}
              </span>
            )}
            <span>{formatBytes(blob.data.size)}</span>
            <div className="flex-1" />
            {isMarkdown && (
              <Button variant="ghost" onClick={() => setRendered(!rendered)}>
                {rendered ? <Code2 size={13} /> : <Eye size={13} />} {rendered ? 'Source' : 'Preview'}
              </Button>
            )}
            {!blob.data.binary && (
              <Link to="/$repo/blame" params={{ repo: name }} search={{ ref, path }}>
                <Button variant="ghost">
                  <UserSquare size={13} /> Blame
                </Button>
              </Link>
            )}
            <Link to="/$repo/commits" params={{ repo: name }} search={{ ref, path }}>
              <Button variant="ghost">
                <History size={13} /> History
              </Button>
            </Link>
            <a href={rawUrl(name, ref, path)} target="_blank" rel="noreferrer">
              <Button variant="ghost">
                <Download size={13} /> Raw
              </Button>
            </a>
          </div>
          {imageExt.test(path) ? (
            <div className="flex justify-center bg-[repeating-conic-gradient(var(--bg-hover)_0_25%,transparent_0_50%)] bg-[length:16px_16px] p-6">
              <img src={rawUrl(name, ref, path)} alt={path} className="max-h-[70vh] max-w-full" />
            </div>
          ) : blob.data.binary ? (
            <p className="px-4 py-8 text-center text-fg-muted">
              Binary file not shown.{' '}
              <a className="text-accent hover:underline" href={rawUrl(name, ref, path)}>
                Download it
              </a>
              .
            </p>
          ) : isMarkdown && rendered ? (
            md.data ? <div className="markdown px-6 py-5" dangerouslySetInnerHTML={{ __html: md.data.html }} /> : <Spinner />
          ) : (
            <>
              {blob.data.truncated && <p className="border-b border-border px-3 py-1.5 text-[12px] text-warning">Showing the first 1 MiB. Use Raw for the whole file.</p>}
              <CodeView code={blob.data.content} path={path} />
            </>
          )}
        </div>
      )}
    </div>
  )
}
