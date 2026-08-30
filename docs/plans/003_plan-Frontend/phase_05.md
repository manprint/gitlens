# Phase 4 — Frontend workspace scaffold

> **Intent:** create `web/` as a buildable, type-checked, linted Vite + React
> workspace wired into the repository's Makefile and CI, producing a real
> `internal/webui/dist` build.
> **Shippable alone?** yes — it replaces the placeholder page with a minimal but
> real application shell that says the interface is under construction.
> **Preconditions:** phase 3 DONE.

## State contract (mandatory)

1. Before touching anything: read [STATE.md](STATE.md). If §1 `Status` is `OPEN`,
   finish or revert that unit first (§6 says how far it got). Run the gate
   commands in STATE.md **§3** and check the result against what §1, §7, and §11
   claim; the repo wins, so correct the file when they disagree.
2. **Open the sub-phase in STATE.md §1 before editing any code**: `Type:
   sub-phase`, its `ID`, `Status: OPEN`, `Intent`, `Next action:`, and §6 set to
   `claimed — nothing written yet`.
3. **Close it after the gates are green**: append the §4 ledger row, reset §6 to
   `none — tree consistent`, update §5 §7 §8 §9 §10 and the §11 board, point §1
   at the next unit with `Status: none`, bump the timestamp. When STATE.md §3 has
   WIP commits on, commit the closed sub-phase and put its sha in the §4 row. A
   sub-phase is not done until this is written.
4. If the session ends mid-sub-phase, leave §1 `OPEN` and write exactly what is
   half-finished into §6 before stopping — plus a `wip(<N.Y>)` commit when WIP
   commits are on.

---

## Directory layout (normative)

Create exactly this shape. Later phases add files inside it; they must not
invent sibling directories.

```text
web/
├── package.json
├── pnpm-lock.yaml
├── tsconfig.json            # solution file, references the two below
├── tsconfig.app.json        # src/**  — DOM libs, strict
├── tsconfig.node.json       # vite.config.ts, scripts/**
├── vite.config.ts
├── eslint.config.js         # flat config
├── .prettierrc.json
├── components.json          # shadcn CLI configuration
├── index.html
├── public/                  # static files copied verbatim (favicon only)
├── scripts/
│   └── gen-api-types.ts     # openapi-typescript wrapper (phase 6)
└── src/
    ├── main.tsx             # entry point
    ├── App.tsx              # router root
    ├── index.css            # Tailwind entry + design tokens
    ├── vite-env.d.ts
    ├── api/                 # generated types + typed client   (phase 6)
    ├── components/
    │   ├── ui/              # shadcn primitives, generated
    │   ├── state/           # honesty primitives               (phase 6)
    │   ├── charts/          # chart wrappers + option builders (phase 9+)
    │   └── layout/          # shell, nav, header               (phase 7)
    ├── features/            # one directory per page
    ├── hooks/
    ├── lib/                 # pure functions: formatting, derivation
    ├── routes/              # route definitions and lazy imports
    └── test/                # test setup, MSW, fixtures        (phase 5)
```

Rule for every later phase: **pure data derivation goes in `src/lib/`, never in
a component.** That is what makes the test strategy in phase 5 possible.

---

## Sub-phases

### 4.1 Package manifest with exact pins

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation.
  **`agent-1:opus` review gate** — the pins encode research facts R1-R16; a
  drifted pin silently breaks linting or coverage.
