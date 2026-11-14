import { ChevronLeft, ChevronRight } from 'lucide-react'
import { Button } from './Button'
import { cx } from './cx'

/**
 * Previous/next pagination. Give `total` + `perPage` to show "Page n of m",
 * or `hasNext` when the total isn't known.
 */
export function Pagination({
  page,
  onPage,
  total,
  perPage,
  hasNext,
  prevLabel = 'Previous',
  nextLabel = 'Next',
  className,
}: {
  page: number
  onPage: (p: number) => void
  total?: number
  perPage?: number
  hasNext?: boolean
  prevLabel?: string
  nextLabel?: string
  className?: string
}) {
  const pages = total != null && perPage ? Math.max(1, Math.ceil(total / perPage)) : null
  const next = pages != null ? page < pages : !!hasNext
  if (page <= 1 && !next) return null
  return (
    <nav aria-label="Pagination" className={cx('flex items-center justify-between gap-3 px-4 py-3 text-sm text-fg-muted', className)}>
      <span className="tabular-nums">
        {pages != null ? (
          <>
            Page {page} of {pages}
            {total != null && <span className="text-fg-faint"> · {total.toLocaleString()} total</span>}
          </>
        ) : (
          <>Page {page}</>
        )}
      </span>
      <div className="flex gap-1.5">
        <Button size="sm" disabled={page <= 1} onClick={() => onPage(page - 1)} icon={<ChevronLeft size={13} />}>
          {prevLabel}
        </Button>
        <Button size="sm" disabled={!next} onClick={() => onPage(page + 1)} iconRight={<ChevronRight size={13} />}>
          {nextLabel}
        </Button>
      </div>
    </nav>
  )
}
