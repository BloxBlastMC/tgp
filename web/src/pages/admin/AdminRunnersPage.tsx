import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Plus } from 'lucide-react'
import { useState } from 'react'
import { api, errorMessage, unwrap, type Runner, type Schemas } from '../../api/client'
import { runnersQuery } from '../../api/queries'
import { Alert, Badge, Button, cx, Dialog, Field, Input, SecretValue, Spinner, Time } from '../../components/ui'

const statusDot: Record<Runner['status'], string> = {
  online: 'bg-success',
  offline: 'bg-fg-faint',
  disabled: 'bg-warning',
}

function Meter({ label, value }: { label: string; value?: number | null }) {
  if (value == null) return <span className="text-xs text-fg-faint">{label} —</span>
  const v = Math.max(0, Math.min(100, value))
  const tone = v >= 90 ? 'bg-danger' : v >= 70 ? 'bg-warning' : 'bg-success'
  return (
    <span className="flex items-center gap-1.5 text-xs text-fg-muted" title={`${label} ${value.toFixed(0)}%`}>
      <span className="w-7">{label}</span>
      <span className="h-1.5 w-16 overflow-hidden rounded bg-bg-hover" role="meter" aria-label={label} aria-valuenow={Math.round(value)} aria-valuemin={0} aria-valuemax={100}>
        <span className={cx('block h-full', tone)} style={{ width: `${v}%` }} />
      </span>
      <span className="w-8 tabular-nums">{value.toFixed(0)}%</span>
    </span>
  )
}

