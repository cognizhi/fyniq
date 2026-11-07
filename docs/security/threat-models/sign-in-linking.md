# Threat model: linking and removing sign-ins

| | |
|---|---|
| Feature and requirement IDs | A person links an account at another sign-in provider to their Fyniq user, lists their sign-ins and removes one: FR-ADM-08, SEC-35 (PRD-001 rev 0.8, rev 0.196); `p1-users-roles` task 2.4 |
| Author, reviewer, date | Claude Code for the founders, 6 Nov 2026; reviewer: a founder at sign-off (second human review waived before production, AGENTS.md) |
| Data and its class | Sign-in identities: the provider's issuer and subject, the provider's ID, when linked and last used (internal; personal data by association with a user); session token hashes |
| Who acts | A person in a browser session with a recent second factor (every role); never the AI, an MCP client or an OAuth client |

## 1. What we are building

From Settings › Profile and security the browser goes to `GET /api/auth/signin?provider=…&link=1`. The flow checks the
request came from Fyniq itself (`Sec-Fetch-Site: same-origin`) and that the browser's session passed a second factor in
the last 5 minutes, then sends the browser to the provider as a sign-in does, with `link` kept in the HttpOnly start
cookie. When the provider's answer passes the sign-in's checks (state, nonce, PKCE, issuer, audience), the callback
calls `link_identity` with the session's token hash instead of starting a session: the database checks the session and
its second factor again, refuses an (issuer, subject) that any user has already (FY271), and adds it to the session's
user. `GET /v1/session/identities` lists the person's sign-ins and `DELETE /v1/session/identities/{identity_id}`
removes one through `unlink_identity`, which ends the sessions it signed in. Both write to the installation's trail in
the same transaction and send the security notice `security.sign_in_methods`. Trust boundaries: browser ↔ API (the
link request, the provider's callback), provider ↔ API (the provider's answer), API ↔ database (the functions are the
only write path to `user_identity`).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | provider → API | Spoofing: someone signs in with a new provider whose email matches a person's and is treated as that person | Account takeover through a provider that lets anyone claim an email | Medium |
| T2 | browser → API, link | Spoofing / elevation: a stolen or hijacked session links the attacker's provider account, which then signs in at will | Lasting way in for the attacker | Medium |
| T3 | browser → API, link | Cross-site request: another site starts a link in the person's browser, which completes silently at a provider they are signed in to | An account the person did not choose is linked | Low |
| T4 | browser → API, callback | Spoofing: an attacker's provider answer (code) is fed to the person's callback (login CSRF), linking the attacker's account | Attacker signs in as the person | Low |
| T5 | API → database | Elevation: one person links an account that signs another person in, moving it between users | Takeover of the other person's sign-in | Low |
| T6 | removal | Denial of service: the person (or a hijacked session) removes the sign-in in use or the last one | Locked out of their account | Medium |
| T7 | removal | Elevation: a session signed in with a removed account keeps working | A removed way in still works | Low |
| T8 | any change | Repudiation: a sign-in linked or removed with no record and no notice | A takeover goes unnoticed | Low |
| T9 | listing and logs | Information disclosure: the provider's subject or a token shown or logged | Correlation across services, replay material | Low |

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:` and `// asvs:`) |
|---|---|---|
| T1 | Identities are (issuer, subject) pairs; a sign-in with an unknown pair signs nobody in, whatever its email; only `link_identity` adds a pair to a user | `TestYourSignIns` (FR-ADM-08; V6.8.1) |
| T2 | Linking needs a session whose second factor is less than 5 minutes old, checked when the link starts, and again by `link_identity` (FY270) when it completes; every link is notified at once | `TestLinkingASignInAddsItToTheSessionsUser`, `TestYourSignIns` (V7.5.1) |
| T3 | The link starts only with `Sec-Fetch-Site: same-origin` (AUTH-SIGNIN-05) | `TestLinkingASignInAddsItToTheSessionsUser`, `TestYourSignIns` |
| T4 | The provider's answer is bound to the browser that started the link by the start cookie's seed (state, nonce, PKCE), as a sign-in is; the link goes to the session in the same browser | `TestTheSignInFlowStartsASessionOnlyForTheBrowserThatBegan`, `TestLinkingASignInAddsItToTheSessionsUser` |
| T5 | `link_identity` refuses a pair any user has (FY271, the primary key behind it); the API holds no grant on `user_identity` | `TestYourSignIns` (the other founder's sign-in stays theirs) |
| T6 | Removing needs a second factor in the last 5 minutes (API and `unlink_identity`, FY270), and never removes the session's own sign-in nor the last one (FY271) | `TestYourSignIns` |
| T7 | `unlink_identity` ends every session the removed pair signed in, in the same transaction | `TestYourSignIns` |
| T8 | Each link and removal writes `identity.linked` / `identity.unlinked` to the installation's trail in the same transaction, and sends the locked security notice, in the app and by email at once | `TestYourSignIns` |
| T9 | The API shows an identity by a hash of its issuer and subject and its provider only; the trail records the provider and issuer, never the subject; the flow logs no token, code or cookie (SEC-15) | `TestYourSignIns` |

## 4. What remains

- Super admins enabling and disabling providers, allowed domains, joining by domain (a pending Employee membership an
  Owner approves) and the last-method guard arrive with the rest of task 2.4 (rev 0.196).
- The security notice goes to the person's organisation's centre; a person in no organisation is told nothing in the
  app (the sign-in alert's email still reaches them for each sign-in).
- Account recovery when every linked provider is lost is SEC-35's (task 2.5).
