# Threat model: <feature>

Copy this file to `docs/security/threat-models/<feature>.md` for every feature that moves money or
handles personal data (SEC-20), fill it in before the code, and keep it with the feature's pull
requests. Short is fine; unanswered is not.

| | |
|---|---|
| Feature and requirement IDs | <e.g. vendor payment batches, FR-AP-07, FR-APR-03> |
| Author, reviewer, date | <names; the reviewer is the second human reviewer where AGENTS.md requires one> |
| Data and its class | <amounts, payee bank details (restricted), NRIC/FIN (restricted), …> |
| Who acts | <roles of Appendix D, the AI on behalf of a user, integrations, scheduled jobs> |

## 1. What we are building

One paragraph and a sketch of the flow: the screens or API operations, the domain commands, the
tables, the outbox work and any outside service. Mark every **trust boundary** (browser ↔ API,
API ↔ database, worker ↔ provider, AI ↔ tools).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | | Spoofing: | | |
| T2 | | Tampering: | | |
| T3 | | Repudiation: | | |
| T4 | | Information disclosure: | | |
| T5 | | Denial of service: | | |
| T6 | | Elevation of privilege: | | |

Also ask: can one person both prepare and approve it (SOD, FM-1)? Can it run twice (idempotency,
posting identity)? Can another organisation see or change it (RLS)? Can the AI or an MCP client reach
it (golden rule 5)? What does it write to logs and to the audit trail (SEC-15, AUD-01)?

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:` and `// asvs:`) |
|---|---|---|
| T1 | | |

## 4. What remains

Residual risks accepted, by whom, and until when; follow-ups with their issue or requirement ID.
