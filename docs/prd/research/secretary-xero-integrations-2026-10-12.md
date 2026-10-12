# Source notes: company secretary, Xero comparison and integrations

Supports PRD-001 revision 0.6 (§8.24, §9.12, §14.2, Appendix A.3). AI-assisted research passes
collected these notes on 12 Oct 2026. The Companies Act was read on Singapore Statutes Online
(SSO). ACRA, Xero and vendor pages were read through a summarising fetch tool, so quotes from
them are as that tool relayed them. "Probe" means a request sent on 12 Oct 2026, not
documentation. This is general information, not legal advice; the company secretary and the
accountant confirm before relying on it.

## A. Corporate secretarial obligations (Companies Act 1967; ACRA)

| # | Rule | Deadline | Section | Verdict | Source (date) |
|---|---|---|---|---|---|
| 1 | The secretary is a natural person whose principal or only place of residence is in Singapore, not debarred; a sole director cannot also be the secretary; AI or software cannot be the secretary | Within 6 months of registration (ACRA guidance); the office may not be vacant for more than 6 months (s171(4A)) | s171(1), (1E), (4A) | Corrected: the Act says "principal or only place of residence"; the 6 months from incorporation is ACRA guidance | SSO (12 Oct 2026); ACRA "after registering a local company" (1 Apr 2026) |
| 2 | AGM within 6 months after the FY end (non-listed). A private company may dispense: all members resolve, or the financial statements are sent to members within 5 months after the FY end, or it is dormant and exempt. Members may still demand an AGM up to 14 days before the 6-month deadline | 6 months; 5-month route | s175, s175A, s203(1)(b) | Verified | SSO; ACRA AGM due dates (11 Feb 2026) |
| 3 | Annual return: fee S$60; late penalty S$300 (up to 3 months late) or S$600; composition of at least S$500; extension of time 60 days for S$200. From 20 Oct 2026 the return confirms ACRA's records of directors, CEOs, secretaries, auditors, members and the registered office, and asks whether anyone holds the CEO role | 7 months after FY end (8 with an overseas branch register) | s197 | Verified | ACRA (14 May, 23 Jun, 29 Jan, 6 Oct 2026) |
| 4 | Financial statements: a solvent exempt private company declares solvency online with the annual return instead of filing statements; otherwise Simplified XBRL (revenue and assets each ≤ S$500k, not publicly accountable) or Full XBRL. Audit exemption: private all year and 2 of 3 (revenue ≤ S$10m, assets ≤ S$10m, ≤ 50 employees) for each of the two preceding years; no 2026 change found | With the annual return | s201, s201A, s205C, Thirteenth Schedule | Verified | ACRA XBRL page (24 Sep 2026); SSO |
| 5 | Appointment or cessation of a director, CEO, secretary or auditor; changes in a director's particulars | 14 days | s173A | Verified | SSO |
| 6 | Registered office change (from 20 Oct 2026 minimum opening hours are abolished; records open at least 2 hours each business day on reasonable notice) | 14 days | s143 | Verified | ACRA commencement notice (6 Oct 2026) |
| 7 | Change of name: special resolution plus Registrar's approval; effective when the Registrar issues the notice | Special resolution lodged within 14 days | s28, s186 | Corrected | SSO |
| 8 | Share allotment (private company): effective only when ACRA updates the electronic register of members; the s161 approval is lodged within 14 days. ACRA guidance expects share changes reported within 14 days; the Act sets no deadline for the return | See text | s63, s161, s186 | Unclear: Act and guidance differ | SSO; ACRA (8 Oct 2026) |
| 9 | Share transfer: a proper instrument of transfer, then the company lodges a notice of transfer; effective on the register update | ACRA guidance 14 days | s126 | Unclear, as row 8 | SSO; ACRA |
| 10 | Registrable controllers: the company's own register within 7 days, then the central register within 2 business days; notices to controllers at least yearly; fine up to S$25,000 | 7 days; 2 business days | s386AG, s386AH | Corrected | ACRA registers page (29 Jan 2026) |
| 11 | Nominee directors: the director informs the company within 30 days; the company updates its register within 7 days and files centrally within 2 business days | 30 days, 7 days, 2 business days | s386AL | Verified | SSO; ACRA |
| 12 | ACRA keeps the electronic register of members (private companies) and the registers of directors, CEOs, secretaries and auditors; the company keeps the register of controllers, the nominee registers and minute books (minutes within one month; written resolutions minuted) | — | s173, s196A, s188 | Corrected: officer registers are ACRA's | ACRA; SSO |
| 13 | Members' written resolutions: ordinary needs a majority, special at least 75% of total voting rights; not for special-notice resolutions; general meetings need 14 days' notice. Directors' resolutions follow the constitution | 14 days | s184A–G, s177, s184 | Verified; directors' resolutions unclear | SSO |
| 14 | Corporate and Accounting Laws (Amendment) Act 2025: phases on 6 May 2026 (director-duty fines, listed AGM in 4 months, buyback approvals) and 20 Oct 2026 (annual-return confirmation and CEO question, registered-office hours, s171(3) reworded, strike-off timing) | — | Act 24 of 2025 | Verified | ACRA (6 Oct 2026) |
| 15 | ACRA API Marketplace: **read** APIs (Entity Information Query, Business Profile API since 18 Nov 2025, Financial Information Query; Corppass, quota-based); **no filing API**; data.gov.sg has a monthly entity dataset | — | — | Corrected: read APIs exist | ACRA API Marketplace (25 May 2026) |

