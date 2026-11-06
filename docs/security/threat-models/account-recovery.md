# Threat model: account recovery

| | |
|---|---|
| Feature and requirement IDs | A person who lost every authenticator recovers with a recovery code and an Owner's or Administrator's confirmation; a cooling-off follows: SEC-35, G4-060, AI-38, FM-3 (PRD-001 rev 0.8, rev 0.196, rev 0.198); `p1-users-roles` task 2.5 |
| Author, reviewer, date | Claude Code for the founders, 7 Nov 2026; reviewer: a founder at sign-off (second human review waived before production, AGENTS.md) |
| Data and its class | Recovery codes (keyed hashes only, under the field key), confirmation codes (SHA-256 of 130 random bits), recovery records, session token hashes |
| Who acts | The person, in a browser session that may have passed no second factor; an Owner or Administrator of their organisation, with step-up; never the AI, an MCP client or an OAuth client |

## 1. What we are building

A person makes ten recovery codes on their profile (`POST /v1/session/recovery-codes`, step-up). When they lose every
authenticator, they sign in with their provider (a session without a second factor), ask an Owner or Administrator in
person or by phone, who creates a one-time confirmation (`POST /v1/members/{member_id}/recovery-confirmations`), and
send both (`POST /v1/session/recovery`). `recover_account` checks both proofs for the session's user, uses them, gives
the session its second factor (method `recovery`), ends the user's other sessions, replaces the codes and records the
recovery with a 24-hour cooling-off; the API revokes the user's tokens, notifies them and every Owner and logs the weekly
review. During the cooling-off the API refuses `authz.HeldInCoolingOff` (API-AUTHZ-08) and the database a link (FY278).
Trust boundaries: browser ↔ API (codes are attacker-controlled input), API ↔ database (the functions are the only path
to the three global tables), person ↔ confirmer (out of band: a conversation).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | browser → API, recovery | Spoofing: someone with the person's provider password (a phished session without a second factor) recovers the account | Takeover past the second factor | Medium |
| T2 | browser → API, recovery | Spoofing: codes guessed online, or another person's code or confirmation used | Takeover | Low |
| T3 | database at rest | Information disclosure: a database copy reveals usable codes | Offline brute force of 50-bit codes | Low |
| T4 | confirmer | Spoofing (social engineering): an attacker talks an Owner or Administrator into confirming | Takeover with a stolen recovery code | Medium |
| T5 | after recovery | Elevation: a recovered attacker approves payments, changes bank details, roles or sign-ins before anyone notices | Money moved, persistence gained | Medium |
| T6 | after recovery | Repudiation: a recovery nobody hears of | A takeover goes unnoticed | Low |
| T7 | after recovery | Elevation: old sessions or OAuth tokens of the lost devices keep working | The thief keeps access | Medium |
| T8 | records | Tampering: a recovery record changed or removed | No evidence | Low |

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:` and `// asvs:`) |
|---|---|---|
| T1 | Two independent proofs: a recovery code (something the person kept) and a confirmation given after a conversation (another person); the provider sign-in alone gives nothing but reading the session and recovering | `TestAccountRecovery` (SEC-35, G4-060; V6.4.1) |
| T2 | Codes are bound to the session's user; each works once; the confirmation lasts 24 hours and works once; a wrong, used or expired proof gives the same refusal (FY277), and requests are rate-limited (FR-INT-16) | `TestAccountRecovery` |
| T3 | Codes are kept only as keyed hashes under the field key (Cloud KMS in production, never in the database); confirmations carry 130 random bits | `TestAccountRecovery` (no code in the trail; V6.4.2) |
| T4 | The guide and the action say to confirm only after speaking to the person on a channel the confirmer chose; the person and every Owner are told of each confirmation; nobody confirms their own, nor anyone in a cooling-off (FY276) | `TestAccountRecovery`, `recovery.test.tsx` |
| T5 | The 24-hour cooling-off holds approving, releases, payee and own bank-detail changes, roles, sign-ins and provider switches, break-glass and confirmations, centrally and for a link in the database; a test pins the list to real operations | `TestAccountRecovery`, `TestTheCoolingOffHoldsRealOperations` |
| T6 | `security.recovery` to the person and every Owner, in the app and by email at once; the weekly review entry (FM-3); events in the installation's and the organisation's trails | `TestAccountRecovery` |
| T7 | The recovery ends every other session of the user and revokes their tokens (AI-38) | `TestAccountRecovery` (V7.4.2) |
| T8 | `account_recovery` refuses any update or delete (FY279); the application roles hold no grant on the tables | grants golden, `TestAccountRecovery` |

## 4. What remains

- The cooling-off's length is fixed at 24 hours until it becomes a setting.
- Owners of other organisations the person belongs to are not told; the weekly review is the confirming organisation's.
- A Super admin recovers through FR-ADM-09's own two-person recovery, not this one.
