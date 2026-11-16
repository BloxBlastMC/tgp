import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Pencil, Plus, SearchX } from 'lucide-react'
import { useEffect, useId, useMemo, useState } from 'react'
import { toneForRole } from '../../lib/permissions'
import { SubHeader } from '../../components/Page'
import { api, errorMessage, unwrap, type Schemas, type User } from '../../api/client'
import { adminUsersQuery, sessionQuery, userReposQuery } from '../../api/queries'
import { RepoAccessPicker } from '../../components/RepoAccessPicker'
import {
  Alert,
  Avatar,
  Badge,
  Button,
  Checkbox,
  DataTable,
  Dialog,
  EmptyState,
  ErrorState,
  Field,
  FilterBar,
  IconButton,
  Input,
  SearchInput,
  SecretValue,
  SegmentedControl,
  Select,
  Time,
  Tooltip,
  useToast,
  type Column,
} from '../../components/ui'

type Role = Schemas['Role']

type AccessFilter = 'all' | Role

export function AdminUsersPage() {
  const users = useQuery(adminUsersQuery)
  const me = useQuery(sessionQuery).data?.user
  const [filter, setFilter] = useState('')
  const [access, setAccess] = useState<AccessFilter>('all')
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState<User | null>(null)
  const [temp, setTemp] = useState<{ username: string; password: string; expires: string } | null>(null)

  const shown = useMemo(() => {
    const f = filter.trim().toLowerCase()
    return (users.data ?? []).filter(
      (u) =>
        (access === 'all' || u.role === access) &&
        (!f || u.username.toLowerCase().includes(f) || u.display_name.toLowerCase().includes(f) || u.email.toLowerCase().includes(f)),
    )
  }, [users.data, filter, access])
  const count = (r: Role) => users.data?.filter((u) => u.role === r).length
  // Deep link from repository settings: /admin/users?edit=<username>.
  const [deepLinked, setDeepLinked] = useState(false)
  useEffect(() => {
    if (deepLinked || !users.data) return
    const want = new URLSearchParams(window.location.search).get('edit')
    setDeepLinked(true)
    if (!want) return
    const u = users.data.find((x) => x.username.toLowerCase() === want.toLowerCase())
    if (u) setEditing(u)
  }, [users.data, deepLinked])

  const columns: Column<User>[] = [
    {
      id: 'user',
      header: 'User',
      sortValue: (u) => u.display_name.toLowerCase(),
      cell: (u) => (
        <button type="button" className="flex min-w-0 items-center gap-2.5 text-left" onClick={() => setEditing(u)}>
          <Avatar user={u} size={28} />
          <span className="min-w-0 leading-tight">
            <span className="block truncate font-medium hover:text-accent">
              {u.display_name}
              {u.id === me?.id && <span className="ml-1.5 text-xs font-normal text-fg-faint">(you)</span>}
            </span>
            <span className="block truncate text-sm text-fg-faint">@{u.username}</span>
          </span>
        </button>
      ),
    },
    { id: 'email', header: 'Email', sortValue: (u) => u.email, cell: (u) => (u.email ? <span className="text-fg-muted">{u.email}</span> : <span className="text-fg-faint">—</span>), className: 'hidden lg:table-cell', headerClassName: 'hidden lg:table-cell' },
    { id: 'role', header: 'Role', sortValue: (u) => u.role, cell: (u) => <Badge tone={toneForRole(u.role)}>{u.role}</Badge> },
    {
      id: 'repos',
      header: 'Repositories',
      cell: (u) =>
        u.role === 'admin' ? (
          <Tooltip content="Admins can see and change every repository.">
            <span tabIndex={0} className="cursor-help text-fg-muted underline decoration-dotted underline-offset-4">
              All
            </span>
          </Tooltip>
        ) : (
          <span className="text-fg-muted">{u.all_repos ? 'All' : 'Selected'}</span>
        ),
    },
    {
      id: 'status',
      header: 'Status',
      cell: (u) => (
        <div className="flex flex-wrap gap-1">
          {u.disabled ? <Badge tone="danger">disabled</Badge> : !u.must_change_password && <Badge tone="success">active</Badge>}
          {u.must_change_password && <Badge tone="warning">temp password</Badge>}
          {u.totp_enabled && <Badge tone="info">2FA</Badge>}
        </div>
      ),
    },
    {
      id: 'login',
      header: 'Last sign-in',
      sortValue: (u) => u.last_login_at ?? '',
      cell: (u) => <span className="text-fg-muted">{u.last_login_at ? <Time iso={u.last_login_at} /> : 'never'}</span>,
      className: 'hidden lg:table-cell',
      headerClassName: 'hidden lg:table-cell',
    },
    {
      id: 'actions',
      header: <span className="sr-only">Actions</span>,
      align: 'right',
      cell: (u) => (
        <IconButton label={`Edit ${u.username}`} size="sm" onClick={() => setEditing(u)}>
          <Pencil size={13} />
        </IconButton>
      ),
    },
  ]

  return (
    <div className="flex flex-col">
      <div className="px-6 pt-6">
        <SubHeader title="Users" as="h2" description="Everyone who can sign in, their role and repository access." className="mb-4" />
      </div>
      <FilterBar
        className="border-t px-6"
        end={
          <Button variant="primary" icon={<Plus size={14} />} onClick={() => setCreating(true)}>
            New user
          </Button>
        }
      >
        <SearchInput value={filter} onChange={setFilter} label="Filter users" placeholder="Filter by name, username or email…" shortcut className="min-w-40 flex-1 basis-40" />
        <SegmentedControl
          label="Filter by access"
          value={access}
          onChange={setAccess}
          options={[
            { value: 'all', label: 'Everyone', count: users.data?.length },
            { value: 'admin', label: 'Admins', count: count('admin') },
            { value: 'maintainer', label: 'Maintainers', count: count('maintainer') },
            { value: 'tester', label: 'Testers', count: count('tester') },
          ]}
        />
      </FilterBar>
      {users.isError ? (
        <ErrorState error={users.error} />
      ) : (
        <DataTable
          aria-label="Users"
          columns={columns}
          rows={users.data ? shown : undefined}
          loading={users.isPending}
          rowKey={(u) => u.id}
          rowProps={(u) => ({ className: u.disabled ? 'opacity-60' : undefined })}
          defaultSort={{ id: 'user', dir: 'asc' }}
          empty={
            <EmptyState size="inline" icon={<SearchX size={18} />} title="No matching people">
              Try a different name or filter.
            </EmptyState>
          }
        />
      )}

      <CreateUserDialog
        open={creating}
        onClose={() => setCreating(false)}
        onCreated={(r) => {
          setCreating(false)
          setTemp({ username: r.user.username, password: r.temp_password, expires: r.temp_password_expires_at })
        }}
      />
      {editing && (
        <EditUserDialog
          key={editing.id}
          user={editing}
          isSelf={editing.id === me?.id}
          onClose={() => setEditing(null)}
          onTempPassword={(r) => {
            setEditing(null)
            setTemp({ username: r.user.username, password: r.temp_password, expires: r.temp_password_expires_at })
          }}
        />
      )}
      <Dialog
        open={temp !== null}
        onClose={() => setTemp(null)}
        title="Temporary password"
        footer={
          <Button variant="primary" onClick={() => setTemp(null)}>
            Done
          </Button>
        }
      >
        {temp && (
          <>
            <p className="text-fg-muted">
              Give <strong className="text-fg">{temp.username}</strong> this temporary password. They'll choose their own password when they first sign in.
            </p>
            <SecretValue value={temp.password} label={`Temporary password for ${temp.username}`} />
            <Alert tone="warning">
              It's shown only once and expires <Time iso={temp.expires} />.
            </Alert>
          </>
        )}
      </Dialog>
    </div>
  )
}

