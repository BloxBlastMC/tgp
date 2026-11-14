import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Shield, X } from 'lucide-react'
import { useState } from 'react'
import { api, errorMessage, unwrap, type BranchProtection } from '../../api/client'
import { protectionsQuery } from '../../api/queries'
import { Alert, Badge, Button, Checkbox, Dialog, Field, Input, Section, Spinner } from '../../components/ui'

type RuleForm = Omit<BranchProtection, 'id' | 'created_at' | 'updated_at'>

const defaults: RuleForm = {
  pattern: '',
  require_pull_request: true,
  required_approvals: 1,
  dismiss_stale_approvals: true,
  require_up_to_date: false,
  block_force_push: true,
  allow_deletion: false,
  admins_bypass: false,
  required_checks: [],
}

function summary(r: BranchProtection): string[] {
  const out: string[] = []
  if (r.require_pull_request) out.push('pull requests only')
  if (r.required_approvals > 0) out.push(`${r.required_approvals} approval${r.required_approvals === 1 ? '' : 's'}`)
  if (r.dismiss_stale_approvals && r.required_approvals > 0) out.push('stale approvals dismissed')
  if (r.require_up_to_date) out.push('up to date')
  if (r.block_force_push) out.push('no force pushes')
  if (!r.allow_deletion) out.push('no deletion')
  if (r.required_checks?.length) out.push(`${r.required_checks.length} required check${r.required_checks.length === 1 ? '' : 's'}`)
  if (r.admins_bypass) out.push('admins may bypass')
  return out
}

