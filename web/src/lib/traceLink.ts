const TRACE_ID = /^[0-9a-f]{32}$/
const PLACEHOLDER = '{trace_id}'

/**
 * The tracing tool's URL for a trace id, or null when there is no safe link:
 * the id must be 32 lowercase hex characters and the template an http(s) URL.
 * Every {trace_id} in the template is replaced.
 */
export function traceLink(template: string | null | undefined, id: string | null | undefined) {
  if (!template || !id || !TRACE_ID.test(id)) return null
  if (!/^https?:\/\//i.test(template) || !template.includes(PLACEHOLDER)) return null
  return template.split(PLACEHOLDER).join(id)
}

/** The short form a row shows: the first 8 characters. */
export function shortTraceId(id: string): string {
  return id.slice(0, 8)
}
