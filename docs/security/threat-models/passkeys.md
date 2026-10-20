# Threat model: passkeys and the second factor

| | |
|---|---|
| Feature and requirement IDs | Passkeys (WebAuthn) for step-up, the mandatory second factor and passkey-signed approvals: FR-ADM-02, SEC-01, SEC-05, FR-APR-03, FR-APR-04, G4-056 (PRD-001 rev 0.43, 0.44); `p1-users-roles` task 2.2, `p1-approval-engine` task 1.2 |
| Author, reviewer, date | Claude Code for the founders, 20 Oct 2026; reviewer: a founder at sign-off (second human review waived before production, AGENTS.md) |
| Data and its class | Passkey public keys, credential IDs, AAGUIDs and signature counters (internal; no secret is stored: the private key never leaves the authenticator); challenges (32 random bytes, single use); session token hashes |
| Who acts | A person in a browser session (every role); never the AI, an MCP client or an OAuth client |

## 1. What we are building

The browser asks the API for a challenge (`POST /v1/session/passkeys/challenge` to register, `POST
/v1/session/step-up/challenge` to step up), passes the options to `navigator.credentials`, and sends the
authenticator's answer back (`POST /v1/session/passkeys`, `POST /v1/session/step-up`). The API checks the answer with
go-webauthn and records it through SECURITY DEFINER functions on two global tables (`webauthn_credential`,
`webauthn_challenge`, migration 000045); a passed step-up moves the session's `step_up_at` and records `passkey`. A
session that passed no second factor is refused everything but reading itself, stepping up and a Super admin's recovery
(`API-AUTHZ-07`). Trust boundaries: browser ↔ API (the answer is attacker-controlled input), API ↔ database (the
functions are the only write path), authenticator ↔ browser (outside Fyniq).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | browser → API, step-up | Spoofing: an answer replayed, from another session, another site (phishing) or another person's passkey | Someone steps a session up without the authenticator: approves changes as its owner | Medium |
| T2 | browser → API, registration | Spoofing / elevation: a hijacked single-factor session registers the attacker's passkey, then steps up at will | Lasting second factor for the attacker | Medium |
| T3 | authenticator | Tampering: a cloned authenticator signs alongside the real one | Second factor shared without the owner knowing | Low |
| T4 | API → database | Tampering: the API (or a bug in it) writes a credential or a step-up that no ceremony proved | Bypass of the ceremony | Low |
| T5 | API logs | Information disclosure: challenges, keys or session tokens in the logs | Replay material leaks | Low |
| T6 | any operation | Elevation of privilege: a session without a second factor uses Fyniq | MFA not mandatory in fact | Medium |
| T7 | revocation | Repudiation: a passkey added or revoked with no record | No trail of who changed their factors | Low |
| T8 | browser → API, approval | Spoofing / replay: a passkey answer for one approval used on another organisation's or request's, on a changed envelope, or late (G4-056) | A change approved that its signer never saw | Medium |
| T9 | approval record | Repudiation: a signer denies having approved | A disputed approval | Low |

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:` and `// asvs:`) |
|---|---|---|
| T1 | Each challenge is 32 random bytes, bound to its session and purpose, taken once within 5 minutes and answered once (the database); the answer must carry this origin and relying party ID, user presence and verification, a signature by a live passkey of the user, and a counter that went up | `TestPasskeyCeremonies`, `TestPasskeys`, `TestAttackPasskeysAreAnsweredChallenges` (FR-ADM-02, SEC-01; V6.5.1, V6.7.2) |
| T2 | Registering needs a session that passed a second factor in the last 5 minutes, in the API and in `webauthn_challenge_issue` / `webauthn_credential_register` | `TestPasskeys`, `TestAttackPasskeysAreAnsweredChallenges` |
| T3 | A counter that repeats or goes back is refused (go-webauthn's clone warning, and `session_step_up` / `webauthn_credential_guard`, FY140) | `TestPasskeyCeremonies`, `TestAttackPasskeysAreAnsweredChallenges` |
| T4 | The API holds no grant on either table; the functions refuse a credential without a taken registration challenge and a step-up without a taken step-up challenge of that session by a live passkey; the owner cannot change a key, un-revoke or delete | `TestAttackPasskeysAreAnsweredChallenges` (V8.2.2) |
| T5 | Refusals log the library's reason and the operation only; the passkey ID on success; never the challenge, key or token (SEC-15) | `TestPasskeys` (log checks) |
| T6 | `authorise` refuses a session with no step-up unless the operation is `SingleFactor` (getSession, startStepUp, stepUp, recoverSuperAdmin), and a test pins that set | `TestOnlyStepUpAndRecoveryNeedNoSecondFactor`, `TestPasskeys` (SEC-01; V6.3.3) |
| T7 | Registering and revoking write an event to the installation's trail in the same transaction (`require_passkey_audit`) | `TestPasskeys` |
| T8 | The approval challenge is 16 random bytes and the envelope's SHA-256, issued only for a pending request's own envelope at the version shown, bound to the session, organisation and request, taken and answered once within 5 minutes in the deciding transaction; the signature trigger refuses a passkey signature that is not the signer's answer to that organisation's, request's and envelope's challenge (FY142) | `TestPasskeySignedApprovals`, `TestAttackPasskeySignaturesAnswerTheirChallenge` (FR-APR-03; V6.5.1, V6.7.2) |
| T9 | The signature keeps the passkey, authenticator data, client data and signature, never changed (FY102), and `passkey.Reverify` checks them again with the passkey's key | `TestAKeptAssertionVerifiesAgain`, `TestPasskeySignedApprovals` |

## 4. What remains

- Attestation is not asked for (`none`, no metadata service): any authenticator the person trusts is accepted.
  Provisional (rev 0.43) until an organisation asks to restrict authenticator models.
- The browser ceremonies have no screen yet (`p1-app-shell`, `p1-users-roles` 4.1): until then the second factor in
  local mode is the sign-in page's tick, and in production the provider's TOTP.
- Approvals signed with a passkey keep their assertion (rev 0.44); approvals signed with the session's step-up (TOTP at
  sign-in, or an earlier passkey step-up) are bound by the signed approval record only, as FR-APR-03 allows for TOTP.
- Losing every authenticator: a member signs in again with the provider's TOTP; a Super admin recovers with two proofs
  (SEC-35). Account recovery for other members arrives with task 2.5.