export function ProtectionSection({ repo, isAdmin }: { repo: string; isAdmin: boolean }) {
  const rules = useQuery(protectionsQuery(repo))
  const qc = useQueryClient()
  const [editing, setEditing] = useState<BranchProtection | 'new' | null>(null)
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: protectionsQuery(repo).queryKey })
    void qc.invalidateQueries({ queryKey: ['repo', repo, 'branches'] })
    void qc.invalidateQueries({ queryKey: ['repo', repo, 'pull'] })
  }
  return (
    <Section
      title="Branch protection"
      description={isAdmin ? 'Rules for branches matching a name or glob such as release/*. The most specific rule applies.' : 'Rules for protected branches. Admins manage them.'}
      actions={
        isAdmin && (
          <Button onClick={() => setEditing('new')}>
            <Plus size={13} /> Add rule
          </Button>
        )
      }
    >
      {rules.isPending ? (
        <Spinner />
      ) : rules.isError ? (
        <Alert>{errorMessage(rules.error)}</Alert>
      ) : rules.data.length === 0 ? (
        <p className="text-fg-faint">No branches are protected.</p>
      ) : (
        <ul className="-my-2 divide-y divide-border" aria-label="Protection rules">
          {rules.data.map((r) => (
            <li key={r.id} className="flex items-center gap-3 py-2">
              <Shield size={14} className="shrink-0 text-fg-faint" />
              <span className="font-mono font-medium">{r.pattern}</span>
              <span className="flex min-w-0 flex-1 flex-wrap gap-1">
                {summary(r).map((s) => (
                  <Badge key={s}>{s}</Badge>
                ))}
              </span>
              {isAdmin && (
                <Button variant="ghost" onClick={() => setEditing(r)} aria-label={`Edit rule ${r.pattern}`}>
                  Edit
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      {editing && <RuleDialog repo={repo} rule={editing === 'new' ? null : editing} onClose={() => setEditing(null)} onSaved={refresh} />}
    </Section>
  )
}

function RuleDialog({ repo, rule, onClose, onSaved }: { repo: string; rule: BranchProtection | null; onClose: () => void; onSaved: () => void }) {
  const [f, setF] = useState<RuleForm>(rule ? { ...rule, required_checks: rule.required_checks ?? [] } : defaults)
  const set = <K extends keyof RuleForm>(k: K, v: RuleForm[K]) => setF((x) => ({ ...x, [k]: v }))
  const save = useMutation({
    mutationFn: () =>
      rule
        ? unwrap(api.PATCH('/repos/{repo}/protections/{id}', { params: { path: { repo, id: rule.id } }, body: f }))
        : unwrap(api.POST('/repos/{repo}/protections', { params: { path: { repo } }, body: f })),
    onSuccess: () => {
      onSaved()
      onClose()
    },
  })
  const del = useMutation({
    mutationFn: () => unwrap(api.DELETE('/repos/{repo}/protections/{id}', { params: { path: { repo, id: rule!.id } } })),
    onSuccess: () => {
      onSaved()
      onClose()
    },
  })
  return (
    <Dialog
      open
      onClose={onClose}
      title={rule ? `Edit rule ${rule.pattern}` : 'Add protection rule'}
      width="max-w-lg"
      footer={
        <>
          {rule && (
            <Button variant="danger" className="mr-auto" loading={del.isPending} onClick={() => window.confirm(`Remove the rule for ${rule.pattern}?`) && del.mutate()}>
              Delete rule
            </Button>
          )}
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" loading={save.isPending} disabled={!f.pattern.trim()} onClick={() => save.mutate()}>
            {rule ? 'Save' : 'Add rule'}
          </Button>
        </>
      }
    >
      <Field label="Branch name or pattern" hint="e.g. main, release/*">
        {(id) => <Input id={id} value={f.pattern} onChange={(e) => set('pattern', e.target.value)} autoFocus className="font-mono" />}
      </Field>
      <Checkbox checked={f.require_pull_request} onChange={(v) => set('require_pull_request', v)} label="Require a pull request" hint="Block direct pushes; changes land by merging pull requests." />
      <Field label="Required approvals" hint="Approvals needed from anyone except the author (testers count). 0 = none.">
        {(id) => (
          <Input id={id} type="number" min={0} max={10} value={f.required_approvals} onChange={(e) => set('required_approvals', Math.max(0, Math.min(10, Number(e.target.value) || 0)))} className="w-24" />
        )}
      </Field>
      <Checkbox checked={f.dismiss_stale_approvals} onChange={(v) => set('dismiss_stale_approvals', v)} label="Dismiss stale approvals" hint="Approvals of an older head commit stop counting when new commits are pushed." />
      <Checkbox checked={f.require_up_to_date} onChange={(v) => set('require_up_to_date', v)} label="Require branches to be up to date" hint="The head must contain the latest base commit before merging." />
      <Checkbox checked={f.block_force_push} onChange={(v) => set('block_force_push', v)} label="Block force pushes" hint="Rewriting the branch's history is rejected." />
      <Checkbox checked={f.allow_deletion} onChange={(v) => set('allow_deletion', v)} label="Allow deletion" hint="Let people with write access delete matching branches." />
      <RequiredChecksEditor value={f.required_checks} onChange={(v) => set('required_checks', v)} />
      <Checkbox checked={f.admins_bypass} onChange={(v) => set('admins_bypass', v)} label="Admins may bypass" hint="Admins can push directly and merge without meeting the requirements (audited)." />
      {(save.isError || del.isError) && <Alert>{errorMessage(save.error ?? del.error)}</Alert>}
    </Dialog>
  )
}

/** Edits the list of required CI check names (`<workflow> / <job>`). */
export function RequiredChecksEditor({ value, onChange }: { value: string[]; onChange: (v: string[]) => void }) {
  const [draft, setDraft] = useState('')
  const add = () => {
    const name = draft.trim()
    if (!name) return
    if (!value.some((v) => v.toLowerCase() === name.toLowerCase())) onChange([...value, name])
    setDraft('')
  }
  return (
    <Field label="Required checks" hint="CI jobs that must pass on the head commit, named “Workflow / job” as shown on pull requests, e.g. CI / test.">
      {(id) => (
        <div className="flex flex-col gap-1.5">
          {value.length > 0 && (
            <ul className="flex flex-wrap gap-1" aria-label="Required checks">
              {value.map((c) => (
                <li key={c} className="flex h-6 items-center gap-1 rounded border border-border-strong bg-bg px-2 font-mono text-[12px]">
                  {c}
                  <button type="button" className="text-fg-faint hover:text-danger" aria-label={`Remove ${c}`} onClick={() => onChange(value.filter((v) => v !== c))}>
                    <X size={12} />
                  </button>
                </li>
              ))}
            </ul>
          )}
          <div className="flex gap-2">
            <Input
              id={id}
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  add()
                }
              }}
              placeholder="CI / test"
              className="font-mono"
            />
            <Button onClick={add} disabled={!draft.trim()}>
              Add
            </Button>
          </div>
        </div>
      )}
    </Field>
  )
}
