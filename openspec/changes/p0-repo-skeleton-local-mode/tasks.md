# Tasks

## 1. Layout and toolchain

- [ ] 1.1 Create the §12.4 layout (`cmd/{api,worker,executor,mcp,migrate,seed}`, `internal/{domain,ports,adapters/local,adapters/gcp,runtime}`, `apps/web`, `contracts/openapi`, `db/migrations`, `oracle`, `deploy/docker`) with a README per top-level directory; verify `go build ./...` and `pnpm -r build` pass (NFR-MAINT-01)
- [ ] 1.2 Pin Go, Node, pnpm, Python, Typst, OpenTofu, golangci-lint, watchexec and goose in `mise.toml`, and add `.devcontainer/` built from it; verify `mise install` on a clean container and `devcontainer build` in CI (DEV-03)

## 2. Ports, clock and build flavours

- [ ] 2.1 Add the `Clock` port with a system clock, a frozen test clock and a "business date" override honoured only outside production; test `// req: DEV-05` freezes time and asserts the override is ignored when `FYNIQ_ENV=production`
- [ ] 2.2 Add the `Secrets`, `BlobStore` and `Queue` ports with local adapters behind `//go:build local`; verify with the adapter conformance suite skeleton (`// req: NFR-PORT-04`)
- [ ] 2.3 Make release binaries refuse to start with a local adapter or an unexpected `FYNIQ_ENV`; test `// req: DEV-07` and `// req: SEC-16` builds both flavours and asserts the exit code and message
- [ ] 2.4 Add the architecture test that forbids cloud SDK imports outside `internal/adapters/<provider>/`; test `// req: NFR-PORT-03` fails on a planted import in a fixture package

## 3. Services and the make contract

- [ ] 3.1 Give every service `/healthz`, `/readyz` (database and dependencies) and `/version` (SemVer, git SHA, build time); test `// req: NFR-IMG-06` calls each endpoint in local mode
- [ ] 3.2 Add the Makefile targets `setup`, `dev`, `up`, `down`, `seed`, `reset`, `test`, `test-e2e`, `evals`, `images`, `ci`, each with a `## description`; verify `make help` lists them and `make test` passes with no cloud credentials (`// req: DEV-02`)
- [ ] 3.3 Extend `deploy/local/` Compose with the services and wire `watchexec` and Vite HMR; record first-run and later-run times for `make setup && make dev` and the change-to-reload time in CI (`// req: DEV-01`, `// req: NFR-DEVX-01`, `// req: NFR-DEVX-03`)
- [ ] 3.4 Make `make test` run every merge gate available so far within the NFR-DEVX-02 budget, and `test-e2e` a Playwright smoke of the web shell; verify both in CI (`// req: NFR-DEVX-02`)

## 4. Sample company and docs

- [ ] 4.1 Write `cmd/seed` as the one sample-company generator, deterministic from a seed and synthetic only; test `// req: DEV-04` asserts two runs with the same seed are byte-identical, and `// req: DEV-08` asserts no value outside `sample-data.json` vocabularies
- [ ] 4.2 Update the AGENTS.md commands table (targets now "Today") and the `fyniq-local-dev` skill; verify `make check` passes
