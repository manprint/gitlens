# Piano 006 — Internazionalizzazione (console, help, notifiche)

Stato: **approvato** 2026-11-20, in esecuzione (non pev-v3). Stato vivo in `STATE.md`. Redatto 2026-11-20 (Opus 5.5).

## 1. Obiettivo

La lingua di tutta l'applicazione si sceglie per configurazione: console, Inline Help, testi generati dal server (finding, eventi, readiness) e notifiche. Oggi le lingue sono italiano e inglese.

Requisito chiave: domani si aggiungono **francese e tedesco senza toccare codice**. Bastano i file di traduzione e una riga nel registro delle lingue; i gate dicono cosa manca.

## 2. Inventario attuale (2026-11-20)

| Area | Dove | Dimensione |
|---|---|---|
| Testi UI in JSX | `web/src/**/*.tsx` (143 file) | ~260 nodi di testo + ~1.300 prop letterali (`label`, `title`, `placeholder`, `aria-label`, …) |
| Didascalie | `web/src/lib/captionsets/*.ts` (7 file) | 1.761 righe |
| Frasi delle regole | `web/src/lib/ruleText.ts` | 176 righe (advisor + alert) |
| Formattazione | `web/src/lib/format/*` (bytes, count, duration, timestamp, …) | ~20 usi di `Intl`/`toLocale*`; `date-fns` usato in 2 punti |
| Inline Help | `web/src/lib/help/topics/*.ts` (15 capitoli) | 12.377 righe, 2.305 testi già EN/IT come coppie `t(en, it)` |
| Testi dal server | `internal/advisor` (Title/Detail/Remediation) | 49 punti in 11 file, prosa inglese con valori interpolati |
| | Regole alert built-in e seed (`internal/alert/builtin.go`, migrazioni) | 14 summary |
| | Readiness, skip reason dei check, errori API | testo inglese libero |
| Notifiche | `internal/alert/notify` | da rifare (item 4): nascono già multilingua |
| Test | vitest: ~2.200 query su testo; e2e: ~240 | tutti in inglese |
| Preferenze | `web/src/lib/preferences.ts` (localStorage) | tema, intervallo, refresh, righe per pagina |

## 3. Decisioni di architettura

