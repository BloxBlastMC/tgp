import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { MoreHorizontal, Plus, Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import { api, errorMessage, unwrap, type Schemas, type User } from '../../api/client'
import { adminUsersQuery, sessionQuery } from '../../api/queries'
import { Alert, Badge, Button, Checkbox, Dialog, Field, Input, SecretValue, Select, Spinner, Time } from '../../components/ui'

type Role = Schemas['Role']

const roleTone = { admin: 'warning', maintainer: 'accent', tester: 'neutral' } as const

export function AdminUsersPage() {
  const users = useQuery(adminUsersQuery)
  const me = useQuery(sessionQuery).data?.user
  const [filter, setFilter] = useState('')
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState<User | null>(null)
  const [temp, setTemp] = useState<{ username: string; password: string; expires: string } | null>(null)

  const shown = useMemo(() => {
    const f = filter.toLowerCase()
    return (users.data ?? []).filter((u) => !f || u.username.includes(f) || u.display_name.toLowerCase().includes(f) || u.email.includes(f))
  }, [users.data, filter])

  return (
    <div className="flex flex-col">
      <div className="flex items-center gap-2 border-b border-border px-4 py-2">
        <div className="relative max-w-xs flex-1">
          <Search size={13} className="absolute top-1/2 left-2 -translate-y-1/2 text-fg-faint" />
          <Input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter users" className="h-7 pl-7" aria-label="Filter users" />
        </div>
        <span className="flex-1 text-[12px] text-fg-faint">{users.data ? `${users.data.length} users` : ''}</span>
        <Button variant="primary" onClick={() => setCreating(true)}>
          <Plus size={14} /> New user
        </Button>
      </div>
      {users.isPending ? (
        <Spinner />
      ) : users.isError ? (
        <div className="p-4">
          <Alert>{errorMessage(users.error)}</Alert>
        </div>
      ) : (
        <table className="w-full text-left">
          <thead className="text-[11px] tracking-wide text-fg-faint uppercase">
            <tr className="border-b border-border">
              <th className="px-4 py-1.5 font-medium">User</th>
              <th className="px-2 py-1.5 font-medium">Role</th>
              <th className="px-2 py-1.5 font-medium">Repositories</th>
              <th className="px-2 py-1.5 font-medium">Status</th>
              <th className="px-2 py-1.5 font-medium">Last login</th>
              <th className="w-10" />
            </tr>
          </thead>
          <tbody>
            {shown.map((u) => (
              <tr key={u.id} className="h-10 border-b border-border hover:bg-bg-subtle">
                <td className="px-4">
                  <button type="button" className="text-left" onClick={() => setEditing(u)}>
                    <span className="font-medium">{u.display_name}</span>
                    <span className="ml-2 font-mono text-[12px] text-fg-faint">{u.username}</span>
                    {u.id === me?.id && <span className="ml-2 text-[11px] text-fg-faint">(you)</span>}
                  </button>
                </td>
                <td className="px-2">
                  <Badge tone={roleTone[u.role]}>{u.role}</Badge>
                </td>
                <td className="px-2 text-fg-muted">{u.all_repos ? 'All' : 'Selected'}</td>
                <td className="px-2">
                  <div className="flex gap-1">
                    {u.disabled && <Badge tone="danger">disabled</Badge>}
                    {u.must_change_password && <Badge tone="warning">temp password</Badge>}
                    {u.totp_enabled && <Badge tone="success">2FA</Badge>}
                  </div>
                </td>
                <td className="px-2 text-fg-muted">
                  <Time iso={u.last_login_at} />
                </td>
                <td className="pr-2">
                  <Button variant="ghost" aria-label={`Edit ${u.username}`} onClick={() => setEditing(u)}>
                    <MoreHorizontal size={14} />
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
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
      <Dialog open={temp !== null} onClose={() => setTemp(null)} title="Temporary password">
        {temp && (
          <>
            <p>
              Give <strong>{temp.username}</strong> this temporary password. They'll choose their own password when they first sign in.
            </p>
            <SecretValue value={temp.password} label="Temporary password" />
            <Alert tone="warning">
              It's shown only once and expires <Time iso={temp.expires} />.
            </Alert>
            <div className="flex justify-end">
              <Button variant="primary" onClick={() => setTemp(null)}>
                Done
              </Button>
            </div>
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
}: {
  role: Role
  allRepos: boolean
  onRole: (r: Role) => void
  onAllRepos: (v: boolean) => void
  disabled?: boolean
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
          hint="Otherwise they only see repositories they're granted."
        />
      )}
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
  const create = useMutation({
    mutationFn: () => unwrap(api.POST('/admin/users', { body: f })),
    onSuccess: (r) => {
      void qc.invalidateQueries({ queryKey: adminUsersQuery.queryKey })
      setF(blank)
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
    >
      <form
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
        <RoleFields role={f.role} allRepos={f.all_repos} onRole={(role) => setF({ ...f, role })} onAllRepos={(all_repos) => setF({ ...f, all_repos })} />
        {create.isError && <Alert>{errorMessage(create.error)}</Alert>}
        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" loading={create.isPending} disabled={!f.username.trim()}>
            Create user
          </Button>
        </div>
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
  const [f, setF] = useState({
    display_name: user.display_name,
    email: user.email,
    role: user.role,
    all_repos: user.all_repos,
  })
  const [confirm, setConfirm] = useState<null | 'reset' | 'totp' | 'disable'>(null)
  const refresh = () => void qc.invalidateQueries({ queryKey: adminUsersQuery.queryKey })
  const path = { params: { path: { username: user.username } } }

  const save = useMutation({
    mutationFn: () => unwrap(api.PATCH('/admin/users/{username}', { ...path, body: f })),
    onSuccess: () => {
      refresh()
      onClose()
    },
  })
  const setDisabled = useMutation({
    mutationFn: (disabled: boolean) => unwrap(api.PATCH('/admin/users/{username}', { ...path, body: { disabled } })),
    onSuccess: () => {
      refresh()
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
    <Dialog open onClose={onClose} title={`Edit ${user.username}`}>
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
          <div className="flex justify-end gap-2">
            <Button variant="ghost" onClick={() => setConfirm(null)}>
              Back
            </Button>
            <Button
              variant="danger"
              loading={reset.isPending || resetTotp.isPending || setDisabled.isPending}
              onClick={() => (confirm === 'reset' ? reset.mutate() : confirm === 'totp' ? resetTotp.mutate() : setDisabled.mutate(true))}
            >
              {confirm === 'reset' ? 'Reset password' : confirm === 'totp' ? 'Turn off 2FA' : 'Disable user'}
            </Button>
          </div>
        </>
      ) : (
        <form
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
          />
          {isSelf && <p className="text-[12px] text-fg-faint">You can't change your own role or disable yourself.</p>}
          <div className="flex flex-wrap gap-2 border-t border-border pt-3">
            {!isSelf && <Button onClick={() => setConfirm('reset')}>Reset password</Button>}
            {user.totp_enabled && <Button onClick={() => setConfirm('totp')}>Reset 2FA</Button>}
            {!isSelf &&
              (user.disabled ? (
                <Button onClick={() => setDisabled.mutate(false)} loading={setDisabled.isPending}>
                  Enable user
                </Button>
              ) : (
                <Button variant="danger" onClick={() => setConfirm('disable')}>
                  Disable user
                </Button>
              ))}
          </div>
          {error && <Alert>{errorMessage(error)}</Alert>}
          <div className="flex justify-end gap-2">
            <Button variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" variant="primary" loading={save.isPending}>
              Save
            </Button>
          </div>
        </form>
      )}
    </Dialog>
  )
}
