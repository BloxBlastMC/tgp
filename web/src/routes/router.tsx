import { type QueryClient, useQuery } from '@tanstack/react-query'
import { createRootRouteWithContext, createRoute, createRouter, Outlet, redirect } from '@tanstack/react-router'
import { useState } from 'react'
import { metaQuery, sessionQuery } from '../api/queries'
import { CommandPalette } from '../components/CommandPalette'
import { EmptyState, Page } from '../components/Page'
import { Sidebar } from '../components/Sidebar'
import { useCommands } from '../lib/commands'
import { useTheme } from '../lib/theme'
import { AdminAuditPage } from '../pages/admin/AdminAuditPage'
import { AdminLayout } from '../pages/admin/AdminLayout'
import { AdminOrgPage } from '../pages/admin/AdminOrgPage'
import { AdminUsersPage } from '../pages/admin/AdminUsersPage'
import { ChangePasswordPage } from '../pages/ChangePasswordPage'
import { LoginPage } from '../pages/LoginPage'
import { SettingsPage } from '../pages/settings/SettingsPage'
import { SetupPage } from '../pages/SetupPage'
import { BlamePage } from '../pages/repos/BlamePage'
import { BlobPage } from '../pages/repos/BlobPage'
import { BranchesPage } from '../pages/repos/BranchesPage'
import { CodePage } from '../pages/repos/CodePage'
import { CommitPage } from '../pages/repos/CommitPage'
import { CommitsPage } from '../pages/repos/CommitsPage'
import { RepoLayout } from '../pages/repos/RepoLayout'
import { RepoSettingsPage } from '../pages/repos/RepoSettingsPage'
import { ReposPage } from '../pages/repos/ReposPage'
import { validateCommits, validateRefPath } from '../pages/repos/search'

type RouterContext = { queryClient: QueryClient }

function AppLayout() {
  const session = useQuery(sessionQuery)
  const [theme, toggleTheme] = useTheme()
  const [paletteOpen, setPaletteOpen] = useState(false)
  const commands = useCommands(session.data, toggleTheme)
  return (
    <div className="flex h-full">
      <Sidebar theme={theme} toggleTheme={toggleTheme} openPalette={() => setPaletteOpen(true)} />
      <main className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
        <Outlet />
      </main>
      <CommandPalette commands={commands} open={paletteOpen} setOpen={setPaletteOpen} />
    </div>
  )
}

function placeholder(title: string, body: string) {
  return function Placeholder() {
    return (
      <Page title={title}>
        <EmptyState title={`No ${title.toLowerCase()} yet`}>{body}</EmptyState>
      </Page>
    )
  }
}

const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: Outlet,
  notFoundComponent: () => (
    <Page title="Not found">
      <EmptyState title="Page not found">That page doesn't exist.</EmptyState>
    </Page>
  ),
})

const setupRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/setup',
  beforeLoad: async ({ context }) => {
    const meta = await context.queryClient.ensureQueryData(metaQuery)
    if (!meta.setup_required) throw redirect({ to: '/' })
  },
  component: SetupPage,
})

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/login',
  validateSearch: (s: Record<string, unknown>): { next?: string } => (typeof s.next === 'string' ? { next: s.next } : {}),
  beforeLoad: async ({ context }) => {
    const meta = await context.queryClient.ensureQueryData(metaQuery)
    if (meta.setup_required) throw redirect({ to: '/setup' })
    const session = await context.queryClient.ensureQueryData(sessionQuery)
    if (session) throw redirect({ to: session.must_change_password ? '/change-password' : '/' })
  },
  component: LoginPage,
})

const changePasswordRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/change-password',
  beforeLoad: async ({ context }) => {
    const session = await context.queryClient.ensureQueryData(sessionQuery)
    if (!session) throw redirect({ to: '/login' })
  },
  component: ChangePasswordPage,
})

/** Everything inside the app shell requires a fully set-up, logged-in user. */
const appRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: 'app',
  beforeLoad: async ({ context, location }) => {
    const meta = await context.queryClient.ensureQueryData(metaQuery)
    if (meta.setup_required) throw redirect({ to: '/setup' })
    const session = await context.queryClient.ensureQueryData(sessionQuery)
    if (!session) throw redirect({ to: '/login', search: location.href === '/' ? {} : { next: location.href } })
    if (session.must_change_password) throw redirect({ to: '/change-password' })
    return { session }
  },
  component: AppLayout,
})

const adminRoute = createRoute({
  getParentRoute: () => appRoute,
  path: '/admin',
  beforeLoad: ({ context }) => {
    if (context.session.user.role !== 'admin') throw redirect({ to: '/' })
  },
  component: AdminLayout,
})

const adminIndexRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: '/',
  beforeLoad: () => {
    throw redirect({ to: '/admin/users' })
  },
})

const repoRoute = createRoute({
  getParentRoute: () => appRoute,
  path: '/$repo',
  component: RepoLayout,
})

const repoRoutes = [
  createRoute({ getParentRoute: () => repoRoute, path: '/', validateSearch: validateRefPath, component: CodePage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/blob', validateSearch: validateRefPath, component: BlobPage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/blame', validateSearch: validateRefPath, component: BlamePage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/commits', validateSearch: validateCommits, component: CommitsPage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/commit/$sha', component: CommitPage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/branches', component: BranchesPage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/settings', component: RepoSettingsPage }),
]

const routeTree = rootRoute.addChildren([
  setupRoute,
  loginRoute,
  changePasswordRoute,
  appRoute.addChildren([
    createRoute({ getParentRoute: () => appRoute, path: '/', component: ReposPage }),
    repoRoute.addChildren(repoRoutes),
    createRoute({ getParentRoute: () => appRoute, path: '/pulls', component: placeholder('Pull requests', 'Pull requests across all repositories you can access.') }),
    createRoute({ getParentRoute: () => appRoute, path: '/issues', component: placeholder('Issues', 'Issues across all repositories you can access.') }),
    createRoute({ getParentRoute: () => appRoute, path: '/ci', component: placeholder('CI', 'Workflow runs across all repositories.') }),
    createRoute({ getParentRoute: () => appRoute, path: '/settings', component: SettingsPage }),
    adminRoute.addChildren([
      adminIndexRoute,
      createRoute({ getParentRoute: () => adminRoute, path: '/users', component: AdminUsersPage }),
      createRoute({ getParentRoute: () => adminRoute, path: '/org', component: AdminOrgPage }),
      createRoute({ getParentRoute: () => adminRoute, path: '/audit', component: AdminAuditPage }),
    ]),
  ]),
])

export function makeRouter(queryClient: QueryClient) {
  return createRouter({
    routeTree,
    context: { queryClient },
    defaultPreload: 'intent',
    defaultPreloadStaleTime: 0,
  })
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof makeRouter>
  }
}
