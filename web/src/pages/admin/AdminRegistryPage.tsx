import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { api, errorMessage, unwrap, type RegistrySettings } from '../../api/client'
import { registrySettingsQuery } from '../../api/queries'
import { Container, SubHeader } from '../../components/Page'
import { Alert, ErrorState, Field, Input, PageSkeleton, SaveBar, Section, useToast } from '../../components/ui'

type NumKey = 'quota_mb' | 'untagged_retention_days' | 'keep_last_tags'

export function AdminRegistryPage() {
  const q = useQuery(registrySettingsQuery)
  const qc = useQueryClient()
  const toast = useToast()
  const [f, setF] = useState<RegistrySettings | null>(null)
  const [patterns, setPatterns] = useState('')
  useEffect(() => {
    if (q.data) {
      setF(q.data)
      setPatterns(q.data.keep_last_tags_patterns.join(', '))
    }
  }, [q.data])
  const save = useMutation({
    mutationFn: (body: RegistrySettings) => unwrap(api.PATCH('/admin/registry/settings', { body })),
    onSuccess: (s) => {
      qc.setQueryData(registrySettingsQuery.queryKey, s)
      setF(s)
      setPatterns(s.keep_last_tags_patterns.join(', '))
      toast.success('Registry settings saved')
    },
  })
  if (q.isError) return <ErrorState error={q.error} />
  if (!f) return <PageSkeleton rows={3} />
  const parsed = patterns
    .split(',')
    .map((p) => p.trim())
    .filter(Boolean)
  const next: RegistrySettings = { ...f, keep_last_tags_patterns: parsed }
  const invalid = ([f.quota_mb, f.untagged_retention_days, f.keep_last_tags] as number[]).some((v) => Number.isNaN(v)) || f.untagged_retention_days < 1
  const dirty = JSON.stringify(next) !== JSON.stringify(q.data)
  const num = (k: NumKey, label: string, hint: string, min = 0) => (
    <Field label={label} hint={hint} error={Number.isNaN(f[k]) || f[k] < min ? `Enter a number of at least ${min}.` : undefined}>
      {(id) => (
        <Input
          id={id}
          type="number"
          min={min}
          value={Number.isNaN(f[k]) ? '' : f[k]}
          onChange={(e) => {
            save.reset()
            setF({ ...f, [k]: e.target.value === '' ? NaN : Number(e.target.value) })
          }}
          className="w-36 tabular-nums"
        />
      )}
    </Field>
  )
  return (
    <Container width="default">
      <SubHeader title="Container registry" as="h2" description="Storage limits and cleanup for the images every repository pushes. Blobs shared between images count once." />
      <form
        className="flex flex-col gap-6"
        aria-label="Registry settings"
        onSubmit={(e) => {
          e.preventDefault()
          save.mutate(next)
        }}
      >
        <Section title="Quota">
          {num('quota_mb', 'Storage per repository (MB)', 'Pushes that would go over it are refused. 0 = no quota')}
        </Section>
        <Section title="Retention" description="Garbage collection runs every minute and frees the storage of blobs no image uses any more.">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            {num('untagged_retention_days', 'Delete untagged versions after (days)', 'Versions of a tagged multi-platform image are kept with it.', 1)}
            {num('keep_last_tags', 'Keep the newest tags per image', 'Older tags matching the patterns below are deleted. 0 = keep all')}
          </div>
          <div className="mt-4">
            <Field label="Tag patterns" hint="Comma-separated globs, such as g* for commit tags. Other tags (such as main or latest) are never pruned.">
              {(id) => <Input id={id} value={patterns} onChange={(e) => setPatterns(e.target.value)} className="font-mono" />}
            </Field>
          </div>
        </Section>
        {save.isError && <Alert>{errorMessage(save.error)}</Alert>}
        <SaveBar
          dirty={dirty}
          saving={save.isPending}
          disabled={invalid}
          saveLabel="Save registry settings"
          onDiscard={() => {
            if (q.data) {
              setF(q.data)
              setPatterns(q.data.keep_last_tags_patterns.join(', '))
            }
          }}
          message={invalid ? 'Fix the highlighted fields.' : dirty ? 'You have unsaved changes.' : save.isSuccess ? 'Saved.' : 'All changes saved.'}
        />
      </form>
    </Container>
  )
}