**D1 — Registro delle lingue: una sola fonte.**
- `web/src/i18n/locales.json`: `[{ "code": "en", "name": "English", "intl": "en-GB", "status": "stable" }, { "code": "it", "name": "Italiano", "intl": "it-IT", "status": "stable" }]`.
- Codici BCP 47. `status` vale `stable` (completa, imposta dai gate) oppure `beta` (ammessa la fallback all'inglese, visibile nel report).
- Il Go ha il suo registro (`internal/i18n`); un test lo confronta con il JSON, così le due liste non divergono.

**D2 — Cataloghi per lingua, inglese di riferimento.**
- File: `web/src/i18n/messages/<code>/<namespace>.ts`, con un namespace per area: `shell`, `fleet`, `cluster`, `instance`, `queries`, `indexes`, `locks`, `ash`, `findings`, `alerts`, `settings`, `captions`, `rules`, `units`, `errors`.
- Chiavi piatte e stabili (`fleet.card.partialWarnings`). Il tipo delle chiavi è derivato dal catalogo `en`: una chiave inesistente è un errore di compilazione.
- Una lingua `stable` deve avere tutte le chiavi; lo impone il compilatore (stesso tipo). Una lingua `beta` è `Partial` e ripiega sull'inglese.

**D3 — Sintassi ICU MessageFormat (sottoinsieme).**
- Supportati: `{name}`, `{count, plural, one {# riga} other {# righe}}`, `select`. Le categorie plurali vengono da `Intl.PluralRules`, quindi francese e tedesco funzionano senza logica nuova.
- È lo standard dei tool di traduzione (Weblate, Crowdin, Lokalise): i cataloghi si esportano e si reimportano.
- Formatter interno di pochi KB invece di FormatJS (budget bundle).
- Formatter gemello in Go per le notifiche. Un unico file di casi (`i18n/testdata/format-cases.json`) è eseguito da entrambe le suite, così i due formatter si comportano allo stesso modo.

**D4 — Runtime web.**
- `I18nProvider` + `useT()` → `t(key, params)`. Un `useFormat()` espone numeri, date, durate e byte formattati con la lingua attiva.
- `<html lang>` è aggiornato. Cambiare lingua non ricarica la pagina.

**D5 — Caricamento.**
- `en` è nel bundle principale: fa da fallback e da lingua dei test.
- Ogni altra lingua è un chunk lazy (`import()` per lingua), caricato prima del primo render, così non c'è un lampo di testo inglese.
- Il test del budget aggiunge un limite per chunk di lingua.

**D6 — Come si sceglie la lingua (in ordine).**
1. Scelta dell'utente: Settings › Preferences, salvata in localStorage come le altre preferenze.
2. Default del server: `PGLENS_LANG` (nuova variabile), esposto dall'API di configurazione della console.
3. `navigator.languages`, con la migliore corrispondenza nel registro.
4. `en`.

Il fuso orario resta un'impostazione separata.

**D7 — Formattazione con `Intl`.**
- Tutti i formatter di `lib/format/*` ricevono la lingua: separatore decimale (`1,5 GiB` in italiano), date, durate relative.
- I simboli delle unità (KiB, ms, %) non cambiano.
- I due usi di `date-fns` passano a `Intl`.
- I grafici ECharts usano i formatter localizzati per assi e tooltip; le stringhe interne di ECharts si registrano con `echarts.registerLocale` dove servono.
- Export CSV/JSON e parametri URL restano neutri: ISO 8601, punto decimale, id inglesi (`?section=locks`).

**D8 — Termini tecnici.**
- Un glossario per lingua (`docs/i18n/glossary.md`) dice cosa resta in inglese: vacuum, autovacuum, WAL, checkpoint, deadlock, ctid, xmin, i nomi `pg_*`, i nomi dei setting, SQL e identificatori.
- Il glossario esistente (`lib/glossary.ts`) entra nel catalogo.
- I valori di stato che arrivano dal server (`firing`, `primary`, …) si mostrano tradotti, ma il valore grezzo resta nei filtri e negli URL.

**D9 — Inline Help per lingua, con riferimenti alle etichette della UI.**
- Ogni lingua ha la sua cartella: `lib/help/topics/<code>/<id>.ts`, con il capitolo completo in quella lingua. Chi traduce lavora su un documento leggibile, non su coppie.
- Un test dello scheletro (sostituisce HELP-004) controlla che tutte le lingue abbiano lo stesso scheletro: stessi id di sezione, stessi tipi e numero di blocchi, SQL identico, stessi marcatori `code` e link.
- Nuovo marcatore `{{ui:chiave}}`: l'help cita l'etichetta della UI prendendola dal catalogo della stessa lingua. L'help nomina sempre il pulsante esattamente come appare a schermo. Questo sostituisce la regola attuale "l'italiano cita l'etichetta inglese".
- La migrazione è automatica: uno script divide le coppie `t(en, it)` in `en/` e `it/`, e il contenuto resta identico.

**D10 — Il server manda dati, il client sceglie la lingua.**
- **Finding**: l'API aggiunge `message: { key, args }` per titolo, dettaglio e rimedio (chiavi per `rule_id`, argomenti presi dall'evidenza). La UI li rende dal catalogo. Il testo inglese attuale resta nell'API per i client esterni e come fallback.
- **Regole alert**: built-in e seed tradotte per id (`ruleText` diventa catalogo). Le regole create dall'operatore mostrano il loro testo così com'è scritto.
- **Eventi, readiness, skip reason, errori API**: un codice stabile più un catalogo. Il dettaglio libero (errori PostgreSQL, testo delle query) resta grezzo, per scelta.
- **Notifiche (Go)**:
  - nuovo package `internal/i18n` con cataloghi `internal/i18n/locales/<code>.json` (embed) e lo stesso formatter ICU;
  - ogni canale ha una lingua (`notification_channels.locale`, NULL = `PGLENS_LANG`), così un team francese e uno italiano ricevono ciascuno nella propria.

