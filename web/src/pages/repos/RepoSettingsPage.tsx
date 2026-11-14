import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { api, errorMessage, unwrap } from '../../api/client'
import { branchesQuery, repoAccessQuery, repoQuery, reposQuery } from '../../api/queries'
import { Alert, Badge, Button, Dialog, Field, Input, Section, Select, Spinner } from '../../components/ui'
import { SecretsSection } from '../../components/ci/SecretsSection'
import { ProtectionSection } from './ProtectionSection'
import { useRepoName } from './RepoLayout'

export function RepoSettingsPage() {
  const name = useRepoName()
  const repo = useQuery(repoQuery(name)).data!
  const isAdmin = repo.permission === 'admin'
  if (repo.permission !== 'write' && !isAdmin) return <div className="p-4"><Alert>You need write access to change settings.</Alert></div>
  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-5 p-6">
      <GeneralSection />
      <ProtectionSection repo={repo.name} isAdmin={isAdmin} />
      <SecretsSection repo={repo.name} />
      <AccessSection isAdmin={isAdmin} />
      {isAdmin && <DangerZone />}
    </div>
  )
}

function useUpdateRepo() {
  const name = useRepoName()
  const qc = useQueryClient()
  const navigate = useNavigate()
  return useMutation({
    mutationFn: (body: { name?: string; description?: string; default_branch?: string; archived?: boolean }) =>
      unwrap(api.PATCH('/repos/{repo}', { params: { path: { repo: name } }, body })),
    onSuccess: (r) => {
      qc.setQueryData(repoQuery(r.name).queryKey, r)
      void qc.invalidateQueries({ queryKey: reposQuery.queryKey })
      if (r.name.toLowerCase() !== name.toLowerCase()) void navigate({ to: '/$repo/settings', params: { repo: r.name } })
    },
  })
}

function GeneralSection() {
  const name = useRepoName()
  const repo = useQuery(repoQuery(name)).data!
  const branches = useQuery({ ...branchesQuery(name), enabled: !repo.empty })
  const [description, setDescription] = useState(repo.description)
  const [branch, setBranch] = useState(repo.default_branch)
  const update = useUpdateRepo()
  return (
    <Section title="General">
      <form
        className="flex max-w-md flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault()
          update.mutate({ description, ...(branch !== repo.default_branch ? { default_branch: branch } : {}) })
        }}
      >
        <Field label="Description">{(id) => <Input id={id} value={description} onChange={(e) => setDescription(e.target.value)} />}</Field>
        <Field label="Default branch" hint="Shown by default and used as the base for new pull requests.">
          {(id) =>
            repo.empty ? (
              <Input id={id} value={repo.default_branch} disabled />
            ) : (
              <Select id={id} value={branch} onChange={(e) => setBranch(e.target.value)}>
                {(branches.data ?? [{ name: repo.default_branch }]).map((b) => (
                  <option key={b.name} value={b.name}>
                    {b.name}
                  </option>
                ))}
              </Select>
            )
          }
        </Field>
        {update.isError && <Alert>{errorMessage(update.error)}</Alert>}
        {update.isSuccess && <Alert tone="success">Saved.</Alert>}
        <Button type="submit" variant="primary" className="self-start" loading={update.isPending}>
          Save
        </Button>
      </form>
    </Section>
  )
}

const sourceLabel = { admin: 'Admin', all_repos: 'All repositories', grant: 'Granted' }

