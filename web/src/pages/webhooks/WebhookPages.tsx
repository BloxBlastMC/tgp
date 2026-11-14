import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { repoQuery } from '../../api/queries'
import { Alert, Section } from '../../components/ui'
import { HookLink, WebhookDetail, WebhookForm, WebhookList } from '../../components/webhooks/Webhooks'
import { useRepoName } from '../repos/RepoLayout'

const tabClass =
  'rounded px-2 py-1 text-[13px] text-fg-muted hover:bg-bg-hover hover:text-fg data-[status=active]:bg-bg-hover data-[status=active]:text-fg'

/** Sub-navigation of a repository's settings. */
export function RepoSettingsNav() {
  const repo = useRepoName()
  return (
    <nav className="flex gap-1" aria-label="Settings sections">
      <Link to="/$repo/settings" params={{ repo }} activeOptions={{ exact: true }} className={tabClass}>
        General
      </Link>
      <Link to="/$repo/settings/webhooks" params={{ repo }} className={tabClass}>
        Webhooks
      </Link>
    </nav>
  )
}

function RepoSettingsShell({ children }: { children: ReactNode }) {
  const name = useRepoName()
  const repo = useQuery(repoQuery(name)).data!
  if (repo.permission !== 'write' && repo.permission !== 'admin')
    return (
      <div className="p-4">
        <Alert>You need write access to manage webhooks.</Alert>
      </div>
    )
  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-5 p-6">
      <RepoSettingsNav />
      {children}
    </div>
  )
}

function useHookId(): number {
  return Number((useParams({ strict: false }) as { id: string }).id)
}

export function RepoWebhooksPage() {
  const repo = useRepoName()
  return (
    <RepoSettingsShell>
      <WebhookList scope={{ kind: 'repo', repo }} />
    </RepoSettingsShell>
  )
}

export function RepoNewWebhookPage() {
  const repo = useRepoName()
  const scope = { kind: 'repo', repo } as const
  return (
    <RepoSettingsShell>
      <Section title="Add webhook" description={<>We'll send a <code className="font-mono">ping</code> right away so you can check the receiver.</>}>
        <WebhookForm scope={scope} />
      </Section>
    </RepoSettingsShell>
  )
}

export function RepoWebhookPage() {
  const repo = useRepoName()
  const id = useHookId()
  return (
    <RepoSettingsShell>
      <WebhookDetail key={id} scope={{ kind: 'repo', repo }} id={id} />
    </RepoSettingsShell>
  )
}

const org = { kind: 'org' } as const

export function AdminWebhooksPage() {
  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-5 p-6">
      <WebhookList scope={org} />
    </div>
  )
}

export function AdminNewWebhookPage() {
  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-5 p-6">
      <HookLink scope={org} className="text-[12px] text-fg-muted hover:text-fg hover:underline">
        ← Webhooks
      </HookLink>
      <Section title="Add organisation webhook" description="Gets events from every repository.">
        <WebhookForm scope={org} />
      </Section>
    </div>
  )
}

export function AdminWebhookPage() {
  const id = useHookId()
  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-5 p-6">
      <WebhookDetail key={id} scope={org} id={id} />
    </div>
  )
}
