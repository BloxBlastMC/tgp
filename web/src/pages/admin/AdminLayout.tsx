import { Link, Outlet } from '@tanstack/react-router'
import { Page } from '../../components/Page'

const tabs = [
  { to: '/admin/users', label: 'Users' },
  { to: '/admin/org', label: 'Organization' },
  { to: '/admin/runners', label: 'Runners' },
  { to: '/admin/ci', label: 'CI settings' },
  { to: '/admin/webhooks', label: 'Webhooks' },
  { to: '/admin/audit', label: 'Audit log' },
] as const

export function AdminLayout() {
  return (
    <Page
      title={
        <div className="flex items-center gap-4">
          <span>Admin</span>
          <nav className="flex gap-1">
            {tabs.map((t) => (
              <Link
                key={t.to}
                to={t.to}
                className="rounded px-2 py-0.5 text-[13px] font-normal text-fg-muted hover:bg-bg-hover hover:text-fg data-[status=active]:bg-bg-hover data-[status=active]:text-fg"
              >
                {t.label}
              </Link>
            ))}
          </nav>
        </div>
      }
    >
      <Outlet />
    </Page>
  )
}
