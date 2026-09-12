# Istruzioni per agente specifiche 

Nota bene: queste si aggiungono a quelle globali in `~/.codex/AGENTS.md`

## Invio di notifiche Telegram.

Sull'host è installato `tgsend`, per inviare notifiche telegram.
Github, per specifiche complete (pubblico): `https://github.com/manprint/tgsend`

## Help del comando

```
fabio@i7-lenovo-work:~/Scrivania$ tgsend --help
Send a message to Telegram

Usage:
  tgsend [flags]

Flags:
  -c, --config string         configuration file path
      --dry-run               validate and preview without credentials or network
  -h, --help                  help for tgsend
      --max-input-bytes int   maximum input size in bytes (default 1048576)
  -m, --message string        message text (mutually exclusive with stdin)
      --monospace             format each body chunk as preformatted text
      --silent                disable Telegram notifications
      --title string          optional bold title
      --type string           optional type: INFO, WARNING, ERROR, or CRITICAL
      --version               print version information as JSON
```

## Criterio di invio delle notifiche:

Inviami una notifica usando `tgsend` quando:

- Una fase di progetto viene chiusa, completa dei sui test (Tutto Verde)
- C'è un errore bloccante che richiede attenzione
- Ci sono domande dell'LLM da sottoporre all'utente.
- Completamento finale del piano di implementazione.
- Altre comunicazioni utili all'utente

## Titolo e type:

Utilizza il titolo e type appropriato per la tipologia di notifica che mi manderai.

## Dimensione massima.

La dimensione massima della notifica che devi inviare non deve superare la dimensione massima dei messaggi telegram.