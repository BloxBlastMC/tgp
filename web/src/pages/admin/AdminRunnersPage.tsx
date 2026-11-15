import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Cpu, Hourglass, Layers, MoreHorizontal, Pencil, Play, Plus, Power, PowerOff, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { api, errorMessage, unwrap, type Runner, type Schemas } from '../../api/client'
import { runnersQuery } from '../../api/queries'
import { Container, SubHeader } from '../../components/Page'
import {
  Alert,
  Badge,
  Button,
  Card,
  DataTable,
  Dialog,
  Dot,
  EmptyState,
  ErrorState,
  Field,
  IconButton,
  Input,
  Menu,
  MenuItem,
  MenuSeparator,
  Meter,
  SecretValue,
  StatCard,
  Time,
  useConfirm,
  useToast,
  type Column,
} from '../../components/ui'

const statusTone = { online: 'success', offline: 'neutral', disabled: 'warning' } as const

function Load({ label, value }: { label: string; value?: number | null }) {
  if (value == null) return <span className="text-xs text-fg-faint">{label} —</span>
  return (
    <span className="flex items-center gap-2 text-xs text-fg-muted" title={`${label} ${value.toFixed(0)}%`}>
      <span className="w-7">{label}</span>
      <Meter value={value} label={label} size="sm" className="w-20" />
      <span className="w-8 tabular-nums">{value.toFixed(0)}%</span>
    </span>
  )
}

