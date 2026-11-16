# Runbook: the canary organisation

**What:** each night at 01:55 Singapore time the platform job `canary.run` posts synthetic transactions in one
organisation kept for it, through the public API, and reads them back. **Requirements:** PRD-001 QA-PROD-01
(production verification), NFR-OBS-02 (the alert); `p1-launch-readiness` design D8, task 6.2. **Decision:** the owner's
of 16 Nov 2026, taken interactively: the canary is a client of the public API, not a database role of its own, so it
proves the whole stack and widens no grant.

## What a night does

The canary (`internal/canary`):

1. Reads the organisation's accounts; the first night loads the standard chart, and makes the customer
   *Fyniq Canary Customer* and the supplier *Fyniq Canary Supplier*.
2. Posts, dated the organisation's today:
   - an invoice of S$100.00 to 4010, issued, and the receipt into 1020 that settles it;
   - a bill of S$40.00 to 6900 (GST code NR), posted, and the payment from 1020 that settles it;
   - a journal of S$5.00 from 1020 to 6900.
3. Reads each back: posted, for its total, and paid; then the trial balance of the month, whose debits must equal its
   credits.

Any difference fails the run. The tick records the failed slot, and the heartbeat check raises the same alert as a
failed ledger verification. The audit-chain verification (02:00) and the ledger verification (02:30) then cover what the
canary posted.

## Setting it up (once per environment)

1. **Make the organisation.** As a Super admin, create an organisation named *Fyniq Canary*, with its financial year
   and periods, in Founder mode. It is the platform's own: never a customer's, and never copied.
2. **Its self-approval threshold** stays above S$100.00, so no canary document waits for a person. A document that
   does wait fails the night, naming the setting.
3. **Its client.** Register an `integration`-class OAuth client for the organisation (FR-INT-02; built with
   `p1-public-api` task 3.1), with the roles the canary's writes need (Accountant), no approval rights, and an expiry.
   Its private key stays in Secret Manager; the jobs image reads it through the client's token source.
4. **The configuration.** Give the jobs image the API's address and the client, so the wiring sets `Kit.Canary`. Until
   then the job logs that no canary is configured and does nothing.
5. **Prove it** `[live-verify]`: run the slot by hand (`jobs run-slot canary.run <slot>`), then check the night's
   documents in the organisation, the trial balance and the job's heartbeat. Record the run in the release evidence.

## When the alert fires

1. Read the job's log line: it names the failing step (a document that did not post, one that reads back otherwise,
   or the trial balance).
2. Open the canary organisation and the documents of the night. A document that posted but reads back otherwise, or a
   trial balance out of balance, is a calculation incident: follow `docs/runbooks/incident-response.md` and the
   bug-fix skill's root-cause review (QA-REL-01).
3. A refusal (401 or 403) means the client's credentials or roles changed: renew them through the access review.
4. Never correct the canary's books by hand: its documents are evidence that the night ran.
