// Units: 6.4 (QA-TRACE-001, QA-TRACE-002). Requirements: REQ-41.
// Pure requirement-register traceability check: no fs, env or docker access.

export const EXEMPT_MARKER = 'Exempt (manual evidence)'
export const EXEMPT_ROWS: readonly string[] = [
  'REQ-01',
  'REQ-02',
  'REQ-03',
  'REQ-17',
  'REQ-18',
  'REQ-22',
  'REQ-23',
  'REQ-24',
  'REQ-25',
]

const TOKEN_RE = /\b[A-Z][A-Z0-9]+(?:-[A-Z0-9]+)*-(?:\d{2,3}\b|\*)/g
const ROW_RE = /^\|\s*(REQ-\d{2})\s*\|/
const HEADER_RE = /^\|\s*ID\s*\|.*\bProof\b/

export interface SourceFile {
  path: string
  text: string
}

export type RowStatus = 'covered' | 'exempt' | 'deferred' | 'uncovered' | 'invalid'

export interface RowResult {
  req: string
  status: RowStatus
  proof: string
  tokens: string[]
  hasMarker: boolean
  resolved: string[]
  reasons: string[]
}

export interface RegisterReport {
  rows: RowResult[]
  failures: string[]
  uncovered: string[]
  deferred: string[]
}

const splitCells = (line: string): string[] =>
  line
    .trim()
    .replace(/^\|/, '')
    .replace(/\|$/, '')
    .split('|')
    .map((c) => c.trim())

export function extractTokens(proof: string): string[] {
  return [...new Set(proof.match(TOKEN_RE) ?? [])]
}

interface Scanned {
  code: string
  strings: string[]
}

/**
 * Minimal lexer: blanks comments out of `code` (strings kept) and collects string literal
 * bodies. `regexLiterals` enables the TS/JS heuristic for `/re/` so quotes inside a regex
 * do not open a bogus string.
 */
function scan(src: string, regexLiterals: boolean): Scanned {
  let code = ''
  const strings: string[] = []
  const n = src.length
  let i = 0
  let prev = ''
  while (i < n) {
    const c = src[i]
    const d = src[i + 1]
    if (c === '/' && d === '/') {
      while (i < n && src[i] !== '\n') i++
      continue
    }
    if (c === '/' && d === '*') {
      const end = src.indexOf('*/', i + 2)
      const stop = end === -1 ? n : end + 2
      code += src.slice(i, stop).replace(/[^\n]/g, ' ')
      i = stop
      continue
    }
    if (c === '"' || c === "'" || c === '`') {
      const quote = c
      let j = i + 1
      let body = ''
      while (j < n) {
        const e = src[j]
        if (e === '\\' && (quote !== '`' || regexLiterals)) {
          body += src.slice(j, j + 2)
          j += 2
          continue
        }
        if (e === quote) break
        if (e === '\n' && quote !== '`') break
        body += e
        j++
      }
      strings.push(body)
      code += src.slice(i, j + 1)
      i = j + 1
      prev = quote
      continue
    }
    if (regexLiterals && c === '/' && (prev === '' || '(,=:[!&|?{};'.includes(prev))) {
      let j = i + 1
      let inClass = false
      while (j < n && src[j] !== '\n') {
        const e = src[j]
        if (e === '\\') {
          j += 2
          continue
        }
        if (e === '[') inClass = true
        else if (e === ']') inClass = false
        else if (e === '/' && !inClass) break
        j++
      }
      code += src.slice(i, j + 1)
      i = j + 1
      prev = '/'
      continue
    }
    code += c
    if (!/\s/.test(c)) prev = c
    i++
  }
  return { code, strings }
}

const escapeRe = (s: string): string => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

interface Indexed {
  strings: string[]
  goCode: string[]
}

function indexSources(files: SourceFile[]): Indexed {
  const strings: string[] = []
  const goCode: string[] = []
  for (const f of files) {
    if (f.path.endsWith('.go')) {
      const s = scan(f.text, false)
      strings.push(...s.strings)
      goCode.push(s.code)
    } else {
      strings.push(...scan(f.text, true).strings)
    }
  }
  return { strings, goCode }
}

function tokenResolves(token: string, idx: Indexed): boolean {
  const prefix = token.endsWith('-*')
  const literal = prefix ? token.slice(0, -1) : token
  const lit = prefix
    ? new RegExp(`(?<![A-Za-z0-9_-])${escapeRe(literal)}`)
    : new RegExp(`(?<![A-Za-z0-9_-])${escapeRe(literal)}(?!\\d)`)
  if (idx.strings.some((s) => lit.test(s))) return true
  const goName = literal.replace(/-/g, '')
  const fn = prefix
    ? new RegExp(`\\bfunc\\s+Test${escapeRe(goName)}`)
    : new RegExp(`\\bfunc\\s+Test${escapeRe(goName)}(?![0-9])`)
  return idx.goCode.some((c) => fn.test(c))
}

export function checkRegister(
  registerText: string,
  sourceFiles: SourceFile[],
  defer: readonly string[] = [],
): RegisterReport {
  const lines = registerText.split('\n')
  const failures: string[] = []
  const rows: RowResult[] = []
  const headerLine = lines.find((l) => HEADER_RE.test(l))
  if (headerLine === undefined) {
    return {
      rows,
      failures: ['register header with an ID and a Proof column not found'],
      uncovered: [],
      deferred: [],
    }
  }
  const proofCol = splitCells(headerLine).findIndex((c) => c === 'Proof')
  const idx = indexSources(sourceFiles)
  const deferred = new Set(defer.map((d) => d.trim()).filter(Boolean))

  for (const line of lines) {
    const m = ROW_RE.exec(line)
    if (!m) continue
    const req = m[1]
    const proof = splitCells(line)[proofCol] ?? ''
    const tokens = extractTokens(proof)
    const hasMarker = proof.includes(EXEMPT_MARKER)
    const reasons: string[] = []
    let status: RowStatus = 'covered'
    let resolved: string[] = []

    if (hasMarker && !EXEMPT_ROWS.includes(req)) {
      status = 'invalid'
      reasons.push(`${req} carries "${EXEMPT_MARKER}" but is not an exempt row`)
    } else if (!hasMarker && tokens.length === 0) {
      status = 'invalid'
      reasons.push(`${req} Proof yields no test-ID token and no exempt marker`)
    } else if (hasMarker) {
      status = 'exempt'
    } else {
      resolved = tokens.filter((t) => tokenResolves(t, idx))
      if (resolved.length === 0) {
        if (deferred.has(req)) {
          status = 'deferred'
        } else {
          status = 'uncovered'
          reasons.push(`${req} no token resolves in test sources: ${tokens.join(', ')}`)
        }
      }
    }
    rows.push({ req, status, proof, tokens, hasMarker, resolved, reasons })
    failures.push(...reasons)
  }

  if (rows.length === 0) failures.push('no REQ rows found in the register')
  for (const d of deferred) {
    if (!rows.some((r) => r.req === d)) failures.push(`deferred ${d} is not a register row`)
  }
  return {
    rows,
    failures,
    uncovered: rows.filter((r) => r.status === 'uncovered').map((r) => r.req),
    deferred: rows.filter((r) => r.status === 'deferred').map((r) => r.req),
  }
}
