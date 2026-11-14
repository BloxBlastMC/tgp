import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { api, errorMessage, unwrap } from '../../api/client'
import { reposQuery } from '../../api/queries'
import { Alert, Button, Checkbox, Dialog, Field, Input, useToast } from '../ui'

const NAME_RE = /^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$/

/** Admin-only: create a repository, then open it. */
export function CreateRepoDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const toast = useToast()
  const blank = { name: '', description: '', default_branch: 'main', auto_init: true }
  const [f, setF] = useState(blank)
  const [touched, setTouched] = useState(false)
  const create = useMutation({
    mutationFn: () => unwrap(api.POST('/repos', { body: f })),
    onSuccess: (r) => {
      void qc.invalidateQueries({ queryKey: reposQuery.queryKey })
      setF(blank)
      setTouched(false)
      onClose()
      toast.success(`Created ${r.name}`)
      void navigate({ to: '/$repo', params: { repo: r.name } })
    },
  })
  const nameError = touched && f.name.trim() && !NAME_RE.test(f.name.trim()) ? "Use letters, digits, '.', '-' and '_', starting with a letter or digit." : null
  const close = () => {
    create.reset()
    setTouched(false)
    onClose()
  }
  return (
    <Dialog open={open} onClose={close} title="New repository" description="A git repository with issues, pull requests, CI and releases." width="max-w-lg">
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault()
          create.mutate()
        }}
      >
        <Field label="Name" hint="Letters, digits, '.', '-' and '_'." error={nameError} required>
          {(id) => (
            <Input
              id={id}
              value={f.name}
              onChange={(e) => setF({ ...f, name: e.target.value })}
              onBlur={() => setTouched(true)}
              placeholder="minigames"
              autoFocus
              required
              aria-invalid={!!nameError || undefined}
              autoComplete="off"
              spellCheck={false}
            />
          )}
        </Field>
        <Field label="Description">{(id) => <Input id={id} value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} placeholder="What's in this repository?" />}</Field>
        <Field label="Default branch">
          {(id) => <Input id={id} value={f.default_branch} onChange={(e) => setF({ ...f, default_branch: e.target.value })} className="font-mono" spellCheck={false} />}
        </Field>
        <div className="rounded-lg border border-border bg-bg-subtle px-3 py-2.5">
          <Checkbox
            checked={f.auto_init}
            onChange={(auto_init) => setF({ ...f, auto_init })}
            label="Initialize with a README"
            hint="Leave unchecked to push an existing repository."
          />
        </div>
        <p className="text-sm text-fg-faint">Maintainers and testers with access to all repositories see it immediately. Grant others access from Admin → Users.</p>
        {create.isError && <Alert>{errorMessage(create.error)}</Alert>}
        <div className="flex justify-end gap-2 pt-1">
          <Button variant="ghost" onClick={close}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" loading={create.isPending} disabled={!f.name.trim() || !!nameError}>
            Create repository
          </Button>
        </div>
      </form>
    </Dialog>
  )
}
