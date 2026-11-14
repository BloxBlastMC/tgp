import { useQuery } from '@tanstack/react-query'
import { AlertTriangle, KeyRound, Settings2, Shield, Users, Webhook } from 'lucide-react'
import type { ReactNode } from 'react'
import { repoQuery } from '../../api/queries'
import { useRepoName } from '../../pages/repos/RepoLayout'
import { SideNav, TwoColumn } from '../layout/Page'

/** Section navigation of a repository's settings. */
export function RepoSettingsNav() {
  const repo = useRepoName()
  const r = useQuery(repoQuery(repo)).data
  const params = { repo }
  return (
    <SideNav
      label="Settings sections"
      groups={[
        {
          items: [
            { to: '/$repo/settings', params, label: 'General', icon: Settings2 },
            { to: '/$repo/settings', params, label: 'Branch protection', icon: Shield, hash: 'protection' },
            { to: '/$repo/settings', params, label: 'Secrets', icon: KeyRound, hash: 'secrets' },
            { to: '/$repo/settings', params, label: 'Access', icon: Users, hash: 'access' },
            { to: '/$repo/settings/webhooks', params, label: 'Webhooks', icon: Webhook, exact: false },
            ...(r?.permission === 'admin' ? [{ to: '/$repo/settings', params, label: 'Danger zone', icon: AlertTriangle, hash: 'danger' }] : []),
          ],
        },
      ]}
    />
  )
}

/** Settings layout: section nav on the left. */
export function RepoSettingsLayout({ children }: { children: ReactNode }) {
  return (
    <TwoColumn nav={<RepoSettingsNav />} className="max-w-5xl">
      <div className="flex flex-col gap-6">{children}</div>
    </TwoColumn>
  )
}
