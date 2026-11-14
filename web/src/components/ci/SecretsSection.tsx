import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, Plus } from 'lucide-react'
import { useState } from 'react'
import { api, errorMessage, unwrap, type Secret } from '../../api/client'
import { orgSecretsQuery, repoSecretsQuery } from '../../api/queries'
import { Alert, Button, Dialog, Field, Input, Section, Spinner, Textarea, Time } from '../ui'

/** Upper-cases and keeps only characters valid in a secret name. */
export function normalizeSecretName(s: string): string {
  return s.toUpperCase().replace(/[^A-Z0-9_]/g, '_')
}

/** CI secrets for a repository, or (without repo) organisation-wide secrets. */
export function SecretsSection({ repo }: { repo?: string }) {
  const q = useQuery(repo ? repoSecretsQuery(repo) : orgSecretsQuery)
  const qc = useQueryClient()
  const [editing, setEditing] = useState<Secret | 'new' | null>(null)
  const refresh = () => void qc.invalidateQueries({ queryKey: (repo ? repoSecretsQuery(repo) : orgSecretsQuery).queryKey })
  const del = useMutation({
    mutationFn: (name: string) =>
      repo ? unwrap(api.DELETE('/repos/{repo}/secrets/{name}', { params: { path: { repo, name } } })) : unwrap(api.DELETE('/admin/secrets/{name}', { params: { path: { name } } })),
    onSuccess: refresh,
  })
  return (
    <Section
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
        <Button onClick={() => setEditing('new')}>
          <Plus size={13} /> Add secret
        </Button>
      }
    >
      {q.isPending ? (
        <Spinner />
      ) : q.isError ? (
        <Alert>{errorMessage(q.error)}</Alert>
      ) : q.data.length === 0 ? (
        <p className="text-fg-faint">No secrets yet.</p>
      ) : (
        <ul className="-my-2 divide-y divide-border" aria-label={repo ? 'Repository secrets' : 'Organization secrets'}>
          {q.data.map((s) => (
            <li key={s.name} className="flex items-center gap-3 py-2">
              <KeyRound size={14} className="shrink-0 text-fg-faint" />
              <span className="font-mono font-medium">{s.name}</span>
              <span className="min-w-0 flex-1 truncate text-[12px] text-fg-faint">
                updated <Time iso={s.updated_at} />
                {s.updated_by && ` by ${s.updated_by.display_name}`}
              </span>
              <Button variant="ghost" onClick={() => setEditing(s)} aria-label={`Update ${s.name}`}>
                Update
              </Button>
              <Button variant="ghost" className="hover:text-danger" loading={del.isPending && del.variables === s.name} onClick={() => window.confirm(`Delete the secret ${s.name}? Workflows using it will get an empty value.`) && del.mutate(s.name)} aria-label={`Delete ${s.name}`}>
                Delete
              </Button>
            </li>
          ))}
        </ul>
      )}
      {del.isError && <Alert>{errorMessage(del.error)}</Alert>}
      {editing && <SecretDialog repo={repo} secret={editing === 'new' ? null : editing} onClose={() => setEditing(null)} onSaved={refresh} />}
    </Section>
  )
}

function SecretDialog({ repo, secret, onClose, onSaved }: { repo?: string; secret: Secret | null; onClose: () => void; onSaved: () => void }) {
  const [name, setName] = useState(secret?.name ?? '')
  const [value, setValue] = useState('')
  const save = useMutation({
    mutationFn: () =>
      repo
        ? unwrap(api.PUT('/repos/{repo}/secrets/{name}', { params: { path: { repo, name } }, body: { value } }))
        : unwrap(api.PUT('/admin/secrets/{name}', { params: { path: { name } }, body: { value } })),
    onSuccess: () => {
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
