import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ArrowLeft, Plus, RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { api, errorMessage, unwrap, type Label } from '../../api/client'
import { labelsQuery, repoQuery } from '../../api/queries'
import { LabelChip } from '../../components/issues/meta'
import { Alert, Button, Field, Input, Spinner } from '../../components/ui'
import { isHexColor, randomLabelColor } from '../../lib/issues'
import { useRepoName } from '../repos/RepoLayout'

function useInvalidateLabels(repo: string) {
  const qc = useQueryClient()
  return () => {
    void qc.invalidateQueries({ queryKey: ['repo', repo, 'labels'] })
    void qc.invalidateQueries({ queryKey: ['repo', repo, 'issues'] })
    void qc.invalidateQueries({ queryKey: ['repo', repo, 'issue'] })
    void qc.invalidateQueries({ queryKey: ['repo', repo, 'pull'] })
    void qc.invalidateQueries({ queryKey: ['repo', repo, 'pulls'] })
  }
}

function LabelForm({ repo, label, onDone }: { repo: string; label?: Label; onDone: () => void }) {
  const invalidate = useInvalidateLabels(repo)
  const [name, setName] = useState(label?.name ?? '')
  const [color, setColor] = useState(label?.color ?? randomLabelColor())
  const [description, setDescription] = useState(label?.description ?? '')
  const hex = color.replace(/^#/, '')
  const save = useMutation({
    mutationFn: () =>
      label
        ? unwrap(api.PATCH('/repos/{repo}/labels/{id}', { params: { path: { repo, id: label.id } }, body: { name, color: hex, description } }))
        : unwrap(api.POST('/repos/{repo}/labels', { params: { path: { repo } }, body: { name, color: hex, description } })),
    onSuccess: () => {
      invalidate()
      onDone()
    },
  })
  const valid = name.trim() !== '' && isHexColor(color)
  return (
    <form
      className="flex flex-col gap-3 border-b border-border bg-bg-subtle px-4 py-3"
      onSubmit={(e) => {
        e.preventDefault()
        if (valid) save.mutate()
      }}
    >
      <div className="flex items-center gap-2">
        <span className="text-sm text-fg-muted">Preview</span>
        <LabelChip label={{ name: name || 'label name', color: isHexColor(color) ? hex : 'cfd3d7' }} />
      </div>
      <div className="grid gap-3 md:grid-cols-[200px_minmax(0,1fr)_200px_auto] md:items-end">
        <Field label="Name">{(id) => <Input id={id} value={name} onChange={(e) => setName(e.target.value)} autoFocus maxLength={50} />}</Field>
        <Field label="Description">{(id) => <Input id={id} value={description} onChange={(e) => setDescription(e.target.value)} maxLength={200} />}</Field>
        <Field label="Color">
          {(id) => (
            <div className="flex items-center gap-1">
              <Button aria-label="Random color" title="Random color" className="px-1.5" style={{ backgroundColor: isHexColor(color) ? `#${hex}` : undefined }} onClick={() => setColor(randomLabelColor())}>
                <RefreshCw size={13} className="mix-blend-difference" />
              </Button>
              <input type="color" value={isHexColor(color) ? `#${hex}` : '#cccccc'} onChange={(e) => setColor(e.target.value.slice(1))} aria-label="Pick color" className="h-7 w-8 cursor-pointer rounded border border-border-strong bg-bg" />
              <Input id={id} value={`#${hex}`} onChange={(e) => setColor(e.target.value.replace(/^#/, ''))} className="h-7 font-mono" />
            </div>
          )}
        </Field>
        <div className="flex gap-2">
          <Button variant="ghost" onClick={onDone}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" loading={save.isPending} disabled={!valid}>
            {label ? 'Save' : 'Create label'}
          </Button>
        </div>
      </div>
      {save.isError && <Alert>{errorMessage(save.error)}</Alert>}
    </form>
  )
}

export function LabelsPage() {
  const repo = useRepoName()
  const r = useQuery(repoQuery(repo)).data!
  const labels = useQuery(labelsQuery(repo))
  const invalidate = useInvalidateLabels(repo)
  const canWrite = r.permission === 'write' || r.permission === 'admin'
  const [editing, setEditing] = useState<number | 'new' | null>(null)
  const del = useMutation({
    mutationFn: (id: number) => unwrap(api.DELETE('/repos/{repo}/labels/{id}', { params: { path: { repo, id } } })),
    onSuccess: invalidate,
  })
  return (
    <div className="flex flex-col">
      <div className="flex items-center gap-3 border-b border-border px-4 py-2">
        <Link to="/$repo/issues" params={{ repo }} className="inline-flex items-center gap-1 text-sm text-fg-muted hover:text-fg">
          <ArrowLeft size={12} /> Issues
        </Link>
        <h2 className="font-semibold">Labels</h2>
        {labels.data && <span className="text-sm text-fg-faint">{labels.data.length}</span>}
        <div className="flex-1" />
        {canWrite && editing !== 'new' && (
          <Button variant="primary" onClick={() => setEditing('new')}>
            <Plus size={14} /> New label
          </Button>
        )}
      </div>
      {editing === 'new' && <LabelForm repo={repo} onDone={() => setEditing(null)} />}
      {del.isError && (
        <div className="p-4">
          <Alert>{errorMessage(del.error)}</Alert>
        </div>
      )}
      {labels.isPending ? (
        <Spinner />
      ) : labels.isError ? (
        <div className="p-4">
          <Alert>{errorMessage(labels.error)}</Alert>
        </div>
      ) : (
        <ul>
          {labels.data.map((l) =>
            editing === l.id ? (
              <li key={l.id}>
                <LabelForm repo={repo} label={l} onDone={() => setEditing(null)} />
              </li>
            ) : (
              <li key={l.id} className="grid grid-cols-[200px_minmax(0,1fr)_120px_auto] items-center gap-3 border-b border-border px-4 py-2 hover:bg-bg-subtle" data-testid="label-row">
                <span>
                  <LabelChip label={l} />
                </span>
                <span className="truncate text-fg-muted">{l.description}</span>
                <Link to="/$repo/issues" params={{ repo }} search={{ labels: l.name }} className="text-sm text-fg-muted hover:text-accent">
                  {l.open_issues ?? 0} open
                </Link>
                <span className="flex gap-1">
                  {canWrite && (
                    <>
                      <Button variant="ghost" onClick={() => setEditing(l.id)}>
                        Edit
                      </Button>
                      <Button
                        variant="ghost"
                        className="hover:text-danger"
                        onClick={() => window.confirm(`Delete the label “${l.name}”? It's removed from every issue and pull request.`) && del.mutate(l.id)}
                      >
                        Delete
                      </Button>
                    </>
                  )}
                </span>
              </li>
            ),
          )}
        </ul>
      )}
    </div>
  )
}
