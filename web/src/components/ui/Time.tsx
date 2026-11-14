import { relativeTime } from '../../lib/format'

export { relativeTime }

/** A relative time ("3 hours ago") with the full date on hover. */
export function Time({ iso, className }: { iso?: string | null; className?: string }) {
  return (
    <time dateTime={iso ?? undefined} title={iso ? new Date(iso).toLocaleString() : undefined} className={className}>
      {relativeTime(iso)}
    </time>
  )
}
