import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { Archive, ArchiveRestore, Lock, Trash2 } from 'lucide-react'
import { useEffect, useState, type ReactNode } from 'react'
import { api, errorMessage, unwrap } from '../../api/client'
import { branchesQuery, repoAccessQuery, repoQuery, reposQuery } from '../../api/queries'
import { SecretsSection } from '../../components/ci/SecretsSection'
import { RepoSettingsLayout } from '../../components/repos/RepoSettingsNav'
import { Alert, Avatar, Badge, Button, Card, EmptyState, Field, Input, Select, SkeletonRows, useConfirm, useToast } from '../../components/ui'
import { canWrite, isRepoAdmin, toneForRole } from '../../lib/permissions'
import { ProtectionSection } from './ProtectionSection'
import { useRepoName } from './RepoLayout'

/** Settings pages need write access; the danger zone needs admin. */
function SettingsShell({ adminOnly, children }: { adminOnly?: boolean; children: (ctx: { repo: string; isAdmin: boolean }) => ReactNode }) {
  const name = useRepoName()
  const repo = useQuery(repoQuery(name)).data!
  const isAdmin = isRepoAdmin(repo.permission)
  if (!canWrite(repo.permission) || (adminOnly && !isAdmin))
    return (
      <EmptyState icon={<Lock size={20} />} title="No access to settings">
        {adminOnly ? 'Only admins can archive or delete this repository.' : 'You need write access to change settings.'}
      </EmptyState>
    )
  return <RepoSettingsLayout>{children({ repo: repo.name, isAdmin })}</RepoSettingsLayout>
}

// Old links pointed at sections of one long page (#protection etc.).
const legacyHash: Record<string, string> = {
  protection: '/$repo/settings/protection',
  secrets: '/$repo/settings/secrets',
  access: '/$repo/settings/access',
  danger: '/$repo/settings/danger',
}

/** Settings → General: name, description and default branch. */
export function RepoSettingsPage() {
  const name = useRepoName()
  const navigate = useNavigate()
  useEffect(() => {
    const to = legacyHash[window.location.hash.slice(1)]
    if (to) void navigate({ to, params: { repo: name }, replace: true })
  }, [name, navigate])
  return <SettingsShell>{() => <GeneralSection />}</SettingsShell>
}

export function RepoProtectionPage() {
  return <SettingsShell>{({ repo, isAdmin }) => <ProtectionSection repo={repo} isAdmin={isAdmin} />}</SettingsShell>
}

export function RepoSecretsPage() {
  return <SettingsShell>{({ repo }) => <SecretsSection repo={repo} />}</SettingsShell>
}

export function RepoAccessPage() {
  return <SettingsShell>{({ isAdmin }) => <AccessSection isAdmin={isAdmin} />}</SettingsShell>
}

export function RepoDangerPage() {
  return <SettingsShell adminOnly>{() => <DangerZone />}</SettingsShell>
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
  const toast = useToast()
  const isAdmin = isRepoAdmin(repo.permission)
  const dirty = description !== repo.description || branch !== repo.default_branch
  return (
    <Card
      title="General"
      description="The repository's name, how it's described and which branch is shown first."
      footer={
          <>
            <span className="mr-auto text-sm text-fg-faint">{dirty ? 'You have unsaved changes.' : ''}</span>
            {dirty && (
              <Button
                variant="ghost"
                onClick={() => {
                  setDescription(repo.description)
                  setBranch(repo.default_branch)
                }}
              >
                Discard
              </Button>
            )}
            <Button type="submit" form="repo-general" variant="primary" loading={update.isPending && update.variables?.name === undefined} disabled={!dirty}>
              Save
            </Button>
          </>
        }
      >
      <div className="flex max-w-lg flex-col gap-4">
        {isAdmin && <RenameField />}
        <form
          id="repo-general"
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault()
            update.mutate(
              { description, ...(branch !== repo.default_branch ? { default_branch: branch } : {}) },
              { onSuccess: () => toast.success('Repository settings saved') },
            )
          }}
        >
          <Field label="Description">{(id) => <Input id={id} value={description} onChange={(e) => setDescription(e.target.value)} placeholder="What's in this repository?" />}</Field>
          <Field label="Default branch" hint="Shown by default and used as the base for new pull requests.">
            {(id) =>
              repo.empty ? (
                <Input id={id} value={repo.default_branch} disabled />
              ) : (
                <Select id={id} value={branch} onChange={(e) => setBranch(e.target.value)} className="font-mono">
                  {(branches.data ?? [{ name: repo.default_branch }]).map((b) => (
                    <option key={b.name} value={b.name}>
                      {b.name}
                    </option>
                  ))}
                </Select>
              )
            }
          </Field>
          {update.isError && update.variables?.name === undefined && <Alert>{errorMessage(update.error)}</Alert>}
        </form>
      </div>
    </Card>
  )
}

