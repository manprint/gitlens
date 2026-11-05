import { readFileSync, readdirSync } from 'node:fs'
import { join, relative } from 'node:path'

import ts from 'typescript'
import { describe, expect, it } from 'vitest'

import { isTileUnit } from './units'

/**
 * Static coverage of captions and of the card contract (D30, D31): every
 * `<MetricTile>`, `<ChartCard>`, `<Section>` and `<DataTable>` usage carries a
 * literal `captionId`, and no `<MetricTile>` puts a number or free text in its
 * `unit`. The files are parsed with TypeScript, so attributes holding arrows,
 * quotes or nested JSX are read exactly.
 *
 * A usage counts as captioned only with `captionId="…"`. A computed
 * `captionId={…}` (a chart or table spec) is "dynamic": it is listed in
 * `DYNAMIC_ALLOWLIST` and the unit that adds it must also test, at run time,
 * that every spec of the list has a caption. A spread (`{...props}`) without
 * `captionId` counts as missing. A wrapper component that renders one of these
 * elements for its callers must itself take a required `captionId`.
 *
 * The allowlists hold the usages that existed when 17.1 added the rule: debt
 * that 17.2 empties. Their counts must match exactly, so a usage that gains a
 * caption removes its allowance (entries are only ever removed) and a new
 * usage without one fails.
 */

const ROOT = join(__dirname, '..')
const TAGS = ['MetricTile', 'ChartCard', 'Section', 'DataTable'] as const
type Tag = (typeof TAGS)[number]

function sourceFiles(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name)
    if (entry.isDirectory()) return sourceFiles(path)
    return entry.name.endsWith('.tsx') && !entry.name.includes('.test.') ? [path] : []
  })
}

interface Usage {
  tag: string
  caption: 'literal' | 'dynamic' | 'missing'
  unit: ts.JsxAttributeValue | undefined | null
}

/** Every opening element of the given tags in a TSX source. */
export function usages(source: string, file = 'x.tsx'): Usage[] {
  const tree = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX)
  const found: Usage[] = []
  const visit = (node: ts.Node) => {
    if (ts.isJsxOpeningElement(node) || ts.isJsxSelfClosingElement(node)) {
      const tag = node.tagName.getText(tree)
      if ((TAGS as readonly string[]).includes(tag)) {
        const attribute = (name: string) =>
          node.attributes.properties.find(
            (property): property is ts.JsxAttribute =>
              ts.isJsxAttribute(property) && property.name.getText(tree) === name,
          )
        const caption = attribute('captionId')
        found.push({
          tag,
          caption:
            caption === undefined
              ? 'missing'
              : caption.initializer !== undefined && ts.isStringLiteral(caption.initializer)
                ? 'literal'
                : 'dynamic',
          unit: attribute('unit') === undefined ? null : attribute('unit')?.initializer,
        })
      }
    }
    ts.forEachChild(node, visit)
  }
  visit(tree)
  return found
}

/**
 * A unit that is not one of the closed list (D31): a template literal, a
 * number, a string outside the list, or a formatted number. `formatUnit` is
 * the allowed way to show a listed unit.
 */
export function badUnit(value: ts.JsxAttributeValue | undefined): boolean {
  if (value === undefined) return false
  let bad = false
  const visit = (node: ts.Node) => {
    if (ts.isTemplateExpression(node) || ts.isNoSubstitutionTemplateLiteral(node)) bad = true
    else if (ts.isNumericLiteral(node)) bad = true
    else if (ts.isJsxText(node) && node.text.trim() !== '') bad = true
    else if (ts.isStringLiteral(node) && !isTileUnit(node.text)) bad = true
    else if (
      ts.isCallExpression(node) &&
      ts.isIdentifier(node.expression) &&
      /^format[A-Z]/.test(node.expression.text) &&
      node.expression.text !== 'formatUnit'
    ) {
      bad = true
    }
    ts.forEachChild(node, visit)
  }
  visit(value)
  return bad
}

