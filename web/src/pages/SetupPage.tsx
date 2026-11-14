import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Check, Plus, Trash2 } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { api, ApiRequestError, ensureCsrf, errorMessage, unwrap, type Schemas } from '../api/client'
import { AuthCard } from '../components/AuthCard'
import { Alert, Badge, Button, Checkbox, cx, Field, Input, SecretValue, Select } from '../components/ui'

type Role = Schemas['Role']
type NewUser = { username: string; display_name: string; email: string; role: Role; all_repos: boolean }

const steps = ['Setup token', 'Admin account', 'Organization', 'Team', 'Review'] as const

export function SetupPage() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [step, setStep] = useState(0)
  const [token, setToken] = useState('')
  const [admin, setAdmin] = useState({ username: '', display_name: '', email: '', password: '', confirm: '' })
  const [org, setOrg] = useState({ name: '', description: '', base_url: window.location.origin })
  const [logo, setLogo] = useState<File | null>(null)
  const [users, setUsers] = useState<NewUser[]>([])
  const [logoError, setLogoError] = useState('')
  const logoUrl = useMemo(() => (logo ? URL.createObjectURL(logo) : ''), [logo])
  useEffect(() => () => URL.revokeObjectURL(logoUrl), [logoUrl])

  const submit = useMutation({
    mutationFn: async () => {
      await ensureCsrf()
      const res = await unwrap(
        api.POST('/setup', {
          body: {
            setup_token: token.trim(),
            admin: { username: admin.username, display_name: admin.display_name, email: admin.email, password: admin.password },
            org,
            users: users.filter((u) => u.username.trim() !== ''),
          },
        }),
      )
      // Setup logged us in as the admin, so the logo can be uploaded now.
      if (logo) {
        const r = await fetch('/api/v1/admin/org/logo', {
          method: 'PUT',
          body: logo,
          headers: {
            'Content-Type': 'application/octet-stream',
            'X-CSRF-Token': document.cookie.match(/(?:^|; )tgp_csrf=([^;]*)/)?.[1] ?? '',
          },
        })
        if (!r.ok) setLogoError((await r.json().catch(() => null))?.error?.message ?? 'Logo upload failed')
      }
      return res
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['meta'] })
      void qc.invalidateQueries({ queryKey: ['session'] })
    },
    onError: (e) => {
      if (e instanceof ApiRequestError && e.status === 403) setStep(0)
    },
  })

  if (submit.data) {
    return (
      <AuthCard title="You're all set" subtitle={`${org.name} is ready.`} wide>
        <div className="flex flex-col gap-4">
          {logoError && <Alert tone="warning">The logo wasn't saved: {logoError}. You can upload it later under Admin → Organization.</Alert>}
          {submit.data.users.length > 0 && (
            <>
              <Alert tone="warning">
                Copy these temporary passwords now: they won't be shown again. Each person sets their own password at first login. They expire in 72 hours.
              </Alert>
              <div className="flex flex-col gap-3">
                {submit.data.users.map((u) => (
                  <div key={u.user.id}>
                    <div className="mb-1 flex items-center gap-2">
                      <span className="font-medium">{u.user.username}</span>
                      <Badge>{u.user.role}</Badge>
                    </div>
                    <SecretValue value={u.temp_password} label={`Temporary password for ${u.user.username}`} />
                  </div>
                ))}
              </div>
            </>
          )}
          <Button variant="primary" className="h-8" onClick={() => void navigate({ to: '/' })}>
            Continue to {org.name}
          </Button>
        </div>
      </AuthCard>
    )
  }

  const adminValid = admin.username.trim() !== '' && admin.password.length >= 10 && admin.password === admin.confirm
  const canNext = [token.trim() !== '', adminValid, org.name.trim() !== '', true, true][step]

  return (
    <AuthCard title="Set up tgp" subtitle="Create the first admin and configure your team's server." wide>
      <ol className="mb-5 flex flex-wrap gap-x-4 gap-y-1 text-sm">
        {steps.map((s, i) => (
          <li key={s} className={cx('flex items-center gap-1.5', i === step ? 'text-fg' : 'text-fg-faint')}>
            <span
              className={cx(
                'flex size-4 items-center justify-center rounded-full text-2xs',
                i < step ? 'bg-success/20 text-success' : i === step ? 'bg-accent-solid text-accent-fg' : 'bg-bg-hover',
              )}
            >
              {i < step ? <Check size={10} /> : i + 1}
            </span>
            {s}
          </li>
        ))}
      </ol>

      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault()
          if (!canNext) return
          if (step < steps.length - 1) setStep(step + 1)
          else submit.mutate()
        }}
      >
        {step === 0 && (
          <>
            <p className="text-fg-muted">
              When tgp-server starts for the first time it prints a one-time <strong className="text-fg">setup token</strong> in its log. This
              proves you run the server.
            </p>
            <Field label="Setup token" hint="Restarting the server prints a new token.">
              {(id) => <Input id={id} value={token} onChange={(e) => setToken(e.target.value)} className="font-mono" autoFocus autoComplete="off" />}
            </Field>
          </>
        )}

        {step === 1 && (
          <>
            <div className="grid grid-cols-2 gap-3">
              <Field label="Username">
                {(id) => <Input id={id} value={admin.username} onChange={(e) => setAdmin({ ...admin, username: e.target.value.toLowerCase() })} autoComplete="username" autoFocus />}
              </Field>
              <Field label="Display name">
                {(id) => <Input id={id} value={admin.display_name} onChange={(e) => setAdmin({ ...admin, display_name: e.target.value })} autoComplete="name" />}
              </Field>
            </div>
            <Field label="Email" hint="Optional. Can also be used to log in.">
              {(id) => <Input id={id} type="email" value={admin.email} onChange={(e) => setAdmin({ ...admin, email: e.target.value })} autoComplete="email" />}
            </Field>
            <div className="grid grid-cols-2 gap-3">
              <Field label="Password" hint="At least 10 characters.">
                {(id) => <Input id={id} type="password" value={admin.password} onChange={(e) => setAdmin({ ...admin, password: e.target.value })} autoComplete="new-password" />}
              </Field>
              <Field label="Confirm password">
                {(id) => <Input id={id} type="password" value={admin.confirm} onChange={(e) => setAdmin({ ...admin, confirm: e.target.value })} autoComplete="new-password" />}
              </Field>
            </div>
            {admin.confirm !== '' && admin.password !== admin.confirm && <Alert tone="warning">Passwords don't match.</Alert>}
          </>
        )}

        {step === 2 && (
          <>
            <Field label="Organization name">
              {(id) => <Input id={id} value={org.name} onChange={(e) => setOrg({ ...org, name: e.target.value })} placeholder="BloxBlast" autoFocus />}
            </Field>
            <Field label="Description" hint="Optional.">
              {(id) => <Input id={id} value={org.description} onChange={(e) => setOrg({ ...org, description: e.target.value })} />}
            </Field>
            <Field label="Base URL" hint="The public address people use to reach this server. Used in clone URLs and webhooks.">
              {(id) => <Input id={id} value={org.base_url} onChange={(e) => setOrg({ ...org, base_url: e.target.value })} placeholder="https://git.example.com" />}
            </Field>
            <Field label="Logo" hint="Optional. PNG, JPEG, GIF or WebP, up to 512 KiB.">
              {(id) => (
                <div className="flex items-center gap-3">
                  {logoUrl && <img src={logoUrl} alt="" className="size-8 rounded object-cover" />}
                  <input
                    id={id}
                    type="file"
                    accept="image/png,image/jpeg,image/gif,image/webp"
                    onChange={(e) => setLogo(e.target.files?.[0] ?? null)}
                    className="text-sm text-fg-muted file:mr-2 file:h-7 file:rounded file:border file:border-border-strong file:bg-bg-elevated file:px-2.5 file:text-fg"
                  />
                </div>
              )}
            </Field>
          </>
        )}

        {step === 3 && (
          <>
            <p className="text-fg-muted">
              Optionally add your team now. Each person gets a temporary password to log in with and then chooses their own. You can add people
              later from the admin panel.
            </p>
            <div className="flex flex-col gap-2">
              {users.map((u, i) => (
                <div key={i} className="flex flex-col gap-2 rounded border border-border bg-bg p-3">
                  <div className="grid grid-cols-[1fr_1fr_auto] gap-2">
                    <Input
                      aria-label="Username"
                      placeholder="username"
                      value={u.username}
                      onChange={(e) => setUsers(users.map((x, j) => (j === i ? { ...x, username: e.target.value.toLowerCase() } : x)))}
                    />
                    <Select
                      aria-label="Role"
                      value={u.role}
                      onChange={(e) => setUsers(users.map((x, j) => (j === i ? { ...x, role: e.target.value as Role } : x)))}
                    >
                      <option value="maintainer">Maintainer</option>
                      <option value="tester">Tester</option>
                      <option value="admin">Admin</option>
                    </Select>
                    <Button variant="ghost" aria-label="Remove" onClick={() => setUsers(users.filter((_, j) => j !== i))}>
                      <Trash2 size={14} />
                    </Button>
                  </div>
                  <div className="grid grid-cols-2 gap-2">
                    <Input
                      aria-label="Display name"
                      placeholder="Display name (optional)"
                      value={u.display_name}
                      onChange={(e) => setUsers(users.map((x, j) => (j === i ? { ...x, display_name: e.target.value } : x)))}
                    />
                    <Input
                      aria-label="Email"
                      placeholder="Email (optional)"
                      value={u.email}
                      onChange={(e) => setUsers(users.map((x, j) => (j === i ? { ...x, email: e.target.value } : x)))}
                    />
                  </div>
                  {u.role !== 'admin' && (
                    <Checkbox
                      checked={u.all_repos}
                      onChange={(v) => setUsers(users.map((x, j) => (j === i ? { ...x, all_repos: v } : x)))}
                      label="Access to all repositories"
                      hint="Otherwise grant specific repositories after creating them."
                    />
                  )}
                </div>
              ))}
              <Button
                className="self-start"
                onClick={() => setUsers([...users, { username: '', display_name: '', email: '', role: 'maintainer', all_repos: true }])}
              >
                <Plus size={14} /> Add person
              </Button>
            </div>
          </>
        )}

        {step === 4 && (
          <dl className="grid grid-cols-[140px_1fr] gap-x-4 gap-y-2">
            <dt className="text-fg-muted">Admin</dt>
            <dd>
              {admin.username}
              {admin.email && <span className="text-fg-muted"> · {admin.email}</span>}
            </dd>
            <dt className="text-fg-muted">Organization</dt>
            <dd>{org.name}</dd>
            <dt className="text-fg-muted">Base URL</dt>
            <dd>{org.base_url || <span className="text-fg-faint">not set</span>}</dd>
            <dt className="text-fg-muted">Logo</dt>
            <dd>{logo ? logo.name : <span className="text-fg-faint">none</span>}</dd>
            <dt className="text-fg-muted">Team members</dt>
            <dd>
              {users.filter((u) => u.username).length === 0 ? (
                <span className="text-fg-faint">none yet</span>
              ) : (
                <div className="flex flex-wrap gap-1.5">
                  {users
                    .filter((u) => u.username)
                    .map((u) => (
                      <span key={u.username} className="inline-flex items-center gap-1">
                        {u.username} <Badge>{u.role}</Badge>
                      </span>
                    ))}
                </div>
              )}
            </dd>
          </dl>
        )}

        {submit.isError && <Alert>{errorMessage(submit.error)}</Alert>}

        <div className="mt-1 flex justify-between">
          <Button variant="ghost" onClick={() => setStep(step - 1)} disabled={step === 0}>
            Back
          </Button>
          <Button type="submit" variant="primary" disabled={!canNext} loading={submit.isPending}>
            {step === steps.length - 1 ? 'Complete setup' : step === 3 && users.length === 0 ? 'Skip' : 'Continue'}
          </Button>
        </div>
      </form>
    </AuthCard>
  )
}
