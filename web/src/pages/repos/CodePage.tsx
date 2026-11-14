import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { BookOpen, File, Folder, FolderGit, History, Link2 } from 'lucide-react'
import { api, ApiRequestError, errorMessage, unwrap } from '../../api/client'
import { readmeQuery, repoQuery, treeQuery } from '../../api/queries'
import { Breadcrumbs } from '../../components/code/Breadcrumbs'
import { RefPicker } from '../../components/code/RefPicker'
import { Alert, Button, SecretValue, Spinner, Time } from '../../components/ui'
import { useRepoName } from './RepoLayout'
import type { RefPathSearch } from './search'

export function CodePage() {
  const name = useRepoName()
  const search = useSearch({ strict: false }) as RefPathSearch
  const navigate = useNavigate()
  const repo = useQuery(repoQuery(name)).data!
  const ref = search.ref ?? repo.default_branch
  const path = search.path ?? ''
  const tree = useQuery({ ...treeQuery(name, { ref, path }), enabled: !repo.empty })
  const readme = useQuery({ ...readmeQuery(name, { ref, path }), enabled: !repo.empty && tree.isSuccess })
  const last = useQuery({
    queryKey: ['repo', name, 'last-commit', ref, path],
    queryFn: () => unwrap(api.GET('/repos/{repo}/commits', { params: { path: { repo: name }, query: { ref, path, per_page: 1 } } })),
    enabled: !repo.empty,
  })

  if (repo.empty) return <EmptyRepo cloneUrl={repo.clone_url} branch={repo.default_branch} />

  const lastCommit = last.data?.commits[0]
  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-3 p-4">
      <div className="flex flex-wrap items-center gap-3">
        <RefPicker repo={name} current={ref} onSelect={(r) => void navigate({ to: '/$repo', params: { repo: name }, search: { ref: r, path: path || undefined } })} />
        <Breadcrumbs repo={repo.name} gitRef={ref} path={path} />
        <div className="flex-1" />
        <Link to="/$repo/commits" params={{ repo: name }} search={{ ref, path: path || undefined }}>
          <Button variant="ghost">
            <History size={14} /> History
          </Button>
        </Link>
      </div>

      {tree.isPending ? (
        <Spinner />
      ) : tree.isError ? (
        <Alert>{tree.error instanceof ApiRequestError && tree.error.status === 404 ? `Nothing at ${path || '/'} on ${ref}.` : errorMessage(tree.error)}</Alert>
      ) : (
        <div className="overflow-hidden rounded-md border border-border">
          {lastCommit && (
            <div className="flex items-center gap-2 border-b border-border bg-bg-subtle px-3 py-2 text-sm">
              <span className="font-medium">{lastCommit.author.name}</span>
              <Link to="/$repo/commit/$sha" params={{ repo: name, sha: lastCommit.sha }} className="min-w-0 flex-1 truncate text-fg-muted hover:text-accent">
                {lastCommit.subject}
              </Link>
              <Link to="/$repo/commit/$sha" params={{ repo: name, sha: lastCommit.sha }} className="font-mono text-fg-faint hover:text-accent">
                {lastCommit.sha.slice(0, 7)}
              </Link>
              <span className="text-fg-faint">
                <Time iso={lastCommit.committer.date} />
              </span>
            </div>
          )}
          <table className="w-full">
            <tbody>
              {path && (
                <tr className="border-b border-border hover:bg-bg-subtle">
                  <td className="px-3 py-1.5" colSpan={2}>
                    <Link
                      to="/$repo"
                      params={{ repo: name }}
                      search={{ ref, path: path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : undefined }}
                      className="text-fg-muted hover:text-accent"
                    >
                      ..
                    </Link>
                  </td>
                </tr>
              )}
              {tree.data.entries.map((e) => (
                <tr key={e.path} className="border-b border-border last:border-0 hover:bg-bg-subtle">
                  <td className="px-3 py-1.5">
                    {e.type === 'dir' ? (
                      <Link to="/$repo" params={{ repo: name }} search={{ ref, path: e.path }} className="flex items-center gap-2 hover:text-accent hover:underline">
                        <Folder size={14} className="shrink-0 text-accent" /> {e.name}
                      </Link>
                    ) : e.type === 'submodule' ? (
                      <span className="flex items-center gap-2 text-fg-muted">
                        <FolderGit size={14} className="shrink-0" /> {e.name} <span className="font-mono text-xs text-fg-faint">@ {e.sha.slice(0, 7)}</span>
                      </span>
                    ) : (
                      <Link to="/$repo/blob" params={{ repo: name }} search={{ ref, path: e.path }} className="flex items-center gap-2 hover:text-accent hover:underline">
                        {e.type === 'symlink' ? <Link2 size={14} className="shrink-0 text-fg-faint" /> : <File size={14} className="shrink-0 text-fg-faint" />} {e.name}
                      </Link>
                    )}
                  </td>
                  <td className="w-24 px-3 py-1.5 text-right text-sm text-fg-faint">{e.type === 'file' ? formatBytes(e.size) : ''}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {readme.data && (
        <div className="overflow-hidden rounded-md border border-border">
          <div className="flex items-center gap-2 border-b border-border bg-bg-subtle px-3 py-2 text-sm font-medium">
            <BookOpen size={13} /> {readme.data.path.split('/').pop()}
          </div>
          <div className="markdown px-6 py-5" dangerouslySetInnerHTML={{ __html: readme.data.html }} />
        </div>
      )}
    </div>
  )
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

function EmptyRepo({ cloneUrl, branch }: { cloneUrl: string; branch: string }) {
  return (
    <div className="mx-auto flex max-w-2xl flex-col gap-4 p-6">
      <div>
        <h2 className="text-lg font-semibold">This repository is empty</h2>
        <p className="text-fg-muted">
          Push an existing repository, or clone it and add files. Authenticate with your username and a{' '}
          <Link to="/settings" className="text-accent hover:underline">
            personal access token
          </Link>
          .
        </p>
      </div>
      <div>
        <p className="mb-1 text-sm font-medium text-fg-muted">Clone URL</p>
        <SecretValue value={cloneUrl} />
      </div>
      <div>
        <p className="mb-1 text-sm font-medium text-fg-muted">Push an existing repository</p>
        <pre className="rounded border border-border bg-bg-subtle p-3 font-mono text-sm leading-5">
          {`git remote add origin ${cloneUrl}\ngit push -u origin ${branch}`}
        </pre>
      </div>
      <div>
        <p className="mb-1 text-sm font-medium text-fg-muted">Start from scratch</p>
        <pre className="rounded border border-border bg-bg-subtle p-3 font-mono text-sm leading-5">
          {`git clone ${cloneUrl}\ncd ${cloneUrl.split('/').pop()!.replace(/\.git$/, '')}\necho "# Hello" > README.md\ngit add README.md && git commit -m "Initial commit"\ngit push -u origin ${branch}`}
        </pre>
      </div>
    </div>
  )
}