function scan() {
  const missing: Record<string, Partial<Record<Tag, number>>> = {}
  const dynamic: Record<string, Partial<Record<Tag, number>>> = {}
  const badUnits: Record<string, number> = {}
  const files = [...sourceFiles(join(ROOT, 'features')), ...sourceFiles(join(ROOT, 'components'))]
  for (const file of files) {
    const name = relative(ROOT, file)
    for (const usage of usages(readFileSync(file, 'utf8'), file)) {
      const tag = usage.tag as Tag
      if (usage.caption === 'missing') (missing[name] ??= {})[tag] = (missing[name]?.[tag] ?? 0) + 1
      if (usage.caption === 'dynamic') (dynamic[name] ??= {})[tag] = (dynamic[name]?.[tag] ?? 0) + 1
      if (tag === 'MetricTile' && usage.unit !== null && badUnit(usage.unit)) {
        badUnits[name] = (badUnits[name] ?? 0) + 1
      }
    }
  }
  return { missing, dynamic, badUnits }
}

/** Usages without a caption: none since 17.2 emptied the list; a new usage must carry one. */
const CAPTION_ALLOWLIST: Record<string, Partial<Record<Tag, number>>> = {}

/** Computed caption ids; each needs a run-time test that its specs all have one. */
const DYNAMIC_ALLOWLIST: Record<string, Partial<Record<Tag, number>>> = {
  'features/ash/AshBreakdown.tsx': { ChartCard: 1 },
  'features/ash/AshChartGrid.tsx': { ChartCard: 1 },
  'features/instance/CatalogSection.tsx': { DataTable: 2 },
  'features/instance/HostCharts.tsx': { ChartCard: 2 },
  'features/instance/HostSection.tsx': { MetricTile: 1 },
  'features/instance/OverviewCharts.tsx': { ChartCard: 1 },
}

/** `MetricTile` usages with a number or free text in `unit`: none since 17.2. */
const CARD_ALLOWLIST: Record<string, number> = {}

describe('caption and card coverage (D30, D31)', () => {
  const result = scan()

  it('CAPTION-COVERAGE-001 every captioned element has a literal caption id or an allowance', () => {
    expect(result.missing).toEqual(CAPTION_ALLOWLIST)
    expect(result.dynamic).toEqual(DYNAMIC_ALLOWLIST)
  })

  it('CARD-COVERAGE-001 no metric tile puts a number or free text in its unit', () => {
    expect(result.badUnits).toEqual(CARD_ALLOWLIST)
  })

  it('CAPTION-COVERAGE-001 the scanner reads tags exactly', () => {
    const source = `const a = <>
      <MetricTile label="A" value={rows.map((r) => r.x)} unit={\`\${n} redundant\`} />
      <Section title="B" actions={<Button onClick={() => go()}>Don't</Button>}>x</Section>
      <Section title="C" captionId="index.duplicate">x</Section>
      <ChartCard {...common} captionId={spec.captionId} />
      <DataTable {...table} />
      <SectionHeader title="not a Section" />
      {/* <MetricTile label="in a comment" /> */}
    </>`
    expect(usages(source).map((usage) => [usage.tag, usage.caption])).toEqual([
      ['MetricTile', 'missing'],
      ['Section', 'missing'],
      ['Section', 'literal'],
      ['ChartCard', 'dynamic'],
      ['DataTable', 'missing'],
    ])
  })

  it('CARD-COVERAGE-001 the unit rule accepts listed units only', () => {
    const unitOf = (jsx: string) => usages(`const a = ${jsx}`)[0]!.unit ?? undefined
    expect(badUnit(unitOf('<MetricTile unit={`${n} redundant`} />'))).toBe(true)
    expect(badUnit(unitOf("<MetricTile unit={pending ? 'loading…' : undefined} />"))).toBe(true)
    expect(badUnit(unitOf('<MetricTile unit={formatCount(n)} />'))).toBe(true)
    expect(badUnit(unitOf('<MetricTile unit="/s" />'))).toBe(true)
    expect(badUnit(unitOf('<MetricTile unit={<>of max</>} />'))).toBe(true)
    expect(badUnit(unitOf("<MetricTile unit={'ms'} />"))).toBe(false)
    expect(badUnit(unitOf('<MetricTile unit="per second" />'))).toBe(false)
    expect(badUnit(unitOf("<MetricTile unit={formatUnit('per second')} />"))).toBe(false)
  })
})
