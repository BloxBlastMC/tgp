import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { api, errorMessage, unwrap, type CISettings } from '../../api/client'
import { ciSettingsQuery } from '../../api/queries'
import { SecretsSection } from '../../components/ci/SecretsSection'
import { Container, SubHeader } from '../../components/Page'
import { Alert, Checkbox, ErrorState, Field, Input, PageSkeleton, SaveBar, Section, useToast } from '../../components/ui'

type NumKey = { [K in keyof CISettings]: CISettings[K] extends number ? K : never }[keyof CISettings]

function NumField({ f, k, label, hint, step, set }: { f: CISettings; k: NumKey; label: string; hint?: string; step?: number; set: (k: NumKey, v: number) => void }) {
  return (
    <Field label={label} hint={hint} error={Number.isNaN(f[k]) ? 'Enter a number.' : undefined}>
      {(id) => (
        <Input
          id={id}
          type="number"
          min={0}
          step={step ?? 1}
          value={Number.isNaN(f[k]) ? '' : f[k]}
          onChange={(e) => set(k, e.target.value === '' ? NaN : Number(e.target.value))}
          className="w-36 tabular-nums"
          aria-invalid={Number.isNaN(f[k]) || undefined}
        />
      )}
    </Field>
  )
}

export function AdminCIPage() {
  const q = useQuery(ciSettingsQuery)
  const qc = useQueryClient()
  const toast = useToast()
  const [f, setF] = useState<CISettings | null>(null)
  useEffect(() => {
    if (q.data) setF(q.data)
  }, [q.data])
  const save = useMutation({
    mutationFn: (body: CISettings) => unwrap(api.PATCH('/admin/ci/settings', { body })),
    onSuccess: (s) => {
      qc.setQueryData(ciSettingsQuery.queryKey, s)
      setF(s)
      toast.success('CI settings saved')
    },
  })
  if (q.isError) return <ErrorState error={q.error} />
  if (!f) return <PageSkeleton rows={4} />
  const set = (k: NumKey, v: number) => {
    save.reset()
    setF({ ...f, [k]: v })
  }
  const invalid = Object.values(f).some((v) => typeof v === 'number' && Number.isNaN(v))
  const dirty = JSON.stringify(f) !== JSON.stringify(q.data)
  return (
    <Container width="default">
      <SubHeader title="CI settings" as="h2" description="Concurrency, runner health, container limits and retention for every repository." />
      <form
        className="flex flex-col gap-6"
        aria-label="CI settings"
        onSubmit={(e) => {
          e.preventDefault()
          save.mutate(f)
        }}
      >
        <Section title="Concurrency" description="How many jobs run at once. The scheduler takes turns between repositories so one busy repository can't starve the rest.">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
            <NumField f={f} set={set} k="max_concurrent_jobs" label="Max jobs (instance)" hint="0 = no limit beyond runner capacity" />
            <NumField f={f} set={set} k="max_jobs_per_repo" label="Max jobs per repository" hint="0 = no limit" />
            <NumField f={f} set={set} k="max_queued_runs_per_repo" label="Max queued runs per repository" hint="Older runs for the same branch are superseded first. 0 = no limit" />
          </div>
          <div className="mt-4">
            <Checkbox checked={f.cancel_superseded} onChange={(v) => setF({ ...f, cancel_superseded: v })} label="Cancel superseded runs" hint="A new push to a branch or pull request cancels its older runs (not on the default branch or tags)." />
          </div>
        </Section>
        <Section title="Runners">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <NumField f={f} set={set} k="runner_max_cpu_percent" label="Max CPU load (%)" hint="Runners above this get no new jobs. 0 = ignore load" />
            <NumField f={f} set={set} k="runner_offline_after_seconds" label="Offline after (seconds)" hint="A silent runner is marked offline and its jobs fail. Minimum 20" />
          </div>
        </Section>
        <Section
          title="Containers"
          description={
            <>
              Limits for each job's container. A workflow can ask for more with <code className="font-mono">resources</code>, up to the maximum.
            </>
          }
        >
          <div className="flex flex-col gap-4">
            <Field
              label="Default image"
              hint={
                <>
                  Used by jobs that don't set <code className="font-mono">container</code>.
                </>
              }
            >
              {(id) => <Input id={id} value={f.default_image} onChange={(e) => setF({ ...f, default_image: e.target.value })} className="font-mono" />}
            </Field>
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
              <NumField f={f} set={set} k="default_cpus" label="Default CPUs" step={0.5} />
              <NumField f={f} set={set} k="default_memory_mb" label="Default memory (MB)" />
              <NumField f={f} set={set} k="default_pids" label="Default process limit" />
              <NumField f={f} set={set} k="max_cpus" label="Max CPUs" step={0.5} />
              <NumField f={f} set={set} k="max_memory_mb" label="Max memory (MB)" />
              <NumField f={f} set={set} k="max_pids" label="Max process limit" />
            </div>
          </div>
        </Section>
        <Section title="Timeouts & logs">
          <div className="grid grid-cols-2 gap-4">
            <NumField f={f} set={set} k="default_timeout_minutes" label="Default job timeout (minutes)" />
            <NumField f={f} set={set} k="max_timeout_minutes" label="Max job timeout (minutes)" />
            <NumField f={f} set={set} k="max_log_mb" label="Max log size per job (MB)" hint="Output beyond this is dropped" />
            <NumField f={f} set={set} k="log_retention_days" label="Keep logs (days)" hint="0 = keep forever" />
          </div>
        </Section>
        <Section title="Storage & retention" description="Artifacts, caches and release files. Expired artifacts and unused caches are deleted automatically.">
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
            <NumField f={f} set={set} k="artifact_max_mb" label="Max artifact size (MB)" hint="Zipped size of one upload" />
            <NumField f={f} set={set} k="artifact_retention_days" label="Keep artifacts (days)" hint="Workflows may ask for less with retention-days." />
            <NumField f={f} set={set} k="release_asset_max_mb" label="Max release file size (MB)" />
            <NumField f={f} set={set} k="cache_quota_mb" label="Cache quota per repository (MB)" hint="Least recently used caches are evicted above it. 0 = no limit" />
            <NumField f={f} set={set} k="cache_retention_days" label="Delete unused caches after (days)" />
          </div>
        </Section>
        {save.isError && <Alert>{errorMessage(save.error)}</Alert>}
        <SaveBar
          dirty={dirty}
          saving={save.isPending}
          disabled={invalid}
          saveLabel="Save CI settings"
          onDiscard={() => q.data && setF(q.data)}
          message={invalid ? 'Fix the highlighted fields.' : dirty ? 'You have unsaved changes.' : save.isSuccess ? 'Saved.' : 'All changes saved.'}
        />
      </form>
      <div className="mt-8">
        <SecretsSection />
      </div>
    </Container>
  )
}
