import { ArrowDown, ArrowUp, ChevronsUpDown } from 'lucide-react'
import { useMemo, useState, type HTMLAttributes, type ReactNode, type TdHTMLAttributes, type ThHTMLAttributes } from 'react'
import { cx } from './cx'
import { Skeleton } from './Feedback'

export function Table({ className, children, ...rest }: HTMLAttributes<HTMLTableElement>) {
  return (
    <table {...rest} className={cx('w-full border-collapse text-left', className)}>
      {children}
    </table>
  )
}

export function THead({ children, className }: { children: ReactNode; className?: string }) {
  return <thead className={cx('sticky top-0 z-[1] bg-bg-subtle text-xs font-medium text-fg-muted shadow-[inset_0_-1px_0_var(--border)]', className)}>{children}</thead>
}

export type SortDir = 'asc' | 'desc'

export function TH({
  children,
  sort,
  onSort,
  align = 'left',
  className,
  ...rest
}: ThHTMLAttributes<HTMLTableCellElement> & { sort?: SortDir | null; onSort?: () => void; align?: 'left' | 'right' | 'center' }) {
  const content = onSort ? (
    <button
      type="button"
      onClick={onSort}
      className={cx('group inline-flex items-center gap-1 hover:text-fg', align === 'right' && 'flex-row-reverse', sort && 'text-fg')}
    >
      {children}
      {sort === 'asc' ? <ArrowUp size={11} /> : sort === 'desc' ? <ArrowDown size={11} /> : <ChevronsUpDown size={11} className="opacity-0 group-hover:opacity-60" />}
    </button>
  ) : (
    children
  )
  return (
    <th
      {...rest}
      scope="col"
      aria-sort={sort === 'asc' ? 'ascending' : sort === 'desc' ? 'descending' : onSort ? 'none' : undefined}
      className={cx('h-8 px-3 font-medium whitespace-nowrap first:pl-4 last:pr-4', align === 'right' ? 'text-right' : align === 'center' ? 'text-center' : 'text-left', className)}
    >
      {content}
    </th>
  )
}

export function TR({ className, children, ...rest }: HTMLAttributes<HTMLTableRowElement>) {
  return (
    <tr {...rest} className={cx('border-b border-border transition-colors last:border-0 hover:bg-bg-hover/60', className)}>
      {children}
    </tr>
  )
}

export function TD({ className, align, children, ...rest }: TdHTMLAttributes<HTMLTableCellElement> & { align?: 'left' | 'right' | 'center' }) {
  return (
    <td {...rest} className={cx('h-11 px-3 align-middle first:pl-4 last:pr-4', align === 'right' && 'text-right', align === 'center' && 'text-center', className)}>
      {children}
    </td>
  )
}

export type Column<T> = {
  id: string
  header: ReactNode
  cell: (row: T) => ReactNode
  /** Makes the column sortable by this value. */
  sortValue?: (row: T) => string | number | null | undefined
  align?: 'left' | 'right' | 'center'
  className?: string
  headerClassName?: string
}

/**
 * A table from column definitions with client-side sorting, skeleton rows
 * while loading and an empty state.
 */
export function DataTable<T>({
  columns,
  rows,
  rowKey,
  rowProps,
  defaultSort,
  sort: controlledSort,
  onSortChange,
  loading,
  empty,
  className,
  'aria-label': ariaLabel,
}: {
  columns: Column<T>[]
  rows: T[] | undefined
  rowKey: (row: T) => string | number
  rowProps?: (row: T) => HTMLAttributes<HTMLTableRowElement> & Record<`data-${string}`, string>
  defaultSort?: { id: string; dir: SortDir }
  /** Controlled sort; when set, rows are expected pre-sorted. */
  sort?: { id: string; dir: SortDir }
  onSortChange?: (s: { id: string; dir: SortDir }) => void
  loading?: boolean
  empty?: ReactNode
  className?: string
  'aria-label'?: string
}) {
  const [ownSort, setOwnSort] = useState(defaultSort)
  const sort = controlledSort ?? ownSort
  const sorted = useMemo(() => {
    if (!rows) return []
    if (controlledSort || !sort) return rows
    const col = columns.find((c) => c.id === sort.id)
    if (!col?.sortValue) return rows
    const get = col.sortValue
    return [...rows].sort((a, b) => {
      const x = get(a) ?? ''
      const y = get(b) ?? ''
      const c = typeof x === 'number' && typeof y === 'number' ? x - y : String(x).localeCompare(String(y), undefined, { numeric: true, sensitivity: 'base' })
      return sort.dir === 'asc' ? c : -c
    })
  }, [rows, sort, columns, controlledSort])

  const toggle = (id: string) => {
    const next: { id: string; dir: SortDir } = sort?.id === id ? { id, dir: sort.dir === 'asc' ? 'desc' : 'asc' } : { id, dir: 'desc' }
    if (onSortChange) onSortChange(next)
    else setOwnSort(next)
  }

  return (
    <div className={cx('overflow-x-auto', className)}>
      <Table aria-label={ariaLabel}>
        <THead>
          <tr>
            {columns.map((c) => (
              <TH
                key={c.id}
                align={c.align}
                className={c.headerClassName}
                sort={c.sortValue ? (sort?.id === c.id ? sort.dir : null) : undefined}
                onSort={c.sortValue ? () => toggle(c.id) : undefined}
              >
                {c.header}
              </TH>
            ))}
          </tr>
        </THead>
        <tbody>
          {loading && !rows
            ? Array.from({ length: 4 }, (_, i) => (
                <tr key={i} className="border-b border-border last:border-0">
                  {columns.map((c, j) => (
                    <TD key={c.id}>
                      <Skeleton className={cx('h-3', j === 0 ? 'w-40' : 'w-16', c.align === 'right' && 'ml-auto')} />
                    </TD>
                  ))}
                </tr>
              ))
            : sorted.map((r) => (
                <TR key={rowKey(r)} {...rowProps?.(r)}>
                  {columns.map((c) => (
                    <TD key={c.id} align={c.align} className={c.className}>
                      {c.cell(r)}
                    </TD>
                  ))}
                </TR>
              ))}
        </tbody>
      </Table>
      {!loading && rows && rows.length === 0 && empty}
    </div>
  )
}
