# Second review · AI and MCP, identity and tenancy (security reviewer)

The record of the second review of this area ([review plan](second-review-plan-2026-10-14.md), Area 3).
It covers PRD-001 revisions 0.16–0.22 and PRD-002 revisions 0.2–0.6. Every link opens the PRD at the
line under review, pinned to commit `7369b46`. The [verification sweep](../research/verify-sweep-2026-10-14.md)
gives the official source for each statutory or vendor fact.

**Building does not wait for this review** (PRD-001 rev 0.22). The rules below are built on the
current recommendation and the evidence on file. Until this pull request merges, the 14 pages listed
at the end show a **Needs additional verification** banner (FR-UX-41). Merging marks the area reviewed in
the [review register](review-status.json), and the next release removes the banner.

## How to review

1. For each check, read the linked lines and write **Agree**, **Change** (say what) or **Question** in
   the Verdict column of this file, in a review comment on that line or as a suggested change.
2. Answer each decision in the last column. A blank answer means the current recommendation stands.
3. Approve the pull request when you are content. If anything changes, the PRD gets a new
   `(rev 0.N)` row, and its specs are updated and stamped in this pull request before it merges.

The P0 identity spike (Ory Hydra, the JWT-bearer path of open question 40, Zoho and Google sign-in) proceeds before this review. A finding here can still change it: say so as early as you can. The AI and MCP surface is P1 and is not exposed to any client until an Owner enables it with dual control (golden rule 6).

## Checks

| # | Check | Where to read | Verdict (Agree · Change · Question) | Notes |
|---|---|---|---|---|
| 1 | Model providers, routes, data classes and egress; registered private endpoints | [§9.9.1](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L1580) · [AI-70](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L1595) · [AI-83](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L1608) · [AI-88](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L1613) · [SEC-37](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L2686) |  |  |
| 2 | The never-list: no write, approve, export or admin tool on the AI or MCP surface | [§9.6](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L1465) |  |  |
| 3 | Data residency (ADR-T2): Opus 4.6 confirmed in Singapore on all three Google pages, Sonnet 4.6 on two of three, the 5.x models on none | [§12.5](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L2302) |  |  |
| 4 | MCP against the 2026-07-28 specification: `iss` checks, credentials keyed by issuer, dynamic client registration deprecated | [§9.7](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L1504) |  |  |
| 5 | ADR-T3 as restated in rev 0.21: accepted by the founder, with P0 exit item 6 as the proof and a failure reopening it; the portable sign-in path kept open | [§12.2.1](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L2153) · [PRD-001 line 2197](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L2197) · [PRD-001 line 2631](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L2631) · [PRD-001 line 3002](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L3002) |  |  |
| 6 | Tokens: audience binding, DPoP where clients support it, revocation within 60 seconds | [AI-36](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L1539) · [AI-38](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L1541) |  |  |
| 7 | Sign-in: the Zoho issuer per data centre; passkeys implemented in the API (Identity Platform has none) | [FR-ADM-08](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L1275) · [PRD-001 line 3008](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L3008) |  |  |
| 8 | First-run setup: two Super admins, offline printed recovery codes and a two-person recovery runbook (open question 36, rev 0.20) | [FR-ADM-06](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L1273) · [open question 36](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L3132) |  |  |
| 9 | NRIC handling while PDPC's guidance is under review; NRIC never used for authentication after 31 Dec 2026 | [CMP-PD-07](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L2760) |  |  |
| 10 | One sign-in across Fyniq and Nemura and the Finance \| People switch | [FR-PPL-02](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L1321) · [PRD-002 NM-INT-02](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-002-nemura-people.md?plain=1#L250) · [PRD-002 NM-INT-12](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-002-nemura-people.md?plain=1#L260) |  |  |
| 11 | Tenancy on every row: ADR-D1's row-level security as the backstop (rev 0.21 accepted ADR-D1) | [§11.3](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L1889) · [§11.4](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L1944) · [INV-06](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L1839) |  |  |
| 12 | The people assistant's data path and own-pay explanations | [PRD-002 NM-AI-01](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-002-nemura-people.md?plain=1#L277) · [PRD-002 NM-AI-06](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-002-nemura-people.md?plain=1#L282) · [AI-83](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L1608) |  |  |
| 13 | The pages that show this area's banner (list below) and the banner's wording | [FR-UX-41](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L576) |  |  |

## Decisions

| # | Decision | Where to read | Current recommendation (built now) | Your answer |
|---|---|---|---|---|
| D1 | Open question 40: the Hydra edition and the MCP-to-API token path | [open question 40](https://github.com/cognizhi/fyniq/blob/7369b463fbd09de30f6b2abc872103fcc07a3021/docs/prd/prd-001-fyniq-platform.md?plain=1#L3136) | Pin the Apache-2.0 build; prove the JWT-bearer grant in the P0 spike (only for the user of an MCP token just validated, at most 5 minutes, scopes intersected by the API, the key in KMS); get an Enterprise Licence quote; decide before P1's MCP work |  |

## Pages that show this area's banner

`first-run-setup` (First-run installation setup), `sign-in-providers` (Sign-in providers), `connected-apps` (Connected apps (MCP and API clients)), `ai-settings` (AI settings and kill switches), `ai-activity-log` (AI activity log), `ai-providers` (AI providers), `ai-provider-connection` (AI provider connection), `ai-model-routes` (Model routes), `ai-route-editor` (Edit a model route), `ai-usage` (AI usage and budgets), `my-ai-plan` (Use your own AI plan), `people-hr-settings` (People and HR), `nm-fyniq-link` (Fyniq link), `nm-assistant` (People assistant).

Ask for a page to be added or removed in this pull request ([review register](review-status.json)).

## Outcome

| | |
|---|---|
| Reviewer | |
| Date | |
| Result | Approved · Approved with changes · Changes requested |
| PRD revision that records the changes | |
