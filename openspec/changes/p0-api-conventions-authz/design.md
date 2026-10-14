# Design

## Context

ADR-T1 names huma for OpenAPI 3.1, checked against `contracts/`. Appendix D is the permission matrix for
Standard mode, and §7.1 lists 18 roles.

## Goals / Non-Goals

**Goals:**
- No endpoint can ship without documentation, a permission and a matrix row.

**Non-Goals:**
- The public API for third parties (P2), webhooks and SDKs other than the TypeScript client.

## Decisions

- **Contract first.** The YAML in `contracts/` is reviewed, and huma's generated document must match it
  in CI. Alternative: code-first generation, rejected because reviewers would see only code.
- **One policy table.** `internal/authz/policy.go` maps operation → required right, attribute rules
  and masked fields. The handler wrapper refuses an operation with no policy, and the matrix test is
  generated from the same table and Appendix D's roles.
- **Problem details.** Every error has `type` (a stable URI per code), `title`, `detail` naming the field
  and the fix (golden rule 13), and `code`.
- **Idempotency store.** The key plus a request hash per organisation, kept 24 hours; a reused key with a
  different body returns 409.
- **Audit events.** Each write endpoint emits through the unit of work (p0-audit-chain); a denied
  request writes a security log entry, not an audit event.
- **Ports.** None new.

## Risks / Trade-offs

- [The matrix grows as roles × endpoints] → generated, not hand-written; a new endpoint without a policy
  fails to compile its registration.
