/*
 * A subset of ICU MessageFormat (plan 006 D3), the twin of internal/i18n:
 * {name}, {n, plural, =0 {…} one {…} other {…}} with # for the number, and
 * {v, select, a {…} other {…}}. Plural categories come from Intl.PluralRules,
 * so a new language needs no code. testdata/format-cases.json runs here and in
 * the Go tests, so both write the same text.
 */

export type MessageArg = string | number | boolean
export type MessageArgs = Readonly<Record<string, MessageArg | undefined>>

const decimalMarks = new Map<string, string>()

function decimalMark(intl: string): string {
  let mark = decimalMarks.get(intl)
  if (mark === undefined) {
    try {
      mark =
        new Intl.NumberFormat(intl).formatToParts(1.5).find((part) => part.type === 'decimal')
          ?.value ?? '.'
    } catch {
      mark = '.'
    }
    decimalMarks.set(intl, mark)
  }
  return mark
}

/** n with at most two decimals and no grouping, in the locale's decimal mark (Go: i18n.Number). */
export function messageNumber(intl: string, n: number): string {
  const text = n.toFixed(2).replace(/0+$/, '').replace(/\.$/, '')
  return text.replace('.', decimalMark(intl))
}

export function pluralCategory(intl: string, n: number): string {
  try {
    return new Intl.PluralRules(intl).select(n)
  } catch {
    return n === 1 ? 'one' : 'other'
  }
}

/** The index of the brace closing the one at `open`, or -1. */
function matchBrace(text: string, open: number): number {
  let depth = 0
  for (let i = open; i < text.length; i++) {
    if (text[i] === '{') depth++
    else if (text[i] === '}') {
      depth--
      if (depth === 0) return i
    }
  }
  return -1
}

/** Go's strings.SplitN(s, ",", 3). */
function splitArg(body: string): string[] {
  const first = body.indexOf(',')
  if (first < 0) return [body]
  const second = body.indexOf(',', first + 1)
  if (second < 0) return [body.slice(0, first), body.slice(first + 1)]
  return [body.slice(0, first), body.slice(first + 1, second), body.slice(second + 1)]
}

/** The branches of a plural or select argument, "sel {text} sel {text} …", in order. */
export function parseBranches(text: string): [string, string][] {
  const out: [string, string][] = []
  let i = 0
  while (i < text.length) {
    while (i < text.length && /[ \n\t]/.test(text[i]!)) i++
    const start = i
    while (i < text.length && text[i] !== '{' && text[i] !== ' ') i++
    const selector = text.slice(start, i)
    while (i < text.length && text[i] !== '{') i++
    if (i >= text.length) break
    const end = matchBrace(text, i)
    if (end < 0) break
    out.push([selector, text.slice(i + 1, end)])
    i = end + 1
  }
  return out
}

export interface MessageArgument {
  name: string
  kind: 'simple' | 'plural' | 'select' | 'other'
  branches: [string, string][]
}

/** Walks a message: literal text goes to `text`, each argument to `arg`. */
function walk(
  message: string,
  text: (char: string) => void,
  arg: (argument: MessageArgument, raw: string) => void,
  hash: () => void = () => text('#'),
): void {
  for (let i = 0; i < message.length;) {
    const char = message[i]!
    if (char === '{') {
      const end = matchBrace(message, i)
      if (end < 0) {
        for (const rest of message.slice(i)) text(rest)
        return
      }
      const body = message.slice(i + 1, end)
      const parts = splitArg(body)
      const name = parts[0]!.trim()
      if (parts.length < 3) arg({ name, kind: 'simple', branches: [] }, body)
      else {
        const kind = parts[1]!.trim()
        arg(
          {
            name,
            kind: kind === 'plural' || kind === 'select' ? kind : 'other',
            branches: parseBranches(parts[2]!),
          },
          body,
        )
      }
      i = end + 1
    } else if (char === '#') {
      hash()
      i++
    } else {
      text(char)
      i++
    }
  }
}

function formatInto(
  out: string[],
  intl: string,
  message: string,
  args: MessageArgs,
  hash: number | null,
): void {
  walk(
    message,
    (char) => out.push(char),
    (argument, raw) => {
      const value = args[argument.name]
      if (argument.kind === 'simple') {
        if (value === undefined) out.push(`{${argument.name}}`)
        else out.push(typeof value === 'number' ? messageNumber(intl, value) : String(value))
        return
      }
      const branches = new Map(argument.branches)
      if (argument.kind === 'plural') {
        const n = typeof value === 'number' ? value : 0
        const text =
          branches.get(`=${messageNumber('en-US', n)}`) ??
          branches.get(pluralCategory(intl, n)) ??
          branches.get('other') ??
          ''
        formatInto(out, intl, text, args, n)
        return
      }
      if (argument.kind === 'select') {
        const text = branches.get(String(value)) ?? branches.get('other') ?? ''
        formatInto(out, intl, text, args, hash)
        return
      }
      out.push(`{${raw}}`)
    },
    () => out.push(hash === null ? '#' : messageNumber(intl, hash)),
  )
}

/** One message filled with its arguments, numbers and plurals in `intl`. */
export function formatMessage(intl: string, message: string, args: MessageArgs = {}): string {
  const out: string[] = []
  formatInto(out, intl, message, args, null)
  return out.join('')
}

/** The argument names a message uses, sorted (Go: i18n.Placeholders). */
export function placeholders(message: string): string[] {
  const names = new Set<string>()
  const visit = (text: string) =>
    walk(
      text,
      () => undefined,
      (argument) => {
        names.add(argument.name)
        for (const [, branch] of argument.branches) visit(branch)
      },
    )
  visit(message)
  return [...names].sort()
}

/** The selectors of every plural argument of a message, nested ones included. */
export function pluralSelectors(message: string): string[][] {
  const out: string[][] = []
  const visit = (text: string) =>
    walk(
      text,
      () => undefined,
      (argument) => {
        if (argument.kind === 'plural') out.push(argument.branches.map(([selector]) => selector))
        for (const [, branch] of argument.branches) visit(branch)
      },
    )
  visit(message)
  return out
}

/** Rebuilds a message with its literal text passed through `map` (the pseudo-locale). */
export function mapMessageText(message: string, map: (text: string) => string): string {
  const out: string[] = []
  let literal = ''
  const flush = () => {
    if (literal !== '') out.push(map(literal))
    literal = ''
  }
  walk(
    message,
    (char) => {
      literal += char
    },
    (argument, raw) => {
      flush()
      if (argument.kind === 'plural' || argument.kind === 'select') {
        const branches = argument.branches
          .map(([selector, text]) => `${selector} {${mapMessageText(text, map)}}`)
          .join(' ')
        out.push(`{${argument.name}, ${argument.kind}, ${branches}}`)
      } else out.push(`{${raw}}`)
    },
    () => {
      flush()
      out.push('#')
    },
  )
  flush()
  return out.join('')
}