function RoleFields({
  role,
  allRepos,
  onRole,
  onAllRepos,
  disabled,
  repos,
  onRepos,
}: {
  role: Role
  allRepos: boolean
  onRole: (r: Role) => void
  onAllRepos: (v: boolean) => void
  disabled?: boolean
  repos: string[]
  onRepos: (r: string[]) => void
}) {
  const help: Record<Role, string> = {
    admin: 'Everything, including this admin panel and creating repositories.',
    maintainer: 'Push, merge pull requests and manage branches, CI and labels on their repositories.',
    tester: 'Clone, file issues, comment, review pull requests and re-run CI on their repositories.',
  }
  return (
    <>
      <Field label="Role" hint={help[role]}>
        {(id) => (
          <Select id={id} value={role} onChange={(e) => onRole(e.target.value as Role)} disabled={disabled}>
            <option value="maintainer">Maintainer</option>
            <option value="tester">Tester</option>
            <option value="admin">Admin</option>
          </Select>
        )}
      </Field>
      {role !== 'admin' && (
        <Checkbox
          checked={allRepos}
          onChange={onAllRepos}
          label="Access to all repositories"
          hint="Including repositories created later."
        />
      )}
      {role !== 'admin' && !allRepos && <RepoAccessPicker selected={repos} onChange={onRepos} />}
    </>
  )
}

