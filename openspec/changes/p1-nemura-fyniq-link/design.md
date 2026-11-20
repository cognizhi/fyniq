# Design

## Context

Nemura's outbox, API clients (single Owner with a fresh passkey), passkeys (`NEMURA_RP_ID`, the shared registrable domain) and approvals exist, and
the archtest forbids Nemura importing Fyniq's code: both sides implement the contract and each tests the same fixtures. Nemura has no link, no payroll
engine yet (`p1-nemura-payroll`, another session's), so payroll events are built and tested from contract fixtures.

## Goals / Non-Goals

**Goals:** the link record and lifecycle on Nemura's side; the signed outbox events and inbox; the approval envelope for a run; the segregation rules across
both apps; one sign-in and the switch; health and restore receipts.
**Non-Goals:** the payroll calculation, payslips, the CPF file (`p1-nemura-payroll`, `p2-payroll-outputs`); a standalone Nemura's journal file (NM-ARC-01);
the people assistant (`p1-nemura-assistant-external`).

## Decisions

- **D1. The shared design is `p1-payroll-link`'s D1 to D12.** The envelope, its canonical bytes, the two credentials, the passkey verification source
  (D3) and the second approver (D11) are decided once there; this change implements Nemura's half and tests the same fixtures.
- **D2. The link record is Nemura's too.** `fyniq_link` (state, epoch, Fyniq organisation, Fyniq endpoint, the link key reference, the client), moved
  through proposed, active, suspended, unlinking and unlinked by the Owner and a second approver; the epoch increases on each re-link and every event
  carries it. Unlinking waits for unsettled work (runs approved and not posted, unknown payment outcomes, holds moving to Fyniq, events waiting).
- **D3. The link key and its custody.** An Ed25519 key pair made when the link is proposed; the private half is held by Nemura's worker role only (sealed
  at the application level, never in logs or the API), the public half is given to Fyniq in the approved link. Rotation creates a new key and a new epoch.
- **D4. The outbox gives delivery, the inbox the outcome (ADR-T7).** Events leave through Nemura's outbox with the envelope signed at send time; the
  worker's HTTP calls go through `internal/egress`; Fyniq's answers (`payroll.run.posted`, `payroll.payments.status`) arrive as signed webhooks into
  Nemura's inbox with the same rules (dedupe by event ID, older versions ignored, tombstones, gap repair).
- **D5. Priority events go ahead of ordinary sync.** `people.tax_hold.changed` and `people.bank_details.changed` use a priority queue so a hold reaches
  Fyniq before the next batch release; the near-pay-day alert fires within the hour (NM-INT-04).
- **D6. Sessions.** Each app keeps its own audience-bound session made from the shared sign-in; deactivation or sign-out in one writes a revocation the other
  reads (a signed `identity.session.revoked` event and a poll fallback) so both end within 60 seconds. People are matched by issuer, subject and tenant,
  never by email; linking a tenant that ran standalone maps each person explicitly and grants no Fyniq role.
- **D7. The switch.** Fyniq's header shows Finance | People when the person's rights span both apps (computed from Fyniq's role set and Nemura's role
  answer, never the other app's token); it keeps the last page in each app; a person with Nemura rights only lands on People home.
- **D8. Restore.** Both apps keep a per-run receipt (submission digest, posting reference, instruction IDs and outcomes) and a reconciliation checkpoint
  (the highest event sequence each side has acknowledged); after a restore, work after the checkpoint is held as unknown and reconciled against the other
  app's receipts before anything is sent or paid.

- **D9. The handshake has three legs and a proposed state (decided 20 Nov 2026 while the owner was away; owner to confirm).** Each app holds the other's public keys, so neither can
  be made first with the other's. Nemura *proposes* (an Owner, fresh passkey): it makes its two keys and the ID Fyniq's client will have at Nemura's own OAuth server, and keeps the link in
  the new state `proposed`. Fyniq links with those (the dual-approved request of `p1-payroll-link` 2.2), makes *its* two keys and registers Nemura's client at Fyniq's OAuth server. Nemura
  *activates* (another Owner or Administrator, fresh passkey) with Fyniq's envelope key and client ID, and registers Fyniq's client at Nemura's own OAuth server under the ID chosen at
  the proposal. *Alternative:* the owner of the link page copies one bundle both ways with a third round trip (rejected: more manual steps, same trust).
- **D10. Two keys per side, never one.** The envelope key (Ed25519) signs what the side sends; the client key (ES256) signs the `private_key_jwt` assertion that gets its token. A stolen token
  has no envelope key, a stolen envelope key has no token (`p1-payroll-link` D1). Both private halves are written once by the API and readable by the worker role alone.
- **D11. Fyniq's client at Nemura is not an ordinary API client.** Its scope, `link.events.write`, is not in `apiclient.Scopes`, so no registration (single Owner or second-approver
  request) can grant it; the link registers the client itself, with that scope alone, under the proposal's ID. Nemura's resource server (`internal/nemura/linkauth`) introspects the bearer
  token at every call (cached at most 30 seconds), accepts only Nemura's audience, the client's own token (subject = client ID) and that scope, and finds the organisation through a global
  route written with the link (`nemura.link_client_route`, read through one function, as the identity directory is). It reads a bearer token on the inbox operation only; every other operation
  still reads a session cookie only. The inbox then re-checks the same link, epoch and client with row-level security on.
