import { useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Archive, FolderGit2 } from 'lucide-react'
import { reposQuery } from '../../api/queries'
import { canTriage, canWrite } from '../../lib/permissions'
import { Dialog, SelectList, type SelectItem } from '../ui'

export type RepoIntent = 'issue' | 'pull'

const titles: Record<RepoIntent, string> = { issue: 'New issue', pull: 'New pull request' }

/** Pick a repository, then go to its new issue / compare page. */
export function RepoChooser({ intent, onClose }: { intent: RepoIntent | null; onClose: () => void }) {
  const repos = useQuery({ ...reposQuery, enabled: intent !== null })
  const navigate = useNavigate()
  const allowed = (repos.data ?? []).filter((r) => !r.archived && !r.empty && (intent === 'pull' ? canWrite(r.permission) : canTriage(r.permission)))
  const items: SelectItem[] = allowed.map((r) => ({
    key: r.name,
    label: r.name,
    search: r.description,
    render: (
      <span className="flex min-w-0 flex-1 items-center gap-2">
        {r.archived ? <Archive size={14} className="text-fg-faint" /> : <FolderGit2 size={14} className="text-fg-faint" />}
        <span className="truncate font-medium">{r.name}</span>
        {r.description && <span className="truncate text-sm text-fg-faint">{r.description}</span>}
      </span>
    ),
  }))
  return (
    <Dialog open={intent !== null} onClose={onClose} title={intent ? titles[intent] : ''} description="Choose a repository." width="max-w-lg">
      <div className="-mx-5 -mb-5 border-t border-border">
        {intent && (
          <SelectList
            items={items}
            selected={[]}
            label="Repositories"
            placeholder="Search repositories…"
            emptyText={repos.isPending ? 'Loading…' : 'No repositories you can do that in.'}
            onPick={(repo) => {
              onClose()
              if (intent === 'issue') void navigate({ to: '/$repo/issues/new', params: { repo } })
              else void navigate({ to: '/$repo/compare/$', params: { repo, _splat: '' } })
            }}
          />
        )}
      </div>
    </Dialog>
  )
}
