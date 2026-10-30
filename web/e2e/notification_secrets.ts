import { readFile } from 'node:fs/promises'
import { join } from 'node:path'

import type { Page } from '@playwright/test'

/** The dev stack the live specs run against (`PGLENS_QA_STACK_DIR` overrides it). */
export const STACK_DIR = process.env.PGLENS_QA_STACK_DIR ?? '/home/agent/pglens-odoo-stack'

/**
 * The dev stack's Google Chat webhook address, read from the stack's `.env`
 * and never printed: an error names the variable, never its value.
 */
export async function stackGoogleChatAddress(): Promise<string> {
  const env = await readFile(join(STACK_DIR, '.env'), 'utf8')
  let address = ''
  for (const line of env.split('\n')) {
    const match = /^\s*(?:export\s+)?PGLENS_GOOGLE_CHAT_WEBHOOK_URL\s*=\s*(.*)$/.exec(line)
    if (match) address = (match[1] ?? '').trim().replace(/^(['"])(.*)\1$/, '$2')
  }
  if (address === '') throw new Error('PGLENS_GOOGLE_CHAT_WEBHOOK_URL is not set in the stack .env')
  return address
}

/**
 * The parts of the dev stack's Google Chat webhook that must never reach the
 * console (I-10): the address, its path, the space id, and the key and token,
 * raw and decoded. Read from the stack's `.env` for NOTI-E2E-001 and
 * NOTI-E2E-002 and never printed.
 */
export async function stackGoogleChatSecrets(address?: string): Promise<string[]> {
  address ??= await stackGoogleChatAddress()
  let url: URL
  try {
    url = new URL(address)
  } catch {
    // The parse error carries the input: a fresh error says only what failed.
    throw new Error('PGLENS_GOOGLE_CHAT_WEBHOOK_URL in the stack .env is not a URL')
  }
  const rawQuery = url.search.replace(/^\?/, '').split('&')
  const space = /\/spaces\/([^/]+)/.exec(url.pathname)?.[1] ?? ''
  const parts = [
    address,
    url.pathname,
    url.search,
    space,
    url.searchParams.get('key') ?? '',
    url.searchParams.get('token') ?? '',
    ...rawQuery.map((pair) => pair.slice(pair.indexOf('=') + 1)),
  ]
  // A part shorter than 8 characters identifies nothing and could match by chance.
  return [...new Set(parts.filter((part) => part.length >= 8))]
}

/**
 * Types `value` into an input the way a paste does, without the value
 * appearing in a step title, an action log or an error message, as it would
 * with `fill` (I-10).
 */
export async function typeSecret(
  field: ReturnType<Page['getByLabel']>,
  value: string,
): Promise<void> {
  await field.evaluate((element, text) => {
    const input = element as HTMLInputElement
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(input, text)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  }, value)
}