export function AdminRunnersPage() {
  const runners = useQuery(runnersQuery)
  const qc = useQueryClient()
  const [token, setToken] = useState<Schemas['RunnerRegistrationToken'] | null>(null)
  const [renaming, setRenaming] = useState<Runner | null>(null)
  const refresh = () => void qc.invalidateQueries({ queryKey: runnersQuery.queryKey })
  const create = useMutation({
    mutationFn: () => unwrap(api.POST('/admin/runners/registration-tokens', { body: { expires_in_hours: 24 } })),
    onSuccess: setToken,
  })
  const update = useMutation({
    mutationFn: ({ id, ...body }: { id: number; disabled?: boolean; name?: string }) => unwrap(api.PATCH('/admin/runners/{id}', { params: { path: { id } }, body })),
    onSuccess: refresh,
  })
  const remove = useMutation({
    mutationFn: (id: number) => unwrap(api.DELETE('/admin/runners/{id}', { params: { path: { id } } })),
    onSuccess: refresh,
  })
  const err = create.error ?? update.error ?? remove.error

  return (
    <div className="flex flex-col">
      <div className="flex items-center gap-4 border-b border-border px-4 py-2">
        {runners.data && (
          <div className="flex gap-4 text-sm text-fg-muted">
            <span>
              <span className="font-medium text-fg tabular-nums">{runners.data.running_jobs}</span> running
            </span>
            <span>
              <span className="font-medium text-fg tabular-nums">{runners.data.queued_jobs}</span> queued
            </span>
            <span>
              <span className="font-medium text-fg tabular-nums">{runners.data.runners.filter((r) => r.status === 'online').length}</span> of {runners.data.runners.length} runners online
            </span>
          </div>
        )}
        <div className="flex-1" />
        <Button variant="primary" loading={create.isPending} onClick={() => create.mutate()}>
          <Plus size={13} /> Add runner
        </Button>
      </div>
      {err && (
        <div className="p-4 pb-0">
          <Alert>{errorMessage(err)}</Alert>
        </div>
      )}
      {runners.isPending ? (
        <Spinner />
      ) : runners.isError ? (
        <div className="p-4">
          <Alert>{errorMessage(runners.error)}</Alert>
        </div>
      ) : runners.data.runners.length === 0 ? (
        <div className="mx-auto mt-20 max-w-md text-center">
          <p className="font-medium">No runners registered</p>
          <p className="mt-1 text-fg-muted">CI jobs wait in the queue until a runner picks them up. Install tgp-runner on a machine with Docker and register it with a token from “Add runner”.</p>
        </div>
      ) : (
        <table className="w-full text-left" aria-label="Runners">
          <thead className="border-b border-border text-xs tracking-wide text-fg-faint uppercase">
            <tr>
              <th className="px-4 py-2 font-medium">Runner</th>
              <th className="px-2 py-2 font-medium">Jobs</th>
              <th className="px-2 py-2 font-medium">Load</th>
              <th className="px-2 py-2 font-medium">Last seen</th>
              <th className="px-4 py-2" />
            </tr>
          </thead>
          <tbody>
            {runners.data.runners.map((r) => (
              <tr key={r.id} className="border-b border-border align-top hover:bg-bg-subtle" data-testid="runner-row">
                <td className="px-4 py-2.5">
                  <div className="flex items-center gap-2">
                    <span className={cx('size-2 shrink-0 rounded-full', statusDot[r.status])} title={r.status} />
                    <span className="font-medium">{r.name}</span>
                    <Badge tone={r.status === 'online' ? 'success' : r.status === 'disabled' ? 'warning' : 'neutral'}>{r.status}</Badge>
                    {r.overloaded && <Badge tone="danger">overloaded</Badge>}
                  </div>
                  <div className="mt-1 flex flex-wrap items-center gap-1 text-xs text-fg-faint">
                    {r.labels.map((l) => (
                      <span key={l} className="rounded border border-border px-1 font-mono">
                        {l}
                      </span>
                    ))}
                    <span>
                      {r.os}/{r.arch} · {r.version}
                    </span>
                  </div>
                </td>
                <td className="px-2 py-2.5">
                  <div className="tabular-nums">
                    {r.busy} / {r.capacity}
                  </div>
                  {r.jobs.map((j) => (
                    <Link key={j.id} to="/$repo/ci/runs/$number" params={{ repo: j.repo, number: String(j.run_number) }} search={{ job: j.id }} className="block max-w-56 truncate text-sm text-fg-muted hover:text-accent">
                      {j.repo} #{j.run_number} · {j.name}
                    </Link>
                  ))}
                </td>
                <td className="px-2 py-2.5">
                  <div className="flex flex-col gap-1">
                    <Meter label="CPU" value={r.cpu_percent} />
                    <Meter label="Mem" value={r.memory_percent} />
                  </div>
                </td>
                <td className="px-2 py-2.5 text-sm text-fg-muted">
                  <Time iso={r.last_seen_at} />
                </td>
                <td className="px-4 py-2.5">
                  <div className="flex justify-end gap-1">
                    <Button variant="ghost" onClick={() => setRenaming(r)} aria-label={`Rename ${r.name}`}>
                      Rename
                    </Button>
                    <Button variant="ghost" onClick={() => update.mutate({ id: r.id, disabled: r.status !== 'disabled' })} aria-label={`${r.status === 'disabled' ? 'Enable' : 'Disable'} ${r.name}`}>
                      {r.status === 'disabled' ? 'Enable' : 'Disable'}
                    </Button>
                    <Button
                      variant="ghost"
                      className="hover:text-danger"
                      onClick={() => window.confirm(`Remove ${r.name}? Its credentials stop working${r.busy ? ` and its ${r.busy} running job(s) fail` : ''}.`) && remove.mutate(r.id)}
                      aria-label={`Remove ${r.name}`}
                    >
                      Remove
                    </Button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {token && (
        <Dialog open onClose={() => setToken(null)} title="Register a runner" width="max-w-xl" footer={<Button variant="primary" onClick={() => setToken(null)}>Done</Button>}>
          <p className="text-fg-muted">
            Run this on the runner machine (it needs Docker and git). The token works once and expires <Time iso={token.expires_at} />. It won't be shown again.
          </p>
          <Field label="Command">{() => <SecretValue value={token.command} label="Register command" />}</Field>
          <Field label="Token">{() => <SecretValue value={token.token} label="Registration token" />}</Field>
        </Dialog>
      )}
      {renaming && <RenameDialog runner={renaming} onClose={() => setRenaming(null)} onSave={(name) => update.mutateAsync({ id: renaming.id, name }).then(() => setRenaming(null))} />}
    </div>
  )
}

function RenameDialog({ runner, onClose, onSave }: { runner: Runner; onClose: () => void; onSave: (name: string) => Promise<unknown> }) {
  const [name, setName] = useState(runner.name)
  const [busy, setBusy] = useState(false)
  return (
    <Dialog
      open
      onClose={onClose}
      title={`Rename ${runner.name}`}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="primary"
            loading={busy}
            disabled={!name.trim() || name === runner.name}
            onClick={() => {
              setBusy(true)
              void onSave(name.trim()).finally(() => setBusy(false))
            }}
          >
            Rename
          </Button>
        </>
      }
    >
      <Field label="Name">{(id) => <Input id={id} value={name} onChange={(e) => setName(e.target.value)} autoFocus />}</Field>
    </Dialog>
  )
}
