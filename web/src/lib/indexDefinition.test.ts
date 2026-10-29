import { describe, expect, it } from 'vitest'

import { parseIndexDefinition, summariseColumns } from './indexDefinition'

const texts = (columns: { text: string }[]) => columns.map((column) => column.text)

describe('parseIndexDefinition', () => {
  it('IDX-DEF-001 reads a plain btree index', () => {
    const parsed = parseIndexDefinition(
      'CREATE INDEX ir_logging__dbname_index ON public.ir_logging USING btree (dbname)',
    )
    expect(parsed.method).toBe('btree')
    expect(parsed.unique).toBe(false)
    expect(texts(parsed.columns)).toEqual(['dbname'])
    expect(parsed.columns[0]?.expression).toBe(false)
    expect(parsed.predicate).toBeNull()
  })

  it('IDX-DEF-002 reads a unique composite index and its partial predicate', () => {
    const parsed = parseIndexDefinition(
      'CREATE UNIQUE INDEX bus_presence_user_unique ON public.bus_presence USING btree (user_id) WHERE (user_id IS NOT NULL)',
    )
    expect(parsed.unique).toBe(true)
    expect(parsed.predicate).toBe('user_id IS NOT NULL')

    const composite = parseIndexDefinition(
      'CREATE INDEX res_device_log__composite_idx ON public.res_device_log USING btree (user_id, session_identifier, platform, browser, last_activity, id) WHERE (revoked = false)',
    )
    expect(texts(composite.columns)).toEqual([
      'user_id',
      'session_identifier',
      'platform',
      'browser',
      'last_activity',
      'id',
    ])
    expect(composite.predicate).toBe('revoked = false')
  })

  it('IDX-DEF-003 keeps expressions whole, commas and casts included', () => {
    const parsed = parseIndexDefinition(
      "CREATE UNIQUE INDEX ir_filters_name_model_uid_unique_action_index ON public.ir_filters USING btree (model_id, COALESCE(user_id, '-1'::integer), lower((name)::text), embedded_parent_res_id)",
    )
    expect(texts(parsed.columns)).toEqual([
      'model_id',
      "COALESCE(user_id, '-1'::integer)",
      'lower((name)::text)',
      'embedded_parent_res_id',
    ])
    expect(parsed.columns.map((column) => column.expression)).toEqual([false, true, true, false])

    const doubled = parseIndexDefinition(
      'CREATE INDEX res_partner__barcode_index ON public.res_partner USING btree (((barcode IS NOT NULL))) WHERE (barcode IS NOT NULL)',
    )
    expect(doubled.columns).toHaveLength(1)
    expect(doubled.columns[0]?.expression).toBe(true)
  })

  it('IDX-DEF-004 reads ordering, operator class, INCLUDE, options and NULLS NOT DISTINCT', () => {
    const parsed = parseIndexDefinition(
      'CREATE UNIQUE INDEX t_idx ON ONLY public.t USING btree (a DESC NULLS LAST, b text_pattern_ops, c COLLATE "C") INCLUDE (d, e) NULLS NOT DISTINCT WITH (fillfactor=70) WHERE ((a > 0) AND (b IS NOT NULL))',
    )
    expect(parsed.columns.map((column) => [column.text, column.modifiers])).toEqual([
      ['a', 'DESC NULLS LAST'],
      ['b', 'text_pattern_ops'],
      ['c', 'COLLATE "C"'],
    ])
    expect(texts(parsed.include)).toEqual(['d', 'e'])
    expect(parsed.nullsNotDistinct).toBe(true)
    expect(parsed.options).toBe('fillfactor=70')
    expect(parsed.predicate).toBe('(a > 0) AND (b IS NOT NULL)')
    expect(summariseColumns(parsed)).toBe('a, b, c + d, e')
  })

  it('IDX-DEF-005 reads other access methods and quoted identifiers', () => {
    const gin = parseIndexDefinition(
      'CREATE INDEX t_trgm ON public.t USING gin (name public.gin_trgm_ops)',
    )
    expect(gin.method).toBe('gin')
    expect(gin.columns[0]).toMatchObject({ text: 'name', modifiers: 'public.gin_trgm_ops' })

    const quoted = parseIndexDefinition(
      'CREATE INDEX t_q ON public.t USING btree ("a,b (x)", "Mixed")',
    )
    expect(texts(quoted.columns)).toEqual(['"a,b (x)"', '"Mixed"'])
  })

  it('IDX-DEF-006 does not throw on an empty or unreadable definition', () => {
    expect(parseIndexDefinition(null).columns).toEqual([])
    expect(parseIndexDefinition('').columns).toEqual([])
    expect(parseIndexDefinition('garbage').columns).toEqual([])
    expect(parseIndexDefinition('CREATE INDEX x ON t USING btree (a').columns).toEqual([])
  })
})
