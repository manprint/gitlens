import axe from 'axe-core'

/**
 * Detached jsdom fragments have no surrounding page landmarks and no computed
 * styles. The Playwright suite re-enables both rules in a real browser.
 */
export const A11Y_DISABLED_RULES = ['region', 'color-contrast'] as const

export type A11yOptions = Omit<axe.RunOptions, 'resultTypes' | 'rules'> & {
  rules?: axe.RunOptions['rules']
}

function truncateOuterHTML(html: string): string {
  return html.length > 200 ? `${html.slice(0, 197)}...` : html
}

function formatViolation(violation: axe.Result): string {
  const nodes = violation.nodes
    .map((node) => `  element: ${truncateOuterHTML(node.html)}`)
    .join('\n')
  return `${violation.id} (${violation.impact}) ${violation.helpUrl}\n${nodes}`
}

export function filterA11yViolations(result: axe.AxeResults): axe.Result[] {
  return result.violations.filter(
    (violation) => violation.impact === 'serious' || violation.impact === 'critical',
  )
}

export async function expectNoA11yViolations(
  container: HTMLElement,
  options: A11yOptions = {},
): Promise<void> {
  const result = await axe.run<axe.AxeResults>(container, {
    ...options,
    resultTypes: ['violations'],
    rules: {
      ...(options.rules ?? {}),
      region: { enabled: false },
      'color-contrast': { enabled: false },
    },
  })
  const violations = filterA11yViolations(result)

  if (violations.length > 0) {
    throw new Error(`Accessibility violations:\n${violations.map(formatViolation).join('\n')}`)
  }
}
