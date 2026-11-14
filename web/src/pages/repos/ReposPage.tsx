import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { Archive, FolderGit2, Plus, Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import { api, errorMessage, unwrap } from '../../api/client'
import { reposQuery, sessionQuery } from '../../api/queries'
import { EmptyState, Page } from '../../components/Page'
import { Alert, Badge, Button, Checkbox, Dialog, Field, Input, Spinner, Time } from '../../components/ui'

export function ReposPage() {
  const repos = useQuery(reposQuery)
  const isAdmin = useQuery(sessionQuery).data?.user.role === 'admin'
  const [filter, setFilter] = useState('')
  const [creating, setCreating] = useState(false)
  const shown = useMemo(() => {
    const f = filter.toLowerCase()
    return (repos.data ?? []).filter((r) => r.name.toLowerCase().includes(f) || r.description.toLowerCase().includes(f))
  }, [repos.data, filter])

  return (
    <Page
      title="Repositories"
      actions={
        isAdmin && (
          <Button variant="primary" onClick={() => setCreating(true)}>
            <Plus size={14} /> New repository
          </Button>
        )
      }
    >
      {repos.isPending ? (
        <Spinner />
      ) : repos.isError ? (
        <div className="p-4">
          <Alert>{errorMessage(repos.error)}</Alert>
        </div>
      ) : repos.data.length === 0 ? (
        <EmptyState title="No repositories yet">
          {isAdmin ? (
            <Button variant="primary" className="mt-3" onClick={() => setCreating(true)}>
              Create the first repository
            </Button>
          ) : (
            "You don't have access to any repositories yet. Ask an admin to grant you access."
          )}
        </EmptyState>
      ) : (
        <>
          <div className="border-b border-border px-4 py-2">
            <div className="relative max-w-xs">
              <Search size={13} className="absolute top-1/2 left-2 -translate-y-1/2 text-fg-faint" />
              <Input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter repositories" className="h-7 pl-7" aria-label="Filter repositories" />
            </div>
          </div>
          <ul>
            {shown.map((r) => (
              <li key={r.id} className="border-b border-border hover:bg-bg-subtle">
                <Link to="/$repo" params={{ repo: r.name }} className="flex items-center gap-3 px-4 py-2.5">
                  <FolderGit2 size={16} className="shrink-0 text-fg-faint" />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="font-medium">{r.name}</span>
                      {r.archived && (
                        <Badge tone="warning">
                          <Archive size={10} className="mr-1" />
                          archived
                        </Badge>
                      )}
                      {r.empty && <Badge>empty</Badge>}
                    </div>
                    {r.description && <p className="truncate text-fg-muted">{r.description}</p>}
                  </div>
                  <span className="shrink-0 text-sm text-fg-faint">{r.pushed_at ? <>Pushed <Time iso={r.pushed_at} /></> : 'No pushes yet'}</span>
                </Link>
              </li>
            ))}
          </ul>
        </>
      )}
      <CreateRepoDialog open={creating} onClose={() => setCreating(false)} />
    </Page>
  )
}

function CreateRepoDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const blank = { name: '', description: '', default_branch: 'main', auto_init: true }
  const [f, setF] = useState(blank)
  const create = useMutation({
    mutationFn: () => unwrap(api.POST('/repos', { body: f })),
    onSuccess: (r) => {
      void qc.invalidateQueries({ queryKey: reposQuery.queryKey })
      setF(blank)
      onClose()
      void navigate({ to: '/$repo', params: { repo: r.name } })
    },
  })
  return (
    <Dialog
      open={open}
      onClose={() => {
        create.reset()
        onClose()
      }}
      title="New repository"
    >
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault()
          create.mutate()
        }}
      >
        <Field label="Name" hint="Letters, digits, '.', '-' and '_'.">
          {(id) => <Input id={id} value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} placeholder="minigames" autoFocus required />}
        </Field>
        <Field label="Description">{(id) => <Input id={id} value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} />}</Field>
        <Field label="Default branch">{(id) => <Input id={id} value={f.default_branch} onChange={(e) => setF({ ...f, default_branch: e.target.value })} />}</Field>
        <Checkbox
          checked={f.auto_init}
          onChange={(auto_init) => setF({ ...f, auto_init })}
          label="Initialize with a README"
          hint="Leave unchecked to push an existing repository."
        />
        <p className="text-sm text-fg-faint">Maintainers and testers with access to all repositories see it immediately. Grant others access from Admin → Users.</p>
        {create.isError && <Alert>{errorMessage(create.error)}</Alert>}
        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" loading={create.isPending} disabled={!f.name.trim()}>
            Create repository
          </Button>
        </div>
      </form>
    </Dialog>
  )
}
