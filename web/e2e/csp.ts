import { expect, type BrowserContext, type TestType } from '@playwright/test'

// Content-Security-Policy violations logged by watched pages since the
// last check.
const violations: string[] = []

/** Records CSP violations from every page in ctx. */
export function watchCSP(ctx: BrowserContext) {
  ctx.on('console', (m) => {
    if (m.type() === 'error' && /Content Security Policy/i.test(m.text())) violations.push(m.text())
  })
}

/**
 * Fails each test whose pages logged a CSP violation, so UI changes that
 * break under the server's policy show up here instead of silently in
 * browsers. Call once per spec file; contexts a spec creates itself need
 * watchCSP too.
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function guardCSP(test: TestType<any, any>) {
  test.beforeEach(({ context }) => watchCSP(context))
  test.afterEach(() => {
    expect(violations.splice(0), 'Content-Security-Policy violations').toEqual([])
  })
}
