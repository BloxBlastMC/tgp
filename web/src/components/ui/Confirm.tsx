import { AlertTriangle } from 'lucide-react'
import { createContext, useCallback, useContext, useRef, useState, type ReactNode } from 'react'
import { Button } from './Button'
import { Dialog } from './Dialog'
import { Field, Input } from './Form'

export type ConfirmOptions = {
  title: string
  description?: ReactNode
  confirmLabel?: string
  cancelLabel?: string
  tone?: 'danger' | 'default'
  /** Require typing this exact text to enable the confirm button. */
  typeToConfirm?: { value: string; label?: string }
}

type ConfirmFn = (o: ConfirmOptions) => Promise<boolean>

const Ctx = createContext<ConfirmFn | null>(null)

/** Promise-based confirmation dialog; replaces window.confirm. */
export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [opts, setOpts] = useState<ConfirmOptions | null>(null)
  const [typed, setTyped] = useState('')
  const resolver = useRef<((v: boolean) => void) | null>(null)
  const confirm = useCallback<ConfirmFn>((o) => {
    resolver.current?.(false)
    setTyped('')
    setOpts(o)
    return new Promise<boolean>((res) => {
      resolver.current = res
    })
  }, [])
  const settle = (v: boolean) => {
    resolver.current?.(v)
    resolver.current = null
    setOpts(null)
  }
  const danger = (opts?.tone ?? 'danger') === 'danger'
  const blocked = !!opts?.typeToConfirm && typed !== opts.typeToConfirm.value
  return (
    <Ctx.Provider value={confirm}>
      {children}
      <Dialog
        open={opts !== null}
        onClose={() => settle(false)}
        title={opts?.title ?? ''}
        icon={
          danger ? (
            <span className="flex size-8 items-center justify-center rounded-full bg-danger/12 text-danger">
              <AlertTriangle size={15} />
            </span>
          ) : undefined
        }
        description={opts?.description}
      >
        <form
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault()
            if (!blocked) settle(true)
          }}
        >
          {opts?.typeToConfirm && (
            <Field label={opts.typeToConfirm.label ?? <>Type <strong className="font-mono">{opts.typeToConfirm.value}</strong> to confirm</>}>
              {(id) => <Input id={id} value={typed} onChange={(e) => setTyped(e.target.value)} autoFocus autoComplete="off" spellCheck={false} />}
            </Field>
          )}
          <div className="mt-1 flex justify-end gap-2">
            <Button variant="ghost" onClick={() => settle(false)}>
              {opts?.cancelLabel ?? 'Cancel'}
            </Button>
            <Button type="submit" variant={danger ? 'danger-solid' : 'primary'} disabled={blocked} autoFocus={!opts?.typeToConfirm}>
              {opts?.confirmLabel ?? 'Confirm'}
            </Button>
          </div>
        </form>
      </Dialog>
    </Ctx.Provider>
  )
}

export function useConfirm(): ConfirmFn {
  const c = useContext(Ctx)
  return c ?? ((o) => Promise.resolve(window.confirm(o.title)))
}
