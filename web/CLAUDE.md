# web/ (console SPA)

React + Vite + TypeScript; built into `internal/webui/dist/index.html` by `make web-build` (that file is regenerated and never committed by hand).

- Gates: `pnpm exec tsc --noEmit`, `pnpm exec eslint .`, `pnpm exec prettier --check .`, `TZ=UTC pnpm exec vitest run`, `make web-coverage-gate web-budget`.
- Derivation in `src/lib/`, components only render. Tests: see `README.md` "How to write a test here".
- Captions: every card/section/table/chart has a `captionId` from `src/lib/captionsets/*.ts`; guide in `README.md` "Captions and summary cards".
- Caption contracts: fits two lines at 1920×1080 (detail goes in `howToRead`, guard VIS-CAP-004); column hint (`meta.help`) stays inside its `th` (VIS-CAP-005); each advisor/alert rule id needs a sentence in `src/lib/ruleText.ts`.
- Summary card unit comes from the closed `TILE_UNITS` in `src/lib/units.ts`.
- Full HD fit (1920×1080) guards: FHD-FIT-001, FHD-MEASURE-001, FHD-INST-001, HDR-E2E-001 in `e2e/`.
- Live specs need `PGLENS_UI_BASE_URL`, `PGLENS_UI_PASSWORD`, `PGLENS_BOOTSTRAP_TOKEN`; never run two Playwright jobs at once.
