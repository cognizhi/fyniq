## What and why

<!-- One or two sentences. The PR title must be a Conventional Commit: type(scope): summary -->

**Requirements:** <!-- e.g. FR-GL-08, AI-34 (PRD-001) -->

## Upgrade notes

<!-- What users, operators or integrators must do, or "None". Breaking change? Add `!` to the
     title and a "BREAKING CHANGE:" footer. release-please copies this into the changelog. -->

None.

## Checklist (PRD-001 §16.3)

- [ ] Tests at the right levels; a regression test for a bug; `// req:` tags
- [ ] `make check` passes (from P0: `make test` offline and `make up` starts)
- [ ] Audit events for new state changes; no secrets, NRIC or bank numbers in logs
- [ ] API change? Contract updated, additive inside `/v1`, examples and error codes documented
- [ ] AI/MCP surface touched? Never-list intact, evals and red-team suites pass
- [ ] UI change? Tokens only, Basic/Expert tag, light/dark/high-contrast checked, keyboard path
- [ ] Docs updated (PRD `(rev 0.N)`, skills, runbooks) — nothing hand-edited in CHANGELOG or VERSION
