import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { api, errorMessage, unwrap } from '../../api/client'
import { orgQuery } from '../../api/queries'
import { Container, SubHeader } from '../../components/Page'
import { Alert, Button, Card, ErrorState, Field, FileDrop, Input, PageSkeleton, useConfirm, useToast } from '../../components/ui'
import { uploadLogo } from '../../lib/upload'

export function AdminOrgPage() {
  const qc = useQueryClient()
  const org = useQuery(orgQuery)
  const confirm = useConfirm()
  const toast = useToast()
  const [f, setF] = useState({ name: '', description: '', base_url: '' })
  const [logoVersion, setLogoVersion] = useState(0)
  useEffect(() => {
    if (org.data) setF({ name: org.data.name, description: org.data.description, base_url: org.data.base_url })
  }, [org.data])
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: orgQuery.queryKey })
    void qc.invalidateQueries({ queryKey: ['meta'] })
  }
  const save = useMutation({
    mutationFn: () => unwrap(api.PATCH('/admin/org', { body: f })),
    onSuccess: () => {
      toast.success('Organization saved')
      refresh()
    },
  })
  const upload = useMutation({
    mutationFn: uploadLogo,
    onSuccess: () => {
      setLogoVersion((v) => v + 1)
      toast.success('Logo updated')
      refresh()
    },
  })
  const remove = useMutation({
    mutationFn: () => unwrap(api.DELETE('/admin/org/logo')),
    onSuccess: () => {
      toast.success('Logo removed')
      refresh()
    },
  })

  if (org.isError) return <ErrorState error={org.error} />
  if (!org.data) return <PageSkeleton rows={3} />
  const dirty = f.name !== org.data.name || f.description !== org.data.description || f.base_url !== org.data.base_url
  return (
    <Container width="default">
      <SubHeader title="Organization" as="h2" description="Your organization's name, address and branding." />
      <div className="flex flex-col gap-6">
        <form
          onSubmit={(e) => {
            e.preventDefault()
            save.mutate()
          }}
        >
          <Card
            title="Profile"
            footer={
              <>
                {dirty && (
                  <Button variant="ghost" onClick={() => setF({ name: org.data.name, description: org.data.description, base_url: org.data.base_url })}>
                    Discard
                  </Button>
                )}
                <Button type="submit" variant="primary" loading={save.isPending} disabled={!dirty || !f.name.trim()}>
                  Save
                </Button>
              </>
            }
          >
            <div className="flex max-w-md flex-col gap-4">
              <Field label="Name" required error={!f.name.trim() ? 'A name is required.' : undefined}>
                {(id) => <Input id={id} value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} required />}
              </Field>
              <Field label="Description">{(id) => <Input id={id} value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} />}</Field>
              <Field label="Base URL" hint="Public address of this server, used in clone URLs and webhooks.">
                {(id) => <Input id={id} value={f.base_url} onChange={(e) => setF({ ...f, base_url: e.target.value })} placeholder="https://git.example.com" className="font-mono" />}
              </Field>
              {save.isError && <Alert>{errorMessage(save.error)}</Alert>}
            </div>
          </Card>
        </form>
        <Card title="Logo" description="PNG, JPEG, GIF or WebP, up to 512 KiB. Shown in the sidebar and on the sign-in page.">
          <div className="flex flex-wrap items-center gap-5">
            <img
              src={org.data.has_logo ? `/api/v1/org/logo?v=${logoVersion}` : '/favicon.svg'}
              alt="Current logo"
              className="size-16 rounded-xl border border-border bg-bg-subtle object-cover shadow-card"
            />
            <FileDrop
              compact
              className="min-w-64 flex-1"
              label="Upload logo"
              accept="image/png,image/jpeg,image/gif,image/webp"
              onFiles={(files) => files[0] && upload.mutate(files[0])}
              disabled={upload.isPending}
            >
              <span className="font-medium text-fg">{upload.isPending ? 'Uploading…' : 'Drop an image here'}</span>
              {!upload.isPending && (
                <>
                  {' '}
                  or <span className="text-accent">browse</span>
                </>
              )}
            </FileDrop>
            {org.data.has_logo && (
              <Button
                variant="ghost"
                icon={<Trash2 size={13} />}
                loading={remove.isPending}
                onClick={async () => {
                  if (await confirm({ title: 'Remove the logo?', description: 'The default tgp icon is shown instead.', confirmLabel: 'Remove logo' })) remove.mutate()
                }}
              >
                Remove
              </Button>
            )}
          </div>
          {(upload.isError || remove.isError) && (
            <div className="mt-3">
              <Alert>{errorMessage(upload.error ?? remove.error)}</Alert>
            </div>
          )}
        </Card>
      </div>
    </Container>
  )
}
