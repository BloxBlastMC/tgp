import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, Pencil, Plus, Shield, ShieldCheck, X } from 'lucide-react'
import { useState } from 'react'
import { api, errorMessage, unwrap, type BranchProtection } from '../../api/client'
import { protectionsQuery, reportedChecksQuery } from '../../api/queries'
import { Alert, Badge, Button, Checkbox, Dialog, EmptyState, Eyebrow, Field, IconButton, Input, Section, SkeletonRows, Tooltip, useConfirm, useToast } from '../../components/ui'

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

/** What a rule enforces, as short phrases ("Requires a pull request", "1 approval", …). Bypass is shown separately. */
export function summary(r: Pick<BranchProtection, 'require_pull_request' | 'required_approvals' | 'dismiss_stale_approvals' | 'require_up_to_date' | 'block_force_push' | 'allow_deletion' | 'required_checks'>): string[] {
  const out: string[] = []
  if (r.require_pull_request) out.push('Requires a pull request')
  if (r.required_approvals > 0) out.push(`${r.required_approvals} approval${r.required_approvals === 1 ? '' : 's'}`)
  if (r.dismiss_stale_approvals && r.required_approvals > 0) out.push('New commits dismiss approvals')
  if (r.require_up_to_date) out.push('Must be up to date with base')
  if (r.required_checks?.length) out.push(`${r.required_checks.length} required check${r.required_checks.length === 1 ? '' : 's'}`)
  if (r.block_force_push) out.push('No force pushes')
  if (!r.allow_deletion) out.push("Can't be deleted")
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
      flush
      title="Branch protection"
      description={isAdmin ? 'Rules for branches matching a name or glob such as release/*. The most specific rule applies.' : 'Rules for protected branches. Admins manage them.'}
      actions={
        isAdmin && (
          <Button icon={<Plus size={13} />} onClick={() => setEditing('new')}>
            Add rule
          </Button>
        )
      }
    >
      {rules.isPending ? (
        <SkeletonRows rows={2} />
      ) : rules.isError ? (
        <div className="p-4">
          <Alert>{errorMessage(rules.error)}</Alert>
        </div>
      ) : rules.data.length === 0 ? (
        <EmptyState size="inline" icon={<Shield size={18} />} title="No branches are protected">
          Protect your default branch to require reviews and passing checks before merging.
        </EmptyState>
      ) : (
        <ul aria-label="Protection rules">
          {rules.data.map((r) => (
            <li key={r.id} className="flex items-start gap-3 border-b border-border px-4 py-3 last:border-0">
              <ShieldCheck size={16} className="mt-0.5 shrink-0 text-success" />
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-mono font-semibold">{r.pattern}</span>
                  {r.admins_bypass && (
                    <Badge tone="warning" title="Admins can push directly and merge without meeting these rules">
                      Admins may bypass
                    </Badge>
                  )}
                </div>
                <p className="mt-0.5 text-sm text-fg-muted">{summary(r).join(' · ')}</p>
              </div>
              {isAdmin && (
                <IconButton label={`Edit rule ${r.pattern}`} size="sm" onClick={() => setEditing(r)}>
                  <Pencil size={13} />
                </IconButton>
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
  const confirm = useConfirm()
  const toast = useToast()
  const set = <K extends keyof RuleForm>(k: K, v: RuleForm[K]) => setF((x) => ({ ...x, [k]: v }))
  const save = useMutation({
    mutationFn: () =>
      rule
        ? unwrap(api.PATCH('/repos/{repo}/protections/{id}', { params: { path: { repo, id: rule.id } }, body: f }))
        : unwrap(api.POST('/repos/{repo}/protections', { params: { path: { repo } }, body: f })),
    onSuccess: () => {
      toast.success(rule ? `Updated the rule for ${f.pattern}` : `Protected ${f.pattern}`)
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
            <Button variant="danger" className="mr-auto" loading={del.isPending} onClick={async () => (await confirm({ title: `Remove the rule for ${rule.pattern}?`, description: 'Matching branches lose these protections immediately.', confirmLabel: 'Delete rule' })) && del.mutate()}>
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
      <Eyebrow className="mt-2">Merging</Eyebrow>
      <Checkbox checked={f.require_pull_request} onChange={(v) => set('require_pull_request', v)} label="Require a pull request" hint="Block direct pushes; changes land by merging pull requests." />
      <Field label="Required approvals" hint="Approvals needed from anyone other than the author, testers included. 0 = none, maximum 10.">
        {(id) => (
          <Input id={id} type="number" min={0} max={10} value={f.required_approvals} onChange={(e) => set('required_approvals', Math.max(0, Math.min(10, Number(e.target.value) || 0)))} className="w-24" />
        )}
      </Field>
      <Checkbox checked={f.dismiss_stale_approvals} onChange={(v) => set('dismiss_stale_approvals', v)} label="Dismiss stale approvals" hint="Approvals of an older head commit stop counting when new commits are pushed." />
      <Checkbox checked={f.require_up_to_date} onChange={(v) => set('require_up_to_date', v)} label="Require branches to be up to date" hint="The head must contain the latest base commit before merging." />
      <RequiredChecksEditor repo={repo} value={f.required_checks} onChange={(v) => set('required_checks', v)} />
      <Eyebrow className="mt-2 border-t border-border pt-4">Branch</Eyebrow>
      <Checkbox checked={f.block_force_push} onChange={(v) => set('block_force_push', v)} label="Block force pushes" hint="Rewriting the branch's history is rejected." />
      <Checkbox checked={f.allow_deletion} onChange={(v) => set('allow_deletion', v)} label="Allow deletion" hint="Let people with write access delete matching branches." />
      <Eyebrow className="mt-2 border-t border-border pt-4">Bypass</Eyebrow>
      <Checkbox checked={f.admins_bypass} onChange={(v) => set('admins_bypass', v)} label="Admins may bypass" hint="Admins can push directly and merge without meeting the requirements (audited)." />
      {(save.isError || del.isError) && <Alert>{errorMessage(save.error ?? del.error)}</Alert>}
    </Dialog>
  )
}

/**
 * Edits the list of required CI check names (`<workflow> / <job>`),
 * suggesting names the repository's runs have reported. A typo would block
 * merging forever, so unknown names get a warning.
 */
export function RequiredChecksEditor({ repo, value, onChange }: { repo: string; value: string[]; onChange: (v: string[]) => void }) {
  const [draft, setDraft] = useState('')
  const reported = useQuery(reportedChecksQuery(repo))
  const known = reported.data ?? []
  const isKnown = (c: string) => known.some((k) => k.toLowerCase() === c.toLowerCase())
  const add = (raw = draft) => {
    const name = raw.trim()
    if (!name) return
    if (!value.some((v) => v.toLowerCase() === name.toLowerCase())) onChange([...value, name])
    setDraft('')
  }
  const suggestions = known.filter((k) => !value.some((v) => v.toLowerCase() === k.toLowerCase()))
  const unknown = reported.isSuccess ? value.filter((c) => !isKnown(c)) : []
  return (
    <Field label="Required checks" hint="CI jobs that must pass on the head commit, named “Workflow / job” as shown on pull requests.">
      {(id) => (
        <div className="flex flex-col gap-1.5">
          {value.length > 0 && (
            <ul className="flex flex-wrap gap-1" aria-label="Required checks">
              {value.map((c) => (
                <li
                  key={c}
                  className={
                    'flex h-6 items-center gap-1 rounded-md border px-2 font-mono text-sm ' +
                    (reported.isSuccess && !isKnown(c) ? 'border-warning/50 bg-warning/10' : 'border-border-strong bg-bg-subtle')
                  }
                >
                  {reported.isSuccess && !isKnown(c) && <AlertTriangle size={11} className="text-warning" aria-label="Not reported by any run" />}
                  {c}
                  <Tooltip content={`Remove ${c}`} describe={false}>
                    <button type="button" className="text-fg-faint hover:text-danger" aria-label={`Remove ${c}`} onClick={() => onChange(value.filter((v) => v !== c))}>
                      <X size={12} />
                    </button>
                  </Tooltip>
                </li>
              ))}
            </ul>
          )}
          {unknown.length > 0 && (
            <p className="text-sm text-warning">
              No run has reported {unknown.length === 1 ? 'this check' : 'these checks'} yet: {unknown.join(', ')}. Check the spelling, or merging stays blocked until a job with that name passes.
            </p>
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
              placeholder={known[0] ?? 'CI / test'}
              className="font-mono"
            />
            <Button onClick={() => add()} disabled={!draft.trim()}>
              Add
            </Button>
          </div>
          {suggestions.length > 0 && (
            <div className="flex flex-wrap items-center gap-1 text-sm text-fg-muted">
              <span>Reported by recent runs:</span>
              {suggestions.map((k) => (
                <button
                  key={k}
                  type="button"
                  onClick={() => add(k)}
                  aria-label={`Require ${k}`}
                  className="inline-flex h-6 items-center gap-1 rounded-md border border-dashed border-border-strong px-2 font-mono text-sm text-fg-muted hover:border-accent hover:text-fg"
                >
                  <Plus size={11} /> {k}
                </button>
              ))}
            </div>
          )}
        </div>
      )}
    </Field>
  )
}
