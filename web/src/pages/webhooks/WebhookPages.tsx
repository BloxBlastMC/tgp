import { useQuery } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { Lock } from 'lucide-react'
import type { ReactNode } from 'react'
import { repoQuery } from '../../api/queries'
import { Container, SubHeader } from '../../components/Page'
import { RepoSettingsLayout } from '../../components/repos/RepoSettingsNav'
import { EmptyState, Section } from '../../components/ui'
import { canWrite } from '../../lib/permissions'
import { AddWebhookLink, WebhookDetail, WebhookForm, WebhookList } from '../../components/webhooks/Webhooks'
import { useRepoName } from '../repos/RepoLayout'

function RepoSettingsShell({ children }: { children: ReactNode }) {
  const name = useRepoName()
  const repo = useQuery(repoQuery(name)).data!
  if (!canWrite(repo.permission))
    return (
      <EmptyState icon={<Lock size={20} />} title="No access to webhooks">
        You need write access to manage webhooks.
      </EmptyState>
    )
  return <RepoSettingsLayout>{children}</RepoSettingsLayout>
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
    <Container width="default">
      <SubHeader title="Webhooks" as="h2" description="Organization webhooks get events from every repository." actions={<AddWebhookLink scope={org} />} />
      <WebhookList scope={org} bare />
    </Container>
  )
}

export function AdminNewWebhookPage() {
  return (
    <Container width="default">
      <SubHeader back={{ to: '/admin/webhooks', label: 'Webhooks' }} title="Add organization webhook" as="h2" description="Gets events from every repository." />
      <Section title="Webhook">
        <WebhookForm scope={org} />
      </Section>
    </Container>
  )
}

export function AdminWebhookPage() {
  const id = useHookId()
  return (
    <Container width="default">
      <WebhookDetail key={id} scope={org} id={id} />
    </Container>
  )
}
