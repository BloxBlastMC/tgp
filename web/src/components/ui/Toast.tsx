import { CheckCircle2, Info, X, XCircle } from 'lucide-react'
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { cx } from './cx'
import { raiseToTop } from './overlay'

type ToastTone = 'success' | 'error' | 'info'
type ToastInput = { title: ReactNode; description?: ReactNode; action?: { label: string; onClick: () => void }; duration?: number }
type ToastItem = ToastInput & { id: number; tone: ToastTone }

type ToastApi = {
  toast: (t: ToastInput & { tone?: ToastTone }) => void
  success: (title: ReactNode, opts?: Omit<ToastInput, 'title'>) => void
  error: (title: ReactNode, opts?: Omit<ToastInput, 'title'>) => void
  info: (title: ReactNode, opts?: Omit<ToastInput, 'title'>) => void
}

const Ctx = createContext<ToastApi | null>(null)

/** Transient notifications, bottom-right, stacked; pause while hovered. */
export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([])
  const region = useRef<HTMLDivElement>(null)
  const next = useRef(1)
  const dismiss = useCallback((id: number) => setItems((l) => l.filter((t) => t.id !== id)), [])
  const toast = useCallback((t: ToastInput & { tone?: ToastTone }) => {
    const id = next.current++
    setItems((l) => [...l.slice(-3), { ...t, tone: t.tone ?? 'info', id }])
  }, [])
  const api = useMemo<ToastApi>(
    () => ({
      toast,
      success: (title, o) => toast({ ...o, title, tone: 'success' }),
      error: (title, o) => toast({ ...o, title, tone: 'error' }),
      info: (title, o) => toast({ ...o, title, tone: 'info' }),
    }),
    [toast],
  )
  // Keep the region above dialogs opened after it.
  useEffect(() => {
    if (items.length) raiseToTop(region.current)
  }, [items])
  return (
    <Ctx.Provider value={api}>
      {children}
      <div
        ref={region}
        popover="manual"
        aria-live="polite"
        className="pointer-events-none fixed! right-4! bottom-4! left-auto! top-auto! z-[60] flex w-[min(360px,calc(100vw-32px))] flex-col gap-2"
      >
        {items.map((t) => (
          <ToastCard key={t.id} t={t} onDismiss={() => dismiss(t.id)} />
        ))}
      </div>
    </Ctx.Provider>
  )
}

function ToastCard({ t, onDismiss }: { t: ToastItem; onDismiss: () => void }) {
  const [paused, setPaused] = useState(false)
  useEffect(() => {
    if (paused) return
    const h = window.setTimeout(onDismiss, t.duration ?? (t.action ? 6000 : 4000))
    return () => window.clearTimeout(h)
  }, [paused, onDismiss, t.duration, t.action])
  const Icon = t.tone === 'success' ? CheckCircle2 : t.tone === 'error' ? XCircle : Info
  return (
    <div
      role={t.tone === 'error' ? 'alert' : 'status'}
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
      className="pointer-events-auto flex animate-toast-in items-start gap-2.5 rounded-lg border border-border-strong bg-surface-2 px-3 py-2.5 text-base shadow-popover"
    >
      <Icon size={15} className={cx('mt-0.5 shrink-0', t.tone === 'success' ? 'text-success' : t.tone === 'error' ? 'text-danger' : 'text-info')} />
      <div className="min-w-0 flex-1">
        <div className="font-medium break-words">{t.title}</div>
        {t.description && <div className="mt-0.5 text-sm break-words text-fg-muted">{t.description}</div>}
      </div>
      {t.action && (
        <button
          type="button"
          onClick={() => {
            t.action!.onClick()
            onDismiss()
          }}
          className="shrink-0 rounded-md px-1.5 py-0.5 text-sm font-medium text-accent hover:bg-bg-hover"
        >
          {t.action.label}
        </button>
      )}
      <button type="button" onClick={onDismiss} aria-label="Dismiss notification" className="-mr-1 shrink-0 rounded p-0.5 text-fg-faint hover:bg-bg-hover hover:text-fg">
        <X size={13} />
      </button>
    </div>
  )
}

const noop: ToastApi = { toast: () => {}, success: () => {}, error: () => {}, info: () => {} }

export function useToast(): ToastApi {
  return useContext(Ctx) ?? noop
}
