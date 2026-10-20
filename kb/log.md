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
## [2026-10-20] compile | docs/user-guide/home.md, docs/user-guide/appearance.md → home.overview, preferences.appearance (new guide pages for the app shell's first slice: Home, search and commands, appearance)

## [2026-10-20] compile | docs/user-guide/approval-policies.md, docs/user-guide/approvals.md → approvals.policies, approvals.decide (approval policies as data, steps, withdrawing a request; PRD rev 0.40)

## [2026-10-20] compile | docs/user-guide/statutory-packs.md → settings.statutory-packs (activating a pack version through the approval engine; PRD rev 0.41)

## [2026-10-20] compile | docs/user-guide/approvals.md → approvals.decide (activating a statutory pack waits for approval; links settings.statutory-packs)

## [2026-10-20] compile | docs/user-guide/team-mode.md (new), docs/user-guide/members-and-roles.md → settings.team-mode (new), members.roles, members.invite (team modes and SOD-6, PRD rev 0.42)

## [2026-10-20] compile | docs/user-guide/approvals.md → approvals.decide (a team mode switch voids waiting requests, rev 0.42)

## [2026-10-20] compile | docs/user-guide/sign-in.md → auth.sign-in (passkeys and the mandatory second factor, PRD rev 0.43)

## [2026-10-20] compile | docs/user-guide/approvals.md → approvals.decide (approvals signed with a passkey, PRD rev 0.44)

## [2026-10-20] compile | docs/user-guide/sign-in.md → auth.sign-in (your devices and sign-in emails, PRD rev 0.45)

## [2026-10-20] compile | docs/user-guide/approvals.md → approvals.decide (the review log, PRD rev 0.46)

## [2026-10-20] compile | home.md, sign-in.md, profile-and-security.md → home.overview, auth.sign-in, preferences.security (Settings and Profile and security pages in the web app)

## [2026-10-20] compile | home.md, sign-in.md, profile-and-security.md → home.overview, auth.sign-in, preferences.security (Settings and Profile and security pages in the web app)

## [2026-10-20] compile | approvals.md → approvals.decide (Inbox, the request page and its confirmation, the review log in the web app)

## [2026-10-20] compile | members-and-roles.md → members.roles, members.invite (Settings › Members and roles in the web app)

## [2026-10-20] compile | team-mode.md, approval-policies.md → settings.team-mode, approvals.policies (Settings › Team mode and Approval policies in the web app)

## [2026-10-20] compile | organisation-settings.md, statutory-packs.md → settings.organisation, settings.change, settings.statutory-packs (Settings › Organisation and Statutory parameter packs in the web app)

## [2026-10-20] compile | chart-of-accounts.md → accounting.chart (new guide for Accounting › Chart of accounts in the web app)

## [2026-10-20] compile | organisation-settings.md → settings.organisation, settings.change (related pages)

## [2026-10-20] compile | contacts-and-items.md → contacts.manage (new guide for Contacts and Items and prices in the web app)

## [2026-10-20] compile | chart-of-accounts.md → accounting.chart (related pages)

## [2026-10-20] compile | home.md → home.overview (Home's Waiting for you and Set up the company)

## [2026-10-20] compile | home.md → home.overview (the roles step on Home's checklist)

## [2026-10-21] compile | docs/user-guide/journals.md, periods-and-close.md (new) and organisation-settings.md (the years fixed once a period is open) → accounting.journals, accounting.periods (new), settings.organisation, settings.change (journal screens and periods page, p1-general-ledger-close)

## [2026-10-21] compile | docs/user-guide/opening-balances.md (new), periods-and-close.md (related link) → accounting.opening-balances (new), accounting.periods (opening balances screen, p1-general-ledger-close 3.1, 7.2)

## [2026-10-21] compile | docs/user-guide/periods-and-close.md (close a month) → accounting.periods (the close page, p1-general-ledger-close 1.2, 7.2)

## [2026-10-21] compile | docs/user-guide/periods-and-close.md (reopen a month) → accounting.periods (reopening, p1-general-ledger-close 1.3, 7.2)

## [2026-10-21] compile | docs/user-guide/general-ledger.md (new) → reports.general-ledger (the general ledger screen, p1-general-ledger-close 4.1, 7.3); journals.md (related link) → accounting.journals

## [2026-10-21] compile | docs/user-guide/organisation-settings.md (self-approval threshold) → settings.organisation, settings.change (journal approvals, p1-general-ledger-close 2.2)

## [2026-10-21] compile | docs/user-guide/journals.md (journals above the self-approval threshold, ask to reverse), approvals.md (journal approvals, self-approved log) → accounting.journals, approvals.decide (p1-general-ledger-close 2.2)

## [2026-10-21] compile | docs/user-guide/journals.md (edit a posted journal) → accounting.journals (p1-general-ledger-close 2.3, 7.1)

## [2026-10-21] compile | docs/user-guide/journals.md (journals that reverse by themselves) → accounting.journals (p1-general-ledger-close 2.4)
