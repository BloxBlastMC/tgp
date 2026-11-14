import { useMutation, useQuery } from '@tanstack/react-query'
import { Link, useNavigate, useParams } from '@tanstack/react-router'
import { ArrowLeft, CheckCircle2, GitCompareArrows, GitPullRequest, XCircle } from 'lucide-react'
import { useEffect, useState } from 'react'
import { api, ApiRequestError, errorMessage, unwrap, type Commit } from '../../api/client'
import { compareQuery, labelsQuery, milestonesQuery, repoQuery, sessionQuery, usersQuery } from '../../api/queries'
import { CommitList } from '../../components/code/CommitList'
import { DiffView } from '../../components/code/DiffView'
import { RefPicker } from '../../components/code/RefPicker'
import { MetaSections, type MetaPatch } from '../../components/issues/meta'
import { SubHeader } from '../../components/Page'
import { MarkdownEditor, useInvalidatePull } from '../../components/pulls/common'
import { ReviewerPicker } from '../../components/pulls/ReviewerPicker'
import { Alert, Avatar, Button, Card, Checkbox, EmptyState, Field, Input, MetaSection, SkeletonRows, useToast } from '../../components/ui'
import { plural } from '../../lib/format'
import { canWrite as canWritePerm } from '../../lib/permissions'
import { compareSplat, parseCompare } from '../../lib/pulls'
import { useRepoName } from '../repos/RepoLayout'

