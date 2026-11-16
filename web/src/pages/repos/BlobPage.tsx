import { useQuery } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { Code2, Download, Eye, History, Link as LinkIcon, UserSquare } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { api, unwrap } from '../../api/client'
import { blobQuery, repoQuery } from '../../api/queries'
import { PathBreadcrumbs } from '../../components/code/Breadcrumbs'
import { CodeView } from '../../components/code/CodeView'
import { RefPicker } from '../../components/code/RefPicker'
import { Alert, ButtonA, ButtonLink, CardHeaderBar, CopyButton, ErrorState, IconButton, SegmentedControl, Sep, Skeleton, Spinner, useCopy } from '../../components/ui'
import { formatBytes, plural } from '../../lib/format'
import { rawUrl } from '../../lib/repoUrls'
import { Container } from '../../components/Page'
import { useRepoName } from './RepoLayout'
import type { RefPathSearch } from './search'

const imageExt = /\.(png|jpe?g|gif|webp|svg)$/i
const markdownExt = /\.(md|markdown)$/i

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
  const [linkCopied, copyLink] = useCopy()
  const md = useQuery({
    queryKey: ['markdown', name, ref, path, blob.data?.sha],
    queryFn: () => unwrap(api.POST('/markdown', { body: { text: blob.data!.content, repo: name, ref } })),
    enabled: isMarkdown && rendered && blob.isSuccess && !blob.data.binary,
  })
  const lineCount = useMemo(() => (blob.data && !blob.data.binary ? blob.data.content.split('\n').length - (blob.data.content.endsWith('\n') ? 1 : 0) : 0), [blob.data])
  useEffect(() => setRendered(true), [path])

  return (
    <Container width="wide" className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <RefPicker repo={name} current={ref} onSelect={(r) => void navigate({ to: '/$repo/blob', params: { repo: name }, search: { ref: r, path } })} />
        <PathBreadcrumbs repo={repo.name} gitRef={ref} path={path} />
        <div className="flex-1" />
        <ButtonLink to="/$repo/commits" params={{ repo: name }} search={{ ref, path }} variant="ghost" icon={<History size={14} />}>
          History
        </ButtonLink>
      </div>
      {blob.isError ? (
        <ErrorState error={blob.error} className="p-0" />
      ) : (
        <div className="overflow-hidden rounded-lg border border-border bg-surface-1 shadow-card">
          <CardHeaderBar className="sticky top-0 z-10 px-4 text-fg-muted">
            {blob.isPending ? (
              <Skeleton className="h-3 w-32" />
            ) : (
              <>
                {!blob.data.binary && (
                  <SegmentedControl
                    size="sm"
                    label="View"
                    value={isMarkdown && rendered ? 'preview' : 'code'}
                    onChange={(v) => {
                      if (v === 'blame') void navigate({ to: '/$repo/blame', params: { repo: name }, search: { ref, path } })
                      else setRendered(v === 'preview')
                    }}
                    options={[
                      ...(isMarkdown ? [{ value: 'preview' as const, label: 'Preview', icon: <Eye size={12} /> }] : []),
                      { value: 'code' as const, label: 'Code', icon: <Code2 size={12} /> },
                      { value: 'blame' as const, label: 'Blame', icon: <UserSquare size={12} /> },
                    ]}
                  />
                )}
                <span className="tabular-nums">
                  {!blob.data.binary && (
                    <>
                      {plural(lineCount, 'line')} <Sep />{' '}
                    </>
                  )}
                  {formatBytes(blob.data.size)}
                </span>
                <div className="flex-1" />
                <ButtonA href={rawUrl(name, ref, path)} target="_blank" rel="noreferrer" variant="ghost" size="sm" icon={<Download size={13} />}>
                  Raw
                </ButtonA>
                {!blob.data.binary && !blob.data.truncated && <CopyButton value={blob.data.content} label="Copy file contents" />}
                <IconButton label="Copy link" tooltip={linkCopied ? 'Link copied!' : 'Copy link'} size="sm" onClick={() => copyLink(window.location.href)}>
                  <LinkIcon size={13} />
                </IconButton>
              </>
            )}
          </CardHeaderBar>
          {blob.isPending ? (
            <div className="flex flex-col gap-2 p-4" role="status" aria-label="Loading">
              {Array.from({ length: 8 }, (_, i) => (
                <Skeleton key={i} className="h-3" style={{ width: `${30 + ((i * 23) % 55)}%` }} />
              ))}
            </div>
          ) : imageExt.test(path) ? (
            <div className="flex justify-center bg-[repeating-conic-gradient(var(--bg-hover)_0_25%,transparent_0_50%)] bg-[length:16px_16px] p-8">
              <img src={rawUrl(name, ref, path)} alt={path} className="max-h-[70vh] max-w-full" />
            </div>
          ) : blob.data.binary ? (
            <p className="px-4 py-10 text-center text-fg-muted">
              Binary file not shown.{' '}
              <a className="text-accent hover:underline" href={rawUrl(name, ref, path)}>
                Download it
              </a>
              .
            </p>
          ) : isMarkdown && rendered ? (
            md.data ? <div className="markdown markdown-doc px-6 py-6 md:px-10 md:py-8" dangerouslySetInnerHTML={{ __html: md.data.html }} /> : <Spinner />
          ) : (
            <>
              {blob.data.truncated && (
                <div className="border-b border-border p-2">
                  <Alert tone="warning">Showing the first 1 MiB. Use Raw for the whole file.</Alert>
                </div>
              )}
              <CodeView code={blob.data.content} path={path} />
            </>
          )}
        </div>
      )}
    </Container>
  )
}