function AccessSection({ isAdmin }: { isAdmin: boolean }) {
  const name = useRepoName()
  const access = useQuery(repoAccessQuery(name))
  return (
    <Section
      title="Access"
      description={
        isAdmin ? (
          <>
            Grant or revoke access per person in{' '}
            <Link to="/admin/users" className="text-accent hover:underline">
              Admin → Users
            </Link>
            .
          </>
        ) : (
          'People who can see this repository. Admins manage access.'
        )
      }
    >
      {access.isPending ? (
        <Spinner />
      ) : (
        <ul className="-my-2 divide-y divide-border">
          {access.data?.map((a) => (
            <li key={a.user.id} className="flex items-center gap-3 py-2">
              <span className="font-medium">{a.user.display_name}</span>
              <span className="font-mono text-[12px] text-fg-faint">{a.user.username}</span>
              <div className="flex-1" />
              <span className="text-[12px] text-fg-faint">{sourceLabel[a.source]}</span>
              <Badge tone={a.permission === 'admin' ? 'warning' : a.permission === 'write' ? 'accent' : 'neutral'}>{a.permission}</Badge>
            </li>
          ))}
        </ul>
      )}
    </Section>
  )
}

function DangerZone() {
  const name = useRepoName()
  const repo = useQuery(repoQuery(name)).data!
  const qc = useQueryClient()
  const navigate = useNavigate()
  const update = useUpdateRepo()
  const [newName, setNewName] = useState(repo.name)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [typed, setTyped] = useState('')
  const del = useMutation({
    mutationFn: () => unwrap(api.DELETE('/repos/{repo}', { params: { path: { repo: name } } })),
    onSuccess: () => {
      qc.removeQueries({ queryKey: ['repo', name] })
      void qc.invalidateQueries({ queryKey: reposQuery.queryKey })
      void navigate({ to: '/' })
    },
  })
  return (
    <section className="rounded-md border border-danger/40">
      <header className="border-b border-danger/40 px-4 py-3 font-medium text-danger">Danger zone</header>
      <div className="flex flex-col divide-y divide-border">
        <form
          className="flex items-end gap-3 p-4"
          onSubmit={(e) => {
            e.preventDefault()
            update.mutate({ name: newName })
          }}
        >
          <div className="flex-1">
            <Field label="Rename" hint="Existing clones must update their remote URL.">
              {(id) => <Input id={id} value={newName} onChange={(e) => setNewName(e.target.value)} />}
            </Field>
          </div>
          <Button type="submit" disabled={newName === repo.name || !newName.trim()} loading={update.isPending && update.variables?.name !== undefined}>
            Rename
          </Button>
        </form>
        <div className="flex items-center gap-3 p-4">
          <div className="flex-1">
            <p className="font-medium">{repo.archived ? 'Unarchive this repository' : 'Archive this repository'}</p>
            <p className="text-[12px] text-fg-muted">{repo.archived ? 'Allow pushes again.' : 'Make it read-only. Nobody can push until it is unarchived.'}</p>
          </div>
          <Button variant="danger" onClick={() => update.mutate({ archived: !repo.archived })} loading={update.isPending && update.variables?.archived !== undefined}>
            {repo.archived ? 'Unarchive' : 'Archive'}
          </Button>
        </div>
        <div className="flex items-center gap-3 p-4">
          <div className="flex-1">
            <p className="font-medium">Delete this repository</p>
            <p className="text-[12px] text-fg-muted">Permanently removes the code, history and access grants. This can't be undone.</p>
          </div>
          <Button variant="danger" onClick={() => setConfirmDelete(true)}>
            Delete
          </Button>
        </div>
        {update.isError && (
          <div className="p-4">
            <Alert>{errorMessage(update.error)}</Alert>
          </div>
        )}
      </div>
      <Dialog open={confirmDelete} onClose={() => setConfirmDelete(false)} title={`Delete ${repo.name}`}>
        <p>
          This permanently deletes <strong>{repo.name}</strong>. Type its name to confirm.
        </p>
        <Input value={typed} onChange={(e) => setTyped(e.target.value)} aria-label="Repository name" autoFocus />
        {del.isError && <Alert>{errorMessage(del.error)}</Alert>}
        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={() => setConfirmDelete(false)}>
            Cancel
          </Button>
          <Button variant="danger" disabled={typed !== repo.name} loading={del.isPending} onClick={() => del.mutate()}>
            Delete forever
          </Button>
        </div>
      </Dialog>
    </section>
  )
}
