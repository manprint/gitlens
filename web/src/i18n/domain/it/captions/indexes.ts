import type { CaptionEntry } from '@/lib/captions'
import type { indexesCaptions as english } from '@/lib/captionsets/indexes'

export const indexes: Readonly<Record<keyof typeof english, CaptionEntry>> = {
  'indexes.page': {
    what: 'Ogni indice segnalato dall’agent per un server, con dimensione, utilizzo e segnalazioni dell’advisor: inutilizzati, duplicati, ridondanti, non validi, mancanti.',
    source:
      'Righe di getInstanceIndexes e segnalazioni index.* di getFindings, per il server e il database scelti nella pagina.',
    unit: 'indici, segnalazioni',
    window: 'Ultima raccolta e ultima valutazione dell’advisor.',
    howToRead:
      'Le schede dividono gli indici in inventario, mancanti con gli statement per crearli, rimovibili, tabelle con righe morte e costo degli indici per tabella.',
  },
  'indexes.inventory': {
    what: 'Ogni indice segnalato dall’agent per il server, una riga per indice, con colonne, metodo di accesso, dimensione, frequenza di scansione e ultima scansione.',
    source:
      'getInstanceIndexes: il campione più recente di metrics_indexes per indice (pg_stat_user_indexes, pg_stat_statio_user_indexes, pg_relation_size) unito al fatto index_def (pg_get_indexdef).',
    unit: 'byte, scansioni al secondo, %',
    window:
      'Ultima raccolta (campione degli ultimi 15 minuti); le scansioni sono tassi sull’ultimo intervallo di raccolta.',
    howToRead:
      'Da verificare conta gli indici con una segnalazione aperta dell’advisor o marcati non validi. L’agent riporta solo gli indici più usati e più grandi per database: un indice mancante può esistere comunque.',
  },
  'indexes.summary': {
    what: 'Indici considerati per tipo, metodo di accesso, database e tabella, con i conteggi di duplicati, ridondanti, non validi e chiavi esterne senza indice.',
    source:
      'Contato nel browser dalle righe di getInstanceIndexes (summariseIndexes) e dalle segnalazioni index.* aperte di getFindings.',
    unit: 'indici',
    window:
      'Ultima raccolta e ultima valutazione dell’advisor, per il database scelto nella pagina.',
    howToRead:
      'I conteggi riguardano solo gli indici segnalati dall’agent. I conteggi per tipo si sovrappongono: un indice unico parziale è sia unico sia parziale.',
  },
  'indexes.findings': {
    what: 'Segnalazioni aperte su indici: inutilizzati, duplicati, prefisso ridondante, non validi, divergenti e chiavi esterne senza indice; una riga per segnalazione.',
    source:
      'Righe di getFindings delle regole dell’advisor index.unused, index.duplicate, index.redundant_prefix, index.invalid, index.divergence e index.fk_unindexed (rules_relation.go, rules_catalog.go).',
    unit: 'segnalazioni',
    window: 'Ultima valutazione dell’advisor; compaiono solo le segnalazioni aperte.',
    howToRead:
      'Espandi una riga per il dettaglio e il correttivo scritto dall’advisor. Una segnalazione è un candidato da rivedere, non un’istruzione.',
  },
  'indexes.foreign-keys': {
    what: 'Chiavi esterne senza un indice in testa alle colonne della tabella figlia: eliminare o modificare una riga della tabella padre scansiona la tabella figlia.',
    source:
      'Regola dell’advisor index.fk_unindexed sui fatti fk_unindexed del controllo fk_indexes, che legge solo i cataloghi (internal/check/fk_indexes.go).',
    unit: 'chiavi esterne',
    window: 'Ultima valutazione dell’advisor delle segnalazioni aperte.',
    howToRead:
      'Sono indici mancanti, quindi non compaiono nell’inventario. Se le righe della tabella padre non vengono mai eliminate o modificate, l’indice non serve. Per database vengono riportate al massimo 200 chiavi esterne, prima le tabelle figlie più grandi.',
    thresholds: 'La gravità è avviso con tabella figlia di 10 MiB o più, info sotto.',
  },
  'indexes.summary-primary': {
    what: 'Indici che sostengono un vincolo di chiave primaria.',
    source: 'getInstanceIndexes is_primary, da pg_index.indisprimary.',
    unit: 'indici',
    window: 'Ultima raccolta.',
  },
  'indexes.summary-unique': {
    what: 'Indici unici che non sono una chiave primaria.',
    source:
      'getInstanceIndexes is_unique senza is_primary, da pg_index.indisunique e indisprimary.',
    unit: 'indici',
    window: 'Ultima raccolta.',
  },
  'indexes.summary-partial': {
    what: 'Indici con un predicato, che coprono solo le righe selezionate dal predicato.',
    source: 'Predicato letto nel browser dal testo pg_get_indexdef dell’indice.',
    unit: 'indici',
    window: 'Ultima raccolta.',
  },
  'indexes.summary-expression': {
    what: 'Indici con una chiave costruita su un’espressione, come lower(email), invece che su una semplice colonna.',
    source: 'Colonne chiave lette nel browser dal testo pg_get_indexdef dell’indice.',
    unit: 'indici',
    window: 'Ultima raccolta.',
  },
  'indexes.summary-composite': {
    what: 'Indici con più di una colonna chiave; le colonne incluse non vengono contate.',
    source: 'Colonne chiave lette nel browser dal testo pg_get_indexdef dell’indice.',
    unit: 'indici',
    window: 'Ultima raccolta.',
  },
  'indexes.summary-invalid': {
    what: 'Indici che PostgreSQL marca non validi: le query non possono usarli, ma le scritture continuano ad aggiornarli.',
    source: 'getInstanceIndexes is_valid, da pg_index.indisvalid.',
    unit: 'indici',
    window: 'Ultima raccolta.',
    howToRead:
      'Ricostruisci o elimina un indice non valido, come suggerisce la regola index.invalid.',
  },
  'indexes.breakdown-method': {
    what: 'Numero e dimensione complessiva degli indici considerati per ogni metodo di accesso, come btree o gin.',
    source:
      'Contato nel browser (summariseIndexes) dal metodo di accesso nel testo pg_get_indexdef e da pg_relation_size di ogni indice.',
    unit: 'indici, byte',
    window: 'Ultima raccolta.',
  },
  'indexes.breakdown-database': {
    what: 'Numero e dimensione complessiva degli indici considerati per ogni database.',
    source:
      'Contato nel browser (summariseIndexes) da getInstanceIndexes: datname e pg_relation_size di ogni indice.',
    unit: 'indici, byte',
    window: 'Ultima raccolta.',
  },
  'indexes.breakdown-tables': {
    what: 'Le otto tabelle con più indici considerati, con numero e dimensione complessiva dei loro indici.',
    source:
      'Contato nel browser (groupByTable) da getInstanceIndexes: tabella e pg_relation_size di ogni indice.',
    unit: 'indici, byte',
    window: 'Ultima raccolta.',
    howToRead:
      'Ogni indice aggiunge lavoro a ogni inserimento e aggiornamento non HOT della tabella: una tabella in alto merita uno sguardo alla scheda Costo degli indici.',
  },
  'indexes.flags': {
    what: 'Etichette di un indice: il tipo, come chiave primaria, unico o parziale, e le segnalazioni dell’advisor per l’indice, come inutilizzato, duplicato o non valido.',
    source:
      'rowFlags (IndexesPanel): il tipo dalla definizione dell’indice e dai flag di pg_index, le segnalazioni delle regole dell’advisor index.* per questo indice.',
    window: 'Ultima raccolta e ultima valutazione dell’advisor.',
    howToRead:
      'Divergente significa che l’indice manca o è definito diversamente su un altro server dello stesso cluster. Mai scansionato è il contatore grezzo dall’ultimo reset delle statistiche, non il verdetto dell’advisor.',
  },
  'indexes.finding-rule': {
    what: 'Regola dell’advisor che ha generato la segnalazione: rosse le critiche, ambra le altre.',
    source: 'rule_id e severity di ogni riga di getFindings; le regole sono in internal/advisor.',
    window: 'Ultima valutazione dell’advisor.',
  },
  'indexes.fk-severity': {
    what: 'Quanto costa l’indice mancante: avviso se la tabella figlia è di 10 MiB o più, info sotto.',
    source:
      'Gravità impostata dalla regola dell’advisor index.fk_unindexed sulla dimensione riportata dal controllo fk_indexes (fkUnindexedWarnBytes).',
    window:
      'Ultima valutazione dell’advisor; la dimensione della tabella è una stima dall’ultimo vacuum o analyze.',
  },
  'indexes.cleanup': {
    what: 'Indici inutilizzati, ripetuti o non validi, con lo spazio su disco e le scritture sugli indici che eliminarli risparmierebbe.',
    source:
      'buildCleanup (indexCleanup.ts) sulle righe di getInstanceIndexes e sulle segnalazioni index.* dell’advisor; scritture sugli indici dai tassi di inserimento, aggiornamento e aggiornamento HOT di getInstanceTables.',
    unit: 'indici, byte, voci di indice al secondo',
    window: 'Ultima raccolta e ultima valutazione dell’advisor.',
    howToRead:
      'I candidati confermati si basano su una segnalazione dell’advisor. I candidati da contatore grezzo non hanno avuto scansioni solo dall’ultimo reset delle statistiche: non sono un verdetto e restano fuori dai totali confermati. Chiavi primarie e indici unici non compaiono mai. pglens non elimina nulla: copia lo script, rivedilo ed eseguilo tu.',
  },
  'indexes.cleanup-reason': {
    what: 'Perché un indice compare: non valido, duplicato, prefisso ridondante o inutilizzato (segnalazioni dell’advisor), oppure mai scansionato (solo contatori grezzi).',
    source:
      'classify (indexCleanup.ts): le segnalazioni dell’advisor sull’indice, altrimenti l’ultimo stato di scansione delle sue statistiche.',
    window: 'Ultima raccolta e ultima valutazione dell’advisor.',
    howToRead:
      'Una riga duplicata è una copia da eliminare; si tiene quella con un vincolo, poi la più scansionata, poi la più grande.',
  },
  'indexes.dead-rows': {
    what: 'Tabelle per righe morte, cioè le vecchie versioni non ancora rimosse dal vacuum, con quota di righe vive, rapporto di vacuum previsto e ultimo vacuum.',
    source:
      'getInstanceTables: il campione più recente di metrics_tables per tabella (pg_stat_user_tables n_dead_tup, n_live_tup); andamento dallo storico della tabella; impostazioni di autovacuum dalle impostazioni dell’istanza.',
    unit: 'righe, %, byte (stimati)',
    window:
      'Ultima raccolta; l’andamento segue le 10 tabelle peggiori nell’intervallo di tempo selezionato.',
    howToRead:
      'Lo spazio morto è stimato come dimensione dell’heap per la quota di dead tuple. Il vacuum previsto è il numero di righe morte a cui autovacuum dovrebbe partire; è approssimato se le impostazioni del server non sono riportate.',
  },
  'indexes.inventory-table': {
    what: 'Una riga per indice: tabella, colonne chiave, tipo e flag, dimensione, frequenza di scansione, rapporto di cache hit, ultima scansione; espandi per i grafici.',
    source:
      'Righe di getInstanceIndexes (pg_stat_user_indexes, pg_statio_user_indexes, pg_relation_size, pg_get_indexdef); i flag vengono dalle segnalazioni dell’advisor index.*.',
    unit: 'byte, scansioni al secondo, %',
    window: 'Ultima raccolta; le scansioni sono tassi sull’ultimo intervallo di raccolta.',
  },
  'indexes.findings-table': {
    what: 'Una riga per segnalazione aperta dell’advisor su un indice, con regola, database, oggetto e titolo; espandi una riga per dettaglio e correttivo.',
    source:
      'Righe di getFindings delle regole dell’advisor index.* (internal/advisor), solo segnalazioni aperte.',
    unit: 'segnalazioni',
    window: 'Ultima valutazione dell’advisor.',
  },
  'indexes.foreign-keys-table': {
    what: 'Una riga per chiave esterna senza indice in testa alle colonne: tabella, colonne, tabella padre e correttivo dell’advisor.',
    source:
      'Righe di getFindings della regola dell’advisor index.fk_unindexed, dal controllo fk_indexes.',
    unit: 'segnalazioni',
    window: 'Ultima valutazione dell’advisor delle segnalazioni aperte.',
  },
  'indexes.cleanup-table': {
    what: 'Una riga per indice rimovibile: perché compare, dimensione e scritture ricevute; espandi per gli statement di DROP e di ricostruzione.',
    source:
      'buildCleanup (indexCleanup.ts) sulle righe di getInstanceIndexes e sulle segnalazioni index.* dell’advisor; scritture dai tassi di inserimento e aggiornamento di getInstanceTables.',
    unit: 'byte, voci di indice al secondo',
    window: 'Ultima raccolta e ultima valutazione dell’advisor.',
  },
  'indexes.dead-rows-table': {
    what: 'Una riga per tabella, prima quelle con più righe morte: righe morte, quota, rapporto di vacuum previsto, ultimo vacuum, quota HOT, scritture e andamento.',
    source:
      'getInstanceTables (pg_stat_user_tables) con le impostazioni di autovacuum dell’istanza e lo storico della tabella per l’andamento.',
    unit: 'righe, %, byte (stimati), al secondo',
    window: 'Ultima raccolta; l’andamento segue l’intervallo di tempo selezionato.',
  },
  'indexes.missing-table': {
    what: 'Una riga per indice mancante: cosa fare, dove è stato trovato, tabella e colonne, perché, e lo statement da copiare.',
    source:
      'buildMissingIndexes (missingIndexes.ts): suggerimenti di indice, chiavi esterne senza indice e colonne spaziali dell’advisor, e tabelle lette da scansioni complete.',
    unit: 'indici',
    window: 'L’intervallo scelto nella scheda per gli statement; ultima valutazione per il resto.',
  },
  'indexes.tax-table': {
    what: 'Una riga per tabella: numero di indici, dimensione rispetto alla tabella, spazio che sembra inutilizzato e voci di indice aggiunte dalle sue scritture.',
    source:
      'buildIndexTax (indexTax.ts) sulle righe di getInstanceIndexes e sui tassi di inserimento, aggiornamento ed eliminazione di getInstanceTables.',
    unit: 'indici, byte, %, voci di indice al secondo',
    window: 'Ultima raccolta; ogni valore è una stima.',
  },
  'indexes.verdict': {
    what: 'Come un indice suggerito pesa le letture che serve rispetto alle scritture che costa: forte, moderato o ad alta scrittura. Ogni verdetto è una stima.',
    source:
      'verdictFor (internal/indexadvice/verdict.go): la frequenza di chiamata degli statement che usano la chiave divisa per le scritture di indice al secondo della tabella.',
    window: 'L’intervallo scelto nella scheda.',
    howToRead:
      'Una tabella senza scritture di indice non è mai ad alta scrittura. Una prima colonna chiave con 20 valori distinti o meno abbassa il verdetto di un gradino.',
    thresholds: 'Rapporto di 10 o più è forte, 1 o più moderato, sotto 1 ad alta scrittura.',
  },
}
