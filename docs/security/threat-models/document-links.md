# Threat model: the online link to a quote or invoice

| | |
|---|---|
| Feature and requirement IDs | The secure online view link of a quote or invoice sent by email, and a quote's acceptance or decline through it: FR-AR-06, FR-AR-16, FR-DOC-05 (PRD-001 rev 0.134; `p1-sales` task 5.3) |
| Author, reviewer, date | Claude Code for the owner, 24 Oct 2026; second review waived before production (AGENTS.md), held with `needs-human-review` if wanted |
| Data and its class | A document's content: the organisation's name, UEN, GST number and bank details; the customer's name and address; the lines, amounts and dates (confidential business data, some personal); the customer's name and comment on an answer (personal) |
| Who acts | Anyone holding the link, with no session (the customer, or whoever the email reaches); members with P on "Invoices, credit notes" (send, revoke); the API |

## 1. What we are building

Emailing a sent quote or an issued invoice (task 5.2) makes a link for that version: a random 256-bit token,
base64url in the URL's fragment, `<origin>/d/#<token>`, kept in the database only as its SHA-256. A fragment is
never sent to a server, so the token reaches no proxy, load-balancer or access log. Opening it, the web app's public
page reads the fragment and calls `GET /v1/public/document` with the token in the `Fyniq-Link` header (and
`…/pdf` for the PDF); with no session, the API hashes the token, finds its organisation through a platform registry (`document_link_route`, as email-in finds its
address), and reads the document in that organisation's transaction. The first opening is recorded as the
document's delivery `viewed`. A quote's link also takes the customer's acceptance (with their name) or decline
(`POST /v1/public/document/accept`, `…/decline`), recorded as the quote's outcome with their comment. A member revokes a link;
otherwise it lasts 180 days, and stops when the document is voided or cancelled, or an invoice is paid in full.

Trust boundaries: the internet ↔ API (public operations, no credentials); API ↔ database (RLS, a security-
definer lookup); email ↔ recipient (the link travels in the email).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | Internet → API | Spoofing: someone guesses or enumerates tokens | Any organisation's quotes and invoices read | Low with 256 bits; high if tokens were short or sequential |
| T2 | Database | Information disclosure: a database reader or a backup yields working links | Every live document readable | Medium |
| T3 | Internet → API | Elevation: a link reads another document, another version or another organisation's data | Data of other documents or tenants | Low |
| T4 | Email → recipient | Information disclosure: the link is forwarded, logged in a proxy, leaked by the Referer header, cached or indexed | A third party reads the document | Medium |
| T5 | Internet → API | Tampering / repudiation: someone with the link accepts or declines a quote, or the customer later denies answering | A wrong outcome; a dispute | Medium |
| T6 | Internet → API | Denial of service: many requests to one link render its PDF again and again | Render service and API load | Medium |
| T7 | API → database | Tampering: a link's expiry pushed out, a revocation undone, a view rewritten | A link outlives its purpose; false delivery evidence | Low |
| T8 | API | Information disclosure: the token in logs, audit events or error messages | Working links in logs | Medium |
| T9 | Internet → API | Information disclosure: a different answer for unknown, expired and revoked links tells an attacker which tokens existed | Confirms leaked tokens | Low |
| T10 | AI / MCP | Elevation: an agent reads or answers through the public operations | Golden rule 5 broken | Low |

SOD: a link only reads, or records the customer's own answer; no money moves. Founder mode changes nothing.
RLS: every tenant table has `organisation_id` and `FORCE ROW LEVEL SECURITY`; the registry holds only hashes,
organisation IDs and link IDs, and only the security-definer function reads it.

## 3. What we do about it

| Threat | Control | Test that proves it |
|---|---|---|
| T1 | 32 bytes from `crypto/rand` (256 bits), base64url; a malformed token is refused before any lookup; the registry's primary key refuses a collision | `TestALinkTokenIsRandomAndKeptHashed` (`// req: FR-AR-06`, `// asvs: V7.2.3`) |
| T2 | Only the SHA-256 of the token is stored, in the tenant row and the registry; the token exists only in the email and the URL | `TestALinkTokenIsRandomAndKeptHashed`, the schema snapshot (no token column) |
| T3 | The registry gives the organisation and the link; the document and version come from the link row in that organisation's transaction (RLS); a link is only of a posted quote, order or invoice at its current version (FY198); an edited invoice's old link stops | `TestDocumentLinksOpenTheirDocumentOnly` |
| T4 | Lasts 180 days at most, until revoked, voided, cancelled or paid in full; responses carry `Cache-Control: no-store`, `X-Robots-Tag: noindex, nofollow` and `Referrer-Policy: no-referrer`; the page shows the document and its PDF and nothing else (no other documents, no account, no balance) | `TestDocumentLinksOpenTheirDocumentOnly` (headers, expiry, revocation, a voided invoice) |
| T5 | An answer only to a sent quote with no answer, at most once (QUOTE-07, FY193); acceptance names who accepts; the outcome records the name, the comment and the link as `customer:<link>`, and the audit event the request's address and agent; the quote's members see it at once | `TestAQuoteIsAnsweredThroughItsLink` (`// req: FR-AR-16`, `// req: FR-DOC-05`) |
| T6 | Each link is counted per minute in the database: 30 reads and 5 answers, then 429 with Retry-After; the edge limits by address (SEC-23) | `TestDocumentLinksAreRateLimited` |
| T7 | A link never changes but in its first view and its revocation, each once (FY197); it is never deleted; every change is audited | `TestALinkChangesOnlyInItsViewAndRevocation` |
| T8 | The token travels in the URL's fragment and a request header, never a path or query, so no access log, proxy or load balancer records it; the API logs no header; audit events name the link's ID, never the token or its hash | `TestDocumentLinksOpenTheirDocumentOnly` checks the log and the audit trail |
| T9 | Unknown, malformed, expired and revoked links answer the same 404 LINK-01 | `TestDocumentLinksOpenTheirDocumentOnly` |
| T10 | The public operations are not tools: the MCP server and the AI have no route to them (§9.6); they take no credential, so a token in an agent's hands is the same as anyone's | The never-list tests |

## 4. What remains

- The public page itself (`customer-document-view`) arrives with the screens (task 8.1); until then the link
  opens the web app's route, which shows nothing more than the API returns.
- A link travels in an email: anyone the email is forwarded to can open it until it is revoked or expires.
  Revoking is a person's action; there is no customer sign-in in P1 (the customer portal is FR-AR-14).
- Per-address limits are the edge's (Cloud Armor); in local mode there are none.
