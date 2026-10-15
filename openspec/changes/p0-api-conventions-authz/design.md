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
- **Idempotency store.** The key plus a request hash per organisation and caller, kept 24 hours; a reused
  key with a different body returns 422 (FR-INT-08; this design said 409). The key is claimed, the command
  runs and the response is saved in one unit of work (the command's own unit of work joins it), so a
  failed command leaves the key free and a concurrent repeat waits, then replays. The worker purges
  expired keys hourly; an expired key not yet purged is refused (422), never replayed.
- **The contract in CI.** huma generates the document; `contracts/openapi/fyniq.v1.yaml` is that document,
  reviewed in the pull request, and `make test` fails while they differ (`make openapi` regenerates it).
  NFR-API-01 names Spectral; P0 checks the same fields with a Go test over the document (7a), and 1.2/1.3
  bring the Node tooling (the client, `oasdiff`, Spectral) in a second pull request.
- **Who is calling, before the OAuth server.** A principal (subject, client, class, organisation, roles)
  behind a bearer token. The release build accepts no token until p0-identity-oauth-first-run; the local
  build accepts the local issuer's dev tokens, which carry the roles themselves.
- **Reading Appendix D.** Any right on a capability lets its holder view it; M is a masked view; a cell's
  qualifier (an amount limit, SOD, FM-1) is carried in the policy and enforced by the approval engine (P1);
  an AI cell without letters ("Proposal") grants nothing through the API. The caller's organisation is
  readable by every member (no Appendix D row). The Company secretary takes the Board column; the HR
  officer (Nemura only) and the Super admin hold no organisation right.
- **Audit events.** Each write endpoint emits through the unit of work (p0-audit-chain); a denied
  request writes a security log entry, not an audit event.
- **Ports.** None new.

## Risks / Trade-offs

- [The matrix grows as roles × endpoints] → generated, not hand-written; a new endpoint without a policy
  fails to compile its registration.
