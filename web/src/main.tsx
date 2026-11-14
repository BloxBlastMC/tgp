import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { MutationCache, QueryCache, QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from '@tanstack/react-router'
import { ApiRequestError } from './api/client'
import { makeRouter } from './routes/router'
import './index.css'

// When the session expires or is revoked mid-use, send the user to the
// right place instead of leaving pages showing errors.
function onApiError(err: unknown) {
  if (!(err instanceof ApiRequestError)) return
  const path = window.location.pathname
  if (err.status === 401 && err.code === 'unauthenticated' && !['/login', '/setup'].includes(path)) {
    queryClient.setQueryData(['session'], null)
    void router.navigate({ to: '/login', search: { next: path } })
  } else if (err.code === 'password_change_required' && path !== '/change-password') {
    void router.navigate({ to: '/change-password' })
  } else if (err.code === 'setup_required' && path !== '/setup') {
    void router.navigate({ to: '/setup' })
  }
}

const queryClient = new QueryClient({
  queryCache: new QueryCache({ onError: onApiError }),
  mutationCache: new MutationCache({ onError: onApiError }),
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      // Don't retry auth/permission/validation errors.
      retry: (count, err) => !(err instanceof ApiRequestError && err.status < 500) && count < 1,
    },
  },
})

const router = makeRouter(queryClient)

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
)
