/** Joins class names, skipping falsy values. */
export function cx(...c: unknown[]): string {
  return c.filter((x): x is string => typeof x === 'string' && x !== '').join(' ')
}
