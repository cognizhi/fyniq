# cmd

One entry point per service or tool, each its own binary and image (PRD-001 §12.3, §12.4), all
sharing `internal/app` for start-up checks (DEV-07, SEC-16), the clock (DEV-05) and the HTTP shell
(`/healthz`, `/readyz`, `/version`, NFR-IMG-06).

| Command | Kind | Arrives with |
|---|---|---|
| `api` | Service: the REST API under `/v1` | p0-api-conventions-authz |
| `ai` | Service: read tools and proposals, no write credential | P1 |
| `mcp` | Service: the MCP endpoint | P1 (token path in p0-identity-oauth-first-run) |
| `worker` | Service: background handlers from the outbox | p0-outbox-background-work |
| `executor` | Service: executes approved proposals under its own identity | P1 |
| `render` | Service: PDFs with Typst | P1 |
| `jobs` | Job: long work (nightly verification, anchoring, imports) | p0-audit-chain |
| `migrate` | Job: database migrations under their own role | p0-database-tenancy-migrations |
| `seed` | Tool: the one sample-company generator (DEV-04) | Now |

Every binary has a release flavour and a local flavour (`-tags local`); see `deploy/docker/`.
