/**
 * Help links without the help content: pages import this, so their bundle does
 * not carry the whole guide (the guide itself loads with the help page).
 */

/** `topic` or `topic#section`. */
export type HelpAnchor = string

export const HELP_PATH = '/help'

export function helpHref(anchor: HelpAnchor): string {
  const [topic, section] = anchor.split('#')
  return `${HELP_PATH}/${topic}${section ? `#${section}` : ''}`
}
