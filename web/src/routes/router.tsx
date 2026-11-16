import { type QueryClient, useQuery } from '@tanstack/react-query'
import { createRootRouteWithContext, createRoute, createRouter, Outlet, redirect } from '@tanstack/react-router'
import { FileQuestion, Menu as MenuIcon } from 'lucide-react'
import { useCallback, useState } from 'react'
import { metaQuery, sessionQuery } from '../api/queries'
import { CommandPalette, shortcutGroups } from '../components/CommandPalette'
import { EmptyState, Page } from '../components/Page'
import { Sidebar } from '../components/Sidebar'
import { ButtonLink, Dialog, IconButton, Kbd } from '../components/ui'
import { modKey, useCommands } from '../lib/commands'
import { HomePage } from '../pages/home/HomePage'
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
import { RepoAccessPage, RepoDangerPage, RepoProtectionPage, RepoSecretsPage, RepoSettingsPage } from '../pages/repos/RepoSettingsPage'
import { ReposPage } from '../pages/repos/ReposPage'
import { validateCommits, validateRefPath } from '../pages/repos/search'
import { ComparePage } from '../pages/pulls/ComparePage'
import { GlobalPullsPage, validateGlobalPulls } from '../pages/pulls/GlobalPullsPage'
import { validatePullList } from '../pages/pulls/PullList'
import { PullPage, validatePull } from '../pages/pulls/PullPage'
import { RepoPullsPage } from '../pages/pulls/RepoPullsPage'
import { GlobalIssuesPage, validateGlobalIssues } from '../pages/issues/GlobalIssuesPage'
import { IssuePage } from '../pages/issues/IssuePage'
import { LabelsPage } from '../pages/issues/LabelsPage'
import { MilestonesPage, validateMilestones } from '../pages/issues/MilestonesPage'
import { NewIssuePage } from '../pages/issues/NewIssuePage'
import { RepoIssuesPage } from '../pages/issues/RepoIssuesPage'
import { validateIssueList } from '../lib/issues'
import { validateRunList, validateRunPage } from '../lib/ci'
import { RepoRunsPage } from '../pages/ci/RepoRunsPage'
import { RunPage } from '../pages/ci/RunPage'
import { GlobalRunsPage, validateGlobalRuns } from '../pages/ci/GlobalRunsPage'
import { AdminRunnersPage } from '../pages/admin/AdminRunnersPage'
import { AdminRegistryPage } from '../pages/admin/AdminRegistryPage'
import { PackageImagePage, validatePackageImage } from '../pages/packages/PackageImagePage'
import { PackagesPage } from '../pages/packages/PackagesPage'
import { AdminCIPage } from '../pages/admin/AdminCIPage'
import { AdminHealthPage } from '../pages/admin/AdminHealthPage'
import { CachesPage, validateCaches } from '../pages/ci/CachesPage'
import { ReleaseDetailPage, ReleasesPage, validateReleases } from '../pages/releases/ReleasesPage'
import { EditReleasePage, NewReleasePage } from '../pages/releases/ReleaseFormPage'
import {
  AdminNewWebhookPage,
  AdminWebhookPage,
  AdminWebhooksPage,
  RepoNewWebhookPage,
  RepoWebhookPage,
  RepoWebhooksPage,
} from '../pages/webhooks/WebhookPages'

type RouterContext = { queryClient: QueryClient }

function ShortcutsDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  return (
    <Dialog open={open} onClose={onClose} title="Keyboard shortcuts" width="max-w-lg">
      <div className="grid gap-5 sm:grid-cols-2">
        {shortcutGroups.map((g) => (
          <div key={g.title} className={g.title === 'Go to' ? 'sm:row-span-2' : ''}>
            <h3 className="mb-2 text-xs font-semibold tracking-wide text-fg-faint uppercase">{g.title}</h3>
            <ul className="flex flex-col gap-1.5">
              {g.items.map((it) => (
                <li key={it.label} className="flex items-center justify-between gap-3">
                  <span className="text-fg-muted">{it.label}</span>
                  <span className="flex shrink-0 gap-0.5">
                    {it.keys.map((k) => (
                      <Kbd key={k}>{k === 'Ctrl' ? modKey() : k}</Kbd>
                    ))}
                  </span>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>
    </Dialog>
  )
}

function NotFoundPage() {
  return (
    <Page title="Not found">
      <EmptyState
        icon={<FileQuestion size={20} />}
        title="Page not found"
        action={
          <ButtonLink to="/" variant="secondary">
            Go home
          </ButtonLink>
        }
      >
        That page doesn't exist or has moved.
      </EmptyState>
    </Page>
  )
}

/** Below 768px the sidebar is a drawer; this bar opens it. */
function MobileTopBar({ onOpen }: { onOpen: () => void }) {
  const meta = useQuery(metaQuery)
  return (
    <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border bg-bg-sidebar px-3 md:hidden">
      <IconButton label="Open navigation" onClick={onOpen}>
        <MenuIcon size={16} />
      </IconButton>
      <img src={meta.data?.has_logo ? '/api/v1/org/logo' : '/favicon.svg'} alt="" className="size-6 shrink-0 rounded-md object-cover" />
      <span className="truncate font-semibold tracking-tight">{meta.data?.org_name || 'tgp'}</span>
    </div>
  )
}

function AppLayout() {
  const session = useQuery(sessionQuery)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const closeDrawer = useCallback(() => setDrawerOpen(false), [])
  const [paletteOpen, setPaletteOpen] = useState(false)
  const [helpOpen, setHelpOpen] = useState(false)
  const openShortcuts = useCallback(() => setHelpOpen(true), [])
  const commands = useCommands(session.data, { openShortcuts })
  return (
    <div className="flex h-full">
      <Sidebar openPalette={() => setPaletteOpen(true)} openShortcuts={openShortcuts} mobileOpen={drawerOpen} onCloseMobile={closeDrawer} />
      <main className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden bg-bg">
        <MobileTopBar onOpen={() => setDrawerOpen(true)} />
        <Outlet />
      </main>
      <CommandPalette commands={commands} open={paletteOpen} setOpen={setPaletteOpen} onHelp={openShortcuts} />
      <ShortcutsDialog open={helpOpen} onClose={() => setHelpOpen(false)} />
    </div>
  )
}

const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: Outlet,
  notFoundComponent: NotFoundPage,
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
  // Signed-in users keep the sidebar on unknown pages.
  notFoundComponent: NotFoundPage,
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
  createRoute({ getParentRoute: () => repoRoute, path: '/issues', validateSearch: validateIssueList, component: RepoIssuesPage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/issues/new', component: NewIssuePage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/issues/$number', component: IssuePage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/labels', component: LabelsPage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/milestones', validateSearch: validateMilestones, component: MilestonesPage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/pulls', validateSearch: validatePullList, component: RepoPullsPage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/pulls/$number', validateSearch: validatePull, component: PullPage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/compare/$', component: ComparePage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/ci', validateSearch: validateRunList, component: RepoRunsPage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/ci/runs/$number', validateSearch: validateRunPage, component: RunPage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/ci/caches', validateSearch: validateCaches, component: CachesPage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/packages', component: PackagesPage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/packages/image', validateSearch: validatePackageImage, component: PackageImagePage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/releases', validateSearch: validateReleases, component: ReleasesPage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/releases/new', component: NewReleasePage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/releases/edit/$id', component: EditReleasePage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/releases/tag/$', component: ReleaseDetailPage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/settings', component: RepoSettingsPage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/settings/protection', component: RepoProtectionPage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/settings/secrets', component: RepoSecretsPage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/settings/access', component: RepoAccessPage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/settings/danger', component: RepoDangerPage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/settings/webhooks', component: RepoWebhooksPage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/settings/webhooks/new', component: RepoNewWebhookPage }),
  createRoute({ getParentRoute: () => repoRoute, path: '/settings/webhooks/$id', component: RepoWebhookPage }),
]

const routeTree = rootRoute.addChildren([
  setupRoute,
  loginRoute,
  changePasswordRoute,
  appRoute.addChildren([
    createRoute({ getParentRoute: () => appRoute, path: '/', component: HomePage }),
    createRoute({ getParentRoute: () => appRoute, path: '/repos', component: ReposPage }),
    repoRoute.addChildren(repoRoutes),
    createRoute({ getParentRoute: () => appRoute, path: '/pulls', validateSearch: validateGlobalPulls, component: GlobalPullsPage }),
    createRoute({ getParentRoute: () => appRoute, path: '/issues', validateSearch: validateGlobalIssues, component: GlobalIssuesPage }),
    createRoute({ getParentRoute: () => appRoute, path: '/ci', validateSearch: validateGlobalRuns, component: GlobalRunsPage }),
    createRoute({ getParentRoute: () => appRoute, path: '/settings', component: SettingsPage }),
    adminRoute.addChildren([
      adminIndexRoute,
      createRoute({ getParentRoute: () => adminRoute, path: '/users', component: AdminUsersPage }),
      createRoute({ getParentRoute: () => adminRoute, path: '/health', component: AdminHealthPage }),
      createRoute({ getParentRoute: () => adminRoute, path: '/org', component: AdminOrgPage }),
      createRoute({ getParentRoute: () => adminRoute, path: '/runners', component: AdminRunnersPage }),
      createRoute({ getParentRoute: () => adminRoute, path: '/ci', component: AdminCIPage }),
      createRoute({ getParentRoute: () => adminRoute, path: '/registry', component: AdminRegistryPage }),
      createRoute({ getParentRoute: () => adminRoute, path: '/webhooks', component: AdminWebhooksPage }),
      createRoute({ getParentRoute: () => adminRoute, path: '/webhooks/new', component: AdminNewWebhookPage }),
      createRoute({ getParentRoute: () => adminRoute, path: '/webhooks/$id', component: AdminWebhookPage }),
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