function CreateUserDialog({
  open,
  onClose,
  onCreated,
}: {
  open: boolean
  onClose: () => void
  onCreated: (r: Schemas['UserWithTempPassword']) => void
}) {
  const qc = useQueryClient()
  const blank = { username: '', display_name: '', email: '', role: 'maintainer' as Role, all_repos: true }
  const [f, setF] = useState(blank)
  const [repos, setRepos] = useState<string[]>([])
  const formId = useId()
  const create = useMutation({
    mutationFn: async () => {
      const r = await unwrap(api.POST('/admin/users', { body: f }))
      if (!f.all_repos && f.role !== 'admin' && repos.length > 0) {
        await unwrap(api.PUT('/admin/users/{username}/repos', { params: { path: { username: r.user.username } }, body: { all_repos: false, repos } }))
      }
      return r
    },
    onSuccess: (r) => {
      void qc.invalidateQueries({ queryKey: adminUsersQuery.queryKey })
      setF(blank)
      setRepos([])
      onCreated(r)
    },
  })
  return (
    <Dialog
      open={open}
      onClose={() => {
        create.reset()
        onClose()
      }}
      title="New user"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" form={formId} variant="primary" loading={create.isPending} disabled={!f.username.trim()}>
            Create user
          </Button>
        </>
      }
    >
      <form
        id={formId}
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault()
          create.mutate()
        }}
      >
        <div className="grid grid-cols-2 gap-3">
          <Field label="Username">
            {(id) => <Input id={id} value={f.username} onChange={(e) => setF({ ...f, username: e.target.value.toLowerCase() })} autoFocus required />}
          </Field>
          <Field label="Display name">{(id) => <Input id={id} value={f.display_name} onChange={(e) => setF({ ...f, display_name: e.target.value })} />}</Field>
        </div>
        <Field label="Email" hint="Optional.">
          {(id) => <Input id={id} type="email" value={f.email} onChange={(e) => setF({ ...f, email: e.target.value })} />}
        </Field>
        <RoleFields
          role={f.role}
          allRepos={f.all_repos}
          onRole={(role) => setF({ ...f, role })}
          onAllRepos={(all_repos) => setF({ ...f, all_repos })}
          repos={repos}
          onRepos={setRepos}
        />
        {create.isError && <Alert>{errorMessage(create.error)}</Alert>}
      </form>
    </Dialog>
  )
}

