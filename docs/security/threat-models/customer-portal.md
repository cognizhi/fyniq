# Threat model: the customer portal

| | |
|---|---|
| Feature and requirement IDs | The customer portal: a private link that opens one customer's invoices still due, their invoice PDFs and statement of account, with no session: FR-AR-14 (PRD-001 rev 0.512; `p2-customer-portal-subscriptions` section 1; design D1 and D2) |
| Author, reviewer, date | Claude Code for the owner, 20 Nov 2026; the access design is `[owner to confirm]`; second review waived before production (AGENTS.md), held with `needs-human-review` |
| Data and its class | A customer's own invoices (amounts, dates, the organisation's name, the customer's name), payment-link URLs, statement lines (confidential business data, some personal); the link's token (a credential) |
| Who acts | Anyone who holds a link (the customer, or whoever it reaches), with no session; members with the right to post on Invoices (make, revoke), to view (list); the API |

## 1. What we are building

A **new external principal**: the holder of a portal link. A member with the right to post on Invoices makes a link for one customer
contact (an active customer): a random 256-bit token, shown to the maker once and kept only as its SHA-256; the address is
`<origin>/portal/#<token>`, so the token is in the URL's fragment, which no server or proxy sees. The portal page reads the fragment and calls
`GET /v1/public/portal` (and `…/invoices/{id}/pdf`, `…/statement`) with the token in the `Fyniq-Portal` header. With no session the API
hashes the token, finds its organisation through a platform registry (`portal_link_token`, read only by the security-definer
`portal_link_route`), and reads the link and **that customer's** documents in the organisation's transaction under row-level
security. The link works 30 days by default and at most 90 (a CHECK), until revoked. The portal reads and shows; it posts nothing and asks no provider for anything
(it shows a payment link a person already made). The first opening is recorded and audited once.

