import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { api, errorMessage, unwrap } from '../../api/client'
import { orgQuery } from '../../api/queries'
import { Alert, Button, Field, Input, Section, Spinner } from '../../components/ui'

export function AdminOrgPage() {
  const qc = useQueryClient()
  const org = useQuery(orgQuery)
  const [f, setF] = useState({ name: '', description: '', base_url: '' })
  const [logoVersion, setLogoVersion] = useState(0)
  useEffect(() => {
    if (org.data) setF({ name: org.data.name, description: org.data.description, base_url: org.data.base_url })
  }, [org.data])
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: orgQuery.queryKey })
    void qc.invalidateQueries({ queryKey: ['meta'] })
  }
  const save = useMutation({ mutationFn: () => unwrap(api.PATCH('/admin/org', { body: f })), onSuccess: refresh })
  const upload = useMutation({
    mutationFn: async (file: File) => {
      const r = await fetch('/api/v1/admin/org/logo', {
        method: 'PUT',
        body: file,
        headers: {
          'Content-Type': 'application/octet-stream',
          'X-CSRF-Token': document.cookie.match(/(?:^|; )tgp_csrf=([^;]*)/)?.[1] ?? '',
        },
      })
      if (!r.ok) throw new Error((await r.json().catch(() => null))?.error?.message ?? 'Upload failed')
    },
    onSuccess: () => {
      setLogoVersion((v) => v + 1)
      refresh()
    },
  })
  const remove = useMutation({ mutationFn: () => unwrap(api.DELETE('/admin/org/logo')), onSuccess: refresh })

  if (!org.data) return <Spinner />
  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-5 p-6">
      <Section title="Organization">
        <form
          className="flex max-w-md flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault()
            save.mutate()
          }}
        >
          <Field label="Name">{(id) => <Input id={id} value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} required />}</Field>
          <Field label="Description">{(id) => <Input id={id} value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} />}</Field>
          <Field label="Base URL" hint="Public address of this server, used in clone URLs and webhooks.">
            {(id) => <Input id={id} value={f.base_url} onChange={(e) => setF({ ...f, base_url: e.target.value })} placeholder="https://git.example.com" />}
          </Field>
          {save.isError && <Alert>{errorMessage(save.error)}</Alert>}
          {save.isSuccess && <Alert tone="success">Saved.</Alert>}
          <Button type="submit" variant="primary" className="self-start" loading={save.isPending}>
            Save
          </Button>
        </form>
      </Section>
      <Section title="Logo" description="PNG, JPEG, GIF or WebP, up to 512 KiB. Shown in the sidebar and on the login page.">
        <div className="flex items-center gap-4">
          <img
            src={org.data.has_logo ? `/api/v1/org/logo?v=${logoVersion}` : '/favicon.svg'}
            alt=""
            className="size-12 rounded border border-border object-cover"
          />
          <label className="inline-flex h-7 cursor-pointer items-center rounded border border-border-strong bg-bg-elevated px-2.5 font-medium hover:bg-bg-hover">
            Upload
            <input
              type="file"
              accept="image/png,image/jpeg,image/gif,image/webp"
              className="sr-only"
              onChange={(e) => {
                const file = e.target.files?.[0]
                if (file) upload.mutate(file)
                e.target.value = ''
              }}
            />
          </label>
          {org.data.has_logo && (
            <Button variant="ghost" onClick={() => remove.mutate()} loading={remove.isPending}>
              Remove
            </Button>
          )}
        </div>
        {(upload.isError || remove.isError) && (
          <div className="mt-3">
            <Alert>{errorMessage(upload.error ?? remove.error)}</Alert>
          </div>
        )}
      </Section>
    </div>
  )
}
