import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, Pencil, Plus, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { api, errorMessage, unwrap, type Secret } from '../../api/client'
import { orgSecretsQuery, repoSecretsQuery } from '../../api/queries'
import { Alert, Button, Dialog, EmptyState, Field, IconButton, Input, Section, SkeletonRows, Textarea, Time, useConfirm, useToast } from '../ui'

/** Upper-cases and keeps only characters valid in a secret name. */
export function normalizeSecretName(s: string): string {
  return s.toUpperCase().replace(/[^A-Z0-9_]/g, '_')
}

/** CI secrets for a repository, or (without repo) organisation-wide secrets. */
export function SecretsSection({ repo }: { repo?: string }) {
  const q = useQuery(repo ? repoSecretsQuery(repo) : orgSecretsQuery)
  const qc = useQueryClient()
  const [editing, setEditing] = useState<Secret | 'new' | null>(null)
  const confirm = useConfirm()
  const toast = useToast()
  const refresh = () => void qc.invalidateQueries({ queryKey: (repo ? repoSecretsQuery(repo) : orgSecretsQuery).queryKey })
  const del = useMutation({
    mutationFn: (name: string) =>
      repo ? unwrap(api.DELETE('/repos/{repo}/secrets/{name}', { params: { path: { repo, name } } })) : unwrap(api.DELETE('/admin/secrets/{name}', { params: { path: { name } } })),
    onSuccess: (_, name) => {
      toast.success(`Deleted ${name}`)
      refresh()
    },
  })
  return (
    <Section
      flush
      title={repo ? 'Secrets' : 'Organization secrets'}
      description={
        repo ? (
          <>
            Encrypted values workflows read as <code className="font-mono">{'${{ secrets.NAME }}'}</code>. Organization secrets with the same name are overridden. Values are never shown again.
          </>
        ) : (
          <>Available to workflows in every repository as <code className="font-mono">{'${{ secrets.NAME }}'}</code>. A repository secret with the same name wins.</>
        )
      }
      actions={
        <Button icon={<Plus size={13} />} onClick={() => setEditing('new')}>
          Add secret
        </Button>
      }
    >
      {q.isPending ? (
        <SkeletonRows rows={2} />
      ) : q.isError ? (
        <div className="p-4">
          <Alert>{errorMessage(q.error)}</Alert>
        </div>
      ) : q.data.length === 0 ? (
        <EmptyState size="inline" icon={<KeyRound size={18} />} title="No secrets yet">
          Add tokens and passwords your workflows need.
        </EmptyState>
      ) : (
        <ul aria-label={repo ? 'Repository secrets' : 'Organization secrets'}>
          {q.data.map((s) => (
            <li key={s.name} className="flex items-center gap-3 border-b border-border px-4 py-2.5 last:border-0">
              <span className="flex size-7 shrink-0 items-center justify-center rounded-md border border-border bg-bg-subtle text-fg-muted">
                <KeyRound size={13} />
              </span>
              <div className="min-w-0 flex-1 leading-tight">
                <div className="truncate font-mono font-medium">{s.name}</div>
                <div className="truncate text-sm text-fg-faint">
                  Updated <Time iso={s.updated_at} />
                  {s.updated_by && ` by ${s.updated_by.display_name}`}
                </div>
              </div>
              <IconButton label={`Update ${s.name}`} size="sm" onClick={() => setEditing(s)}>
                <Pencil size={13} />
              </IconButton>
              <IconButton
                label={`Delete ${s.name}`}
                size="sm"
                className="hover:bg-danger/10 hover:text-danger"
                loading={del.isPending && del.variables === s.name}
                onClick={async () => {
                  if (await confirm({ title: `Delete the secret ${s.name}?`, description: 'Workflows that use it will get an empty value.', confirmLabel: 'Delete secret' })) del.mutate(s.name)
                }}
              >
                <Trash2 size={13} />
              </IconButton>
            </li>
          ))}
        </ul>
      )}
      {del.isError && (
        <div className="p-4 pt-0">
          <Alert>{errorMessage(del.error)}</Alert>
        </div>
      )}
      {editing && <SecretDialog repo={repo} secret={editing === 'new' ? null : editing} onClose={() => setEditing(null)} onSaved={refresh} />}
    </Section>
  )
}

function SecretDialog({ repo, secret, onClose, onSaved }: { repo?: string; secret: Secret | null; onClose: () => void; onSaved: () => void }) {
  const toast = useToast()
  const [name, setName] = useState(secret?.name ?? '')
  const [value, setValue] = useState('')
  const save = useMutation({
    mutationFn: () =>
      repo
        ? unwrap(api.PUT('/repos/{repo}/secrets/{name}', { params: { path: { repo, name } }, body: { value } }))
        : unwrap(api.PUT('/admin/secrets/{name}', { params: { path: { name } }, body: { value } })),
    onSuccess: () => {
      toast.success(secret ? `Updated ${name}` : `Added ${name}`)
      onSaved()
      onClose()
    },
  })
  return (
    <Dialog
      open
      onClose={onClose}
      title={secret ? `Update ${secret.name}` : 'Add secret'}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" loading={save.isPending} disabled={!name || !value} onClick={() => save.mutate()}>
            {secret ? 'Update secret' : 'Add secret'}
          </Button>
        </>
      }
    >
      <Field label="Name" hint="Letters, digits and _; can't start with a digit, TGP_ or GITHUB_.">
        {(id) => <Input id={id} value={name} disabled={!!secret} onChange={(e) => setName(normalizeSecretName(e.target.value))} autoFocus={!secret} className="font-mono" placeholder="DEPLOY_TOKEN" />}
      </Field>
      <Field label="Value" hint={secret ? 'The current value is replaced; it is never shown.' : 'Stored encrypted; masked as *** in logs.'}>
        {(id) => <Textarea id={id} value={value} onChange={(e) => setValue(e.target.value)} autoFocus={!!secret} rows={4} className="font-mono" spellCheck={false} autoComplete="off" />}
      </Field>
      {save.isError && <Alert>{errorMessage(save.error)}</Alert>}
    </Dialog>
  )
}
