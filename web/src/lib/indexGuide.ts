import { PSEUDO_LOCALE } from '@/i18n/locales'
import { pseudoLocalize } from '@/i18n/pseudo'
import { activeLocale, translate } from '@/i18n/store'
import type { LangText } from '@/lib/help/types'

/**
 * The flags of the Indexes tab: what each means and what to do about it. The
 * console shows the text in its language (badge tooltips and row details); the
 * Inline Help shows both languages.
 */

export const FLAG_LABELS = [
  'invalid',
  'unused',
  'duplicate',
  'redundant prefix',
  'diverges',
  'never scanned',
  'primary',
  'unique',
  'partial',
  'expression',
  'include',
] as const

export type FlagLabel = (typeof FLAG_LABELS)[number]

export interface FlagGuide {
  /** `kind` describes the index; `problem` is an advisor finding or a raw-counter warning. */
  kind: 'kind' | 'problem'
  meaning: LangText
  action: LangText
}

export const FLAG_GUIDE: Readonly<Record<FlagLabel, FlagGuide>> = {
  invalid: {
    kind: 'problem',
    meaning: {
      en: 'A CREATE INDEX CONCURRENTLY or REINDEX CONCURRENTLY failed and left the index half built: queries never use it, yet every write still updates it.',
      it: 'Un CREATE INDEX CONCURRENTLY o REINDEX CONCURRENTLY è fallito e ha lasciato l’indice costruito a metà: le query non lo usano mai, ma ogni scrittura lo aggiorna comunque.',
    },
    action: {
      en: 'Drop it and create it again (both statements are in the row details), or run REINDEX INDEX CONCURRENTLY on it.',
      it: 'Eliminalo e ricrealo (entrambi i comandi sono nel dettaglio della riga), oppure esegui REINDEX INDEX CONCURRENTLY.',
    },
  },
  unused: {
    kind: 'problem',
    meaning: {
      en: 'The advisor saw no scan of this index, on this server and on its standbys, for the whole unused-index window (7 days by default).',
      it: 'L’advisor non ha visto nessuna scansione di questo indice, su questo server e sui suoi standby, per tutta la finestra di osservazione (7 giorni di default).',
    },
    action: {
      en: 'Make sure no rare job (month-end report, yearly batch) needs it, then drop it from the Cleanup tab. Keep its CREATE statement to undo.',
      it: 'Verifica che nessun job raro (report di fine mese, batch annuale) lo usi, poi eliminalo dalla tab Cleanup. Conserva il suo CREATE per poter tornare indietro.',
    },
  },
  duplicate: {
    kind: 'problem',
    meaning: {
      en: 'Another index of the same table has exactly the same definition: both are updated on every write, but only one is needed.',
      it: 'Un altro indice della stessa tabella ha esattamente la stessa definizione: entrambi vengono aggiornati a ogni scrittura, ma ne serve uno solo.',
    },
    action: {
      en: 'Keep one (the Cleanup tab names which) and drop the other. If both are unique, drop the one that does not back a constraint.',
      it: 'Tienine uno (la tab Cleanup indica quale) ed elimina l’altro. Se sono entrambi unique, elimina quello che non sostiene un constraint.',
    },
  },
  'redundant prefix': {
    kind: 'problem',
    meaning: {
      en: 'Its columns are the leading columns of a wider index, which can serve the same lookups.',
      it: 'Le sue colonne sono le colonne iniziali di un indice più ampio, che può servire le stesse ricerche.',
    },
    action: {
      en: 'Drop it unless it is unique, much smaller and very busy. Check with EXPLAIN that the wider index is chosen.',
      it: 'Eliminalo, a meno che sia unique, molto più piccolo e molto usato. Verifica con EXPLAIN che venga scelto l’indice più ampio.',
    },
  },
  diverges: {
    kind: 'problem',
    meaning: {
      en: 'The same table has different indexes on the servers of the cluster (for example on a logical replica).',
      it: 'La stessa tabella ha indici diversi sui server del cluster (per esempio su una replica logica).',
    },
    action: {
      en: 'Create or drop the index so that every server that serves this table has the same set.',
      it: 'Crea o elimina l’indice in modo che ogni server che serve questa tabella abbia lo stesso insieme di indici.',
    },
  },
  'never scanned': {
    kind: 'problem',
    meaning: {
      en: 'No scan since the statistics were last reset. It is the raw PostgreSQL counter, not a verdict: the reset may be recent, or the index too small for the advisor to judge.',
      it: 'Nessuna scansione dall’ultimo reset delle statistiche. È il contatore grezzo di PostgreSQL, non un verdetto: il reset può essere recente, o l’indice troppo piccolo perché l’advisor lo giudichi.',
    },
    action: {
      en: 'Do not drop it yet: wait for the advisor to flag it unused, or check how old the counters are (pg_stat_database.stats_reset).',
      it: 'Non eliminarlo ancora: aspetta che l’advisor lo segnali come unused, oppure controlla quanto sono vecchi i contatori (pg_stat_database.stats_reset).',
    },
  },
  primary: {
    kind: 'kind',
    meaning: {
      en: 'It backs the primary key of the table.',
      it: 'Sostiene la chiave primaria della tabella.',
    },
    action: { en: 'Nothing to do: never drop it.', it: 'Nessuna azione: non eliminarlo mai.' },
  },
  unique: {
    kind: 'kind',
    meaning: {
      en: 'It enforces that no two rows share the key (a UNIQUE constraint or index).',
      it: 'Garantisce che due righe non abbiano la stessa chiave (constraint o indice UNIQUE).',
    },
    action: {
      en: 'Keep it: it protects the data, not only the speed. Drop it only as the duplicate of another unique index.',
      it: 'Tienilo: protegge i dati, non solo le prestazioni. Eliminalo solo se è il duplicato di un altro indice unique.',
    },
  },
  partial: {
    kind: 'kind',
    meaning: {
      en: 'It covers only the rows that match its WHERE clause, so it is smaller.',
      it: 'Copre solo le righe che soddisfano la sua clausola WHERE, quindi è più piccolo.',
    },
    action: {
      en: 'A query uses it only when its own WHERE implies the same condition: the predicate is in the row details.',
      it: 'Una query lo usa solo se la sua WHERE implica la stessa condizione: il predicato è nel dettaglio della riga.',
    },
  },
  expression: {
    kind: 'kind',
    meaning: {
      en: 'It indexes an expression, such as lower(email), instead of a plain column.',
      it: 'Indicizza un’espressione, come lower(email), invece di una colonna semplice.',
    },
    action: {
      en: 'A query uses it only when it writes exactly the same expression.',
      it: 'Una query lo usa solo se scrive esattamente la stessa espressione.',
    },
  },
  include: {
    kind: 'kind',
    meaning: {
      en: 'It carries extra INCLUDE columns, so a query can be answered from the index alone (index-only scan).',
      it: 'Contiene colonne INCLUDE aggiuntive, così una query può essere risolta leggendo solo l’indice (index-only scan).',
    },
    action: {
      en: 'Nothing to do. Keep the included columns few: each one makes every write heavier.',
      it: 'Nessuna azione. Tieni poche colonne incluse: ognuna rende più pesante ogni scrittura.',
    },
  },
}

/** A guide text in the console language; a language without its own text reads in English. */
export function guideText(text: LangText): string {
  const locale = activeLocale()
  if (locale === PSEUDO_LOCALE) return pseudoLocalize(text.en)
  return locale === 'it' ? text.it : text.en
}

/** The tooltip of a flag badge: what it means, then what to do. */
export function flagTitle(label: FlagLabel): string {
  const guide = FLAG_GUIDE[label]
  return `${guideText(guide.meaning)} ${translate('indexes.lib.guide.whatToDo')} ${guideText(guide.action)}`
}
