# Threat model: announcement banners

| | |
|---|---|
| Feature and requirement IDs | Banners under the header, published to an organisation or to everyone, and people's dismissals: FR-NTF-03, FR-ADM-07 (PRD-001 rev 0.112) |
| Author, reviewer, date | Claude Code for the owner, 24 Oct 2026; second review waived before production (AGENTS.md), held with `needs-human-review` if wanted |
| Data and its class | A banner's message and link (internal, written by an Owner, Administrator or Super admin); who dismissed which banner (personal data, PDPA) |
| Who acts | The Owner and Administrator (C on Announcements to the organisation); an active Super admin (everyone); every member (sees and dismisses); the Auditor (lists) |

## 1. What we are building

An Owner or Administrator publishes a banner to their organisation's members, or to some roles; a Super admin publishes
one to every organisation's members, such as a maintenance window or an incident. A banner has a message, a link (a path
in Fyniq or an https address), a severity, a start and an end, and may be dismissed unless critical. The web app shows
the live banners meant for the person under the header; a dismissal is kept on the server for that person. The
organisation's banners are tenant rows (`announcement`); the installation's are a global table
(`installation_announcement`) written only through SECURITY DEFINER functions that check an active Super admin.

Trust boundaries: browser ↔ API (publish, withdraw, dismiss, list); API ↔ database (RLS, definer functions).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | Browser → API | Spoofing: a link to a look-alike sign-in page in a banner everyone trusts | Phishing of members' credentials | Medium: the Owner may be tricked, or an account taken over |
| T2 | Browser → API | Tampering: a banner's message or link changed after it was reviewed | Members see something nobody published | Low |
| T3 | Browser → API | Elevation: someone who is not a Super admin publishes to everyone, or a member to the organisation | A banner to people the author has no say over | Low |
| T4 | Browser → web app | Tampering: markup or script in a message or a `javascript:` link | Script in every member's session (XSS) | Medium |
| T5 | Browser → web app | Denial of service: a critical banner that cannot be dismissed covers the page | Members cannot work | Low |
| T6 | Database | Repudiation: a banner published or withdrawn with no record of who | No accountability | Low |
| T7 | API → database | Information disclosure: one organisation's banners or dismissals seen by another | Internal notices leak | Low |

## 3. What we do about it

| Threat | Control | Test that proves it |
|---|---|---|
| T1 | A link is a path in Fyniq or an https address, nothing else; the web app marks an outside link as such and opens it in a new tab with `rel="noopener noreferrer"` (task 4.1); publishing needs C on Announcements (the Owner and Administrator) or an active Super admin with step-up | `TestBannersAndAnnouncements` (a `javascript:` link refused), the matrix rows |
| T2 | A banner never changes but by being withdrawn, once (FY190); to say more, publish another | `TestBannersKeepTheirHistory` |
| T3 | The organisation's: the policy and RLS; the installation's: the API's Super admin check and step-up, then `publish_installation_announcement`, which refuses anyone but an active Super admin (FY085); no role holds a grant on the global table | `TestAnnouncementsToEveryone`, `TestBannersKeepTheirHistory` |
| T4 | Messages are plain text, at most 300 characters, rendered as text, never HTML; links are checked as in T1 | `TestBannersAndAnnouncements`; the web app's component tests (task 4.1) |
| T5 | A banner sits under the header and never covers the focused element (SC 2.4.11); every banner has an end; critical ones are meant for what everyone must see, and are withdrawn early when no longer true | The banner screen's tests (task 4.1) |
| T6 | Publishing and withdrawing are audited: the organisation's in its trail (`require_audit`), the installation's in the installation's trail as the Super admin | Both API tests read the trail |
| T7 | `organisation_id`, `FORCE ROW LEVEL SECURITY` and one tenant policy on both tenant tables; the installation's banners are read only through `installation_announcements` | The catalogue test; `TestBannersKeepTheirHistory` |

SOD: no money moves and no record changes. The AI and MCP have no route to any of it (Own notifications and Announcements
grant the AI nothing).

## 4. What remains

- Platform banners for incidents and maintenance are published by a Super admin by hand; publishing them automatically
  from the incident runbook, and the "What's new" note (FR-UX-35), are not built.
- A dismissal is kept per organisation: a person who belongs to two organisations dismisses an installation banner in
  each.
