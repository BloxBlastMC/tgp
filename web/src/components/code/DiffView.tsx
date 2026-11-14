import { ChevronDown, ChevronRight, Columns2, Plus, Rows2 } from 'lucide-react'
import { createContext, Fragment, useContext, useMemo, useState, type ReactNode } from 'react'
import type { Diff, DiffFile, Schemas } from '../../api/client'
import { commentAnchor } from '../../lib/pulls'
import { Badge, cx } from '../ui'

type Line = Schemas['DiffLine']
type Mode = 'unified' | 'split'
type Side = 'old' | 'new'

/**
 * Optional hooks for showing content under diff lines (pull request
 * comment threads) and a gutter button to start a comment on a line.
 */
export type DiffAnnotations = {
  render: (path: string, side: Side, line: number) => ReactNode
  onComment?: (path: string, side: Side, line: number) => void
}

const AnnotationsContext = createContext<DiffAnnotations | undefined>(undefined)

function loadMode(): Mode {
  try {
    return localStorage.getItem('tgp-diff-mode') === 'split' ? 'split' : 'unified'
  } catch {
    return 'unified'
  }
}

const statusTone = { added: 'success', deleted: 'danger', renamed: 'accent', copied: 'accent', modified: 'neutral' } as const

export function DiffStat({ additions, deletions }: { additions: number; deletions: number }) {
  return (
    <span className="font-mono text-sm">
      <span className="text-success">+{additions}</span> <span className="text-danger">−{deletions}</span>
    </span>
  )
}

/** A whole diff: summary bar, file list and one card per file. */
export function DiffView({ diff, annotations }: { diff: Diff; annotations?: DiffAnnotations }) {
  const [mode, setModeState] = useState<Mode>(loadMode)
  const setMode = (m: Mode) => {
    setModeState(m)
    try {
      localStorage.setItem('tgp-diff-mode', m)
    } catch {
      /* ignore */
    }
  }
  return (
    <AnnotationsContext.Provider value={annotations}>
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <div className="text-fg-muted">
          {diff.files.length} {diff.files.length === 1 ? 'file' : 'files'} changed · <DiffStat additions={diff.additions} deletions={diff.deletions} />
        </div>
        <div className="flex rounded border border-border-strong">
          {(['unified', 'split'] as const).map((m) => (
            <button
              key={m}
              type="button"
              onClick={() => setMode(m)}
              className={cx('flex h-6 items-center gap-1 px-2 text-sm', mode === m ? 'bg-bg-hover text-fg' : 'text-fg-muted hover:text-fg')}
              aria-pressed={mode === m}
            >
              {m === 'unified' ? <Rows2 size={12} /> : <Columns2 size={12} />} {m === 'unified' ? 'Unified' : 'Split'}
            </button>
          ))}
        </div>
      </div>
      {diff.truncated && (
        <div className="rounded border border-warning/30 bg-warning/10 px-3 py-2 text-sm text-warning">
          This diff is large, so some files or lines are not shown. Use the CLI or git locally for the full diff.
        </div>
      )}
      {diff.files.map((f, i) => (
        <FileDiff key={`${f.old_path}->${f.new_path}-${i}`} file={f} mode={mode} />
      ))}
    </div>
    </AnnotationsContext.Provider>
  )
}

function pathFor(file: DiffFile, side: Side) {
  return side === 'old' ? file.old_path : file.new_path
}

function CommentButton({ file, line }: { file: DiffFile; line: Line | null }) {
  const ann = useContext(AnnotationsContext)
  const a = line && commentAnchor(line)
  if (!ann?.onComment || !a) return null
  return (
    <button
      type="button"
      onClick={() => ann.onComment!(pathFor(file, a.side), a.side, a.line)}
      className="absolute top-0.5 -left-0.5 hidden size-4 items-center justify-center rounded bg-accent-solid text-accent-fg group-hover:flex focus:flex"
      aria-label={`Comment on ${pathFor(file, a.side)} line ${a.line}`}
      title="Comment on this line"
    >
      <Plus size={11} />
    </button>
  )
}

/** Annotation row content for the given lines (nothing if empty). */
function useExtras(file: DiffFile, lines: (Line | null)[]): ReactNode {
  const ann = useContext(AnnotationsContext)
  if (!ann) return null
  const parts: ReactNode[] = []
  const seen = new Set<string>()
  for (const l of lines) {
    const a = l && commentAnchor(l)
    if (!a) continue
    const k = `${a.side}:${a.line}`
    if (seen.has(k)) continue
    seen.add(k)
    const node = ann.render(pathFor(file, a.side), a.side, a.line)
    if (node) parts.push(<Fragment key={k}>{node}</Fragment>)
  }
  return parts.length ? parts : null
}

function ExtrasRow({ file, lines, cols }: { file: DiffFile; lines: (Line | null)[]; cols: number }) {
  const extras = useExtras(file, lines)
  if (!extras) return null
  return (
    <tr>
      <td colSpan={cols} className="border-y border-border bg-bg-subtle px-3 py-2 font-sans text-base leading-normal whitespace-normal">
        <div className="flex max-w-3xl flex-col gap-2">{extras}</div>
      </td>
    </tr>
  )
}

