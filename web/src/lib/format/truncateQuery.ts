export const TRUNCATION_MARKER = '… [truncated]'

export function truncateQuery(text: string, max = 2048, locale = 'en-US'): string {
  void locale
  if (max < 1) return TRUNCATION_MARKER

  const encoder = new TextEncoder()
  const bytes = encoder.encode(text)
  if (bytes.length <= max) return text

  const markerBytes = encoder.encode(TRUNCATION_MARKER)
  const prefixBudget = Math.max(0, max - markerBytes.length)
  let prefixBytes = bytes.slice(0, prefixBudget)
  let prefix = ''
  while (prefixBytes.length > 0) {
    try {
      prefix = new TextDecoder('utf-8', { fatal: true }).decode(prefixBytes)
      break
    } catch {
      prefixBytes = prefixBytes.slice(0, -1)
    }
  }
  return `${prefix}${TRUNCATION_MARKER}`
}

export function isTruncatedQuery(text: string): boolean {
  return text.includes(TRUNCATION_MARKER)
}
