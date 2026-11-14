import { readCookie } from '../api/client'

/**
 * Sends a file as the raw request body (with the CSRF header) and reports
 * upload progress. Rejects with the API's error message on failure.
 */
export function sendFile(url: string, file: Blob, opts: { method?: 'PUT' | 'POST'; onProgress?: (fraction: number) => void; name?: string } = {}): Promise<void> {
  const label = opts.name ? `${opts.name}: ` : ''
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open(opts.method ?? 'PUT', url)
    xhr.withCredentials = true
    xhr.setRequestHeader('Content-Type', 'application/octet-stream')
    xhr.setRequestHeader('X-CSRF-Token', readCookie('tgp_csrf'))
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) opts.onProgress?.(e.loaded / e.total)
    }
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) return resolve()
      reject(new Error(label + errorText(xhr.responseText, xhr.statusText)))
    }
    xhr.onerror = () => reject(new Error(`${label}network error`))
    xhr.send(file)
  })
}

/** The API's error message from a JSON error body, else a fallback. */
export function errorText(body: string, fallback: string): string {
  try {
    return (JSON.parse(body) as { error?: { message?: string } }).error?.message ?? (fallback || 'Upload failed')
  } catch {
    return fallback || 'Upload failed'
  }
}

/** Uploads the organization logo. */
export function uploadLogo(file: File): Promise<void> {
  return sendFile('/api/v1/admin/org/logo', file)
}