function FileDiff({ file, mode }: { file: DiffFile; mode: Mode }) {
  const big = file.additions + file.deletions > 800
  const [open, setOpen] = useState(!big && file.status !== 'deleted')
  const name = file.status === 'renamed' ? `${file.old_path} → ${file.new_path}` : file.new_path || file.old_path
  return (
    <div className="overflow-hidden rounded-md border border-border" id={`diff-${encodeURIComponent(file.new_path)}`}>
      <button
        type="button"
        onClick={() => setOpen(!open)}
        className="sticky top-0 z-10 flex w-full items-center gap-2 border-b border-border bg-bg-subtle px-3 py-1.5 text-left"
      >
        {open ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        <span className="min-w-0 flex-1 truncate font-mono text-code">{name}</span>
        {file.status !== 'modified' && <Badge tone={statusTone[file.status]}>{file.status}</Badge>}
        {file.old_mode && file.new_mode && file.old_mode !== file.new_mode && (
          <span className="font-mono text-xs text-fg-faint">
            {file.old_mode} → {file.new_mode}
          </span>
        )}
        <DiffStat additions={file.additions} deletions={file.deletions} />
      </button>
      {open &&
        (file.binary ? (
          <p className="px-4 py-3 text-fg-muted">Binary file not shown.</p>
        ) : file.hunks.length === 0 ? (
          <p className="px-4 py-3 text-fg-muted">{file.status === 'renamed' ? 'File renamed without changes.' : 'No content changes.'}</p>
        ) : (
          <div className="overflow-x-auto font-mono text-code">
            {mode === 'unified' ? <Unified file={file} /> : <Split file={file} />}
            {file.truncated && <p className="border-t border-border px-4 py-2 font-sans text-sm text-warning">Some lines of this file are not shown.</p>}
          </div>
        ))}
      {!open && big && <p className="px-4 py-2 text-sm text-fg-muted">Large diff hidden. Click the header to show it.</p>}
    </div>
  )
}

const rowTone = { add: 'bg-success/10', del: 'bg-danger/10', context: '' }
const sign = { add: '+', del: '-', context: ' ' }

function HunkHeader({ header, cols }: { header: string; cols: number }) {
  return (
    <tr className="bg-accent-subtle text-fg-muted">
      <td colSpan={cols} className="px-3 py-0.5">
        {header}
      </td>
    </tr>
  )
}

function Unified({ file }: { file: DiffFile }) {
  return (
    <table className="w-full border-collapse">
      <tbody>
        {file.hunks.map((h, hi) => (
          <Fragment key={hi}>
            <HunkHeader header={h.header} cols={3} />
            {h.lines.map((l, li) => (
              <Fragment key={li}>
              <tr className={cx('group', rowTone[l.type])}>
                <td className="relative w-1 px-2 text-right text-fg-faint select-none">
                  <CommentButton file={file} line={l} />
                  {l.old_line ?? ''}
                </td>
                <td className="w-1 px-2 text-right text-fg-faint select-none">{l.new_line ?? ''}</td>
                <td className="pr-4 whitespace-pre">
                  <span className="select-none text-fg-faint">{sign[l.type]}</span>
                  {l.content}
                  {l.no_newline && <span className="ml-2 text-xs text-fg-faint select-none">⏎ no newline at end of file</span>}
                </td>
              </tr>
              <ExtrasRow file={file} lines={[l]} cols={3} />
              </Fragment>
            ))}
          </Fragment>
        ))}
      </tbody>
    </table>
  )
}

/** Pairs deletions with following additions so they sit side by side. */
function pairLines(lines: Line[]): [Line | null, Line | null][] {
  const rows: [Line | null, Line | null][] = []
  let i = 0
  while (i < lines.length) {
    const l = lines[i]!
    if (l.type === 'context') {
      rows.push([l, l])
      i++
      continue
    }
    const dels: Line[] = []
    const adds: Line[] = []
    while (i < lines.length && lines[i]!.type === 'del') dels.push(lines[i++]!)
    while (i < lines.length && lines[i]!.type === 'add') adds.push(lines[i++]!)
    for (let k = 0; k < Math.max(dels.length, adds.length); k++) rows.push([dels[k] ?? null, adds[k] ?? null])
  }
  return rows
}

function Split({ file }: { file: DiffFile }) {
  const hunks = useMemo(() => file.hunks.map((h) => ({ header: h.header, rows: pairLines(h.lines) })), [file])
  const cell = (l: Line | null, side: 'old' | 'new') => {
    if (!l) return [<td key="n" className="w-1 bg-bg-subtle" />, <td key="c" className="w-1/2 bg-bg-subtle" />]
    const tone = l.type === 'context' ? '' : side === 'old' ? 'bg-danger/10' : 'bg-success/10'
    // Context lines are commented on the new side only.
    const commentable = side === 'new' || l.type === 'del'
    return [
      <td key="n" className={cx('relative w-1 px-2 text-right text-fg-faint select-none', tone)}>
        {commentable && <CommentButton file={file} line={l} />}
        {side === 'old' ? l.old_line : l.new_line}
      </td>,
      <td key="c" className={cx('w-1/2 pr-3 whitespace-pre', tone)}>
        {l.content}
      </td>,
    ]
  }
  return (
    <table className="w-full table-fixed border-collapse">
      <colgroup>
        <col className="w-12" />
        <col />
        <col className="w-12" />
        <col />
      </colgroup>
      <tbody>
        {hunks.map((h, hi) => (
          <Fragment key={hi}>
            <HunkHeader header={h.header} cols={4} />
            {h.rows.map(([a, b], ri) => (
              <Fragment key={ri}>
                <tr className="group">
                  {cell(a, 'old')}
                  {cell(b, 'new')}
                </tr>
                <ExtrasRow file={file} lines={[a?.type === 'del' ? a : null, b]} cols={4} />
              </Fragment>
            ))}
          </Fragment>
        ))}
      </tbody>
    </table>
  )
}