- **D12. Endpoints are deployment configuration (owner to confirm).** Where each side's API and token endpoint are comes from the deployment's settings, not from a row of the link, so the
  egress allow-list is fixed when the service starts and a link cannot point the worker at another host.
- **D13. A retry is the same event.** The sender fixes the event ID and sequence when the event is queued and signs the time at each attempt; Fyniq's dedupe by event ID then
  absorbs a retry after a lost answer.

- **D14. The ask at release has a client of its own (decided 21 Nov 2026 while the owner was away; owner to confirm).** Fyniq asks Nemura, the hold's authority, whether a person is held (`GET /v1/link/people/{person_id}/hold`) with a
  second client whose only scope is `link.holds.read` and whose key the API holds, so the API never holds the key that signs events (the worker's alone). Nemura registers it at the approval, under an ID it chose at the proposal;
  a token of one client is no token of the other (the route says which each is). *Alternatives:* a SELECT for the API on the events' key (rejected: widens a grant, breaks custody); the ask through the worker (rejected: slow
  and a second hop in a release).

- **D15. One sign-in is one identity-provider tenant (owner's decision, 21 Nov 2026).** Neither app is the other's identity provider: both sign people in through the same provider's tenant (ADR-T3), so an identity is the same `(issuer, subject)` in both and the approver's passkey, enrolled in Fyniq against it, is found by Fyniq's `UserForIdentity`. A Nemura session records the provider and the identity it was signed in with (migration 000048); the pay run's approver is that identity, not the member's earliest-linked one (a member can be linked to one identity per issuer, so the earliest could be one Fyniq has never seen). The preparer, who is not signing, stays the earliest-linked.
- **D16. What each app tells the other of an identity (design D7's answer from the other app; decided 21 Nov 2026 while the owner was away; owner to confirm).** One event type, `identity.access.changed`, both ways, signed with the link's key like every event: Fyniq to Nemura `{issuer, subject, finance, passkey}` over `link.events.write`; Nemura to Fyniq `{issuer, subject, people_access, payroll_approver}` over `people.projection.write`. *Why events:* a read across the link needs a new scope (a widening), a push over the scopes the link already has needs none, and a forged answer can only show a door or a reminder. The version is the sending event's sequence (it rises with every event of the link, so a newer answer always wins). *Alternatives:* a `link.sync.read` read of roles (rejected: the owner limited that scope to identities for replay and health, and role names would cross); roles by name in the event (rejected: minimal means two answers).
- **D17. The switch rule and the hint.** Fyniq's switch shows when the person holds a finance role here and Nemura has said the identity is an active member there; Nemura's when Fyniq has said the identity holds a finance role (any active member, not only the Owner, Administrators and Payroll administrators it was limited to before). Both carry `&provider=<the session's provider>`, which the destination's sign-in checks against the providers it offers, so the person is sent straight to their provider and no sign-in page shows; a real provider's re-use of its own session without a prompt is `[live-verify]`. The passkey warning is a hint before the signature; Fyniq's check after it (`PPL-19`) decides.
- **D18. An HR-only person signing in through Fyniq (left, owner's decision).** Landing on Nemura needs Nemura's rights by identity before Fyniq knows the person's organisation: a cross-organisation lookup by identity, a widening. Not built: they open Nemura, whose switch takes them to Fyniq once Fyniq has said they hold a finance role. *Options:* (a) leave as built (recommended: no lookup, no widening); (b) a deployment setting that names Nemura on Fyniq's "no organisation yet" page (no lookup, a static link); (c) a global identity directory (rejected: it breaks tenant isolation).
- **D19. Nemura ends sessions here, narrowly (owner's decision, 21 Nov 2026: `identity.sessions.write`, mapped users only).** The link's client holds one more scope; it opens one operation, `POST /v1/people-link/session-events`. Nemura queues `identity.session.revoked` with a sign-out (the identity of that session, recorded by D15) and a deactivation (each identity of the member): an issuer, a subject and a reason, never an email. Fyniq ends the sessions of the user behind the identity **only when that user is mapped to a person of the link** (`mapPersonToUser`: the person is Nemura's own), changes no membership, role or passkey, and answers `applied` for every valid event, so the call is no way to learn who has an account. A mapped user who belongs to two Fyniq organisations signs in again in both (sessions are the user's own): accepted, because only people the organisation itself mapped can be reached and they only lose a session. A link made before the scope existed keeps the six it has and is approved again to hold the seventh (the client is registered with the scopes then in force). The 60-second poll fallback is not built. *Alternatives:* end only the sessions that began in this organisation (sessions carry none); a scope on the events' client at Nemura (rejected: a stolen events token must not end sessions).
- **D20. The link's event log by identity (owner's decision, 21 Nov 2026: `link.sync.read`).** The read-only client that asks for a hold also holds `link.sync.read` at Nemura's OAuth server and reads `GET /v1/link/sync`: each event's sequence, ID, type, object, version and state, the highest sequence queued, paged by cursor, no payload. Fyniq compares it with its inbox to find a gap and to show health. The key stays with the API role (the worker never holds it), so the comparison is made when the link's health is asked for, not by a scheduled job. The events' client cannot read it and the reading client cannot send events (the token's check and the service's). A link made before the scope existed is approved again to hold it.
- **D21. Health and repair are asked for, not scheduled (decided 21 Nov 2026 while the owner was away; owner to confirm).** Fyniq's reading key is the API role's alone (D14, D20), so no job can use it. `GET /v1/people-link/health` reads Nemura's log by identity (up to 40 pages of 500 events), sets the events Nemura says it **sent** against Fyniq's inbox and names the runs of sequence numbers the inbox lacks (a waiting or failing event is on its way, a refused one is Nemura's own record: neither is a gap); `POST /v1/people-link/repair` asks Nemura for each run once, as `link.replay.requested` through the events' client, which Nemura already answers with the current state (now including what an identity may enter). What cannot be read (no link, not active, no ask key on a link made before the scope, Nemura silent, another epoch) is said and never guessed. A replay is answered at new sequence numbers, so each request is kept (`people_link_replay`) and the numbers asked for in the last day are shown as asked for, not missing (a second repair asks for nothing, and they show as missing again after a day). A log longer than one look (20,000 events) is read from a watermark and the health says the oldest part was left out.
- **D22. An access answer belongs to its link's epoch.** A sequence starts again at 1 with each link, so Fyniq's and Nemura's `identity.access.changed` are kept with the epoch they came in (Nemura 000052, Fyniq 000918): the newest (epoch, version) wins and only the live link's epoch is read, so an answer of an ended link decides no switch and no reminder and cannot block the new link's first answer. *Alternative:* a scheduled gap check (rejected: it needs the key in the worker, a widening); the 24-hour notice and the alert near pay day then ride on the page being opened and on Nemura's own notices (left).

## Risks / Trade-offs

- Cross-app tests need two databases and two services: the stack tests use each app's own Testcontainers database and a loopback pair, never the cloud.
- A session-revocation window of 60 seconds is a poll interval plus the event; the test fixes the clock, the cloud's latency is `[live-verify]`.

## Open questions

None blocking; D3 and D11 of `p1-payroll-link` await the owner.
