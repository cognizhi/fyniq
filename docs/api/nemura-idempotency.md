# Nemura's Idempotency-Key

Every write of Nemura's API (`POST`, `PUT`, `DELETE` under `/v1`, except the passkey ceremonies, which answer one-time challenges) accepts an `Idempotency-Key` header: 1 to 255 printable characters without spaces, a UUID is best.

- **Scope.** A key belongs to the person who sent it, in their organisation. Two people may use the same string; neither sees the other's.
- **A repeat** of the same request (same method, path, query and body) returns the first response with `Idempotent-Replayed: true`, and the operation does not run again.
- **The same key with a different request** answers 422 `NM_IDEMPOTENCY_MISMATCH`.
- **Two identical requests at once** run once: the second waits (up to 15 seconds) for the first and returns its answer; if the first is still running it answers 409 `NM_IDEMPOTENCY_CONFLICT`.
- **Window.** A key is kept 24 hours, then forgotten; the same string may then be used for a new request. A request whose process died is not waited for beyond two minutes. A response of 500 or above is not kept: the same key may try again.
- **No session** means no key is kept: the operation answers 401 as it would without one. A request body over 1 MiB runs without a key's protection.
- **What is stored.** The status, the content type, `Location` and `ETag` and, for an answer that is not sensitive and under 64 KiB, its body, in `nemura.idempotency_key` (written by the API's own role, row-level security by organisation). The key is the caller's own random string and the hash is of the request, so nothing in the table is personal data.
- **What is never stored.** The answer of an operation that may carry pay, a bank account, an NRIC, a file of pay or the text of a question: the payroll operations (`/v1/payroll/...` including the payment and journal files), a person's pay, one-offs, year to date, joint election and tax-clearance hold, the NRIC and bank-account operations, a person's own details and emergency contacts, leave settlement, the questions to the people team and the assistant. Only the status is kept. A repeat of such a write that returned a body answers 409 `NM_IDEMPOTENCY_CONFLICT` and runs nothing: read the result from the record. A repeat of one that returned no body (204) replays its status.

Proved by `TestAWriteWithAKeyRunsOnceAndAReplayReturnsTheFirstAnswer`, `TestTwoIdenticalRequestsInFlightRunOnce` (race detector), `TestTheAnswerOfASensitiveWriteIsNeverStored` and, against the database, `TestTheIdempotencyKeysOfWritesAreClaimedOnceAndKeptForADay`.