export function AdminRunnersPage() {
  const runners = useQuery(runnersQuery)
  const qc = useQueryClient()
  const confirm = useConfirm()
  const toast = useToast()
  const [token, setToken] = useState<Schemas['RunnerRegistrationToken'] | null>(null)
  const [renaming, setRenaming] = useState<Runner | null>(null)
  const refresh = () => void qc.invalidateQueries({ queryKey: runnersQuery.queryKey })
  const create = useMutation({
    mutationFn: () => unwrap(api.POST('/admin/runners/registration-tokens', { body: { expires_in_hours: 24 } })),
    onSuccess: setToken,
  })
  const update = useMutation({
    mutationFn: ({ id, ...body }: { id: number; disabled?: boolean; name?: string }) => unwrap(api.PATCH('/admin/runners/{id}', { params: { path: { id } }, body })),
    onSuccess: (r, v) => {
      if (v.disabled !== undefined) toast.success(v.disabled ? `Disabled ${r.name}` : `Enabled ${r.name}`)
      else if (v.name) toast.success(`Renamed to ${r.name}`)
      refresh()
    },
  })
  const remove = useMutation({
    mutationFn: (id: number) => unwrap(api.DELETE('/admin/runners/{id}', { params: { path: { id } } })),
    onSuccess: () => {
      toast.success('Runner removed')
      refresh()
    },
  })
  const err = create.error ?? update.error ?? remove.error
  const list = runners.data?.runners ?? []
  const online = list.filter((r) => r.status === 'online').length
  const slots = list.reduce((n, r) => n + (r.status === 'online' ? r.capacity : 0), 0)

  const columns: Column<Runner>[] = [
    {
      id: 'name',
      header: 'Runner',
      sortValue: (r) => r.name.toLowerCase(),
      cell: (r) => (
        <div className="py-1">
          <div className="flex items-center gap-2">
            <Dot tone={statusTone[r.status]} pulse={r.status === 'online' && r.busy > 0} label={r.status} />
            <span className="font-medium">{r.name}</span>
            <Badge tone={statusTone[r.status]}>{r.status}</Badge>
            {r.overloaded && <Badge tone="danger">overloaded</Badge>}
            {r.host_mode && r.host_mode !== 'off' && (
              <Badge tone="warning" title={r.host_mode === 'all' ? 'Runs host jobs (container: none) of every run, pull requests included' : 'Runs host jobs (container: none) of trusted runs: pushes to the default branch, dispatches and releases'}>
                host jobs: {r.host_mode}
              </Badge>
            )}
          </div>
          <div className="mt-1 flex flex-wrap items-center gap-1 text-xs text-fg-faint">
            {r.labels.map((l) => (
              <Badge key={l} className="font-mono">
                {l}
              </Badge>
            ))}
            <span>
              {r.os}/{r.arch} · {r.version}
            </span>
          </div>
        </div>
      ),
    },
    {
      id: 'jobs',
      header: 'Jobs',
      sortValue: (r) => r.busy,
      cell: (r) => (
        <div className="py-1">
          <div className="tabular-nums">
            {r.busy} / {r.capacity}
          </div>
          {r.jobs.map((j) => (
            <Link key={j.id} to="/$repo/ci/runs/$number" params={{ repo: j.repo, number: String(j.run_number) }} search={{ job: j.id }} className="block max-w-56 truncate text-sm text-fg-muted hover:text-accent">
              {j.repo} #{j.run_number} · {j.name}
            </Link>
          ))}
        </div>
      ),
    },
    {
      id: 'load',
      header: 'Load',
      cell: (r) => (
        <div className="flex flex-col gap-1 py-1">
          <Load label="CPU" value={r.cpu_percent} />
          <Load label="Mem" value={r.memory_percent} />
        </div>
      ),
    },
    { id: 'seen', header: 'Last seen', sortValue: (r) => r.last_seen_at ?? '', cell: (r) => <span className="text-sm text-fg-muted"><Time iso={r.last_seen_at} /></span> },
    {
      id: 'actions',
      header: <span className="sr-only">Actions</span>,
      align: 'right',
      cell: (r) => (
        <Menu
          placement="bottom-end"
          label={`${r.name} actions`}
          trigger={(t) => (
            <IconButton {...t} label={`Actions for ${r.name}`} size="sm">
              <MoreHorizontal size={14} />
            </IconButton>
          )}
        >
          <MenuItem icon={<Pencil size={13} />} onSelect={() => setRenaming(r)}>
            Rename
          </MenuItem>
          <MenuItem icon={r.status === 'disabled' ? <Power size={13} /> : <PowerOff size={13} />} onSelect={() => update.mutate({ id: r.id, disabled: r.status !== 'disabled' })}>
            {r.status === 'disabled' ? 'Enable' : 'Disable'}
          </MenuItem>
          <MenuSeparator />
          <MenuItem
            icon={<Trash2 size={13} />}
            tone="danger"
            onSelect={async () => {
              const ok = await confirm({
                title: `Remove ${r.name}?`,
                description: `Its credentials stop working${r.busy ? ` and its ${r.busy} running job(s) fail` : ''}.`,
                confirmLabel: 'Remove runner',
              })
              if (ok) remove.mutate(r.id)
            }}
          >
            Remove
          </MenuItem>
        </Menu>
      ),
    },
  ]

  return (
    <Container width="wide">
      <SubHeader
        title="Runners"
        as="h2"
        description="Machines that run CI jobs in Docker. Each pulls jobs from this server."
        actions={
          <Button variant="primary" icon={<Plus size={13} />} loading={create.isPending} onClick={() => create.mutate()}>
            Add runner
          </Button>
        }
      />
      {runners.data && (
        <div className="mb-5 grid grid-cols-2 gap-3 md:grid-cols-4">
          <StatCard label="Runners online" value={`${online} / ${list.length}`} icon={<Cpu size={15} />} tone={list.length && !online ? 'danger' : online ? 'success' : undefined} />
          <StatCard label="Job slots" value={slots} icon={<Layers size={15} />} hint="on online runners" />
          <StatCard label="Running jobs" value={runners.data.running_jobs} icon={<Play size={15} />} />
          <StatCard label="Queued jobs" value={runners.data.queued_jobs} icon={<Hourglass size={15} />} tone={runners.data.queued_jobs > 0 ? 'warning' : undefined} />
        </div>
      )}
      {err && <Alert className="mb-4">{errorMessage(err)}</Alert>}
      {runners.isError ? (
        <ErrorState error={runners.error} className="p-0" />
      ) : runners.data && list.length === 0 ? (
        <Card>
          <EmptyState
            icon={<Cpu size={20} />}
            title="No runners registered"
            action={
              <Button icon={<Plus size={13} />} loading={create.isPending} onClick={() => create.mutate()}>
                Register a runner
              </Button>
            }
          >
            CI jobs wait in the queue until a runner picks them up. Install tgp-runner on a machine with Docker and register it with a one-time token.
          </EmptyState>
        </Card>
      ) : (
        <div className="overflow-hidden rounded-lg border border-border bg-surface-1 shadow-card">
          <DataTable
            aria-label="Runners"
            columns={columns}
            rows={runners.data ? list : undefined}
            loading={runners.isPending}
            rowKey={(r) => r.id}
            rowProps={() => ({ 'data-testid': 'runner-row' })}
            defaultSort={{ id: 'name', dir: 'asc' }}
          />
        </div>
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
    </Container>
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
