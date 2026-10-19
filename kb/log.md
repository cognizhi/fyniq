# Knowledge-base log

Append-only. One entry per compile, edit, lint or rollback, written with
`python3 scripts/kb.py log <op> "<text>"`. Newest last.

## [2026-10-12] compile | founders-money.md → founders.balance, founders.settle-balance, founders.pay-choice (initial compile, kb-schema@1)

## [2026-10-12] compile | founders.balance: add aliases (kb-schema@1 optional field; no vectors, AI-58 rev 0.5)

## [2026-10-13] compile | docs/user-guide/founders-money.md → founders-balance, founders-pay-choice, founders-settle-balance (product renamed Fyniq; wording only)

## [2026-10-14] compile | docs/user-guide/claims-my-claims.md, claims-approve-team.md, nemura/leave-apply.md, nemura/leave-sick.md → claims.make-a-claim, claims.approve-team-claims, leave.apply, leave.report-sick (new guide pages for the personal workspace and Nemura leave; all draft)

## [2026-10-14] compile | docs/user-guide/needs-verification.md → review.needs-verification (new: the FR-UX-41 banner, PRD-001 rev 0.22); founders.settle-balance links to it

## [2026-10-15] compile | docs/user-guide/sign-in.md, docs/user-guide/setup-first-run.md → auth.sign-in, installation.setup (new: the P0 sign-in and first-run setup screens, p0-identity-oauth-first-run slice 4c)

## [2026-10-20] compile | docs/user-guide/organisation-settings.md → settings.organisation, settings.change (new: organisation settings with recommended values, effective dates and the second-approver gate, p1-organisation-chart task 2.3; draft until the accountant reviews the tolerances and threshold)

## [2026-10-20] compile | docs/user-guide/members-and-roles.md → members.roles, members.invite (new: members, roles and invitations with the founder bootstrap, PRD-001 rev 0.35, p1-users-roles task 2.1)

## [2026-10-20] compile | docs/user-guide/approvals.md (new), members-and-roles.md, organisation-settings.md → approvals.decide (new), members.roles, members.invite, settings.change, settings.organisation (rev 0.36: changes under FM-1 now wait for a second approver instead of being refused)