**Not verified:** the Regulations that set the 7-day and 2-business-day periods (ACRA guidance
used); s143 as it reads from 20 Oct 2026; the register of charges; fees for individual Bizfile
lodgements; directors' written resolutions (constitution); the 25% controller threshold; stamp
duty on share transfers.

## B. Xero (Singapore), 12 Oct 2026

Plans: Starter S$39, Standard S$70, Premium S$95 a month including GST; prices rise from 1 Nov
2026.

| Feature | Plan | Notes |
|---|---|---|
| Quotes and invoices; Pay now via Stripe and others; reminders; repeating; view tracking | All (Starter: 20 invoices, 5 bills) | PayNow not mentioned |
| InvoiceNow | Yes | — |
| Multi-currency | Premium | — |
| Bank feeds | Yes | 21,000+ institutions |
| Suggested matches, bank rules, bulk reconcile, cash coding | Standard and above for bulk | — |
| JAX auto-reconcile | Standard and Premium (Starter add-on) | Per bank account; uses rule, match, "memory" and "prediction" (how other Xero users coded similar items); only when "highly confident"; reviewable and undoable |
| Short-term cash flow | 30 / 60 / 180 days by plan | From invoices, bills and quotes, with scenarios |
| Benchmarking | Premium | Anonymised, aggregated Xero customer data by industry and country; 9 metrics; Singapore coverage not confirmed |
| Hubdoc / smart document capture | All | Draft bills for approval; email forwarding; duplicates flagged |
| Bills and batch payments | Yes | Planned payment dates, repeating bills, approvals, bank file (Singapore format not stated) |
| Expenses (Xero Me) | Add-on | Receipts, mileage, approvals |
| Purchase orders, inventory, fixed assets | Yes | — |
| Projects | Add-on | — |
| Payroll | Not native in Singapore | Partner apps (Talenox, HReasily, SimplePay) |
| GST F5 filed to IRAS | Yes | F7 and F8 not confirmed |
| JAX chat | All (beta) | Answers on data, how-to, creates invoices and quotes on request; "won't run in the background unless you ask" |
| JAX roadmap (9 Jul 2026) | SG availability not stated | Bill Protection, Payment Follow Ups, Cash Flow Actions, XeroForce agent builder, Claude and Copilot integrations; "autonomously perform complex tasks, under your supervision" |

## C. Sign-in, email and chat providers

