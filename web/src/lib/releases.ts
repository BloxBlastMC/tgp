import { readCookie, type Release } from '../api/client'

const enc = encodeURIComponent

/** The release's title: its name, or the tag when unnamed. */
export function releaseTitle(r: Pick<Release, 'name' | 'tag'>): string {
  return r.name.trim() || r.tag
}

export function archiveUrl(repo: string, ref: string, format: 'zip' | 'tar.gz'): string {
  return `/api/v1/repos/${enc(repo)}/archive?ref=${enc(ref)}&format=${format}`
}

export function assetUrl(repo: string, releaseId: number, assetId: number): string {
  return `/api/v1/repos/${enc(repo)}/releases/${releaseId}/assets/${assetId}`
}

export function artifactZipUrl(repo: string, id: number): string {
  return `/api/v1/repos/${enc(repo)}/artifacts/${id}/zip`
}

/** "expires in 3 days", "expires in 5 hours", "expired". */
export function expiresText(iso: string, now = Date.now()): string {
  const ms = new Date(iso).getTime() - now
  if (!Number.isFinite(ms) || ms <= 0) return 'expired'
  const h = ms / 3_600_000
  if (h < 1) return `expires in ${Math.max(1, Math.round(ms / 60_000))} min`
  if (h < 48) {
    const n = Math.round(h)
    return `expires in ${n} hour${n === 1 ? '' : 's'}`
  }
  const d = Math.round(h / 24)
  return `expires in ${d} days`
}

/** Percentage (0–100) of the quota used; null when there's no quota. */
export function usagePercent(used: number, quota: number): number | null {
  if (!quota || quota <= 0) return null
  return Math.min(100, Math.max(0, (used / quota) * 100))
}

/** Badge labels for a release, in display order. */
export function releaseBadges(r: Pick<Release, 'draft' | 'prerelease' | 'latest'>): ('Draft' | 'Pre-release' | 'Latest')[] {
  const out: ('Draft' | 'Pre-release' | 'Latest')[] = []
  if (r.draft) out.push('Draft')
  if (r.prerelease) out.push('Pre-release')
  if (r.latest && !r.draft) out.push('Latest')
  return out
}

/** Tag names that git accepts (a practical subset of check-ref-format). */
export function tagError(tag: string): string | null {
  const t = tag.trim()
  if (!t) return 'Enter a tag'
  if (t !== tag) return "Tags can't start or end with spaces"
  if (/[\s~^:?*[\\]|\.\.|@\{|\/\/|^\/|\/$|\.lock$|^-|\.$/.test(t) || /[\x00-\x1f\x7f]/.test(t)) return `"${t}" isn't a valid tag name`
  return null
}

/**
 * Uploads a file as a release asset with progress (fetch can't report
 * upload progress, so this uses XMLHttpRequest).
 */
export function uploadAsset(repo: string, releaseId: number, file: File, onProgress?: (fraction: number) => void, overwrite = false): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    const q = `name=${enc(file.name)}${overwrite ? '&overwrite=true' : ''}`
    xhr.open('POST', `/api/v1/repos/${enc(repo)}/releases/${releaseId}/assets?${q}`)
    xhr.withCredentials = true
    xhr.setRequestHeader('Content-Type', 'application/octet-stream')
    xhr.setRequestHeader('X-CSRF-Token', readCookie('tgp_csrf'))
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) onProgress?.(e.loaded / e.total)
    }
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) return resolve()
      let msg = xhr.statusText || 'Upload failed'
      try {
        msg = (JSON.parse(xhr.responseText) as { error?: { message?: string } }).error?.message ?? msg
      } catch {
        /* not JSON */
      }
      reject(new Error(`${file.name}: ${msg}`))
    }
    xhr.onerror = () => reject(new Error(`${file.name}: network error`))
    xhr.send(file)
  })
}
