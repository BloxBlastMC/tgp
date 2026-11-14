import { ChevronDown, ChevronRight } from 'lucide-react'
import { memo, useEffect, useRef, useState } from 'react'
import { api, unwrap, type Job, type LogLine } from '../../api/client'
import { ansiColors, appendLines, buildLogItems, lineLevel, parseAnsi } from '../../lib/ci'
import { cx } from '../ui'

export type LogState = { lines: LogLine[]; complete: boolean; truncated: boolean; error?: string; live: boolean }

async function fetchLogs(repo: string, id: number, after: number) {
  return unwrap(api.GET('/repos/{repo}/jobs/{id}/logs', { params: { path: { repo, id }, query: { after, limit: 20000 } } }))
}

/**
 * Loads a job's log and follows it while the job runs: server-sent events
 * from /logs/stream, falling back to polling getJobLogs if the stream fails.
 * `onJob` is called when the stream reports a job change.
 */
export function useJobLogs(repo: string, job: Job, onJob?: (j: Job) => void): LogState {
  const [state, setState] = useState<LogState>({ lines: [], complete: false, truncated: false, live: false })
  const running = job.status !== 'completed'
  const onJobRef = useRef(onJob)
  onJobRef.current = onJob

  const lastRef = useRef(0)
  useEffect(() => {
    lastRef.current = 0
    setState({ lines: [], complete: false, truncated: false, live: false })
  }, [repo, job.id])

  useEffect(() => {
    let cancelled = false
    let es: EventSource | null = null
    let timer: number | undefined
    const apply = (lines: LogLine[], extra: Partial<LogState> = {}) => {
      if (cancelled) return
      if (lines.length) lastRef.current = Math.max(lastRef.current, lines[lines.length - 1]!.n)
      setState((s) => ({ ...s, ...extra, lines: appendLines(s.lines, lines) }))
    }
    // Load everything available, page by page.
    const drain = async (): Promise<boolean> => {
      for (;;) {
        const last = lastRef.current
        const r = await fetchLogs(repo, job.id, last)
        apply(r.lines, { truncated: r.truncated, complete: r.complete })
        if (r.complete) return true
        if (r.lines.length === 0 || r.next <= last) return false
      }
    }
    const poll = async () => {
      try {
        const done = await drain()
        if (done || cancelled) return
      } catch (e) {
        if (cancelled) return
        setState((s) => ({ ...s, error: e instanceof Error ? e.message : String(e) }))
      }
      timer = window.setTimeout(poll, 2000)
    }
    const stream = () => {
      if (typeof EventSource === 'undefined') return void poll()
      es = new EventSource(`/api/v1/repos/${encodeURIComponent(repo)}/jobs/${job.id}/logs/stream?after=${lastRef.current}`)
      setState((s) => ({ ...s, live: true }))
      es.addEventListener('line', (ev) => {
        try {
          apply([JSON.parse((ev as MessageEvent).data) as LogLine])
        } catch {
          /* ignore malformed */
        }
      })
      const jobEvent = (ev: Event) => {
        try {
          onJobRef.current?.(JSON.parse((ev as MessageEvent).data) as Job)
        } catch {
          /* ignore */
        }
      }
      es.addEventListener('job', jobEvent)
      es.addEventListener('end', (ev) => {
        jobEvent(ev)
        es?.close()
        es = null
        setState((s) => ({ ...s, live: false }))
        void poll() // pick up the final lines and the truncated flag
      })
      es.onerror = () => {
        if (!es) return
        es.close()
        es = null
        if (cancelled) return
        setState((s) => ({ ...s, live: false }))
        void poll()
      }
    }
    void (async () => {
      if (!running) return void poll()
      try {
        await drain()
      } catch {
        /* the stream below retries */
      }
      if (!cancelled) stream()
    })()
    return () => {
      cancelled = true
      es?.close()
      window.clearTimeout(timer)
    }
    // Restart when the job id changes, or when it goes from finished to running (re-run).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [repo, job.id, running])

  return state
}

/** A line of log text with ANSI colours. */
export const AnsiText = memo(function AnsiText({ text }: { text: string }) {
  const segs = parseAnsi(text)
  return (
    <>
      {segs.map((s, i) =>
        s.fg === undefined && s.bg === undefined && !s.bold ? (
          <span key={i}>{s.text}</span>
        ) : (
          <span
            key={i}
            style={{ color: s.fg !== undefined ? ansiColors[s.fg] : undefined, backgroundColor: s.bg !== undefined ? ansiColors[s.bg] + '55' : undefined }}
            className={s.bold ? 'font-bold' : undefined}
          >
            {s.text}
          </span>
        ),
      )}
    </>
  )
})

const levelClass = {
  error: 'bg-danger/10 text-danger',
  warning: 'bg-warning/10 text-warning',
  notice: 'bg-info/10 text-info',
  command: 'text-accent',
}

function Line({ line, level, text, indent }: { line: LogLine; level?: keyof typeof levelClass; text?: string; indent?: boolean }) {
  return (
    <div className={cx('flex hover:bg-bg-hover/60', level && levelClass[level])} data-line={line.n}>
      <a href={`#L${line.n}`} id={`L${line.n}`} className="w-12 shrink-0 pr-3 text-right text-fg-faint select-none hover:text-fg-muted">
        {line.n}
      </a>
      <span className={cx('min-w-0 flex-1 break-all whitespace-pre-wrap', indent && 'pl-4')}>
        {level === 'error' && <span className="font-semibold">Error: </span>}
        {level === 'warning' && <span className="font-semibold">Warning: </span>}
        <AnsiText text={text ?? line.text} />
      </span>
    </div>
  )
}

function Group({ title, header, children }: { title: string; header: LogLine; children: LogLine[] }) {
  const [open, setOpen] = useState(false)
  return (
    <div>
      <button type="button" onClick={() => setOpen((o) => !o)} className="flex w-full text-left hover:bg-bg-hover/60" aria-expanded={open}>
        <span className="w-12 shrink-0 pr-3 text-right text-fg-faint select-none">{header.n}</span>
        <span className="flex items-center gap-1 font-medium">
          {open ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
          <AnsiText text={title} />
        </span>
      </button>
      {open &&
        children.map((l) => {
          const { text, level } = lineLevel(l)
          return <Line key={l.n} line={l} text={text} level={level} indent />
        })}
    </div>
  )
}

/** The lines of one step, with groups folded. */
export function StepLog({ lines }: { lines: LogLine[] }) {
  const items = buildLogItems(lines)
  return (
    <div className="font-mono text-[12px] leading-[18px]">
      {items.map((it) =>
        it.kind === 'group' ? (
          <Group key={it.line.n} title={it.title} header={it.line} children={it.children} />
        ) : (
          <Line key={it.line.n} line={it.line} level={it.level} />
        ),
      )}
    </div>
  )
}