- **Files:** `web/package.json`, `web/pnpm-lock.yaml`, `web/.npmrc`
- **Change:**
  1. `web/package.json` with `"name": "pglens-web"`, `"private": true`,
     `"type": "module"`, and `"packageManager": "pnpm@<version installed>"`.
  2. Dependencies, pinned exactly (no `^`, no `~`) so a fresh install cannot
     drift from the versions this plan was designed against:
     ```json
     "dependencies": {
       "@tanstack/react-query": "5.102.8",
       "@tanstack/react-table": "9.2.4",
       "@tanstack/react-virtual": "3.14.10",
       "@xyflow/react": "12.11.5",
       "class-variance-authority": "0.7.1",
       "clsx": "2.1.1",
       "date-fns": "4.4.0",
       "echarts": "6.1.0",
       "echarts-for-react": "3.0.6",
       "lucide-react": "1.37.0",
       "openapi-fetch": "0.17.0",
       "react": "19.2.8",
       "react-dom": "19.2.8",
       "react-router-dom": "7.18.3",
       "tailwind-merge": "3.6.0",
       "uplot": "1.6.32",
       "zustand": "5.0.15"
     }
     ```
  3. Dev dependencies:
     ```json
     "devDependencies": {
       "@eslint/js": "10.0.1",
       "@tailwindcss/vite": "4.3.3",
       "@types/node": "26.4.0",
       "@types/react": "19.2.18",
       "@types/react-dom": "19.2.5",
       "@vitejs/plugin-react": "6.1.1",
       "eslint": "10.9.1",
       "eslint-plugin-react-hooks": "7.1.1",
       "eslint-plugin-react-refresh": "0.5.5",
       "globals": "17.11.0",
       "openapi-typescript": "7.13.0",
       "prettier": "3.9.6",
       "prettier-plugin-tailwindcss": "0.8.1",
       "tailwindcss": "4.3.3",
       "tw-animate-css": "1.4.0",
       "typescript": "6.0.3",
       "typescript-eslint": "8.68.0",
       "vite": "8.2.2"
     }
     ```
     Test-only dependencies are added in phase 5, not here, so a failure in this
     sub-phase has one cause.
  4. **`typescript` is `6.0.3`, not `7.0.2`.** `typescript-eslint@8.68.0`
     declares `typescript >=4.8.4 <6.1.0` (R1); installing TypeScript 7 leaves
     the project with no typed linting. This is decision **D13**. Put a comment
     in `web/README.md` (created in 4.8) rather than in `package.json`, which
     does not support comments.
  5. Scripts:
     ```json
     "scripts": {
       "dev": "vite",
       "build": "tsc -b && vite build",
       "preview": "vite preview",
       "typecheck": "tsc -b --noEmit",
       "lint": "eslint .",
       "lint:fix": "eslint . --fix",
       "format": "prettier --write .",
       "format:check": "prettier --check ."
     }
     ```
  6. `web/.npmrc` with `engine-strict=true` so a Node below Vite 8's floor (R2)
     fails at install time with a clear message rather than at build time with a
     confusing one. Add `"engines": {"node": "^20.19.0 || >=22.12.0"}` to
     `package.json`.
  7. Run `pnpm install` and commit `pnpm-lock.yaml`. The lockfile is part of the
     contract; CI installs with `--frozen-lockfile`.
- **Unit tests:** none yet (no test runner until phase 5). The proof is
  `pnpm install --frozen-lockfile && pnpm build` succeeding in 4.7.
- **e2e tests:** none.
- **Done:** `pnpm install --frozen-lockfile` succeeds; `pnpm ls typescript`
  reports `6.0.3`; `agent-1:opus` has checked the pins against
  [overview.md](overview.md) § References; closed in `STATE.md`.