**D11 — Gate di qualità.**
- **I18N-001**: ogni lingua ha le stesse chiavi del riferimento, gli stessi segnaposto e le categorie plurali richieste dalla lingua (`Intl.PluralRules(...).resolvedOptions().pluralCategories`).
- **I18N-002**: nessun testo visibile scritto nel codice. Regola ESLint `no-literal-string` (solo JSX e prop testuali), con allowlist per unità, SQL e identificatori. Si attiva area per area, man mano che si migra.
- **I18N-003**: pseudo-lingua `en-XA` (accenti e testi più lunghi del 35%, tra parentesi), solo in sviluppo e test.
  - Le guardie Full HD (FHD-FIT-001, FHD-MEASURE-001, FHD-INST-001, HDR-E2E-001) girano in `en`, `it` ed `en-XA`. Se entra `en-XA`, entrano francese e tedesco, che sono più lunghi.
  - La pseudo-lingua mostra a colpo d'occhio anche ogni stringa non estratta.
- **I18N-004**: cataloghi Go con la stessa parità; i casi di formattazione condivisi passano in TS e in Go.
- **I18N-005**: parità dello scheletro dell'help (D9).
- **Test unitari**: l'helper di render usa `en` di default, quindi i ~1.900 test attuali restano invariati. Ogni area aggiunge uno smoke test in `it`.
- **Report**: `pnpm i18n:report` (chiavi mancanti o in fallback per lingua) e `make i18n-check` in CI.

**D12 — Flusso di traduzione.**
- `pnpm i18n:export <code>` / `i18n:import <code>` producono e leggono JSON ICU piatto (UI, help, server), per tool o traduttori esterni.
- Le traduzioni restano nel repo; nessun servizio esterno è obbligatorio.

## 4. Fasi

Ogni fase si chiude con i gate verdi: Go `make fmt-check lint vet-tags test`; web tsc, eslint, prettier, vitest, coverage, budget.

| Fase | Contenuto | Uscita |
|---|---|---|
| **F0 Fondamenta** | Registro (D1), provider/`useT`/`useFormat` (D4), formatter ICU TS + Go con casi condivisi (D3), risoluzione lingua (D6), selettore in Settings › Preferences, `<html lang>`, pseudo-lingua, I18N-001/003/004, budget per chunk | Si cambia lingua dalla UI; i cataloghi esistono (quasi vuoti); test verdi |
| **F1 Server** | `PGLENS_LANG`, esposto all'API (OpenAPI aggiornata); migrazione `notification_channels.locale`; package `internal/i18n` | Default della lingua letto dal server; il Go formatta da catalogo |
| **F2 Shell e componenti comuni** | Layout, header, sidebar, breadcrumb, controlli tabella (ricerca, filtri, pager, ordinamento), time range picker, stati vuoti ed errore, freshness badge, `lib/format/*` su `Intl` | Shell completa in IT; FHD verde in en/it/en-XA |
| **F3 Pagine** (una unità per area) | Fleet · Cluster · Instance (overview, relations, catalog, internals, host) · Queries · Indexes · Locks · ASH · Findings · Alerts/Events/Rules/Silences · Settings · Login/NotFound. Per ogni area: estrazione, traduzione IT, I18N-002 attivo sull'area, smoke test IT | Tutte le pagine in IT; nessuna stringa letterale |
| **F4 Didascalie e testi di dominio** | `captionsets` (titolo, sottotitolo, `howToRead`), `ruleText`, glossario, etichette delle unità; glossario dei termini (D8) | Didascalie e frasi delle regole in IT; VIS-CAP-004/005 verdi in IT ed en-XA |
| **F5 Testi dal server** | `message {key,args}` per i finding (49 punti), codici per eventi, readiness, skip reason ed errori API; la UI rende dal catalogo con fallback | Finding, eventi e readiness tradotti; API retrocompatibile |
| **F6 Help** | Script di migrazione `t(en,it)` → `en/` + `it/`; marcatore `{{ui:…}}`; I18N-005 al posto di HELP-004; caricamento lazy per lingua; i link della console verso l'help restano invariati | Help identico a oggi, per lingua, con le etichette che seguono la UI |
| **F7 Notifiche** | Il describer dell'item 4 usa `internal/i18n`; lingua per canale in Settings › Notifications; test di rendering golden per lingua | Notifiche in en/it, scelta per canale |
| **F8 Gate e documentazione** | I18N-002 attivo ovunque; e2e live in `it`; README e CLAUDE.md con "Aggiungere una lingua"; capitolo dell'help sulle Preferences aggiornato | `make i18n-check` in CI; procedura documentata |
| **F9 Rilascio** | Build delle immagini, deploy sulle VM (backup di `compose.yml`), `PGLENS_LANG=it` sul deploy, verifica visiva a 1920 | In produzione con rollback pronto |

