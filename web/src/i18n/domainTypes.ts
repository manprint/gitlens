import type { CaptionEntry, CaptionId, ColumnHelpId } from '@/lib/captions'
import type { DocumentedEventType } from '@/lib/events'
import type { GlossaryEntry, Term } from '@/lib/glossary'
import type { NeedName, RuleTextId } from '@/lib/ruleText'
import type { TileUnit } from '@/lib/units'

/**
 * The domain texts of a language other than English (plan 006 F4): captions,
 * column hints, rule sentences, rule inputs, glossary entries, tile units and
 * cluster event descriptions.
 * English stays where it is defined (src/lib); the compiler requires every id
 * here. Loaded as its own chunk next to the language's catalog.
 */
export interface DomainTexts {
  captions: Readonly<Record<CaptionId, CaptionEntry>>
  columnHelp: Readonly<Record<ColumnHelpId, string>>
  rules: Readonly<Record<RuleTextId, string>>
  needs: Readonly<Record<NeedName, string>>
  /** The inputs of a rule that needs none. */
  noNeeds: string
  glossary: Readonly<Record<Term, GlossaryEntry>>
  /** A tile unit after any figure but 1. */
  units: Readonly<Record<TileUnit, string>>
  /** A tile unit after the figure 1, where it differs from `units`. */
  unitsOne: Readonly<Partial<Record<TileUnit, string>>>
  /** Title and operator guidance of each cluster event type; `unknown` for the rest. */
  events: Readonly<Record<DocumentedEventType | 'unknown', { title: string; whatToCheck: string }>>
}