### 4.2 TypeScript configuration

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/tsconfig.json`, `web/tsconfig.app.json`,
  `web/tsconfig.node.json`
- **Change:**
  1. `tsconfig.json` is a solution file: `{"files": [], "references": [{"path":
     "./tsconfig.app.json"}, {"path": "./tsconfig.node.json"}]}`.
  2. `tsconfig.app.json` covers `src/**`. Required compiler options, all of them:
     `"target": "ES2023"`, `"lib": ["ES2023", "DOM", "DOM.Iterable"]`,
     `"module": "ESNext"`, `"moduleResolution": "bundler"`, `"jsx": "react-jsx"`,
     `"strict": true`, `"noUncheckedIndexedAccess": true`,
     `"exactOptionalPropertyTypes": true`, `"noImplicitOverride": true`,
     `"noFallthroughCasesInSwitch": true`, `"noUnusedLocals": true`,
     `"noUnusedParameters": true`, `"verbatimModuleSyntax": true`,
     `"isolatedModules": true`, `"skipLibCheck": true`, `"noEmit": true`,
     `"composite": true`.
     `noUncheckedIndexedAccess` matters more than usual here: nearly every API
     response is an array whose first element the UI wants, and this option is
     what forces the code to handle "the series is empty" instead of rendering
     `undefined` as a value. Do not relax it later; if a phase finds it painful,
     that pain is the honesty invariant I-2 asserting itself.
  3. Path alias `"paths": {"@/*": ["./src/*"]}` with `"baseUrl": "."`, matched by
     the Vite `resolve.alias` in 4.3 and by the shadcn `components.json` in 4.5.
     One alias only; do not add a second.
  4. `tsconfig.node.json` covers `vite.config.ts` and `scripts/**` with
     `"module": "ESNext"`, `"moduleResolution": "bundler"`,
     `"types": ["node"]`, `"composite": true`, `"noEmit": true`.
- **Unit tests:** none.
- **e2e tests:** none.
- **Done:** `pnpm typecheck` exits 0 on the scaffold; closed in `STATE.md`.

### 4.3 Vite configuration and the dev proxy

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/vite.config.ts`, `web/index.html`, `web/src/main.tsx`,
  `web/src/App.tsx`, `web/src/vite-env.d.ts`
- **Change:**
  1. `vite.config.ts`:
     ```ts
     export default defineConfig({
       plugins: [react(), tailwindcss()],
       resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
       build: {
         outDir: '../internal/webui/dist',
         emptyOutDir: true,
         sourcemap: false,
         chunkSizeWarningLimit: 900,
       },
       server: {
         port: 5173,
         proxy: {
           '/api': { target: 'http://127.0.0.1:8080', changeOrigin: false },
         },
       },
     })
     ```
     - `outDir` points **outside** `web/` straight into the Go embed directory.
       That is why `emptyOutDir: true` is required — Vite refuses to clear a
       directory outside the project root without it — and it is why
       `.gitignore` (phase 3 § 3.1) protects the placeholder.
     - `changeOrigin: false` keeps the `Host` header intact so the session
       cookie's `SameSite=Strict` behaves in development exactly as in
       production. Cookies are same-origin through the proxy, which is the whole
       reason for proxying rather than pointing the client at
       `http://localhost:8080` directly.
     - `sourcemap: false` keeps the embedded binary small; a debug build can be
       produced with `pnpm build --sourcemap` when needed.
  2. `index.html` with `<html lang="en">`, a `<title>pglens</title>`,
     `<meta name="viewport" content="width=device-width, initial-scale=1" />`,
     a `<div id="root"></div>` and `<script type="module" src="/src/main.tsx">`.
     Add `<meta name="color-scheme" content="dark light" />`.
  3. `src/main.tsx` mounting `<App />` into `#root` inside `React.StrictMode`.
  4. `src/App.tsx` rendering a single centred panel that states the interface is
     under construction and names the phase-appropriate next step. It is replaced
     in phase 7; keep it to twenty lines.
  5. Add a Vite build-info define so the UI can report its own version:
     `define: { __PGLENS_BUILD__: JSON.stringify(process.env.PGLENS_VERSION ?? 'dev') }`,
     declared in `src/vite-env.d.ts` as `declare const __PGLENS_BUILD__: string`.
- **Unit tests:** none.
- **e2e tests:** none.
- **Done:** `pnpm build` writes `internal/webui/dist/index.html` and hashed
  assets, and `internal/webui/dist/.built` is created by 4.6; closed in
  `STATE.md`.

### 4.4 Tailwind CSS 4 and the design tokens

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/src/index.css`
- **Change:**
  1. Tailwind 4 is configured from CSS (R9). `src/index.css` starts with:
     ```css
     @import "tailwindcss";
     @import "tw-animate-css";
     @custom-variant dark (&:where(.dark, .dark *));
     ```
     There is no `tailwind.config.js` and none must be created.
  2. Define the design tokens in `@theme` and the light/dark palettes in
     `:root` / `.dark` as CSS custom properties. The palette is a **dense
     dark-first operations console**, not a marketing page:
     - Neutrals: a near-black background, two elevated surfaces, a hairline
       border, and three text weights (primary, secondary, muted).
     - Semantic status colours mapped one-to-one onto the API's vocabulary, so a
       component never invents a colour: `--status-ok`, `--status-degraded`,
       `--status-critical`, `--status-unknown`, `--status-muted`,
       `--status-stale`. `--status-unknown` must be visually distinct from
       `--status-ok`; grey-on-grey defeats invariant I-2.
     - Severity colours for alerts and findings: `--severity-info`,
       `--severity-warning`, `--severity-critical`.
     - A monospace stack for identifiers, query text and numbers; a UI sans
       stack for prose. Tabular figures (`font-variant-numeric: tabular-nums`)
       on every numeric cell so columns do not jitter while polling.
     - Density variables `--row-h-compact` and `--row-h-comfortable`; tables
       default to compact.
  3. Set `color-scheme` on `:root` and default the document to dark by adding
     `class="dark"` on `<html>` in `index.html`; phase 7 § 7.5 makes it
     switchable.
  4. Add a focus-visible ring token and apply it globally, so keyboard
     navigation is usable from the first page — the accessibility gate in phase 5
     will otherwise fail every page at once.
- **Unit tests:** none (styling). Phase 5 § 5.7 adds the accessibility assertion
  that gives these tokens teeth.
- **e2e tests:** none.
- **Done:** `pnpm build` produces a stylesheet containing the tokens; a manual
  `pnpm dev` shows dark, legible text; closed in `STATE.md`.

### 4.5 shadcn/ui installation and the first primitives

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/components.json`, `web/src/lib/utils.ts`,
  `web/src/components/ui/**`
- **Change:**
  1. Create `components.json` for the shadcn CLI 4.x (R10) with
     `"style": "new-york"`, `"rsc": false`, `"tsx": true`,
     `"tailwind": {"css": "src/index.css", "baseColor": "neutral",
     "cssVariables": true}`, and aliases `"components": "@/components"`,
     `"utils": "@/lib/utils"`, `"ui": "@/components/ui"`,
     `"lib": "@/lib"`, `"hooks": "@/hooks"`.
  2. Add `src/lib/utils.ts` with the standard `cn` helper
     (`clsx` + `tailwind-merge`).
  3. Install the primitives this plan actually uses, and no others:
     `pnpm dlx shadcn@4.19.0 add button card badge table tabs tooltip dialog
     dropdown-menu select input label separator skeleton alert sheet popover
     scroll-area sonner`.
     Adding components speculatively bloats the bundle and the coverage
     denominator.
  4. Review each generated file once for the repository's professional standard:
     no emojis, no informal copy. The generated components are source code from
     this point on and are linted and type-checked like any other file.
- **Unit tests:** none — generated primitives are covered by the components that
  use them. Exclude `src/components/ui/**` from the coverage denominator in
  phase 5 § 5.6 and record the exclusion there.
- **e2e tests:** none.
- **Done:** `pnpm typecheck` and `pnpm lint` are clean with the primitives
  present; closed in `STATE.md`.

### 4.6 ESLint, Prettier, and the project's own rules

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `web/eslint.config.js`, `web/.prettierrc.json`,
  `web/.prettierignore`, `web/.eslintignore` (only if the flat config needs it)
- **Change:**
  1. Flat config composing `@eslint/js` recommended,
     `typescript-eslint` **type-checked** configuration (`recommendedTypeChecked`
     plus `stylisticTypeChecked`) with
     `languageOptions.parserOptions.projectService: true`,
     `eslint-plugin-react-hooks` recommended, and
     `eslint-plugin-react-refresh` for the Vite fast-refresh constraint.
     Type-checked linting is the point of pinning TypeScript to 6.0.3; a
     syntax-only config would make D13 pointless.
  2. Ignore `dist`, `../internal/webui/dist`, `coverage`, and
     `src/api/generated.ts` (phase 6 generates it; linting generated code
     produces noise nobody acts on).
  3. Project-specific rules that enforce this plan's invariants:
     - `"no-restricted-globals": ["error", {"name": "fetch", "message": "use the
       typed client from @/api/client"}]` — invariant I-3 and the 401 policy
       depend on every request going through one place.
     - `"no-restricted-syntax"` with a selector banning
       `CallExpression[callee.name='Number']` in `src/api/**` and
       `src/features/**`, message: "cluster_id and queryid are strings (I-5); use
       the string form". A targeted ban is better than a lint plugin nobody
       maintains.
     - `"@typescript-eslint/no-floating-promises": "error"` and
       `"@typescript-eslint/no-misused-promises": "error"` — an unawaited mutation
       in an event handler is the classic source of a UI that silently does
       nothing.
     - `"@typescript-eslint/switch-exhaustiveness-check": "error"` — the status
       and severity unions must be handled exhaustively, so a new API enum value
       becomes a compile error rather than an unrendered branch.
     - `"react-hooks/exhaustive-deps": "error"` (not `warn`).
  4. Prettier with `"semi": false`, `"singleQuote": true`, `"printWidth": 100`,
     `"trailingComma": "all"`, and the `prettier-plugin-tailwindcss` plugin so
     class order is normalised and diffs stay readable. Prettier does not run in
     the lint gate; `format:check` runs in CI as its own step so a formatting
     failure is distinguishable from a lint failure.
- **Unit tests:** none. Prove the custom rules by writing a file that violates
  each one, observing the error, and deleting it; record in `STATE.md` §7 that
  all four custom rules were demonstrated to fire.
- **e2e tests:** none.
- **Done:** `pnpm lint` and `pnpm format:check` are clean; each custom rule was
  demonstrated to fire; closed in `STATE.md`.

### 4.7 Makefile and CI integration

- **Model:** `agent-2:sonnet`
- **Assignment:** `agent-2:sonnet` — implementation
- **Files:** `Makefile`, `.github/workflows/ci.yml`
- **Change:**
  1. Add to `Makefile`, and to the `.PHONY` list at `Makefile:6`:
     ```make
     PNPM ?= pnpm
     WEB  := web

     web-install:
     	cd $(WEB) && $(PNPM) install --frozen-lockfile

     web-typecheck:
     	cd $(WEB) && $(PNPM) run typecheck

     web-lint:
     	cd $(WEB) && $(PNPM) run lint && $(PNPM) run format:check

     web-build:
     	cd $(WEB) && PGLENS_VERSION=$(VERSION) $(PNPM) run build
     	@printf 'built_at=%s\nvite=8.2.2\n' "$$(date -u +%Y-%m-%dT%H:%M:%SZ)" > internal/webui/dist/.built
     ```
     `web-test` and `web-coverage-gate` are added in phase 5; do not stub them
     here — a target that silently succeeds is worse than a missing one.
  2. Extend `ci-local-unit` with `web-install`, `web-lint`, `web-typecheck`,
     `web-build`, placed **after** the Go steps so a Go failure is reported
     first.
  3. Extend `clean` to remove `web/node_modules`, `web/dist` and
     `internal/webui/dist` except the committed `index.html`. Be careful: the
     rule must not delete the placeholder. Use
     `git checkout -- internal/webui/dist/index.html` after the removal, or
     delete only the non-placeholder entries.
  4. `.github/workflows/ci.yml`: add a `web` job running on `ubuntu-latest`
     with `actions/checkout@v4`, `pnpm/action-setup@v4`,
     `actions/setup-node@v4` pinned to Node 24 with
     `cache: 'pnpm'` and `cache-dependency-path: web/pnpm-lock.yaml`, then
     `make web-install web-lint web-typecheck web-build`. It runs in parallel
     with the Go jobs; it does not depend on them.
  5. Add `web/node_modules` and `web/dist` to `.gitignore` if they are not
     already covered.
- **Unit tests:** none.
- **e2e tests:** none.
- **Done:** `make ci-local-unit` runs the web chain and passes; the CI file is
  valid YAML (`python3 -c "import yaml,sys;yaml.safe_load(open('.github/workflows/ci.yml'))"`
  or an equivalent check); closed in `STATE.md`.

### 4.8 Update README.md and add `web/README.md`

Mandatory closing sub-phase. User guide only — no implementation detail.

- **Model:** `agent-3:haiku`
- **Assignment:** `agent-3:haiku` — documentation
- **Files:** `README.md`, `web/README.md` (new), `CONTRIBUTING.md`
- **Change:**
  - `README.md` **Requirements**: add "Node 20.19+ or 22.12+ and pnpm, only to
    build the web interface from source". **Building**: add
    `make web-build` with one line saying it produces the interface that
    `pglens-server` serves, and note that `make build` alone produces a binary
    with a placeholder page. **Running the checks**: add `make web-lint`,
    `make web-typecheck`, `make web-build`.
  - `web/README.md`: a short contributor-facing page — how to run `pnpm dev`
    against a local server on `:8080` through the proxy, the directory layout
    from this file, the rule that pure derivation lives in `src/lib/`, and the
    TypeScript 6.0.3 pin with its one-line reason and revisit condition (D13).
    This file may reference the plan because it is a contributor document, not
    the user README.
  - `CONTRIBUTING.md`: add the web gate commands to the existing list of checks
    a change must pass.
- **Unit tests:** none (documentation).
- **e2e tests:** none — every documented command was executed.
- **Done:** a contributor can install, run and build the frontend from the two
  READMEs alone; the root README contains no implementation detail; gates green;
  closed in `STATE.md` with the §11 docs row for phase 4 set.

---

## Phase gates

- **Fmt:** `make fmt-check` and `cd web && pnpm format:check`
- **Lint:** `make lint` and `make web-lint`
- **Typecheck:** `make web-typecheck`
- **Test subset:** `make test` and `make coverage-gate` (Go side unchanged)
- **Build:** `make web-build` then `make build`, and
  `go test ./internal/webui/...` proving the embed now reports a real build
- **Regression guard:** `make test-e2e` still green
- **README:** requirements, build commands and the new `web/README.md`

## Phase done criterion

`make web-install web-lint web-typecheck web-build` succeeds from a clean
checkout, `internal/webui/dist` contains a hashed Vite build and a `.built`
marker, `pglens-server` serves that build at `/`, `make ci-local-unit` includes
and passes the web chain, and `STATE.md` §11 shows phase 4 `DONE` with every
sub-phase closed.
