import type { GlossaryEntry, Term } from '@/lib/glossary'

export const glossary: Readonly<Record<Term, GlossaryEntry>> = {
  TPS: { short: 'Transazioni al secondo: transazioni confermate divise per i secondi trascorsi.' },
  WAL: {
    short:
      'Write-ahead log: il log su cui PostgreSQL scrive ogni modifica prima dei file dei dati.',
    long: 'La replica, il point-in-time recovery e il crash recovery leggono tutti il WAL.',
  },
  LSN: { short: 'Log sequence number: posizione in byte nel write-ahead log.' },
  XID: {
    short:
      'ID di transazione: il numero a 32 bit che PostgreSQL assegna a ogni transazione che scrive.',
  },
  MVCC: {
    short:
      'Multi-version concurrency control: gli aggiornamenti scrivono nuove versioni delle righe, così i lettori non attendono mai gli scrittori.',
    long: 'Le vecchie versioni delle righe restano come dead tuple finché il vacuum non le rimuove.',
  },
  TOAST: {
    short:
      'Lo storage che PostgreSQL usa per i valori grandi, tenuti fuori linea in una tabella di supporto.',
  },
  HOT: {
    short:
      'Heap-only tuple update: un aggiornamento che non modifica colonne indicizzate e quindi non scrive voci di indice.',
  },
  ASH: {
    short:
      'Active session history: campioni di ciò che faceva ogni sessione connessa, presi ogni pochi secondi.',
  },
  FK: {
    short: 'Chiave esterna: una colonna che deve corrispondere a una chiave di un’altra tabella.',
  },
  IPC: {
    short:
      'Comunicazione tra processi: un’attesa per un altro processo del server, come un worker parallelo.',
  },
  PK: {
    short: 'Chiave primaria: la colonna o le colonne che identificano ogni riga di una tabella.',
  },
  SLRU: {
    short:
      'Cache semplici a meno uso recente che PostgreSQL tiene per lo stato delle transazioni e altri piccoli dati simili.',
  },
  GIN: {
    short: 'Generalized inverted index: un tipo di indice per array, JSON e ricerca full-text.',
  },
  GiST: {
    short: 'Generalized search tree: un tipo di indice per dati geometrici, range e simili.',
  },
  BRIN: {
    short:
      'Block range index: un indice piccolo che conserva riepiloghi di intervalli di pagine della tabella.',
  },
  OID: {
    short: 'Object identifier: il numero interno che PostgreSQL assegna agli oggetti del catalogo.',
  },
  DDL: { short: 'Data definition language: statement che creano, modificano o eliminano oggetti.' },
  autovacuum: {
    short:
      'Il processo in background che fa vacuum e analyze sulle tabelle quando sono cambiate abbastanza righe.',
  },
  vacuum: {
    short: 'L’operazione che rimuove le dead tuple, così il loro spazio può essere riutilizzato.',
  },
  bloat: {
    short:
      'Spazio di una tabella o di un indice occupato da righe morte o vuote invece che da dati validi.',
  },
  'dead tuples': {
    short:
      'Versioni delle righe che nessuna transazione può più vedere, in attesa che il vacuum le rimuova.',
  },
  checkpoint: {
    short:
      'Il punto in cui PostgreSQL ha scritto nei file dei dati ogni pagina modificata, quindi il WAL più vecchio non serve più al recupero.',
  },
  'replication slot': {
    short:
      'Un marcatore con nome sul primario che conserva il WAL necessario a un consumatore, anche se è disconnesso.',
  },
  'replay lag': {
    short:
      'Quanto uno standby è in ritardo rispetto al primario nell’applicare il WAL già ricevuto.',
  },
  standby: {
    short: 'Un server che riapplica il WAL di un primario e serve query di sola lettura.',
  },
  primary: { short: 'Il server che accetta le scritture in un cluster replicato.' },
  wraparound: {
    short:
      'Il punto in cui gli ID di transazione finirebbero; il vacuum deve congelare le righe vecchie molto prima.',
  },
  'sequential scan': {
    short: 'Lettura di un’intera tabella dall’inizio alla fine invece di usare un indice.',
  },
  'index scan': {
    short: 'Ricerca delle righe tramite un indice invece di leggere tutta la tabella.',
  },
  pg_stat_statements: {
    short:
      'L’estensione di PostgreSQL che registra le statistiche di esecuzione per ogni statement normalizzato.',
  },
  'cache hit ratio': {
    short:
      'La quota di letture di pagine servite dagli shared buffers invece che dal sistema operativo o dal disco.',
  },
  xmin: {
    short:
      'La transazione più vecchia che una sessione o uno slot richiede ancora; il vacuum non può rimuovere versioni più recenti.',
  },
  queryid: {
    short:
      'L’identificatore che pg_stat_statements assegna a uno statement normalizzato; confrontabile solo nello stesso cluster.',
  },
  'wait event': {
    short:
      'Ciò che una sessione sta aspettando nel momento del campionamento, per esempio un lock, I/O o un client.',
  },
  backend: {
    short:
      'Il processo server che PostgreSQL avvia per ogni connessione client; esegue le query di quella connessione.',
  },
  'shared buffers': {
    short: 'La cache di memoria propria di PostgreSQL per le pagine di tabelle e indici.',
  },
  tuple: { short: 'Una versione di una riga memorizzata in una tabella.' },
  relation: { short: 'Una tabella, un indice o un altro oggetto memorizzato, come una sequenza.' },
  multixact: {
    short:
      'Un gruppo di transazioni che detengono un lock sulla stessa riga; ha una propria cache, piccola.',
  },
  tier: {
    short:
      'Un livello di permessi che pglens ha su un server monitorato (da T0 a T2); i livelli più alti consentono più controlli.',
  },
  heap: {
    short:
      'Lo storage principale di una tabella, senza i suoi indici né i dati TOAST tenuti fuori linea.',
  },
  'permission tier': {
    short:
      'Il livello di permessi che pglens ha su un server (da T0 a T2); i livelli più alti abilitano più controlli.',
  },
  T0: { short: 'Livello di permessi 0: il ruolo di monitoraggio può leggere solo le statistiche.' },
  T1: {
    short:
      'Livello di permessi 1: tutto ciò che è in T0, più EXPLAIN per mostrare i piani delle query.',
  },
  T2: {
    short:
      'Livello di permessi 2: tutto ciò che è in T1, più l’annullamento o la chiusura di altre sessioni, se il target lo consente.',
  },
  EXPLAIN: {
    short: 'Il comando PostgreSQL che mostra i passi scelti per eseguire uno statement.',
  },
  ANALYZE: {
    short:
      'L’opzione di EXPLAIN che esegue anche lo statement e riporta il tempo reale di ogni passo.',
  },
}
