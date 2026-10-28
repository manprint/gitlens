import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

import { ASH_PALETTE } from './ash.options'
import { buildChartTheme, oklchToRgb, resolveTokens } from './chartTheme'
import { LAG_PALETTE } from './lag.options'

const css = readFileSync(
  resolve(dirname(fileURLToPath(import.meta.url)), '../../index.css'),
  'utf8',
)

function readerOf(tokens: Record<string, string>) {
  return (name: string) => tokens[name] ?? ''
}

describe('chartTheme', () => {
  it('turns oklch tokens into rgb, which the canvas renderer can parse', () => {
    expect(oklchToRgb('oklch(1 0 0)')).toBe('rgb(255, 255, 255)')
    expect(oklchToRgb('oklch(0 0 0)')).toBe('rgb(0, 0, 0)')
    expect(oklchToRgb('oklch(0.628 0.2577 29.23)')).toBe('rgb(255, 0, 0)')
    expect(oklchToRgb('oklch(50% 0 0 / 0.5)')).toBe('rgba(99, 99, 99, 0.5)')
    expect(oklchToRgb('#ff0000')).toBe('#ff0000')
  })

  it('resolves var() colours throughout an option without touching the original', () => {
    const formatter = () => 'label'
    const option = {
      color: ['var(--a)', 'var(--missing)'],
      series: [{ itemStyle: { color: 'var(--b)' }, name: 'var(--a) is text' }],
      tooltip: { formatter },
    }

    const resolved = resolveTokens(option, readerOf({ '--a': 'oklch(1 0 0)', '--b': '#123456' }))

    expect(resolved).toEqual({
      color: ['rgb(255, 255, 255)', 'var(--missing)'],
      series: [{ itemStyle: { color: '#123456' }, name: 'var(--a) is text' }],
      tooltip: { formatter },
    })
    expect(resolved.tooltip.formatter).toBe(formatter)
    expect(option.color[0]).toBe('var(--a)')
  })

  it('draws axes, legend and tooltip from the page tokens', () => {
    const theme = buildChartTheme(
      readerOf({
        '--border': '#888888',
        '--card': '#222222',
        '--font-ui': 'Inter Variable, sans-serif',
        '--foreground': 'oklch(1 0 0)',
        '--muted-foreground': 'oklch(0 0 0)',
        '--popover': '#111111',
        '--popover-foreground': '#eeeeee',
      }),
    )

    expect(theme).toMatchObject({
      legend: { textStyle: { color: 'rgb(255, 255, 255)' } },
      textStyle: { color: 'rgb(0, 0, 0)', fontFamily: 'Inter Variable, sans-serif' },
      timeAxis: {
        axisLabel: { color: 'rgb(0, 0, 0)' },
        splitLine: { lineStyle: { color: '#888888' } },
      },
      tooltip: {
        backgroundColor: '#111111',
        borderColor: '#888888',
        textStyle: { color: '#eeeeee' },
      },
    })
  })

  it('finds every chart colour token defined in both themes', () => {
    const light = css.slice(css.indexOf(':root {'), css.indexOf('.dark {'))
    const tokens = [...Object.values(ASH_PALETTE), ...Object.values(LAG_PALETTE)].map(
      (value) => /^var\((--[\w-]+)\)$/.exec(value)?.[1] ?? value,
    )

    for (const token of new Set(tokens)) {
      expect(light, `${token} in the light theme`).toContain(`${token}:`)
    }
    // Wait-event colours are tuned per theme, so each has its own dark value.
    for (const token of new Set(tokens.filter((token) => token.startsWith('--wait-')))) {
      expect(css.slice(css.indexOf('.dark {')), `${token} in the dark theme`).toContain(`${token}:`)
    }
  })
})
