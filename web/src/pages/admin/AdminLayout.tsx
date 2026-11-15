import { Outlet } from '@tanstack/react-router'
import { Activity, Boxes, Building2, Cpu, History, Settings2, Users, Webhook } from 'lucide-react'
import { Page, SideNav } from '../../components/Page'

export function AdminLayout() {
  return (
    <Page title="Admin" description="Organization-wide settings and operations" bodyClassName="flex flex-col xl:flex-row">
      <div className="w-full shrink-0 border-b border-border px-3 py-2 xl:w-56 xl:border-r xl:border-b-0 xl:py-5">
        <SideNav
          label="Admin sections"
          collapseBelow="xl"
          groups={[
            {
              title: 'People',
              items: [
                { to: '/admin/users', label: 'Users', icon: Users },
                { to: '/admin/audit', label: 'Audit log', icon: History },
              ],
            },
            {
              title: 'Platform',
              items: [
                { to: '/admin/org', label: 'Organization', icon: Building2 },
                { to: '/admin/health', label: 'Health', icon: Activity },
              ],
            },
            {
              title: 'CI',
              items: [
                { to: '/admin/runners', label: 'Runners', icon: Cpu },
                { to: '/admin/ci', label: 'CI settings', icon: Settings2 },
                { to: '/admin/registry', label: 'Container registry', icon: Boxes },
              ],
            },
            {
              title: 'Integrations',
              items: [{ to: '/admin/webhooks', label: 'Webhooks', icon: Webhook, exact: false }],
            },
          ]}
        />
      </div>
      <div className="min-h-0 min-w-0 flex-1 overflow-auto">
        <Outlet />
      </div>
    </Page>
  )
}
