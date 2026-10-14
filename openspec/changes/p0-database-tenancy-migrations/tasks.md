# Tasks

## 1. Migrations and roles

- [x] 1.1 Add goose with `cmd/migrate`, the first migration (roles, schema, extensions) and squawk in `make test`; verify `make reset` applies all migrations to an empty database and squawk passes (`// req: SEC-03`)
- [x] 1.2 Create `fyniq_migrator`, one role per service and a read-only role, with no standing human write role; test `// req: SEC-03` asserts each role's grants from the catalogue

## 2. Tenancy

- [x] 2.1 Add `organisation_id`, `FORCE ROW LEVEL SECURITY` and a policy on every tenant table, keyed on `current_setting('app.org_id', true)`; test `// req: INV-06` (catalogue test: every table has RLS forced and a policy) and `// req: CTL-IAM-05`
- [x] 2.2 Make the unit of work set `app.org_id` with `set_config(…, true)` first in every transaction; test `// req: SEC-02` writes as organisation A and reads as organisation B on a pooled connection, and sees nothing

## 3. Ledger tables and invariants

- [x] 3.1 Create organisations, accounts, periods, journal entries and lines, the ledger revision counter, posting identity and idempotency keys, with money as `NUMERIC(20,4)` and FX rates as `NUMERIC(20,10)`; verify with `make reset` and the schema snapshot test
- [x] 3.2 Enforce balanced entries at commit with a deferred constraint trigger; attack test `// req: INV-01` inserts an unbalanced entry directly in SQL and expects the commit to fail
- [x] 3.3 Make posted entries and lines immutable by grant and trigger; attack test `// req: INV-02` tries UPDATE and DELETE as every role and expects each to fail
- [x] 3.4 Reject postings into a closed period and to non-leaf, inactive or foreign accounts; attack tests `// req: INV-03` and `// req: INV-04`
- [x] 3.5 Enforce minor-unit scale per currency; attack test `// req: INV-05`
- [x] 3.6 Add the materialised balances cache, updated in the posting transaction, and its rebuild; test `// req: INV-08` compares the cache with a recomputation after random postings, and `// req: INV-09` asserts the trial balance nets to zero
- [x] 3.7 Take locks in the fixed order under READ COMMITTED, with the ledger revision taken by every entry; race test `// req: ENG-47` runs a posting and a period close on independent connections and asserts exactly one order commits, and `// req: ENG-46` asserts revisions are gap-free per organisation

## 4. Storage port

- [x] 4.1 Define `StoragePort` and `UnitOfWork` and implement them in `internal/adapters/postgres` with pgx v5 and sqlc; test `// req: NFR-PORT-01` and `// req: NFR-PORT-07`
- [x] 4.2 Write the storage conformance suite and run it against PostgreSQL 18 with Testcontainers; test `// req: NFR-PORT-04` and `// req: NFR-PORT-02`
- [ ] 4.3 Collect the attack and race tests under `make test` as the QA-ENG-05 suite; verify it blocks merge in CI (`// req: QA-ENG-05`), then `make check`
