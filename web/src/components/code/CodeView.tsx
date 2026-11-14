import { useEffect, useMemo, useState } from 'react'
import { highlightLines } from '../../lib/highlight'
import { cx } from '../ui'

/**
 * Source view with line numbers, lazy syntax highlighting and #L<n>
 * anchors. Clicking a line number selects it (shift-click for a range).
 */
export function CodeView({ code, path }: { code: string; path: string }) {
  const lines = useMemo(() => {
    const ls = code.split('\n')
    if (ls.length > 1 && ls[ls.length - 1] === '') ls.pop()
    return ls
  }, [code])
  const [html, setHtml] = useState<string[] | null>(null)
  const [sel, setSel] = useState<[number, number] | null>(() => parseHash(window.location.hash))

  useEffect(() => {
    let live = true
    setHtml(null)
    highlightLines(code, path).then((h) => live && setHtml(h), () => {})
    return () => {
      live = false
    }
  }, [code, path])

  useEffect(() => {
    if (sel) document.getElementById(`L${sel[0]}`)?.scrollIntoView({ block: 'center' })
    // only on mount
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const select = (n: number, extend: boolean) => {
    const next: [number, number] = extend && sel ? [Math.min(sel[0], n), Math.max(sel[0], n)] : [n, n]
    setSel(next)
    history.replaceState(null, '', next[0] === next[1] ? `#L${next[0]}` : `#L${next[0]}-L${next[1]}`)
  }

  return (
    <div className="overflow-x-auto py-1.5 font-mono text-code">
      <table className="w-full border-collapse">
        <tbody>
          {lines.map((line, i) => {
            const n = i + 1
            const selected = sel && n >= sel[0] && n <= sel[1]
            return (
              <tr key={i} id={`L${n}`} className={cx('scroll-mt-24', selected ? 'bg-warning/12' : 'hover:bg-bg-hover/40')}>
                <td
                  className={cx('w-1 cursor-pointer pr-4 pl-4 text-right align-top tabular-nums select-none hover:text-fg', selected ? 'text-fg' : 'text-fg-faint')}
                  onClick={(e) => select(n, e.shiftKey)}
                >
                  {n}
                </td>
                <td className="code-line pr-4 whitespace-pre">
                  {html?.[i] !== undefined ? <span dangerouslySetInnerHTML={{ __html: html[i] || ' ' }} /> : line || ' '}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

function parseHash(h: string): [number, number] | null {
  const m = h.match(/^#L(\d+)(?:-L(\d+))?$/)
  if (!m) return null
  const a = Number(m[1])
  const b = m[2] ? Number(m[2]) : a
  return [Math.min(a, b), Math.max(a, b)]
}