| Item | Finding | Source (date) |
|---|---|---|
| Identity Platform | Custom OIDC providers (code or implicit flow; discovery document required), SAML, per-tenant providers and TOTP MFA | Google Cloud docs (9 Oct 2026) |
| Firebase Auth emulator | Mocks any provider with a fake sign-in page; real ID tokens accepted via `signInWithCredential`; TOTP and tenants not documented | Firebase docs (6 Oct 2026) |
| Zoho OpenID Connect | Discovery at `https://accounts.zoho.com/.well-known/openid-configuration`; scopes `openid email profile phone`; `id_token` from the token endpoint (RS256/RS384); PKCE S256; identify users by `sub` | Zoho Accounts docs; live discovery |
| Zoho data centres | us, eu, in, au, jp, uk, ca, sa, ae and sg (`accounts.zoho.sg`); each has its own issuer, so one Identity Platform provider per data centre. Inference to test: the implicit flow will fail (no `id_token` response type), and the issuer in Zoho's sample token lacks `https://` | Zoho `serverinfo` (live); multi-DC docs |
| ZeptoMail (renamed Zoho CPaaS, Sep 2026) | Send API `POST https://cpaas.zoho.com/v1.1/email` (old `api.zeptomail.com` still answers); header `Authorization: Zoho-enczapikey <key>`; SMTP `smtp.zeptomail.com` 465/587; **no idempotency key** (`client_reference` only); webhooks for opens, clicks, bounces and complaints, **no "delivered" event** for email; DKIM and CNAME for domain verification; documented data centres US, EU, AU, IN, CN, JP; sandbox since Mar 2025; send-only | Zoho docs (24–25 Sep 2026) |
| Resend | SMTP `smtp.resend.com`; `Idempotency-Key` kept 24 hours; webhooks via Svix (delivered, delayed, bounced, complained, opened, clicked, failed, received, suppressed); sending regions us-east-1, eu-west-1, sa-east-1, ap-northeast-1 (Tokyo); **account data, metadata and logs stored in the US**; inbound receiving since 3 Nov 2025 (webhook carries metadata, body fetched by API) | Resend docs; blog (3 Nov 2025) |
| Slack | A private app with a bot token and `chat.postMessage` (`chat:write`); incoming webhooks are fixed to one channel; buttons need an interactivity URL and a 3-second reply; single-workspace apps need no Marketplace listing; Singapore data residency on Business+ and Enterprise | Slack docs (changelog 3 Jun 2025) |
| Zoho Cliq | REST API v2 `channelsbyname/{name}/message` (as a bot with `bot_unique_name`), `buddies/{email}/message`; OAuth scope `ZohoCliq.Webhooks.CREATE`; webhook tokens (5 per user); 50 requests a minute per user; cards with up to 5 buttons (`open.url` for deep links); data centres com, eu, in, com.au, jp, zohocloud.ca | Zoho Cliq docs |
| Notification libraries | Go: `nikoksr/notify` (MIT), `containrrr/shoutrrr` (MIT); Apprise (Python, BSD-2); Novu (MIT core plus a commercial `ee/`); Knock (hosted service) | GitHub (12 Oct 2026) |

**Not verified:** an end-to-end Zoho sign-in through Identity Platform (a P0 exit test); whether
the undocumented `*.zoho.sg` endpoints for ZeptoMail and Cliq keep data in Singapore; whether
ZeptoMail has any inbound feature; Resend's DNS details and the exact wording of its data-location
statement.

**Correction (13 Oct 2026, PRD rev 0.8).** The 25% controller threshold in the "Not verified" list is
now confirmed: ACRA's registrable-controllers guidance (page dated 29 Jan 2026) gives significant
interest as more than 25% of the shares or of the voting power, and significant control as the right
to appoint or remove directors holding a majority of board votes, more than 25% of member votes, or
significant influence or control. It also requires a notice to each registrable controller at least
once a year. The Regulations behind the 7-day and 2-business-day periods remain unverified. See the
[validation record](../reviews/prd-001-deep-review-validation-2026-10-13.md), F50.
