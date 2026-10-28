import { useSyncExternalStore } from 'react'

type ColorScheme = 'dark' | 'light'

/** Reads one CSS custom property, such as `--primary`; empty when it is not defined. */
export type TokenReader = (name: string) => string

const CSS_VARIABLE = /^var\((--[\w-]+)\)$/u
const OKLCH =
  /^oklch\(\s*([\d.]+)(%?)\s+([\d.]+)\s+([\d.]+)(?:deg)?\s*(?:\/\s*([\d.]+)(%?))?\s*\)$/u

function subscribe(onChange: () => void): () => void {
  const observer = new MutationObserver(onChange)
  observer.observe(document.documentElement, { attributeFilter: ['class'] })
  return () => observer.disconnect()
}

interface TokenSnapshot {
  scheme: ColorScheme
  read: TokenReader
}

let snapshot: TokenSnapshot | undefined
const serverSnapshot: TokenSnapshot = { scheme: 'light', read: () => '' }

function currentSnapshot(): TokenSnapshot {
  const scheme = document.documentElement.classList.contains('dark') ? 'dark' : 'light'
  if (snapshot?.scheme !== scheme) {
    const style = getComputedStyle(document.documentElement)
    snapshot = { scheme, read: (name) => style.getPropertyValue(name).trim() }
  }
  return snapshot
}

/**
 * Reads the design tokens of the current theme. The header changes the theme
 * by toggling the dark class on the document element, so the reader follows
 * that class, and a new reader is returned whenever the theme changes.
 */
export function useTokenReader(): TokenReader {
  return useSyncExternalStore(subscribe, currentSnapshot, () => serverSnapshot).read
}

function toSrgbChannel(linear: number): number {
  const clamped = Math.min(1, Math.max(0, linear))
  const encoded = clamped <= 0.0031308 ? 12.92 * clamped : 1.055 * clamped ** (1 / 2.4) - 0.055
  return Math.round(encoded * 255)
}

/**
 * ECharts draws on a canvas and parses colours itself, so it understands
 * neither var() nor oklch(). The design tokens are oklch, so they are turned
 * into rgb() here; any other colour is passed through unchanged.
 */
export function oklchToRgb(value: string): string {
  const match = OKLCH.exec(value.trim())
  if (!match) return value
  const [, rawL = '0', lPercent, rawC = '0', rawH = '0', rawAlpha, alphaPercent] = match
  const lightness = Number(rawL) / (lPercent ? 100 : 1)
  const chroma = Number(rawC)
  const hue = (Number(rawH) * Math.PI) / 180
  const a = chroma * Math.cos(hue)
  const b = chroma * Math.sin(hue)
  const l = (lightness + 0.3963377774 * a + 0.2158037573 * b) ** 3
  const m = (lightness - 0.1055613458 * a - 0.0638541728 * b) ** 3
  const s = (lightness - 0.0894841775 * a - 1.291485548 * b) ** 3
  const red = toSrgbChannel(4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s)
  const green = toSrgbChannel(-1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s)
  const blue = toSrgbChannel(-0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s)
  if (rawAlpha === undefined) return `rgb(${red}, ${green}, ${blue})`
  const alpha = Number(rawAlpha) / (alphaPercent ? 100 : 1)
  return `rgba(${red}, ${green}, ${blue}, ${alpha})`
}

function resolveColor(value: string, read: TokenReader): string {
  const name = CSS_VARIABLE.exec(value)?.[1]
  if (name === undefined) return value
  const resolved = read(name)
  // Without the stylesheet (in tests, say) the token stays as written.
  return resolved === '' ? value : oklchToRgb(resolved)
}

/** Replaces every `var(--token)` string in a chart option with the token's colour. */
export function resolveTokens<T>(value: T, read: TokenReader): T {
  if (typeof value === 'string') return resolveColor(value, read) as T
  if (Array.isArray(value)) return value.map((item: unknown) => resolveTokens(item, read)) as T
  if (typeof value !== 'object' || value === null) return value
  // Dates, class instances and the like are left alone; options are plain data.
  if (Object.getPrototypeOf(value) !== Object.prototype) return value
  const resolved: Record<string, unknown> = {}
  for (const [key, item] of Object.entries(value)) resolved[key] = resolveTokens(item, read)
  return resolved as T
}

/**
 * An ECharts theme drawn from the design tokens, so axes, legends, tooltips
 * and the zoom slider match the page in both themes instead of keeping the
 * library's light defaults.
 */
export function buildChartTheme(read: TokenReader): Record<string, unknown> {
  const color = (name: string) => resolveColor(`var(${name})`, read)
  const text = color('--muted-foreground')
  const foreground = color('--foreground')
  const border = color('--border')
  const axis = {
    axisLine: { lineStyle: { color: border } },
    axisTick: { lineStyle: { color: border } },
    axisLabel: { color: text },
    nameTextStyle: { color: text },
    splitLine: { lineStyle: { color: border } },
  }
  return {
    backgroundColor: 'transparent',
    textStyle: { color: text, fontFamily: read('--font-ui') || 'sans-serif' },
    legend: {
      inactiveColor: border,
      pageIconColor: foreground,
      pageIconInactiveColor: border,
      pageTextStyle: { color: text },
      textStyle: { color: foreground },
    },
    tooltip: {
      backgroundColor: color('--popover'),
      borderColor: border,
      textStyle: { color: color('--popover-foreground') },
    },
    categoryAxis: axis,
    logAxis: axis,
    timeAxis: axis,
    valueAxis: axis,
    dataZoom: {
      borderColor: border,
      dataBackground: { areaStyle: { color: border }, lineStyle: { color: border } },
      fillerColor: 'rgba(128, 128, 128, 0.15)',
      handleStyle: { borderColor: text, color: color('--card') },
      moveHandleStyle: { color: border },
      textStyle: { color: text },
    },
  }
}
