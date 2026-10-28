export type SqlTokenKind = 'keyword' | 'string' | 'number' | 'parameter' | 'comment' | 'text'

export interface SqlToken {
  kind: SqlTokenKind
  text: string
}

const KEYWORDS = new Set(
  `select from where and or not in is null as on join left right inner outer full cross lateral
  group by order having limit offset fetch first next rows row only distinct union all except
  intersect insert into values update set delete returning create table index view alter drop
  with recursive case when then else end exists between like ilike similar asc desc nulls last
  begin commit rollback for share nowait skip locked using natural window over partition
  materialized refresh analyze explain vacuum truncate copy conflict do nothing any some
  cast true false interval default primary key references unique check constraint`
    .split(/\s+/u)
    .filter(Boolean),
)

const TOKEN_PATTERN =
  /(--[^\n]*|\/\*[\s\S]*?\*\/)|('(?:[^']|'')*'?)|(\$\d+)|(\b\d+(?:\.\d+)?\b)|([A-Za-z_][A-Za-z0-9_]*)|("(?:[^"]|"")*"?)|(\s+)|([\s\S])/gu

/**
 * Splits SQL into display tokens for highlighting. It is a lexer for colour,
 * not a parser: every input character lands in exactly one token, so joining
 * the token texts gives back the input unchanged.
 */
export function tokenizeSql(sql: string): SqlToken[] {
  const tokens: SqlToken[] = []

  function push(kind: SqlTokenKind, text: string) {
    const last = tokens.at(-1)
    if (last?.kind === kind && kind === 'text') {
      last.text += text
      return
    }
    tokens.push({ kind, text })
  }

  for (const match of sql.matchAll(TOKEN_PATTERN)) {
    const [text, comment, string, parameter, number, word] = match
    if (comment !== undefined) push('comment', text)
    else if (string !== undefined) push('string', text)
    else if (parameter !== undefined) push('parameter', text)
    else if (number !== undefined) push('number', text)
    else if (word !== undefined) push(KEYWORDS.has(word.toLowerCase()) ? 'keyword' : 'text', text)
    else push('text', text)
  }

  return tokens
}