/** Rename, with a confirmation because clones and links use the name. */
function RenameField() {
  const name = useRepoName()
  const repo = useQuery(repoQuery(name)).data!
  const update = useUpdateRepo()
  const confirm = useConfirm()
  const toast = useToast()
  const [newName, setNewName] = useState(repo.name)
  const renaming = update.isPending && update.variables?.name !== undefined
  return (
    <form
      onSubmit={async (e) => {
        e.preventDefault()
        const ok = await confirm({
          title: `Rename ${repo.name} to ${newName.trim()}?`,
          description: 'Existing clones keep working only after they update their remote URL. Links to the old name stop working.',
          confirmLabel: 'Rename repository',
          tone: 'default',
        })
        if (ok) update.mutate({ name: newName.trim() }, { onSuccess: (r) => toast.success(`Renamed to ${r.name}`) })
      }}
    >
      <Field label="Name" hint="Renaming changes the clone URL; existing clones must update their remote." error={update.isError && update.variables?.name !== undefined ? errorMessage(update.error) : undefined}>
        {(id) => (
          <div className="flex gap-2">
            <Input id={id} value={newName} onChange={(e) => setNewName(e.target.value)} className="font-mono" spellCheck={false} autoComplete="off" />
            <Button type="submit" disabled={newName.trim() === repo.name || !newName.trim()} loading={renaming}>
              Rename
            </Button>
          </div>
        )}
      </Field>
    </form>
  )
}

// An admin's badge already says admin, so only the other sources get a label.
const sourceLabel: Record<string, string> = { all_repos: 'All repositories', grant: 'Granted' }

function AccessSection({ isAdmin }: { isAdmin: boolean }) {
  const name = useRepoName()
  const access = useQuery(repoAccessQuery(name))
  return (
    <Card
      flush
      title="Access"
      description={
        isAdmin ? (
          <>
            Who can see this repository and why. Use <span className="font-medium text-fg">Manage</span> to change someone's access in{' '}
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
        <SkeletonRows rows={3} avatar />
      ) : (
        <ul>
          {access.data?.map((a) => (
            <li key={a.user.id} className="flex items-center gap-3 border-b border-border px-4 py-2.5 last:border-0">
              <Avatar user={a.user} size={24} />
              <div className="min-w-0 flex-1 leading-tight">
                <div className="truncate font-medium">{a.user.display_name}</div>
                <div className="truncate text-sm text-fg-faint">{a.user.username}</div>
              </div>
              {sourceLabel[a.source] && <span className="text-sm text-fg-muted">{sourceLabel[a.source]}</span>}
              <Badge tone={toneForRole(a.permission)}>{a.permission}</Badge>
              {isAdmin && a.source !== 'admin' && (
                <Link to="/admin/users" search={{ edit: a.user.username }} className="text-sm text-accent hover:underline" aria-label={`Manage ${a.user.username}'s access`}>
                  Manage
                </Link>
              )}
            </li>
          ))}
        </ul>
      )}
    </Card>
  )
}

function DangerRow({ title, description, children }: { title: string; description: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-wrap items-center gap-3 border-b border-border px-4 py-3.5 last:border-0">
      <div className="min-w-0 flex-1">
        <p className="font-medium">{title}</p>
        <p className="text-sm text-fg-muted">{description}</p>
      </div>
      {children}
    </div>
  )
}

function DangerZone() {
  const name = useRepoName()
  const repo = useQuery(repoQuery(name)).data!
  const qc = useQueryClient()
  const navigate = useNavigate()
  const update = useUpdateRepo()
  const confirm = useConfirm()
  const toast = useToast()
  const del = useMutation({
    mutationFn: () => unwrap(api.DELETE('/repos/{repo}', { params: { path: { repo: name } } })),
    onSuccess: () => {
      qc.removeQueries({ queryKey: ['repo', name] })
      void qc.invalidateQueries({ queryKey: reposQuery.queryKey })
      toast.success(`Deleted ${repo.name}`)
      void navigate({ to: '/repos' })
    },
  })
  return (
    <Card title="Danger zone" tone="danger" flush>
      <DangerRow
        title={repo.archived ? 'Unarchive this repository' : 'Archive this repository'}
        description={repo.archived ? 'Allow pushes again.' : 'Make it read-only. Nobody can push until it is unarchived.'}
      >
        <Button
          variant={repo.archived ? 'secondary' : 'danger'}
          icon={repo.archived ? <ArchiveRestore size={13} /> : <Archive size={13} />}
          loading={update.isPending && update.variables?.archived !== undefined}
          onClick={async () => {
            const ok = await confirm(
              repo.archived
                ? { title: `Unarchive ${repo.name}?`, description: 'People with write access can push again.', confirmLabel: 'Unarchive', tone: 'default' }
                : { title: `Archive ${repo.name}?`, description: 'The repository becomes read-only: no pushes, merges or new issues until it is unarchived.', confirmLabel: 'Archive' },
            )
            if (ok) update.mutate({ archived: !repo.archived }, { onSuccess: (r) => toast.success(r.archived ? 'Repository archived' : 'Repository unarchived') })
          }}
        >
          {repo.archived ? 'Unarchive' : 'Archive'}
        </Button>
      </DangerRow>
      <DangerRow title="Delete this repository" description="Permanently removes the code, history and access grants. This can't be undone.">
        <Button
          variant="danger"
          icon={<Trash2 size={13} />}
          loading={del.isPending}
          onClick={async () => {
            const ok = await confirm({
              title: `Delete ${repo.name}?`,
              description: (
                <>
                  This permanently deletes <strong className="text-fg">{repo.name}</strong>, its issues, pull requests, releases and CI history.
                </>
              ),
              confirmLabel: 'Delete forever',
              typeToConfirm: { value: repo.name, label: 'Repository name' },
            })
            if (ok) del.mutate()
          }}
        >
          Delete
        </Button>
      </DangerRow>
      {(update.isError || del.isError) && (
        <div className="p-4">
          <Alert>{errorMessage(update.error ?? del.error)}</Alert>
        </div>
      )}
    </Card>
  )
}
