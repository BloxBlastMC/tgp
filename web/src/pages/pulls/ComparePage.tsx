import { useMutation, useQuery } from '@tanstack/react-query'
import { Link, useNavigate, useParams } from '@tanstack/react-router'
import { ArrowLeft, CheckCircle2, GitCommitHorizontal, XCircle } from 'lucide-react'
import { useEffect, useState } from 'react'
import { api, ApiRequestError, errorMessage, unwrap } from '../../api/client'
import { branchesQuery, compareQuery, repoQuery, sessionQuery, usersQuery } from '../../api/queries'
import { DiffView } from '../../components/code/DiffView'
import { Avatar, MarkdownEditor, useInvalidatePull } from '../../components/pulls/common'
import { ReviewerPicker } from '../../components/pulls/ReviewerPicker'
import { Alert, Button, Checkbox, Field, Input, Select, Spinner, Time } from '../../components/ui'
import { compareSplat, parseCompare } from '../../lib/pulls'
import { useRepoName } from '../repos/RepoLayout'
import type { Commit } from '../../api/client'

export function CommitList({ repo, commits }: { repo: string; commits: Commit[] }) {
  return (
    <ul className="overflow-hidden rounded-md border border-border">
      {commits.map((c) => (
        <li key={c.sha} className="flex items-center gap-3 border-b border-border px-3 py-2 last:border-0 hover:bg-bg-subtle">
          <GitCommitHorizontal size={14} className="shrink-0 text-fg-faint" />
          <Link to="/$repo/commit/$sha" params={{ repo, sha: c.sha }} className="min-w-0 flex-1 truncate hover:text-accent">
            {c.subject}
          </Link>
          <span className="shrink-0 text-[12px] text-fg-faint">
            {c.author.name} · <Time iso={c.author.date} />
          </span>
          <Link to="/$repo/commit/$sha" params={{ repo, sha: c.sha }} className="shrink-0 font-mono text-[12px] text-fg-muted hover:text-accent">
            {c.sha.slice(0, 7)}
          </Link>
        </li>
      ))}
    </ul>
  )
}

export function ComparePage() {
  const name = useRepoName()
  const { _splat } = useParams({ strict: false }) as { _splat?: string }
  const navigate = useNavigate()
  const repo = useQuery(repoQuery(name)).data!
  const me = useQuery(sessionQuery).data!.user
  const branches = useQuery(branchesQuery(name))
  const parsed = parseCompare(_splat)
  const base = parsed.base ?? repo.default_branch
  const head = parsed.head ?? ''
  const ready = !!head && head !== base
  const cmp = useQuery({ ...compareQuery(name, base, head), enabled: ready })
  const canWrite = repo.permission === 'write' || repo.permission === 'admin'

  const go = (b: string, h: string) => void navigate({ to: '/$repo/compare/$', params: { repo: name, _splat: compareSplat(b, h) }, replace: true })

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-4 p-4">
      <div>
        <Link to="/$repo/pulls" params={{ repo: name }} className="inline-flex items-center gap-1 text-[12px] text-fg-muted hover:text-fg">
          <ArrowLeft size={12} /> Pull requests
        </Link>
        <h2 className="mt-1 text-[15px] font-semibold">{ready ? 'Open a pull request' : 'Compare changes'}</h2>
        <p className="text-fg-muted">Choose a branch to merge into the base branch.</p>
      </div>
      <div className="flex flex-wrap items-end gap-3 rounded-md border border-border bg-bg-subtle px-3 py-2.5">
        <div className="w-56">
          <Field label="Base">
            {(id) => (
              <Select id={id} value={base} onChange={(e) => go(e.target.value, head)}>
                {(branches.data ?? [{ name: base }]).map((b) => (
                  <option key={b.name}>{b.name}</option>
                ))}
              </Select>
            )}
          </Field>
        </div>
        <span className="pb-1.5 text-fg-faint">←</span>
        <div className="w-56">
          <Field label="Compare">
            {(id) => (
              <Select id={id} value={head} onChange={(e) => go(base, e.target.value)}>
                <option value="">Choose a branch…</option>
                {(branches.data ?? []).filter((b) => b.name !== base).map((b) => (
                  <option key={b.name}>{b.name}</option>
                ))}
              </Select>
            )}
          </Field>
        </div>
        {cmp.data && (
          <div className="flex items-center gap-1.5 pb-1.5 text-[12px]">
            {cmp.data.mergeable === 'clean' ? (
              <>
                <CheckCircle2 size={13} className="text-success" /> <span className="text-success">Can be merged.</span>
              </>
            ) : (
              <>
                <XCircle size={13} className="text-danger" />
                <span className="text-danger">Conflicts in {cmp.data.conflict_files.join(', ')}.</span>
                <span className="text-fg-muted">You can still open the pull request.</span>
              </>
            )}
            <span className="text-fg-faint">
              · {cmp.data.ahead_by} ahead, {cmp.data.behind_by} behind
            </span>
          </div>
        )}
      </div>
      {!ready ? (
        <p className="py-10 text-center text-fg-muted">{head === base ? 'Pick two different branches.' : 'Pick the branch with your changes.'}</p>
      ) : cmp.isPending ? (
        <Spinner />
      ) : cmp.isError ? (
        <Alert>{cmp.error instanceof ApiRequestError && cmp.error.status === 404 ? `Branch not found: ${cmp.error.message}` : errorMessage(cmp.error)}</Alert>
      ) : cmp.data.ahead_by === 0 ? (
        <p className="py-10 text-center text-fg-muted">
          <span className="font-mono">{head}</span> has nothing that isn't already in <span className="font-mono">{base}</span>.
        </p>
      ) : (
        <>
          {cmp.data.existing_pull ? (
            <Alert tone="info">
              A pull request for these branches is already open:{' '}
              <Link to="/$repo/pulls/$number" params={{ repo: name, number: String(cmp.data.existing_pull) }} className="font-medium underline">
                #{cmp.data.existing_pull}
              </Link>
            </Alert>
          ) : canWrite ? (
            <NewPullForm key={`${base}...${head}`} repo={name} base={base} head={head} commits={cmp.data.commits} me={me.username} />
          ) : (
            <Alert tone="info">Opening pull requests needs write access.</Alert>
          )}
          <section>
            <h3 className="mb-1.5 text-[12px] font-medium text-fg-muted">
              {cmp.data.commits.length} commit{cmp.data.commits.length === 1 ? '' : 's'}
            </h3>
            <CommitList repo={name} commits={cmp.data.commits} />
          </section>
          <DiffView diff={cmp.data.diff} />
        </>
      )}
    </div>
  )
}

