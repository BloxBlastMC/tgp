import { Link } from '@tanstack/react-router'
import { Fragment } from 'react'

/** repo / dir / dir / file breadcrumbs linking to tree views. */
export function Breadcrumbs({ repo, gitRef, path }: { repo: string; gitRef: string; path: string }) {
  const parts = path ? path.split('/') : []
  return (
    <nav className="flex min-w-0 flex-wrap items-center gap-1 font-mono text-base" aria-label="Path">
      <Link to="/$repo" params={{ repo }} search={{ ref: gitRef }} className="font-semibold text-accent hover:underline">
        {repo}
      </Link>
      {parts.map((p, i) => {
        const sub = parts.slice(0, i + 1).join('/')
        const last = i === parts.length - 1
        return (
          <Fragment key={sub}>
            <span className="text-fg-faint">/</span>
            {last ? (
              <span className="font-semibold">{p}</span>
            ) : (
              <Link to="/$repo" params={{ repo }} search={{ ref: gitRef, path: sub }} className="text-accent hover:underline">
                {p}
              </Link>
            )}
          </Fragment>
        )
      })}
    </nav>
  )
}
