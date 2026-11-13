import { createRootRoute, createRoute, createRouter, Outlet } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { Sidebar } from '../components/Sidebar'
import { EmptyState, Page } from '../components/Page'
import { metaQuery } from '../api/queries'

function RootLayout() {
  const meta = useQuery(metaQuery)
  return (
    <div className="flex h-full">
      <Sidebar />
      <main className="flex min-w-0 flex-1 flex-col">
        {meta.data?.setup_required && (
          <div className="border-b border-border bg-accent-subtle px-4 py-1.5 text-[12px]">
            This server hasn't been set up yet. First-start setup arrives in the next milestone.
          </div>
        )}
        {meta.isError && (
          <div className="border-b border-border bg-danger/15 px-4 py-1.5 text-[12px] text-danger">
            Can't reach the tgp API: {meta.error.message}
          </div>
        )}
        <Outlet />
      </main>
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

const rootRoute = createRootRoute({
  component: RootLayout,
  notFoundComponent: () => (
    <Page title="Not found">
      <EmptyState title="Page not found">That page doesn't exist.</EmptyState>
    </Page>
  ),
})

const routes = [
  createRoute({ getParentRoute: () => rootRoute, path: '/', component: placeholder('Repositories', 'Admins create repositories for the team.') }),
  createRoute({ getParentRoute: () => rootRoute, path: '/pulls', component: placeholder('Pull requests', 'Pull requests across all repositories you can access.') }),
  createRoute({ getParentRoute: () => rootRoute, path: '/issues', component: placeholder('Issues', 'Issues across all repositories you can access.') }),
  createRoute({ getParentRoute: () => rootRoute, path: '/ci', component: placeholder('CI', 'Workflow runs across all repositories.') }),
  createRoute({ getParentRoute: () => rootRoute, path: '/admin', component: placeholder('Admin', 'Users, runners, CI limits and server settings.') }),
]

export const router = createRouter({
  routeTree: rootRoute.addChildren(routes),
  defaultPreload: 'intent',
})

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
