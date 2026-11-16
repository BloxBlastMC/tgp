import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { BookOpen, CornerLeftUp, File, FileCode2, FileImage, FileText, Folder, FolderGit, GitBranch, History, Link2, Tag, Terminal } from 'lucide-react'
import type { ComponentType } from 'react'
import { api, ApiRequestError, errorMessage, unwrap } from '../../api/client'
import { branchesQuery, readmeQuery, repoQuery, tagsQuery, treeQuery } from '../../api/queries'
import { Container } from '../../components/Page'
import { PathBreadcrumbs } from '../../components/code/Breadcrumbs'
import { commitAuthor } from '../../components/code/CommitList'
import { RefPicker } from '../../components/code/RefPicker'
import { Alert, Avatar, Card, CardHeaderBar, CopyButton, Skeleton, Time } from '../../components/ui'
import { formatBytes, shortSha } from '../../lib/format'
import { useRepoName } from './RepoLayout'
import type { RefPathSearch } from './search'

function fileIcon(name: string): ComponentType<{ size?: number; className?: string }> {
  if (/\.(png|jpe?g|gif|webp|svg|ico)$/i.test(name)) return FileImage
  if (/\.(md|markdown|txt|rst)$/i.test(name)) return FileText
  if (/\.(java|kt|go|ts|tsx|js|jsx|py|rb|rs|c|h|cpp|cs|php|sh|gradle|ya?ml|toml|json|xml|html|css|sql)$/i.test(name)) return FileCode2
  return File
}

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
  const branches = useQuery({ ...branchesQuery(name), enabled: !repo.empty })
  const tags = useQuery({ ...tagsQuery(name), enabled: !repo.empty })

  if (repo.empty) return <EmptyRepo cloneUrl={repo.clone_url} branch={repo.default_branch} name={repo.name} />

  const lastCommit = last.data?.commits[0]
  const parent = path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : undefined
  return (
    <Container width="wide" className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <RefPicker repo={name} current={ref} onSelect={(r) => void navigate({ to: '/$repo', params: { repo: name }, search: { ref: r, path: path || undefined } })} />
        <PathBreadcrumbs repo={repo.name} gitRef={ref} path={path} />
        <div className="flex-1" />
        <nav aria-label="Repository history" className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-fg-muted">
          <Link to="/$repo/branches" params={{ repo: name }} className="inline-flex items-center gap-1.5 hover:text-accent">
            <GitBranch size={13} className="text-fg-faint" />
            {branches.data ? <span className="font-semibold text-fg tabular-nums">{branches.data.length}</span> : null}
            {branches.data?.length === 1 ? 'branch' : 'branches'}
          </Link>
          <Link to="/$repo/branches" params={{ repo: name }} hash="tags" className="inline-flex items-center gap-1.5 hover:text-accent">
            <Tag size={13} className="text-fg-faint" />
            {tags.data ? <span className="font-semibold text-fg tabular-nums">{tags.data.length}</span> : null}
            {tags.data?.length === 1 ? 'tag' : 'tags'}
          </Link>
          <Link to="/$repo/commits" params={{ repo: name }} search={{ ref, path: path || undefined }} className="inline-flex items-center gap-1.5 hover:text-accent">
            <History size={13} className="text-fg-faint" />
            {path ? 'History' : 'Commits'}
          </Link>
        </nav>
      </div>

      {tree.isError ? (
        <Alert>{tree.error instanceof ApiRequestError && tree.error.status === 404 ? `Nothing at ${path || '/'} on ${ref}.` : errorMessage(tree.error)}</Alert>
      ) : (
        <div className="overflow-hidden rounded-lg border border-border bg-surface-1 shadow-card">
          <CardHeaderBar className="min-h-11 px-4">
            {lastCommit ? (
              <>
                <Avatar user={commitAuthor(lastCommit)} size={20} />
                <span className="shrink-0 font-medium">{lastCommit.author.name}</span>
                <Link to="/$repo/commit/$sha" params={{ repo: name, sha: lastCommit.sha }} className="min-w-0 flex-1 truncate text-fg-muted hover:text-accent">
                  {lastCommit.subject}
                </Link>
                <Link to="/$repo/commit/$sha" params={{ repo: name, sha: lastCommit.sha }} className="shrink-0 font-mono text-fg-faint hover:text-accent">
                  {shortSha(lastCommit.sha)}
                </Link>
                <span className="shrink-0 text-fg-faint">
                  <Time iso={lastCommit.committer.date} />
                </span>
              </>
            ) : (
              <>
                <Skeleton className="size-5 rounded-full" />
                <Skeleton className="h-3 w-64" />
              </>
            )}
          </CardHeaderBar>
          {tree.isPending ? (
            <div role="status" aria-label="Loading">
              {Array.from({ length: 5 }, (_, i) => (
                <div key={i} className="flex items-center gap-2.5 border-b border-border px-4 py-2.5 last:border-0">
                  <Skeleton className="size-4" />
                  <Skeleton className={['h-3 w-32', 'h-3 w-24', 'h-3 w-40', 'h-3 w-28', 'h-3 w-20'][i]} />
                </div>
              ))}
            </div>
          ) : (
            <table className="w-full">
              <tbody>
                {path && (
                  <tr className="border-b border-border hover:bg-bg-hover">
                    <td className="px-4 py-2" colSpan={2}>
                      <Link to="/$repo" params={{ repo: name }} search={{ ref, path: parent }} className="flex items-center gap-2.5 text-fg-muted hover:text-accent" aria-label="Parent directory">
                        <CornerLeftUp size={14} className="shrink-0" /> ..
                      </Link>
                    </td>
                  </tr>
                )}
                {tree.data.entries.map((e) => {
                  const Icon = fileIcon(e.name)
                  return (
                    <tr key={e.path} className="border-b border-border last:border-0 hover:bg-bg-hover">
                      <td className="px-4 py-2">
                        {e.type === 'dir' ? (
                          <Link to="/$repo" params={{ repo: name }} search={{ ref, path: e.path }} className="flex items-center gap-2.5 hover:text-accent hover:underline">
                            <Folder size={15} className="shrink-0 fill-accent/20 text-accent" /> {e.name}
                          </Link>
                        ) : e.type === 'submodule' ? (
                          <span className="flex items-center gap-2.5 text-fg-muted">
                            <FolderGit size={15} className="shrink-0" /> {e.name} <span className="font-mono text-xs text-fg-faint">@ {shortSha(e.sha)}</span>
                          </span>
                        ) : (
                          <Link to="/$repo/blob" params={{ repo: name }} search={{ ref, path: e.path }} className="flex items-center gap-2.5 hover:text-accent hover:underline">
                            {e.type === 'symlink' ? <Link2 size={15} className="shrink-0 text-fg-faint" /> : <Icon size={15} className="shrink-0 text-fg-faint" />} {e.name}
                          </Link>
                        )}
                      </td>
                      <td className="w-28 px-4 py-2 text-right text-sm text-fg-faint tabular-nums">{e.type === 'file' ? formatBytes(e.size) : ''}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          )}
        </div>
      )}

      {readme.data && (
        <div className="overflow-hidden rounded-lg border border-border bg-surface-1 shadow-card">
          <CardHeaderBar className="px-4 font-medium">
            <BookOpen size={14} className="text-fg-muted" /> {readme.data.path.split('/').pop()}
          </CardHeaderBar>
          <div className="markdown markdown-doc px-6 py-6 md:px-10 md:py-8" dangerouslySetInnerHTML={{ __html: readme.data.html }} />
        </div>
      )}
    </Container>
  )
}

function Snippet({ title, code }: { title: string; code: string }) {
  return (
    <div>
      <p className="mb-1.5 text-sm font-medium text-fg-muted">{title}</p>
      <div className="relative">
        <pre className="overflow-x-auto rounded-lg border border-border bg-bg-subtle px-4 py-3 pr-12 font-mono text-sm leading-6">{code}</pre>
        <CopyButton value={code} label={`Copy: ${title}`} className="absolute top-2 right-2" />
      </div>
    </div>
  )
}

function EmptyRepo({ cloneUrl, branch, name }: { cloneUrl: string; branch: string; name: string }) {
  return (
    <Container width="default">
      <Card>
        <div className="flex flex-col gap-5">
          <div>
            <h2 className="text-lg font-semibold">This repository is empty</h2>
            <p className="mt-1 text-fg-muted">
              Push an existing repository, or clone it and add files. Authenticate with your username and a{' '}
              <Link to="/settings" hash="tokens" className="text-accent hover:underline">
                personal access token
              </Link>
              .
            </p>
          </div>
          <div>
            <p className="mb-1.5 text-sm font-medium text-fg-muted">Clone URL</p>
            <div className="flex items-center gap-2 rounded-md border border-border-strong bg-bg py-1 pr-1 pl-2.5">
              <code className="min-w-0 flex-1 font-mono text-code break-all select-all">{cloneUrl}</code>
              <CopyButton value={cloneUrl} label="Copy clone URL" />
            </div>
          </div>
          <Snippet title="Push an existing repository" code={`git remote add origin ${cloneUrl}\ngit push -u origin ${branch}`} />
          <Snippet
            title="Start from scratch"
            code={`git clone ${cloneUrl}\ncd ${name}\necho "# ${name}" > README.md\ngit add README.md && git commit -m "Initial commit"\ngit push -u origin ${branch}`}
          />
          <p className="flex items-center gap-2 text-sm text-fg-muted">
            <Terminal size={14} className="text-fg-faint" /> Or with the tgp CLI: <code className="font-mono text-fg">tgp repo clone {name}</code>
          </p>
        </div>
      </Card>
    </Container>
  )
}