export function ComparePage() {
  const name = useRepoName()
  const { _splat } = useParams({ strict: false }) as { _splat?: string }
  const navigate = useNavigate()
  const repo = useQuery(repoQuery(name)).data!
  const me = useQuery(sessionQuery).data!.user
  const parsed = parseCompare(_splat)
  const base = parsed.base ?? repo.default_branch
  const head = parsed.head ?? ''
  const ready = !!head && head !== base
  const cmp = useQuery({ ...compareQuery(name, base, head), enabled: ready })
  const canWrite = canWritePerm(repo.permission)

  const go = (b: string, h: string) => void navigate({ to: '/$repo/compare/$', params: { repo: name, _splat: compareSplat(b, h) }, replace: true })

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-4 px-6 py-5">
      <SubHeader
        className="mb-1"
        back={{ to: '/$repo/pulls', params: { repo: name }, label: 'Pull requests' }}
        title={ready ? 'Open a pull request' : 'Compare changes'}
        description="Choose a branch to merge into the base branch."
      />
      <div className="flex flex-wrap items-center gap-2.5 rounded-lg border border-border bg-surface-1 px-3 py-2.5 shadow-card">
        <GitCompareArrows size={15} className="text-fg-faint" />
        <RefPicker repo={name} current={base} prefix="base:" label="Choose a base branch" onSelect={(r) => go(r, head)} />
        <ArrowLeft size={14} className="text-fg-faint" aria-label="merges from" />
        <RefPicker repo={name} current={head} prefix="compare:" label="Choose a branch to compare" onSelect={(r) => go(base, r)} />
        {cmp.data && (
          <div className="flex flex-wrap items-center gap-1.5 text-sm">
            {cmp.data.mergeable === 'clean' ? (
              <>
                <CheckCircle2 size={14} className="text-success" /> <span className="font-medium text-success">Can be merged.</span>
              </>
            ) : (
              <>
                <XCircle size={14} className="text-danger" />
                <span className="font-medium text-danger">Conflicts in {cmp.data.conflict_files.join(', ')}.</span>
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
        <EmptyState size="inline" icon={<GitCompareArrows size={18} />} title={head === base ? 'Pick two different branches' : 'Pick the branch with your changes'}>
          Compare a branch against {base} to review its commits and open a pull request.
        </EmptyState>
      ) : cmp.isPending ? (
        <SkeletonRows rows={4} />
      ) : cmp.isError ? (
        <Alert>{cmp.error instanceof ApiRequestError && cmp.error.status === 404 ? `Branch not found: ${cmp.error.message}` : errorMessage(cmp.error)}</Alert>
      ) : cmp.data.ahead_by === 0 ? (
        <EmptyState size="inline" icon={<CheckCircle2 size={18} />} title="Nothing to compare">
          <span className="font-mono">{head}</span> has nothing that isn't already in <span className="font-mono">{base}</span>.
        </EmptyState>
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
          <section className="mt-2">
            <h3 className="mb-2 text-base font-semibold">{plural(cmp.data.commits.length, 'commit')}</h3>
            <CommitList repo={name} commits={cmp.data.commits} browse={false} />
          </section>
          <DiffView diff={cmp.data.diff} />
        </>
      )}
    </div>
  )
}

function NewPullForm({ repo, base, head, commits, me }: { repo: string; base: string; head: string; commits: Commit[]; me: string }) {
  const navigate = useNavigate()
  const toast = useToast()
  const users = useQuery(usersQuery)
  const first = commits.length === 1 ? commits[0] : undefined
  const [title, setTitle] = useState(first ? first.subject : head.replace(/[-_/]+/g, ' ').replace(/^\w/, (c) => c.toUpperCase()))
  const [body, setBody] = useState(first?.body ?? '')
  const [reviewers, setReviewers] = useState<string[]>([])
  const [draft, setDraft] = useState(false)
  const [meta, setMeta] = useState<{ labels: string[]; assignees: string[]; milestone: number }>({ labels: [], assignees: [], milestone: 0 })
  const labels = useQuery(labelsQuery(repo))
  const milestones = useQuery(milestonesQuery(repo, 'open'))
  const ms = (milestones.data ?? []).find((m) => m.id === meta.milestone)
  const patchMeta = (p: MetaPatch) => setMeta((m) => ({ labels: p.labels ?? m.labels, assignees: p.assignees ?? m.assignees, milestone: p.milestone ?? m.milestone }))
  const invalidate = useInvalidatePull(repo, 0)
  const create = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST('/repos/{repo}/pulls', {
          params: { path: { repo } },
          body: { title, body, head, base, draft, reviewers, labels: meta.labels, assignees: meta.assignees, milestone: meta.milestone || undefined },
        }),
      ),
    onSuccess: (pr) => {
      invalidate()
      toast.success(`Opened pull request #${pr.number}`)
      void navigate({ to: '/$repo/pulls/$number', params: { repo, number: String(pr.number) } })
    },
  })
  useEffect(() => create.reset(), [base, head]) // eslint-disable-line react-hooks/exhaustive-deps
  const people = (users.data ?? []).filter((u) => reviewers.includes(u.username))
  return (
    <form
      className="grid gap-6 md:grid-cols-[minmax(0,1fr)_256px]"
      onSubmit={(e) => {
        e.preventDefault()
        create.mutate()
      }}
    >
      <Card
        footer={
          <>
            <Checkbox checked={draft} onChange={setDraft} label="Create as draft" className="mr-auto" />
            <Button type="submit" variant="primary" loading={create.isPending} disabled={!title.trim()} icon={<GitPullRequest size={14} />}>
              Create pull request
            </Button>
          </>
        }
      >
        <div className="flex flex-col gap-4">
          <Field label="Title">{(id) => <Input id={id} value={title} onChange={(e) => setTitle(e.target.value)} autoFocus size="lg" />}</Field>
          <MarkdownEditor value={body} onChange={setBody} repo={repo} label="Description" placeholder="Describe the change: what, why, how to test." minRows={8} />
          {create.isError && <Alert>{errorMessage(create.error)}</Alert>}
        </div>
      </Card>
      <aside className="flex flex-col gap-4">
        <MetaSection title="Reviewers" action={<ReviewerPicker value={reviewers} exclude={[me]} onChange={setReviewers} />}>
          {people.length === 0 ? (
            <p className="text-sm text-fg-faint">No reviewers yet.</p>
          ) : (
            <ul className="flex flex-col gap-2">
              {people.map((u) => (
                <li key={u.id} className="flex items-center gap-2">
                  <Avatar user={u} size={20} /> {u.display_name}
                </li>
              ))}
            </ul>
          )}
        </MetaSection>
        <MetaSections
          repo={repo}
          canEdit
          me={me}
          labels={(labels.data ?? []).filter((l) => meta.labels.includes(l.name))}
          assignees={(users.data ?? []).filter((u) => meta.assignees.includes(u.username))}
          milestone={ms ? { id: ms.id, title: ms.title, state: ms.state, due_on: ms.due_on } : null}
          onPatch={patchMeta}
        />
      </aside>
    </form>
  )
}
