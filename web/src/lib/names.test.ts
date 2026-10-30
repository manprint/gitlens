import { describe, expect, it } from 'vitest'

import {
  clusterLabel,
  hasDisplayName,
  instanceAddress,
  instanceLabel,
  NAME_MAX_LENGTH,
  trimName,
  validateDisplayName,
} from './names'

const INSTANCE = { addr: 'db.example.test', port: 5432 }

describe('names', () => {
  it('CLNAME-UI-001 validateDisplayName mirrors the server rules', () => {
    // Clear: empty, blank, surrounding space only.
    expect(validateDisplayName('')).toEqual({ ok: true, value: null })
    expect(validateDisplayName('   ')).toEqual({ ok: true, value: null })
    // Accepted, trimmed.
    expect(validateDisplayName('prod-eu')).toEqual({ ok: true, value: 'prod-eu' })
    expect(validateDisplayName('  prod  ')).toEqual({ ok: true, value: 'prod' })
    expect(validateDisplayName('Produzione è ok')).toEqual({ ok: true, value: 'Produzione è ok' })
    // 64 characters pass, 65 do not.
    expect(validateDisplayName('a'.repeat(NAME_MAX_LENGTH))).toEqual({
      ok: true,
      value: 'a'.repeat(64),
    })
    expect(validateDisplayName('a'.repeat(65)).ok).toBe(false)
    // Code points, not UTF-16 units: 64 emoji are 128 units and still pass.
    expect(validateDisplayName('😀'.repeat(64)).ok).toBe(true)
    expect(validateDisplayName('😀'.repeat(65)).ok).toBe(false)
    // Control, format and separator characters inside the name are refused.
    expect(validateDisplayName('a\tb').ok).toBe(false)
    expect(validateDisplayName('a\nb').ok).toBe(false)
    expect(validateDisplayName('a​b').ok).toBe(false)
    expect(validateDisplayName('a b').ok).toBe(false)
    expect(validateDisplayName('a b').ok).toBe(false)
    expect(validateDisplayName('a\u0085b').ok).toBe(false)
  })

  it('CLNAME-UI-001 trims the white space Go trims, not what JavaScript trims', () => {
    expect(trimName('  name 　')).toBe('name')
    expect(trimName('\u0085name')).toBe('name')
    // U+FEFF is not white space for the server, which refuses it as a format character.
    expect(trimName('﻿name')).toBe('﻿name')
    expect(validateDisplayName('﻿name').ok).toBe(false)
  })

  it('CLNAME-UI-001 labels fall back when there is no name', () => {
    expect(clusterLabel({ cluster_id: '42', name: 'prod-eu' })).toBe('prod-eu')
    expect(clusterLabel({ cluster_id: '42', name: null })).toBe('Cluster 42')
    expect(clusterLabel({ cluster_id: '42' })).toBe('Cluster 42')
    expect(clusterLabel({ cluster_id: '42', name: null }, 'Unnamed cluster')).toBe(
      'Unnamed cluster',
    )
    expect(instanceAddress(INSTANCE)).toBe('db.example.test:5432')
    expect(instanceLabel({ ...INSTANCE, display_name: 'odoo-db-1' })).toBe('odoo-db-1')
    expect(instanceLabel({ ...INSTANCE, display_name: null })).toBe('db.example.test:5432')
    // display_name is optional in InstanceSummary: undefined reads as null.
    expect(instanceLabel(INSTANCE)).toBe('db.example.test:5432')
    expect(hasDisplayName({ display_name: 'odoo-db-1' })).toBe(true)
    expect(hasDisplayName({ display_name: null })).toBe(false)
    expect(hasDisplayName({})).toBe(false)
  })
})
