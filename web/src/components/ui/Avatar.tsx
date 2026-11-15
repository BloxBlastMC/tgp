import { avatarHue, initials } from '../../lib/avatar'
import { cx } from './cx'

type AvatarUser = { display_name?: string; username: string }

/** Initials on a stable per-user colour. Decorative: pair with a visible name. */
export function Avatar({ user, size = 20, className, title = true }: { user: AvatarUser; size?: number; className?: string; title?: boolean }) {
  const name = user.display_name || user.username
  const text = size < 16 ? initials(name).slice(0, 1) : initials(name)
  return (
    <span
      className={cx('inline-flex shrink-0 items-center justify-center rounded-full font-semibold text-white select-none', className)}
      style={{
        width: size,
        height: size,
        fontSize: Math.max(8, Math.round(size * (text.length > 1 ? 0.4 : 0.48))),
        backgroundColor: `var(--avatar-${avatarHue(user.username)})`,
        letterSpacing: text.length > 1 ? '-0.02em' : undefined,
      }}
      title={title ? name : undefined}
      aria-hidden
    >
      {text}
    </span>
  )
}

/** Overlapping avatars, with a +N for the rest. */
export function AvatarStack({ users, max = 3, size = 18 }: { users: AvatarUser[]; max?: number; size?: number }) {
  if (users.length === 0) return null
  const shown = users.slice(0, max)
  const rest = users.length - shown.length
  return (
    <span className="inline-flex items-center" title={users.map((u) => u.display_name || u.username).join(', ')}>
      {shown.map((u, i) => (
        <span key={u.username} className={cx('relative inline-flex rounded-full', i > 0 && '-ml-1')} style={{ zIndex: shown.length - i }}>
          <Avatar user={u} size={size} title={false} className="ring-2 ring-bg" />
        </span>
      ))}
      {rest > 0 && (
        <span
          className="-ml-1 inline-flex items-center justify-center rounded-full bg-bg-active text-2xs font-semibold text-fg-muted ring-2 ring-bg"
          style={{ width: size, height: size }}
          aria-hidden
        >
          +{rest}
        </span>
      )}
    </span>
  )
}
