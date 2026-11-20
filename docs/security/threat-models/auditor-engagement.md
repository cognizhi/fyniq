# Threat model: the auditor's engagement, requests and samples

| | |
|---|---|
| Feature and requirement IDs | The Auditor's engagement (FR-UX-42), the Auditor role (§6.2) and two new Appendix D rows, Auditor engagement and Auditor requests and samples (rev 0.574) |
| Author, reviewer, date | Claude Code for the owner, 23 Nov 2026; second review waived before production (AGENTS.md), held with `needs-human-review` because the Auditor gains its first C |
| Data and its class | Engagements, requests and answers (confidential business data); samples (posted ledger lines as the books show them: confidential business data); evidence is referred to by ID, never copied |
| Who acts | An Owner (opens, confirms, closes, answers); the engagement's Auditor (asks, accepts, draws); an Owner, Controller or Accountant (answers); the Administrator, Controller, Accountant and Auditor read |

## 1. What we are building

An Owner opens a time-boxed engagement for a member holding the Auditor role and a second Owner confirms it (the same Owner in Founder mode). While
it is active and between its dates the auditor asks for items, a named person answers with evidence, the auditor accepts the answer, and the auditor
draws samples of posted lines by a rule with a stored seed. The ledger is only read.

Trust boundaries: member ↔ API (rights, then the engagement's own auditor and dates); API ↔ database (column-limited grants, guards, row-level security).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | Roles | Elevation: the Auditor's new C reaches anything beyond requests and samples; another Auditor writes to an engagement that is not theirs | An auditor changes the books or another firm's engagement | Medium |
| T2 | Opening | Elevation: one Owner opens and confirms an engagement alone in Team mode; a member without the Auditor role is named | An unreviewed grant of the auditor's channel | Low |
| T3 | Window | Tampering: requests or samples added to a pending or closed engagement, or outside its dates; an engagement kept open for years | The auditor's channel outlives its purpose | Medium |
| T4 | Sample | Tampering: a sample chosen by hand, edited afterwards, or drawn again until it looks right | A biased audit sample | Medium |
| T5 | Request | Tampering: a request's text changed after it was answered; an answer changed after it was accepted | The record of what was asked and said is rewritten | Medium |
| T6 | Answer | Information disclosure: evidence named that the answerer cannot see, or copied into the engagement | A file leaves its record's rights | Low |
| T7 | Database | Information disclosure: one organisation reads another's engagements | Cross-tenant disclosure | Low |

## 3. Mitigations, and what proves each

| # | Mitigation | Test |
|---|---|---|
| T1 | The Auditor's C is on one row, Auditor requests and samples; the Auditor holds no C on any other row (a test names this one exception); asking, accepting and drawing need the engagement's own auditor (AUDENG-02) | `TestAuditorReadsEverythingAndChangesNothing`, `TestTheAuditorsEngagementIsOpenedByAnOwnerAndAskedAndSampledByTheAuditorOnly`, the matrix rows |
| T2 | Opening is the Owner's alone; in Team mode another Owner confirms (AUDENG-04); the named auditor must hold the Auditor role and the assignee a role that answers (AUDENG-05) | `TestTheAuditorsEngagementIsOpenedByAnOwnerAndAskedAndSampledByTheAuditorOnly` |
| T3 | A database guard refuses a request or sample unless the engagement is active; the API refuses outside the dates (AUDENG-01); an engagement is at most 366 days and never reopened | `TestAnEngagementOnlyGoesForwardAndTakesNothingWhileNotActive`, the API test above |
| T4 | A draw is a pure function of the lines, the rule and a stored seed (SHA-256 ranks); a sample is insert-only and the database refuses any update or delete, even by the schema's owner | `TestTheDrawIsReproducibleAndKeepsToTheRule`, `TestAnEngagementOnlyGoesForward...` |
| T5 | A request changes only by being answered, then closed, and keeps its text; the database refuses any other change or delete | `TestAnEngagementOnlyGoesForward...` |
| T6 | An answer names evidence by ID, each checked to exist in the organisation's evidence; the files are read under the evidence rules, not through the engagement | the API test |
| T7 | Row-level security on all three tables; the worker holds no write | the adapter test and the grants golden |

## 4. Residual risk and what is not built

- The dual control is the in-feature second-Owner confirm, not an approval-engine object; in Founder mode one Owner may confirm alone. `[owner to confirm]`
- The evidence an answer names is not re-checked against the auditor's own right to read it: the Auditor holds V on Evidence.
- A sample reads the ledger in pages of 500 entries and stops at 500,000 qualifying lines; its time on a very large ledger is not measured.
