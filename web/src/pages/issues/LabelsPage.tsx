import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Pencil, Plus, RefreshCw, SearchX, Tag, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { api, errorMessage, unwrap, type Label } from '../../api/client'
import { labelsQuery, repoQuery } from '../../api/queries'
import { LabelChip } from '../../components/issues/meta'
import { SubHeader, Container } from '../../components/Page'
import { Alert, Button, cx, Dialog, EmptyState, ErrorState, Field, IconButton, Input, List, SearchInput, SkeletonRows, useConfirm, useToast } from '../../components/ui'
import { isHexColor, randomLabelColor } from '../../lib/issues'
import { canWrite as canWritePerm } from '../../lib/permissions'
import { useRepoName } from '../repos/RepoLayout'

const presets = ['d73a4a', 'e99695', 'f9d0c4', 'fbca04', 'fef2c0', '0e8a16', 'c2e0c6', 'a2eeef', '1d76db', 'bfdadc', '5319e7', 'd4c5f9', 'b60205', '006b75', 'cfd3d7', '000000']

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

function LabelDialog({ repo, label, onDone }: { repo: string; label?: Label; onDone: () => void }) {
  const invalidate = useInvalidateLabels(repo)
  const toast = useToast()
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
      toast.success(label ? 'Label updated' : 'Label created')
      onDone()
    },
  })
  const valid = name.trim() !== '' && isHexColor(color)
  return (
    <Dialog open onClose={onDone} title={label ? `Edit label` : 'New label'} width="max-w-lg">
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault()
          if (valid) save.mutate()
        }}
      >
        <div className="flex min-h-14 items-center justify-center rounded-lg border border-dashed border-border-strong bg-bg-subtle">
          <LabelChip label={{ name: name || 'label preview', color: isHexColor(color) ? hex : 'cfd3d7' }} />
        </div>
        <Field label="Name">{(id) => <Input id={id} value={name} onChange={(e) => setName(e.target.value)} autoFocus maxLength={50} placeholder="bug" />}</Field>
        <Field label="Description">{(id) => <Input id={id} value={description} onChange={(e) => setDescription(e.target.value)} maxLength={200} placeholder="Something isn't working" />}</Field>
        <Field label="Color" error={!isHexColor(color) ? 'Use a 6-digit hex color, like #d73a4a.' : undefined}>
          {(id) => (
            <div className="flex flex-col gap-2.5">
              <div className="flex flex-wrap gap-1.5" role="group" aria-label="Preset colors">
                {presets.map((p) => (
                  <button
                    key={p}
                    type="button"
                    onClick={() => setColor(p)}
                    aria-label={`#${p}`}
                    aria-pressed={hex.toLowerCase() === p}
                    className={cx('size-6 rounded-md ring-offset-2 ring-offset-surface-2 transition-transform hover:scale-110', hex.toLowerCase() === p && 'ring-2 ring-accent')}
                    style={{ backgroundColor: `#${p}` }}
                  />
                ))}
              </div>
              <div className="flex items-center gap-1.5">
                <IconButton label="Random color" variant="secondary" onClick={() => setColor(randomLabelColor())}>
                  <RefreshCw size={13} />
                </IconButton>
                <input
                  type="color"
                  value={isHexColor(color) ? `#${hex}` : '#cccccc'}
                  onChange={(e) => setColor(e.target.value.slice(1))}
                  aria-label="Pick color"
                  className="h-8 w-9 cursor-pointer rounded-md border border-border-strong bg-bg p-0.5"
                />
                <Input id={id} value={`#${hex}`} onChange={(e) => setColor(e.target.value.replace(/^#/, ''))} className="w-32 font-mono" aria-invalid={!isHexColor(color) || undefined} />
              </div>
            </div>
          )}
        </Field>
        {save.isError && <Alert>{errorMessage(save.error)}</Alert>}
        <div className="flex justify-end gap-2 pt-1">
          <Button variant="ghost" onClick={onDone}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" loading={save.isPending} disabled={!valid}>
            {label ? 'Save' : 'Create label'}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}

export function LabelsPage() {
  const repo = useRepoName()
  const r = useQuery(repoQuery(repo)).data!
  const labels = useQuery(labelsQuery(repo))
  const invalidate = useInvalidateLabels(repo)
  const confirm = useConfirm()
  const toast = useToast()
  const canWrite = canWritePerm(r.permission)
  const [editing, setEditing] = useState<Label | 'new' | null>(null)
  const [filter, setFilter] = useState('')
  const del = useMutation({
    mutationFn: (id: number) => unwrap(api.DELETE('/repos/{repo}/labels/{id}', { params: { path: { repo, id } } })),
    onSuccess: () => {
      toast.success('Label deleted')
      invalidate()
    },
  })
  const shown = useMemo(() => {
    const f = filter.trim().toLowerCase()
    return (labels.data ?? []).filter((l) => !f || l.name.toLowerCase().includes(f) || l.description.toLowerCase().includes(f))
  }, [labels.data, filter])
  return (
    <Container width="wide" className="flex flex-col">
      <SubHeader
        back={{ to: '/$repo/issues', params: { repo }, label: 'Issues' }}
        title="Labels"
        description={labels.data ? `${labels.data.length} labels to categorize issues and pull requests.` : 'Categorize issues and pull requests.'}
        actions={
          canWrite && (
            <Button variant="primary" icon={<Plus size={14} />} onClick={() => setEditing('new')}>
              New label
            </Button>
          )
        }
      />
      {labels.data && labels.data.length > 0 && (
        <div className="mb-3">
          <SearchInput value={filter} onChange={setFilter} label="Filter labels" placeholder="Filter labels…" shortcut />
        </div>
      )}
      {del.isError && <Alert className="mb-3">{errorMessage(del.error)}</Alert>}
      {labels.isPending ? (
        <List>
          <SkeletonRows rows={5} />
        </List>
      ) : labels.isError ? (
        <ErrorState error={labels.error} className="p-0" />
      ) : labels.data.length === 0 ? (
        <EmptyState size="inline" icon={<Tag size={18} />} title="No labels yet" action={canWrite && <Button onClick={() => setEditing('new')}>Create a label</Button>}>
          Labels help you sort and filter issues and pull requests.
        </EmptyState>
      ) : shown.length === 0 ? (
        <EmptyState size="inline" icon={<SearchX size={18} />} title="No matching labels" />
      ) : (
        <List>
          {shown.map((l) => (
            <li key={l.id} className="grid grid-cols-[minmax(140px,220px)_minmax(0,1fr)_auto_auto] items-center gap-4 border-b border-border px-4 py-2.5 transition-colors last:border-0 hover:bg-bg-hover/60" data-testid="label-row">
              <span className="min-w-0">
                <LabelChip label={l} />
              </span>
              <span className="truncate text-fg-muted">{l.description || <span className="text-fg-faint">No description</span>}</span>
              <Link to="/$repo/issues" params={{ repo }} search={{ labels: l.name }} className="text-sm whitespace-nowrap text-fg-muted tabular-nums hover:text-accent">
                {l.open_issues ?? 0} open
              </Link>
              <span className="flex w-14 justify-end gap-0.5">
                {canWrite && (
                  <>
                    <IconButton label={`Edit ${l.name}`} size="sm" onClick={() => setEditing(l)}>
                      <Pencil size={13} />
                    </IconButton>
                    <IconButton
                      label={`Delete ${l.name}`}
                      size="sm"
                      className="hover:bg-danger/10 hover:text-danger"
                      onClick={async () => {
                        const ok = await confirm({
                          title: `Delete the label “${l.name}”?`,
                          description: "It's removed from every issue and pull request.",
                          confirmLabel: 'Delete label',
                        })
                        if (ok) del.mutate(l.id)
                      }}
                    >
                      <Trash2 size={13} />
                    </IconButton>
                  </>
                )}
              </span>
            </li>
          ))}
        </List>
      )}
      {editing && <LabelDialog key={editing === 'new' ? 'new' : editing.id} repo={repo} label={editing === 'new' ? undefined : editing} onDone={() => setEditing(null)} />}
    </Container>
  )
}
