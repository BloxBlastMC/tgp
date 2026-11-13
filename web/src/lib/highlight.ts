import type { BundledLanguage, BundledTheme, HighlighterGeneric } from 'shiki'

/**
 * Lazy Shiki highlighting. The highlighter and each language load on first
 * use, so the main bundle stays small. Output uses CSS variables for both
 * themes (see .shiki rules in index.css).
 */
let highlighter: Promise<HighlighterGeneric<BundledLanguage, BundledTheme>> | null = null

const byExt: Record<string, string> = {
  ts: 'typescript', tsx: 'tsx', js: 'javascript', jsx: 'jsx', mjs: 'javascript', cjs: 'javascript',
  go: 'go', rs: 'rust', java: 'java', kt: 'kotlin', kts: 'kotlin', py: 'python', rb: 'ruby', php: 'php',
  c: 'c', h: 'c', cc: 'cpp', cpp: 'cpp', hpp: 'cpp', cs: 'csharp', swift: 'swift', scala: 'scala',
  sh: 'shellscript', bash: 'shellscript', zsh: 'shellscript', ps1: 'powershell',
  json: 'json', jsonc: 'jsonc', yml: 'yaml', yaml: 'yaml', toml: 'toml', xml: 'xml', html: 'html',
  css: 'css', scss: 'scss', less: 'less', md: 'markdown', markdown: 'markdown', sql: 'sql',
  graphql: 'graphql', proto: 'proto', lua: 'lua', vue: 'vue', svelte: 'svelte', gradle: 'groovy',
  groovy: 'groovy', properties: 'properties', ini: 'ini', dockerfile: 'docker', tf: 'terraform',
  mk: 'make', diff: 'diff', patch: 'diff', mcfunction: 'mcfunction',
}

const byName: Record<string, string> = {
  dockerfile: 'docker', makefile: 'make', 'cmakelists.txt': 'cmake', justfile: 'just', '.gitignore': 'shellscript',
}

export function languageFor(path: string): string {
  const name = path.split('/').pop()!.toLowerCase()
  if (byName[name]) return byName[name]
  const ext = name.includes('.') ? name.split('.').pop()! : ''
  return byExt[ext] ?? 'text'
}

/** Highlights code, returning HTML lines (one string per source line). */
export async function highlightLines(code: string, path: string): Promise<string[] | null> {
  const lang = languageFor(path)
  if (lang === 'text' || code.length > 500_000) return null
  highlighter ??= import('shiki').then(({ createHighlighter }) =>
    createHighlighter({ themes: ['github-dark-dimmed', 'github-light'], langs: [] }),
  )
  const h = await highlighter
  try {
    if (!h.getLoadedLanguages().includes(lang)) await h.loadLanguage(lang as BundledLanguage)
  } catch {
    return null
  }
  const tokens = h.codeToTokens(code, {
    lang: lang as BundledLanguage,
    themes: { dark: 'github-dark-dimmed', light: 'github-light' },
    defaultColor: false,
  })
  const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
  return tokens.tokens.map((line) =>
    line
      .map((t) => {
        const style = Object.entries(t.htmlStyle ?? {})
          .map(([k, v]) => `${k}:${v}`)
          .join(';')
        return style ? `<span style="${style}">${esc(t.content)}</span>` : esc(t.content)
      })
      .join(''),
  )
}