Ordine e dipendenze: F0 → F1 → (F2 → F3 → F4) ‖ F5 ‖ F6 → F7 → F8 → F9. F5 e F6 sono indipendenti da F3, quindi si possono parallelizzare.

Stima: 8 sessioni circa (F0–F1: 1; F2–F4: 3–4; F5: 1; F6: 1; F7: 0,5; F8–F9: 1). Il grosso è l'estrazione di ~1.500 stringhe UI e 1.761 righe di didascalie.

## 5. Aggiungere una lingua (es. francese), a piano completato

1. `web/src/i18n/locales.json`: aggiungere `{ "code": "fr", "name": "Français", "intl": "fr-FR", "status": "beta" }`.
2. `pnpm i18n:scaffold fr`: copia i cataloghi `en` in `messages/fr/`, `lib/help/topics/fr/` e `internal/i18n/locales/fr.json`.
3. Tradurre: a mano, oppure `pnpm i18n:export fr` → tool → `pnpm i18n:import fr`.
4. Lanciare `pnpm i18n:report fr` (deve dare 0 mancanti), `make i18n-check` e le guardie FHD in `fr`.
5. Impostare `"status": "stable"`.

Nessun file di codice cambia. Il compilatore e i gate segnalano ogni chiave mancante o segnaposto diverso.

## 6. Rischi e mitigazioni

| Rischio | Mitigazione |
|---|---|
| Testi più lunghi (DE +30%) rompono il fit Full HD | Pseudo-lingua `en-XA` nelle guardie FHD fin da F0; didascalie su due righe (VIS-CAP-004) verificate per lingua |
| ~2.200 query dei test su testo inglese | Test in `en` di default; nuovi smoke test per lingua; nessuna riscrittura di massa |
| Stringhe dimenticate | I18N-002 (ESLint) area per area, più la pseudo-lingua che le rende visibili |
| Lingua mista per testo libero (errori PG, query, regole dell'operatore) | Scelta dichiarata: il testo libero resta com'è; nella UI va marcato come "originale" dove serve |
| Peso del bundle | Lingue in chunk lazy; limite per chunk nel test del budget; formatter interno |
| Divergenza tra registro e formatter Go/TS | Test di parità sul registro (D1) e casi di formattazione condivisi (D3) |
| Testi del server cambiati senza aggiornare il catalogo | `message.key` validato contro il catalogo `en` in un test Go che legge i JSON |

## 7. Decisioni aperte per l'utente

1. La lingua di default del deploy attuale: `PGLENS_LANG=it`?
2. Il selettore di lingua solo in Settings › Preferences, o anche nell'header?
3. Il glossario dei termini che restano in inglese (D8): confermare la lista iniziale.
