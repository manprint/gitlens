// Units: 6.4 (QA-TRACE-001, QA-TRACE-002). Requirements: REQ-41.
// Node-only (Playwright project `harness`): no browser, no stack.
import { readdir, readFile } from 'node:fs/promises'
import { dirname, join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'

import { expect, test } from '@playwright/test'

import { checkRegister, EXEMPT_MARKER, type SourceFile } from './traceability'

test.describe.configure({ mode: 'parallel', retries: 0 })

const HERE = dirname(fileURLToPath(import.meta.url))
const REPO = join(HERE, '..', '..', '..')
const WEB = join(REPO, 'web')
const REGISTER = join(REPO, 'docs', 'plans', '005_plan-ConsoleInsights', 'overview.md')
const SKIP_DIRS = new Set(['node_modules', '.git', 'vendor', 'dist', '_artifacts', 'test-results'])
const CODE_EXT = /\.(?:ts|tsx|js|mjs|go)$/

async function walk(dir: string, accept: (rel: string) => boolean): Promise<string[]> {
  const out: string[] = []
  let entries
  try {
    entries = await readdir(dir, { withFileTypes: true })
  } catch {
    return out
  }
  for (const e of entries) {
    const full = join(dir, e.name)
    if (e.isDirectory()) {
      if (!SKIP_DIRS.has(e.name)) out.push(...(await walk(full, accept)))
    } else if (accept(relative(REPO, full))) {
      out.push(full)
    }
  }
  return out
}

async function testSources(): Promise<SourceFile[]> {
  const paths = new Set<string>()
  const add = async (root: string, accept: (rel: string) => boolean) => {
    for (const p of await walk(root, accept)) paths.add(p)
  }
  await add(join(WEB, 'e2e'), (r) => r.endsWith('.ts') && !r.startsWith('web/e2e/qa/traceability'))
  await add(join(WEB, 'src'), (r) => /\.test\.[a-z]+$/.test(r))
  await add(join(REPO, 'test'), (r) => CODE_EXT.test(r))
  await add(REPO, (r) => r.endsWith('_test.go'))
  return Promise.all(
    [...paths].map(async (p) => ({ path: relative(REPO, p), text: await readFile(p, 'utf8') })),
  )
}

const deferList = () =>
  (process.env.PGLENS_TRACE_DEFER ?? '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)

const HEADER = '| ID | Request | Owning units | Proof |\n|----|----|----|----|\n'
const row = (req: string, proof: string) => `| ${req} | r | u | ${proof} |\n`
const src = (path: string, text: string): SourceFile => ({ path, text })

test('QA-TRACE-001 every REQ row of the overview register resolves in test sources', async () => {
  const register = await readFile(REGISTER, 'utf8')
  const files = await testSources()
  expect(files.length).toBeGreaterThan(0)
  expect(files.some((f) => f.path.startsWith('web/e2e/qa/traceability'))).toBe(false)

  const report = checkRegister(register, files, deferList())
  expect(report.rows.length).toBeGreaterThan(0)
  // A regex that matches nothing can never pass: every row yields a token or the marker.
  const yieldless = report.rows.filter((r) => r.tokens.length === 0 && !r.hasMarker)
  expect(yieldless.map((r) => r.req)).toEqual([])
  if (report.deferred.length > 0) {
    test.info().annotations.push({ type: 'deferred', description: report.deferred.join(',') })
  }
  expect(report.failures).toEqual([])
})

test.describe('QA-TRACE-002 negative controls (synthetic sources only)', () => {
  // Built from pieces so the literal never appears verbatim in this file.
  const zzz = ['ZZZ', '999'].join('-')
  const zzzPrefix = ['ZZZ', '*'].join('-')
  const fooPrefix = ['FOO', '*'].join('-')
  const fooId = ['FOO', '001'].join('-')

  test('QA-TRACE-002 unknown ID and unknown prefix are reported uncovered', () => {
    const register = HEADER + row('REQ-04', `\`${zzz}\``) + row('REQ-05', `\`${zzzPrefix}\``)
    const r = checkRegister(register, [src('a.spec.ts', `test('OTHER-001', () => {})`)])
    expect(r.uncovered).toEqual(['REQ-04', 'REQ-05'])
    expect(r.failures).toHaveLength(2)
  })

  test('QA-TRACE-002 prefix token passes when a source holds a matching literal', () => {
    const register = HEADER + row('REQ-04', `\`${fooPrefix}\``)
    const ok = checkRegister(register, [src('a.spec.ts', `test('${fooId} works', () => {})`)])
    expect(ok.failures).toEqual([])
    expect(ok.rows[0].status).toBe('covered')
    const comment = checkRegister(register, [src('a.spec.ts', `// ${fooId}\n/* ${fooId} */`)])
    expect(comment.uncovered).toEqual(['REQ-04'])
  })

  test('QA-TRACE-002 Go hyphen-drop names resolve, comments do not', () => {
    const register = HEADER + row('REQ-20', '`INT-RO-001`') + row('REQ-04', '`IDXE-INT-*`')
    const go = src('x_test.go', 'package x\nfunc TestINTRO001(t *testing.T) {}\n')
    const goPrefix = src('y_test.go', 'package x\nfunc TestIDXEINTFoo(t *testing.T) {}\n')
    expect(checkRegister(register, [go, goPrefix]).failures).toEqual([])
    const commented = src('z_test.go', '// func TestINTRO001(t *testing.T)\n')
    expect(checkRegister(register, [commented]).uncovered).toEqual(['REQ-20', 'REQ-04'])
  })

  test('QA-TRACE-002 exempt marker passes on an exempt row and fails elsewhere', () => {
    const ok = checkRegister(HEADER + row('REQ-01', `G-STACK (${EXEMPT_MARKER})`), [])
    expect(ok.failures).toEqual([])
    expect(ok.rows[0].status).toBe('exempt')
    const bad = checkRegister(HEADER + row('REQ-04', `X (${EXEMPT_MARKER})`), [])
    expect(bad.rows[0].status).toBe('invalid')
    expect(bad.failures).toHaveLength(1)
  })

  test('QA-TRACE-002 a row with no token and no marker fails; deferral is reported not failed', () => {
    const none = checkRegister(HEADER + row('REQ-04', 'plain words'), [])
    expect(none.failures).toHaveLength(1)
    const reg = HEADER + row('REQ-21', `\`${zzzPrefix}\``)
    const deferred = checkRegister(reg, [], ['REQ-21'])
    expect(deferred.failures).toEqual([])
    expect(deferred.deferred).toEqual(['REQ-21'])
    expect(checkRegister(reg, []).uncovered).toEqual(['REQ-21'])
  })
})
