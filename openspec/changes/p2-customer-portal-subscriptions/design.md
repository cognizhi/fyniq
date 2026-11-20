# Design

## D1. How a customer gets in

| Option | For | Against |
|---|---|---|
| A. A signed, expiring, revocable link scoped to one customer, the token in the URL fragment and sent in a header (the pattern of the document links, `p1-sales` 5.3) | No account to run, no password or recovery flow, no cookie and no CSRF; the fragment never reaches a log; revocable and short-lived | Whoever holds the link acts as the customer until it expires: a forwarded email is a forwarded login |
| B. Customer logins (passkey or email code) through the identity provider | Per-person identity, MFA | A second user population, recovery, support load, and a customer contact becomes a user |
| C. The staff session of a member | Nothing to build | Never: a customer must not hold a staff session |

Decision: **A** (`[owner to confirm]`). Rules: 256-bit random token, SHA-256 only in the database; one link per
customer contact, many per customer allowed; default life 30 days, at most 90; revoked by a person with the right;
shown once to the creator and emailed to the contact's billing email by the existing send path; the portal is read-only
and its one action, paying, is a navigation to the provider's page of a link that already exists (the portal never asks
a provider for a link). Every open is counted (`opened_at`, `last_opened_at`, `open_count`), not logged per request.

## D2. Isolation

The portal's handlers never call the authentication middleware and a portal token is no bearer token: the staff,
integration, MCP and AI routes reject it, and the portal routes reject every bearer token. The organisation is found
through a platform registry and a SECURITY DEFINER lookup by token hash (`document_link_route`'s pattern); every read
takes the `Scope{organisation, contact}` the link row gives and filters by that contact in the query, never by anything
the client sends; a document that is not that contact's answers the same 404 as one that does not exist. Unknown,
expired and revoked tokens answer one identical 404 (no oracle). Per-IP and per-token rate limits through the existing
public-operation limiter; the token is never in a log, an audit event or an error.

## D3. Subscriptions

A contract and its amendments are records, not postings: invoices and credit notes still post through the existing rules
(`invoice@1`, spreads on 2400). An amendment (kind `upgrade`, `downgrade`, `cancel`, `renew`) has an effective date,
the new MRR, evidence and the documents it caused; the schedule is derived month by month from the contract's terms and
its amendments from their dates; an amendment dated in a closed period is refused (FR-GL-12). Contracted MRR is the
monthly value of active contracts at month end. The accounting of each kind of amendment follows the revenue policy
(FR-ORG-10) `[accountant to confirm]`.

## D4. Disputes and promise-to-pay

A side record per invoice (`invoice_dispute`: kind `dispute` with an amount and reason, or `promise` with a date;
status open or resolved), since a posted invoice never changes. Open records pause the invoice's reminders until
resolved or the promised date passes; ageing reports disputed and undisputed amounts apart.

## D5. Sequencing

Portal first (the security-critical piece), then disputes and promises, then subscriptions, then FR-PRJ-11.
