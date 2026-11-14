import { useMutation } from '@tanstack/react-query'
import { AlertTriangle, CheckCircle2, GitBranch, GitMerge, GitPullRequestClosed, Loader2, ShieldAlert, XCircle } from 'lucide-react'
import { useState } from 'react'
import { api, errorMessage, unwrap, type PullRequest } from '../../api/client'
import { ChecksRow } from '../../components/ci/Checks'
import { useInvalidatePull } from '../../components/pulls/common'
import { Alert, Button, Checkbox, cx, Input, Select, Textarea, Time, useConfirm } from '../../components/ui'
import { canBypass, decisionInfo, defaultMergeMessage, loadMergeMethod, saveMergeMethod, type MergeMethod } from '../../lib/pulls'

function StatusRow({ ok, warn, children }: { ok?: boolean; warn?: boolean; children: React.ReactNode }) {
  return (
    <li className="flex items-start gap-2.5 py-2">
      {ok ? (
        <CheckCircle2 size={15} className="mt-0.5 shrink-0 text-success" />
      ) : warn ? (
        <AlertTriangle size={15} className="mt-0.5 shrink-0 text-warning" />
      ) : (
        <XCircle size={15} className="mt-0.5 shrink-0 text-danger" />
      )}
      <div className="min-w-0 flex-1">{children}</div>
    </li>
  )
}

const methodLabel: Record<MergeMethod, string> = {
  merge: 'Create a merge commit',
  squash: 'Squash and merge',
  rebase: 'Rebase and merge',
}

export function MergeBox({ repo, pr }: { repo: string; pr: PullRequest }) {
  const invalidate = useInvalidatePull(repo, pr.number)
  const canWrite = pr.viewer_permission === 'write' || pr.viewer_permission === 'admin'
  const deleteBranch = useMutation({
    mutationFn: () => unwrap(api.POST('/repos/{repo}/branches/delete', { params: { path: { repo } }, body: { name: pr.head.branch, sha: pr.head.sha } })),
    onSuccess: invalidate,
  })

  if (pr.state === 'merged') {
    return (
      <div className="flex items-center gap-3 rounded-lg border border-done/35 bg-done/8 px-4 py-3">
        <span className="flex size-8 shrink-0 items-center justify-center rounded-full bg-done text-white">
          <GitMerge size={16} />
        </span>
        <div className="min-w-0 flex-1">
          <p className="font-medium">Merged</p>
          <p className="text-sm text-fg-muted">
            {pr.merged_by?.display_name ?? 'Someone'} merged this <Time iso={pr.merged_at} />
            {pr.merge_commit && (
              <>
                {' '}
                as <span className="font-mono">{pr.merge_commit.slice(0, 7)}</span>
              </>
            )}
            {pr.merge_method && ` (${pr.merge_method})`}.
          </p>
          {deleteBranch.isError && <Alert>{errorMessage(deleteBranch.error)}</Alert>}
        </div>
        {pr.head.exists && canWrite && (
          <Button onClick={() => deleteBranch.mutate()} loading={deleteBranch.isPending} icon={<GitBranch size={13} />}>
            Delete branch
          </Button>
        )}
        {!pr.head.exists && <span className="text-sm text-fg-faint">Branch {pr.head.branch} deleted</span>}
      </div>
    )
  }
  if (pr.state === 'closed') {
    return (
      <div className="flex items-center gap-3 rounded-lg border border-danger/35 bg-danger/8 px-4 py-3">
        <span className="flex size-8 shrink-0 items-center justify-center rounded-full bg-danger text-white">
          <GitPullRequestClosed size={16} />
        </span>
        <div>
          <p className="font-medium">Closed</p>
          <p className="text-sm text-fg-muted">This pull request was closed without merging.</p>
        </div>
      </div>
    )
  }
  return <OpenMergeBox repo={repo} pr={pr} canWrite={canWrite} />
}