Trust boundaries: the internet ↔ API (public operations, no credentials but the token); API ↔ database (RLS, a security-definer lookup);
whoever the link is given to ↔ the customer (the link travels outside Fyniq, by the maker's own means).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | Internet → API | Spoofing: someone guesses or enumerates tokens | Any customer's invoices read | Low with 256 bits |
| T2 | Database | Information disclosure: a database reader or a backup yields working links | Every live portal readable | Medium |
| T3 | API | Elevation: a link reads another customer's documents of the same organisation (an invoice ID that is not theirs), or another organisation's | Cross-customer and cross-tenant disclosure | Medium: it is a handler's filter, not only a database one |
| T4 | Any principal ↔ portal | Elevation: a staff, integration, MCP or AI token opens the portal, or the portal's token opens a staff, API or MCP route | A principal acts as another | Low |
| T5 | Link holder | Elevation or repudiation: the portal is made to do more than read (create a payment link at the provider, post a receipt, change anything) | Funds or books changed by an unauthenticated party | Low |
| T6 | Internet → API | Denial of service: many requests to one link render PDFs and statements again and again | Render service and API load | Medium |
| T7 | Email or chat → recipient | Information disclosure: the link is forwarded, logged by a proxy, leaked by the Referer header, cached or indexed | A third party reads a customer's account | Medium |
| T8 | API | Information disclosure: the token in logs, audit events, errors or the staff's list | Working links in logs | Medium |
| T9 | Internet → API | Information disclosure: a different answer for unknown, expired, revoked links, other customers' documents and missing ones tells an attacker what exists | Confirms leaked tokens and document IDs | Medium |
| T10 | API → database | Tampering: a link's expiry pushed out, a revocation undone, the customer changed, a link deleted | A link outlives its purpose | Low |
| T11 | Staff | Elevation: someone without the right makes links for customers, or for a vendor or inactive contact | A back door to a customer's data | Low |
| T12 | PDPA | A customer's personal data (name, address on the invoice) is shown to whoever holds the link; the link outlives the relationship | An over-long exposure of personal data | Medium |

SOD: a link only reads; nobody's approval is bypassed because the portal changes nothing. Founder mode changes nothing.

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:` and `// asvs:`) |
|---|---|---|
| T1 | 256 bits from `crypto/rand`; a malformed token is refused before any lookup; per-link and per-address limits | `TestAPortalOpensItsCustomersDocumentsOnly` (malformed, unknown, a path), `TestAPortalLinkIsRateLimitedPerLink` |
| T2 | Only the SHA-256 is stored; the registry has no grant; `portal_link_route` returns an organisation and a link for a hash and nothing else | `TestEachRoleHoldsExactlyItsGrants` (the grants snapshot), `TestAPortalLinkIsMadeByThoseWhoMayPostAndForCustomersOnly` |
| T3 | Every portal read takes the customer from the link's own row (`portal.Scope`), never from the request; `portal.InvoiceOf` reads the invoice's customer and answers not found for any other, a draft, a void or a credit note; the receivables read is filtered to the link's customer; RLS separates organisations | `TestAPortalOpensItsCustomersDocumentsOnly` (another customer's invoice ID, a draft, one that does not exist; the other customer's portal opens none of the first's; a statement never shows another customer's lines) |
| T4 | The portal's handlers never read a bearer token and the staff routes never read `Fyniq-Portal`; the portal token is no bearer token (401 on every staff route, as a header and as a bearer token); a staff token without the header opens nothing; the AI's grants have no route | `TestAPortalOpensItsCustomersDocumentsOnly`, the matrix rows `matrixPortal` |
| T5 | The portal's domain package has no write: it reads receivables, statements and the payment-link table, and the only write in the request is the link's first opening (a column of the link); the portal never calls a connector | `TestAPortalOpensItsCustomersDocumentsOnly` (nothing the portal does changes a posting or a link but its first opening) |
| T6 | 60 reads a minute per link (429 with Retry-After), counted in the request's own transaction, so a request refused with the one 404 (a probe of an invoice ID) is not counted against the link and probing is bounded by the per-address limit of every `/v1/public/` route (SEC-23) alone | `TestAPortalLinkIsRateLimitedPerLink` |
| T7 | The token is in the fragment (never sent to a server); every answer is `Cache-Control: no-store`, `X-Robots-Tag: noindex, nofollow` and `Referrer-Policy: no-referrer`; the Pay link is `rel=noopener noreferrer`; the page tells the customer to keep the link private | `TestAPortalOpensItsCustomersDocumentsOnly` (headers), `customerPortal.test.tsx` |
| T8 | The token is never in a log line, an audit event (it records the contact, the label and the expiry), an error or the staff's list | `TestAPortalOpensItsCustomersDocumentsOnly` (the log; the list) |
| T9 | One problem, `PORTAL-01`, for a malformed, unknown, expired or revoked link and for any document that is not the customer's, with the same words | `TestAPortalOpensItsCustomersDocumentsOnly` (the answers are compared) |
| T10 | A trigger lets a link change only in its first opening and its revocation, once each; nothing deletes a link or its registry row; the 90-day cap is a CHECK | `TestAPortalLinkIsMadeByThoseWhoMayPostAndForCustomersOnly` (91 days), the database's guard (FYP10) |
| T11 | Making and revoking need the right to post on Invoices (the matrix); the database refuses a link for a contact that is not an active customer (FYP11) and the API says so first | `TestAPortalLinkIsMadeByThoseWhoMayPostAndForCustomersOnly`, `matrixPortal` |
| T12 | Short life by default (30 days), revocation at once, a list that shows when each link was first opened, and a customer made inactive ends their portal (`PORTAL-01`); the portal shows only what the customer's own PDF shows | `TestAPortalOpensItsCustomersDocumentsOnly`; the PDPA review below |

PDPA (data inventory and DPIA): the portal shows a customer's own invoices and statement to whoever the customer or the maker gives the link
to; the lawful basis is the customer's existing contract, the purpose is collecting what they owe, and retention follows the invoices' (FR-AUD,
FR-PDP). No new personal data is collected from the customer: the page asks for none. The `portal_link` table is exported with its
token hash dropped (the tenant export's `Dropped` list). `[counsel to confirm]` that no separate consent is needed.

## 4. What remains

- Whoever holds a link acts as the customer until it ends: a forwarded email is a forwarded login. This is the design's accepted residual
  risk (option A against customer logins, design D1); the controls above bound it in time, scope and reach, and revocation is immediate.
- Fyniq does not email the link: the maker sends it, which keeps the choice of recipient with a person (a mistyped address is theirs to avoid).
  An email through the existing send path is a later addition.
- A payment on the provider's page is recorded by reconciliation, never by the portal; the portal does not say "paid" until the books do.
- Paid invoices are not listed (the statement shows them); a payment-link click-through is not recorded.
