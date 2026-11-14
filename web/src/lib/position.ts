// Positioning for floating panels (menus, popovers, tooltips): place next to
// an anchor, flip to the other side when there's no room, and clamp into the
// viewport.

export type Side = 'top' | 'bottom' | 'left' | 'right'
export type Align = 'start' | 'center' | 'end'
export type Placement = Side | `${Side}-${Exclude<Align, 'center'>}`

type Rect = { top: number; left: number; width: number; height: number }

export function parsePlacement(p: Placement): [Side, Align] {
  const [side, align] = p.split('-') as [Side, Align | undefined]
  return [side, align ?? 'center']
}

const opposite: Record<Side, Side> = { top: 'bottom', bottom: 'top', left: 'right', right: 'left' }

export function computePosition(
  anchor: Rect,
  panel: { width: number; height: number },
  placement: Placement,
  viewport: { width: number; height: number },
  gap = 4,
  margin = 8,
): { top: number; left: number; side: Side } {
  let [side, align] = parsePlacement(placement)
  const fits = (s: Side) => {
    if (s === 'bottom') return anchor.top + anchor.height + gap + panel.height <= viewport.height - margin
    if (s === 'top') return anchor.top - gap - panel.height >= margin
    if (s === 'right') return anchor.left + anchor.width + gap + panel.width <= viewport.width - margin
    return anchor.left - gap - panel.width >= margin
  }
  if (!fits(side) && fits(opposite[side])) side = opposite[side]

  let top: number
  let left: number
  if (side === 'top' || side === 'bottom') {
    top = side === 'bottom' ? anchor.top + anchor.height + gap : anchor.top - gap - panel.height
    left =
      align === 'start' ? anchor.left : align === 'end' ? anchor.left + anchor.width - panel.width : anchor.left + (anchor.width - panel.width) / 2
  } else {
    left = side === 'right' ? anchor.left + anchor.width + gap : anchor.left - gap - panel.width
    top =
      align === 'start' ? anchor.top : align === 'end' ? anchor.top + anchor.height - panel.height : anchor.top + (anchor.height - panel.height) / 2
  }
  const clamp = (v: number, size: number, max: number) => Math.max(margin, Math.min(v, max - size - margin))
  return { top: Math.round(clamp(top, panel.height, viewport.height)), left: Math.round(clamp(left, panel.width, viewport.width)), side }
}
