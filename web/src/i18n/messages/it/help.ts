import type { help as english } from '../en/help'
import type { Translation } from '../../types'

export const help: Translation<typeof english> = {
  'help.page.title': 'Guida utente',
  'help.page.summary':
    'Ogni pagina della console pglens spiegata: cosa significa ogni valore, come leggerlo e cosa fare. Scegli un capitolo, oppure cerca.',
  'help.page.chapters': 'Capitoli',
  'help.page.onThisPage': 'In questa pagina',
  'help.page.search': 'Cerca nella guida',
  'help.page.noHits': 'Nessuna sezione corrisponde. Prova un’altra parola.',
  'help.page.hits': '{count, plural, one {# sezione corrisponde} other {# sezioni corrispondono}}',
  'help.page.whatToDo': 'Cosa fare:',
  'help.page.sectionLink': 'Link a questa sezione: {title}',
  'help.page.notFound':
    'Non esiste un capitolo della guida con questo nome. Scegline uno dall’elenco.',
  'help.page.openFleet': 'Apri la flotta e scegli un server: questa pagina appartiene a un server',
  'help.page.loading': 'Caricamento della guida…',
}
