# apps

The web apps, in one pnpm workspace (PRD-001 §12.4).

| App | What |
|---|---|
| `web/` | Fyniq's React 19 + Vite single-page app. P0 is a shell that loads the design tokens and shows the API's version; the screens are built in P1 from `docs/design/screens/`. It calls the API only through the client generated from `contracts/openapi/` (golden rule 4) |

`apps/nemura-web/` arrives with Nemura's P1 (PRD-002). Run with `make dev` (Vite, hot reload) or
`make up` (the built image behind Caddy); test with `make test` (typecheck) and `make test-e2e`.