function EditUserDialog({
  user,
  isSelf,
  onClose,
  onTempPassword,
}: {
  user: User
  isSelf: boolean
  onClose: () => void
  onTempPassword: (r: Schemas['UserWithTempPassword']) => void
}) {
  const qc = useQueryClient()
  const toast = useToast()
  const [f, setF] = useState({
    display_name: user.display_name,
    email: user.email,
    role: user.role,
    all_repos: user.all_repos,
  })
  const [confirm, setConfirm] = useState<null | 'reset' | 'totp' | 'disable'>(null)
  const refresh = () => void qc.invalidateQueries({ queryKey: adminUsersQuery.queryKey })
  const path = { params: { path: { username: user.username } } }
  const granted = useQuery(userReposQuery(user.username))
  const [repos, setRepos] = useState<string[] | null>(null)
  const selectedRepos = repos ?? granted.data?.repos ?? []
  const formId = useId()

  const save = useMutation({
    mutationFn: async () => {
      const { all_repos, ...profile } = f
      await unwrap(api.PATCH('/admin/users/{username}', { ...path, body: profile }))
      if (f.role !== 'admin') {
        await unwrap(api.PUT('/admin/users/{username}/repos', { ...path, body: { all_repos, repos: selectedRepos } }))
      }
      void qc.invalidateQueries({ queryKey: userReposQuery(user.username).queryKey })
    },
    onSuccess: () => {
      refresh()
      toast.success(`Saved ${user.username}`)
      onClose()
    },
  })
  const setDisabled = useMutation({
    mutationFn: (disabled: boolean) => unwrap(api.PATCH('/admin/users/{username}', { ...path, body: { disabled } })),
    onSuccess: (_, disabled) => {
      refresh()
      toast.success(disabled ? `Disabled ${user.username}` : `Enabled ${user.username}`)
      onClose()
    },
  })
  const reset = useMutation({
    mutationFn: () => unwrap(api.POST('/admin/users/{username}/reset-password', path)),
    onSuccess: (r) => {
      refresh()
      onTempPassword(r)
    },
  })
  const resetTotp = useMutation({
    mutationFn: () => unwrap(api.POST('/admin/users/{username}/reset-totp', path)),
    onSuccess: () => {
      refresh()
      onClose()
    },
  })
  const error = save.error ?? setDisabled.error ?? reset.error ?? resetTotp.error

  return (
    <Dialog
      open
      onClose={onClose}
      title={`Edit ${user.username}`}
      description={
        <span className="text-sm">
          {user.email || 'No email'} · last signed in {user.last_login_at ? <Time iso={user.last_login_at} /> : 'never'}
        </span>
      }
      width="max-w-lg"
      icon={<Avatar user={user} size={32} />}
      footer={
        confirm ? (
          <>
            <Button variant="ghost" onClick={() => setConfirm(null)}>
              Back
            </Button>
            <Button
              variant="danger"
              loading={reset.isPending || resetTotp.isPending || setDisabled.isPending}
              onClick={() => (confirm === 'reset' ? reset.mutate() : confirm === 'totp' ? resetTotp.mutate() : setDisabled.mutate(true))}
            >
              {confirm === 'reset' ? 'Reset password' : confirm === 'totp' ? 'Turn off two-factor' : 'Disable user'}
            </Button>
          </>
        ) : (
          <>
            <Button variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" form={formId} variant="primary" loading={save.isPending}>
              Save
            </Button>
          </>
        )
      }
    >
      {confirm ? (
        <>
          <p>
            {confirm === 'reset' && (
              <>
                Generate a new temporary password for <strong>{user.username}</strong>? They'll be signed out everywhere and must choose a new password.
              </>
            )}
            {confirm === 'totp' && (
              <>
                Turn off two-factor authentication for <strong>{user.username}</strong>? Do this only if they lost their authenticator.
              </>
            )}
            {confirm === 'disable' && (
              <>
                Disable <strong>{user.username}</strong>? They'll be signed out and their tokens will stop working until re-enabled.
              </>
            )}
          </p>
          {error && <Alert>{errorMessage(error)}</Alert>}
        </>
      ) : (
        <form
          id={formId}
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault()
            save.mutate()
          }}
        >
          <div className="grid grid-cols-2 gap-3">
            <Field label="Display name">{(id) => <Input id={id} value={f.display_name} onChange={(e) => setF({ ...f, display_name: e.target.value })} />}</Field>
            <Field label="Email">{(id) => <Input id={id} type="email" value={f.email} onChange={(e) => setF({ ...f, email: e.target.value })} />}</Field>
          </div>
          <RoleFields
            role={f.role}
            allRepos={f.all_repos}
            onRole={(role) => setF({ ...f, role })}
            onAllRepos={(all_repos) => setF({ ...f, all_repos })}
            disabled={isSelf}
            repos={selectedRepos}
            onRepos={setRepos}
          />
          {isSelf && <p className="text-sm text-fg-faint">You can't change your own role or disable yourself.</p>}
          <div className="flex flex-wrap items-center gap-2 rounded-lg border border-border bg-bg-subtle p-3">
            <span className="mr-auto text-sm font-medium text-fg-muted">Account</span>
            {!isSelf && <Button size="sm" onClick={() => setConfirm('reset')}>Reset password</Button>}
            {user.totp_enabled && <Button size="sm" onClick={() => setConfirm('totp')}>Reset two-factor</Button>}
            {!isSelf &&
              (user.disabled ? (
                <Button size="sm" onClick={() => setDisabled.mutate(false)} loading={setDisabled.isPending}>
                  Enable user
                </Button>
              ) : (
                <Button size="sm" variant="danger" onClick={() => setConfirm('disable')}>
                  Disable user
                </Button>
              ))}
          </div>
          {error && <Alert>{errorMessage(error)}</Alert>}
        </form>
      )}
    </Dialog>
  )
}