function NewPullForm({ repo, base, head, commits, me }: { repo: string; base: string; head: string; commits: Commit[]; me: string }) {
  const navigate = useNavigate()
  const users = useQuery(usersQuery)
  const first = commits.length === 1 ? commits[0] : undefined
  const [title, setTitle] = useState(first ? first.subject : head.replace(/[-_/]+/g, ' ').replace(/^\w/, (c) => c.toUpperCase()))
  const [body, setBody] = useState(first?.body ?? '')
  const [reviewers, setReviewers] = useState<string[]>([])
  const [draft, setDraft] = useState(false)
  const invalidate = useInvalidatePull(repo, 0)
  const create = useMutation({
    mutationFn: () => unwrap(api.POST('/repos/{repo}/pulls', { params: { path: { repo } }, body: { title, body, head, base, draft, reviewers } })),
    onSuccess: (pr) => {
      invalidate()
      void navigate({ to: '/$repo/pulls/$number', params: { repo, number: String(pr.number) } })
    },
  })
  useEffect(() => create.reset(), [base, head]) // eslint-disable-line react-hooks/exhaustive-deps
  const people = (users.data ?? []).filter((u) => reviewers.includes(u.username))
  return (
    <form
      className="grid gap-4 md:grid-cols-[1fr_220px]"
      onSubmit={(e) => {
        e.preventDefault()
        create.mutate()
      }}
    >
      <div className="flex flex-col gap-3 rounded-md border border-border p-3">
        <Field label="Title">{(id) => <Input id={id} value={title} onChange={(e) => setTitle(e.target.value)} autoFocus />}</Field>
        <MarkdownEditor value={body} onChange={setBody} repo={repo} label="Description" placeholder="Describe the change: what, why, how to test." minRows={6} />
        {create.isError && <Alert>{errorMessage(create.error)}</Alert>}
        <div className="flex items-center justify-end gap-3">
          <Checkbox checked={draft} onChange={setDraft} label="Create as draft" />
          <Button type="submit" variant="primary" loading={create.isPending} disabled={!title.trim()}>
            Create pull request
          </Button>
        </div>
      </div>
      <aside className="flex flex-col gap-2">
        <div className="flex items-center justify-between border-b border-border pb-1 text-[12px] font-medium text-fg-muted">
          Reviewers
          <ReviewerPicker value={reviewers} exclude={[me]} onChange={setReviewers} />
        </div>
        {people.length === 0 ? (
          <p className="text-[12px] text-fg-faint">No reviewers yet.</p>
        ) : (
          <ul className="flex flex-col gap-1">
            {people.map((u) => (
              <li key={u.id} className="flex items-center gap-2">
                <Avatar user={u} size={18} /> {u.display_name}
              </li>
            ))}
          </ul>
        )}
      </aside>
    </form>
  )
}