function OpenMergeBox({ repo, pr, canWrite }: { repo: string; pr: PullRequest; canWrite: boolean }) {
  const invalidate = useInvalidatePull(repo, pr.number)
  const [method, setMethodState] = useState<MergeMethod>(loadMergeMethod)
  const [msg, setMsg] = useState(() => defaultMergeMessage(pr, loadMergeMethod()))
  const [del, setDel] = useState(true)
  const setMethod = (m: MergeMethod) => {
    setMethodState(m)
    saveMergeMethod(m)
    setMsg(defaultMergeMessage(pr, m))
  }
  const merge = useMutation({
    mutationFn: (bypass: boolean) =>
      unwrap(
        api.POST('/repos/{repo}/pulls/{number}/merge', {
          params: { path: { repo, number: pr.number } },
          body: {
            method,
            sha: pr.head.sha,
            delete_branch: del,
            bypass_protection: bypass,
            ...(method !== 'rebase' ? { title: msg.title, message: msg.message } : {}),
          },
        }),
      ),
    onSuccess: invalidate,
  })
  const update = useMutation({
    mutationFn: () => unwrap(api.POST('/repos/{repo}/pulls/{number}/update-branch', { params: { path: { repo, number: pr.number } }, body: { sha: pr.head.sha } })),
    onSuccess: invalidate,
  })
  const decision = decisionInfo(pr.review_decision)
  const behind = pr.behind_by ?? 0
  const ruleBlockers = pr.blockers.filter((b) => !['conflict', 'permission', 'behind', 'checks'].includes(b.code))
  const checksBlocker = pr.blockers.find((b) => b.code === 'checks')
  const bypass = !pr.can_merge && pr.viewer_permission === 'admin' && canBypass(pr)
  const confirm = useConfirm()
  const ready = pr.can_merge
  const conflict = pr.mergeable === 'conflict'
  return (
    <div className={cx('overflow-hidden rounded-lg border bg-surface-1 shadow-card', ready ? 'border-success/40' : conflict ? 'border-danger/35' : 'border-border')} aria-label="Merge status">
      <div className="flex items-center gap-3 border-b border-border px-4 py-3">
        <span className={cx('flex size-8 shrink-0 items-center justify-center rounded-full text-white', ready ? 'bg-success' : conflict ? 'bg-danger' : 'bg-fg-faint')}>
          {ready ? <GitMerge size={16} /> : conflict ? <XCircle size={16} /> : <ShieldAlert size={16} />}
        </span>
        <div className="min-w-0">
          <p className="font-semibold">{ready ? 'Ready to merge' : conflict ? 'This branch has conflicts' : pr.draft ? 'This pull request is a draft' : 'Merging is blocked'}</p>
          <p className="text-sm text-fg-muted">{ready ? 'All requirements are met.' : 'Resolve the items below before merging.'}</p>
        </div>
      </div>
      <ul className="divide-y divide-border px-4 py-1">
        {pr.checks && <ChecksRow repo={repo} checks={pr.checks} blocker={checksBlocker} />}
        {checksBlocker && pr.checks && pr.checks.total === 0 && pr.checks.missing_required.length === 0 && <StatusRow>{checksBlocker.message}</StatusRow>}
        {pr.mergeable === 'clean' && <StatusRow ok>This branch has no conflicts with {pr.base.branch}.</StatusRow>}
        {pr.mergeable === 'conflict' && (
          <StatusRow>
            <p>This branch has conflicts that must be resolved.</p>
            <p className="font-mono text-sm text-fg-muted">{pr.conflict_files.join(', ')}</p>
            <p className="text-sm text-fg-faint">
              Resolve locally: <code className="font-mono">git merge {pr.base.branch}</code> on {pr.head.branch}, then push.
            </p>
          </StatusRow>
        )}
        {pr.mergeable === 'unknown' && (
          <li className="flex items-center gap-2 py-1.5 text-fg-muted">
            <Loader2 size={15} className="animate-spin" /> Checking mergeability…
          </li>
        )}
        {decision ? (
          <StatusRow ok={decision.tone === 'success'} warn={decision.tone === 'warning'}>
            {decision.label}
            {pr.protection && pr.protection.required_approvals > 0 && (
              <span className="text-fg-muted">
                {' '}
                · {pr.protection.required_approvals} approval{pr.protection.required_approvals === 1 ? '' : 's'} required
              </span>
            )}
          </StatusRow>
        ) : (
          <StatusRow warn>No reviews yet.</StatusRow>
        )}
        {behind > 0 && (
          <StatusRow warn={!pr.blockers.some((b) => b.code === 'behind')}>
            <div className="flex items-center gap-3">
              <span className="flex-1">
                This branch is {behind} commit{behind === 1 ? '' : 's'} behind {pr.base.branch}.
              </span>
              {canWrite && pr.mergeable !== 'conflict' && (
                <Button onClick={() => update.mutate()} loading={update.isPending}>
                  Update branch
                </Button>
              )}
            </div>
            {update.isError && <Alert>{errorMessage(update.error)}</Alert>}
          </StatusRow>
        )}
        {ruleBlockers.map((b) => (
          <StatusRow key={b.code}>
            {b.message}
            {b.bypassable && <span className="text-sm text-fg-faint"> (admins may bypass)</span>}
          </StatusRow>
        ))}
        {pr.blockers.some((b) => b.code === 'permission') && <StatusRow warn>Only maintainers and admins can merge.</StatusRow>}
      </ul>
      {canWrite && (
        <div className="flex flex-col gap-2 border-t border-border bg-bg-subtle px-4 py-3">
          <div className="flex flex-wrap items-center gap-2">
            <Select size="sm" value={method} onChange={(e) => setMethod(e.target.value as MergeMethod)} className="w-56" aria-label="Merge method">
              {(Object.keys(methodLabel) as MergeMethod[]).map((m) => (
                <option key={m} value={m}>
                  {methodLabel[m]}
                </option>
              ))}
            </Select>
            <Checkbox checked={del} onChange={setDel} label="Delete branch after merging" />
          </div>
          {method !== 'rebase' && (
            <>
              <Input value={msg.title} onChange={(e) => setMsg({ ...msg, title: e.target.value })} aria-label="Commit title" className="font-mono" />
              <Textarea value={msg.message} onChange={(e) => setMsg({ ...msg, message: e.target.value })} aria-label="Commit message" rows={2} className="font-mono" placeholder={method === 'squash' ? 'Commit message (defaults to the list of commits)' : 'Commit message'} />
            </>
          )}
          {merge.isError && <Alert>{errorMessage(merge.error)}</Alert>}
          <div className="flex gap-2">
            <Button variant="primary" disabled={!pr.can_merge} loading={merge.isPending && !merge.variables} onClick={() => merge.mutate(false)} icon={<GitMerge size={14} />}>
              {methodLabel[method]}
            </Button>
            {bypass && (
              <Button
                variant="danger"
                loading={merge.isPending && merge.variables}
                onClick={async () => {
                  const ok = await confirm({
                    title: 'Merge without meeting the requirements?',
                    description: 'This bypasses branch protection and is recorded in the audit log.',
                    confirmLabel: 'Merge anyway',
                  })
                  if (ok) merge.mutate(true)
                }}
              >
                Merge without requirements
              </Button>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
