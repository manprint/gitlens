// @vitest-environment node
import { ESLint } from 'eslint'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const WEB_ROOT = resolve(__dirname, '../..')

describe('untranslated literal scan', () => {
  it('UI-I18N-007-01 the literal rule covers the plan 007 feature folders logs and workbook', async () => {
    const eslint = new ESLint({ cwd: WEB_ROOT })
    for (const file of [
      'src/features/logs/LogsPage.tsx',
      'src/features/logs/LogClassSummary.tsx',
      'src/features/workbook/WorkbookPage.tsx',
      'src/features/workbook/VariantEditor.tsx',
    ]) {
      const config = (await eslint.calculateConfigForFile(resolve(WEB_ROOT, file))) as {
        rules?: Record<string, unknown>
      }
      const setting = config.rules?.['pglens/no-literal-string']
      const level: unknown = Array.isArray(setting) ? (setting as unknown[])[0] : setting
      expect([2, 'error'], file).toContain(level)
    }
  })
})
