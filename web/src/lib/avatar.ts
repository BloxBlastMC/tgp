// Initials avatars: a stable colour per username and up to two initials.

export const AVATAR_HUES = 8

/** 1..AVATAR_HUES, stable for a given key. */
export function avatarHue(key: string): number {
  let h = 0
  for (let i = 0; i < key.length; i++) h = (h * 31 + key.charCodeAt(i)) >>> 0
  return (h % AVATAR_HUES) + 1
}

/** "Mia Chen" → "MC", "tom" → "T". */
export function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return '?'
  const first = parts[0]![0] ?? ''
  const last = parts.length > 1 ? (parts[parts.length - 1]![0] ?? '') : ''
  return (first + last).toUpperCase()
}
