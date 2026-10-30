import { describe, expect, it } from 'vitest'

import {
  DEFAULT_INSTANCE_SECTION,
  INSTANCE_SECTIONS,
  legacyHashToSection,
  readInstanceSection,
} from './instanceSections'

describe('instance sections registry', () => {
  it('INSTSEC-001 defaults to Overview and INSTSEC-003 falls back for unknown values', () => {
    expect(DEFAULT_INSTANCE_SECTION).toBe('overview')
    expect(readInstanceSection(null)).toBe('overview')
    expect(readInstanceSection('host')).toBe('host')
    expect(readInstanceSection('Host')).toBe('overview')
  })

  it('INSTSEC-005 maps every legacy in-page anchor, and #host, to its section', () => {
    for (const section of INSTANCE_SECTIONS) {
      expect(legacyHashToSection(`#${section.legacyHash}`)).toBe(section.id)
    }
    expect(legacyHashToSection('#host')).toBe('host')
    expect(legacyHashToSection('#workload')).toBeNull()
    expect(legacyHashToSection('')).toBeNull()
  })

  it('INSTSEC-006 only Overview and Host are chart sections', () => {
    expect(INSTANCE_SECTIONS.map((section) => section.id)).toEqual([
      'overview',
      'host',
      'settings',
      'relations',
      'catalog',
      'logical',
      'internals',
    ])
    expect(
      INSTANCE_SECTIONS.filter((section) => section.kind === 'charts').map((section) => section.id),
    ).toEqual(['overview', 'host'])
  })
})
