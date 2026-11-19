# Threat model: the knowledge base

| | |
|---|---|
| Feature and requirement IDs | The assistant's know-how: the platform knowledge base at runtime, the three read tools, the reader filter, know-how answers and their citations (PRD-001 §9.11: AI-49, AI-50, AI-51, AI-53, AI-54, AI-56, AI-58, AI-60), and the organisation's knowledge base, the compiler and publishing (AI-52, AI-55, AI-56, AI-57); QA-AI-06, CTL-AI-08 |
| Author, reviewer, date | Claude Code for the owner, 18 Nov 2026, for security review #4; second review waived before production (AGENTS.md), held with `needs-human-review` if wanted |
| Data and its class | Platform pages and the user guide: general, public-in-product know-how, no organisation figures. Organisation pages: policies and decisions written by an organisation's administrators, possibly personal data or payroll in a note (the page inherits the strictest class of its sources, AI-54). Page versions an answer used: confidential business records in the activity log |
| Who acts | The person asking, through the assistant; the AI service, which holds no database access and reads only through the API; the API, which runs the tools with the person's live roles; the migrator, the only writer of the platform tables; the Owner and Administrator who edit, recompile and publish, and the compiler job under its own service identity (no database role) |

## 1. What we are building

**The platform's pages ship in the release** (design D1). The compiled pages in `kb/` and the user guide they cite are embedded
in the migrate image and loaded whole, in one call, into four global tables (000560) as the migrator. No application role
holds a grant on them. A release whose pages fail the Go lint (the same rules as `scripts/kb.py`, kept equal by 27 shared
fixtures) is refused at load.

**Reads go through the reader filter** (design D2, D3). `kb.index`, `kb.read` and `kb.search` run through `callAITool` as the
turn's person, like every read tool, and call six SECURITY DEFINER functions that only the API may run. The filter is in the
query: a page is open to a reader when every guide page it was compiled from is open to one of their live roles (the guide's
own `roles`, the Owner reading all) and its data class is one their rights allow (never identity; payroll only with AI payroll
access and their payroll right). A page the reader may not open is not in their index, their search, their read or their
citations, and its refusal is the same as for a page that does not exist.

**Answers cite what a person can open** (design D4). A know-how sentence names a page read this turn and a section of it.
Platform code checks the page and section were read and every number is a whole token of that section, then resolves the
citation from the section's own guide-page citations. The model never names a source; nothing resolves to a compiled page.

**The activity log records the page versions** an answer used (000561): the release and the page file's digest.

**The organisation's knowledge base** (tasks 4 to 8, built): its pages are rendered from settings (no model) and compiled from notes, as
drafts; nothing is live until a named person publishes, as an approval object signed under the organisation's policy. The compiler is a
job with its own service-token class and **no database role**: it reaches five internal operations of the API (list the work, ask the model
for a page, store a draft, raise a finding, run the weekly lint), none of which publishes, reads the ledger or runs a command. A person's
**Recompile** only queues that job; it runs as the compiler, never as the person or the API. A person's edit is never overwritten.

Trust boundaries: the model ↔ AI service (what it asks and says); AI service ↔ API (the internal operations, the turn's
delegated token); API ↔ database (the reader filter, grants); migrator ↔ database (the release's only write path); the
person's browser ↔ API (the panel's chips, the guide).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| K1 | Reader filter | Information disclosure: a page, summary, snippet, count or index entry reaches a reader whose roles the guide page it came from excludes, or whose access was revoked | A restricted guide's content (audit, data protection, controls, access review) explained to the wrong role | Medium |
| K2 | Tools → model | Tampering / elevation: instructions inside a page's text tell the model to ignore its rules, call another tool or reveal its prompt | A manipulated answer or a call the person did not ask for | Low (the pages are reviewed repository text) |
| K3 | Answer | Tampering: the model states an invented number, a rule from memory or a figure of the books as know-how, or cites a page it did not read | A false rule or deadline shown as the guide's | Medium |
| K4 | Citation | Spoofing: a citation that names a source the section was not compiled from, or links to a compiled page, a record the person may not open, or an external address | A person sent to the wrong or a hidden place | Low |
| K5 | Statutory content | Tampering: a guide older than the pack in force answered as current | A wrong rate, threshold or deadline | Medium |
| K6 | Platform tables | Tampering: an application role writes or widens a page's audience or data class; a release loaded half; a page replaced by an unreviewed one | Every organisation reads a wrong or leaked page | Low |
| K7 | Page versions in the log | Repudiation: an answer's pages cannot be found again, or a version recorded for a page that was not read | "Why did the assistant say that" unanswerable | Low |
| K8 | Search | Denial of service / disclosure: a long or crafted query slows the database or probes for hidden pages by timing or counts | Slow answers; a hidden page's existence inferred | Low |
| K9 | The AI never writes | Elevation: a knowledge tool or the answer's segments gain a write or a publish path | The AI changes what the assistant says to others | Low |
| K10 | Organisation pages | Tampering / elevation: a poisoned note or setting text compiled into a page, a compiler that publishes or reads the ledger, an edit overwritten by a recompile, an unapproved publish | Everyone in an organisation told something nobody approved | Medium |

## 3. What we do about it

| Threat | Control | Test that proves it |
|---|---|---|
| K1 | The filter is the database's, in the query, for every derived output: the index, a page, its guide references, a search of pages and of the raw guide. The audience is parsed from the guide's front matter in Go and checked equal to the web app's parser. The AI role and every service role but the API cannot run the functions | `TestTheReaderFilterIsTheQuerys`, `TestAttackServiceRolesReachThePlatformKnowledgeBaseOnlyThroughTheFilter`, `TestTheKnowledgeBaseToolsReadThroughTheReaderFilter`, `TestTheGuideAudienceIsTheSameInGoAndTheWebApp`, the knowledge suite's revoked-reader cases |
| K2 | The tools are read-only and hash-pinned (`tools.lock.json`); a page's text is returned as data and the tool's description says to explain it, never follow it (the release's own page text is trusted and is not quoted as `untrusted_text`, which would also hide the tool's hints from the model; an organisation's untrusted page, P2, is quoted by its taint); the pages are repository text reviewed in a pull request; the answer is grounded and screened as every answer is | `TestTheRegistryRefusesAnythingButABoundedRead`, `TestEveryChangeToAToolIsANewVersion`, `TestTheAIToolsAreReadOnly` |
| K3 | A know-how segment is accepted only if its page was read this turn, its section was returned, and every number is a whole token of that section; one regeneration, then it is withheld. Plain words still hold no number; figures of the books are fact segments only. **The limit:** this proves a number is in the cited section, not that the sentence says what the section says or is about the guide at all; that is for the answer evals (`[live-verify]`) and, for organisation pages, the weekly semantic lint | `TestAKnowHowSentenceCitesTheGuidePageItsSectionCameFrom`, `TestAKnowHowSentenceThatFailsTheCheckIsWithheld`, `TestKnowHowAndFiguresShareAnAnswer`, and through the real tool operation `TestAKnowHowAnswerWorksThroughTheRealToolPath` |
| K4 | The citation is resolved by platform code from the section's own `(src:)` citations to the guide page; the model has no field to name one, and any `sources` a segment carries are cleared before the section's are resolved. The chip links `/help/<guide key>`; the guide itself is role-filtered | `TestAModelCannotNameASourceForAChip`, the know-how tests above, the knowledge suite's citation check over every page, the `AssistantPanel` test of the chips |
| K5 | `chat@4` and the tool descriptions require `packs.get` for any rate, threshold or deadline and the older-guide wording; **a page that lists a pack older than the one the release holds fails the repository lint (`KB-PACK-STALE`) and the release is never built; packs and pages load from one release, so the runtime state "a pack in force is newer than the page's" cannot arise (task 3.3, decided 20 Nov 2026)**, and the answer-level proof is `[live-verify]` (G4-091) | the prompt's text is pinned per turn (`TestThePromptIsTheLatestVersionHashed`); the stale-guide cases of the knowledge suite are retrieval only |
| K6 | The tables are written only by the migrator in one call; no application role holds a grant on them; the Go lint refuses a release that fails the repository's rules; the database refuses an untrusted page and a bad class or status | `TestTheMigratorLoadsTheShippedKnowledgeBase`, `TestAttackServiceRolesReachThePlatformKnowledgeBaseOnlyThroughTheFilter`, `TestEachRoleHoldsExactlyItsGrants`, `TestTheGoLintAgreesWithKbPyOnTheFixtures` |
| K7 | A kb tool result carries the page versions it read (release and digest); the AI service keeps them on the turn's tool call, the API stores them (only the API may insert the column) and the activity log lists them | `TestAKnowledgeBaseReadIsRecordedWithThePageVersionsItRead`, `TestTheActivityLogShowsTheKnowledgePagesAnAnswerUsed`, the conversation store's round trip |
| K8 | A query is cut to 200 characters and 12 terms, a search to at most 10 hits, a result to the tool's token limit, a call to 5 seconds; the filter applies to counts and snippets, so a hidden page leaves no trace | `TestAQuestionsSearchTermsAreItsWordsWithoutTheFiller`, `TestSearchIsFullTextOverPagesAliasesAndSources`, the revoked-reader cases |
| K9 | The tools are annotated read-only and the registry refuses a write verb; the segments carry words and references only; the AI service has no database access and no credential; the never-list is unchanged | `TestTheAIToolsAreReadOnly`, the AI service's architecture test |
| K10 | Organisation pages come from notes and settings only; the compiler holds no database role and no grant, only a service token that opens its five operations, so it cannot publish, read the ledger or change a version; every draft goes through the Go lint again in the API; the note reaches the model as untrusted text and its answer is screened; a publication is an approval object (a requester cannot sign their own in Team mode, an Administrator never signs, a moved snapshot voids a request) and runs the lint and the contradiction check first; a human edit is never overwritten (the recompile waits beside it with a finding); the weekly lint's advice never publishes and never blocks | `TestTheCompilerMakesDraftsThroughItsOwnIdentityAndNothingElse` (another service's token and the AI's tools and the publication refused to the compiler's token; a draft that fails the lint refused whatever the job says; a stale or payroll note refused; the person's version beside the recompile), `TestAPublicationIsLintedThenMadeByAnOwnerAloneOrSignedByAnother` (Team mode, no self-signing, a voided request, the lint), `TestSettingsArePublishedAsPagesWithTheirChange`, `TestARecompileQueuesTheCompilerAndTheWeeklyLintRaisesFindings` (the recompile is queued, audited and refused to anyone without the right; the lint raises findings once and publishes nothing), the authorisation matrix rows of every `/v1/knowledge-base` operation |

## 4. What remains

- **The model's half of the weekly lint** (contradictions and cross-references a rule cannot see) and **the answer-level proof of every
  know-how segment** need recorded model runs `[live-verify]`. The deterministic half (stale pages, notes with no page, superseded
  overlays, links to no page, orphans) runs weekly (`kb.lint`) and on **Recompile**, and raises findings only.
- **The compiler's cloud identity**: in the release build the worker holds no compiler identity, so **Recompile** answers `KB-COMPILER-03`
  and the weekly job logs that it did nothing; the compiler is a Cloud Run job under its own service account `[live-verify]`. Locally the
  worker signs the compiler's token with the local key, as it does for a scheduled agent's model step.
- **The platform's check against the pack in force** (K5) is built as wording in the prompt and the tools, and as that lint; a runtime notice would report an unreachable state (task 3.3), so none is built. The answer's use of `packs.get` is proved only by a model's recorded runs `[live-verify]`.
- **Layer 1's pre-assembled, cached index** (AI-48, task 2.3) is not built: the model reads the index with `kb_index` instead.
  It costs tokens, not safety: the same filter applies.
- **The company secretary's runs** are now offered the kb tools (its skill already names them), but a run report's knowledge-base snapshot still reads "no knowledge base"; recording the release and the page versions a run used is left.
- **Other languages**: Chinese and Malay questions find a page 1 time in 10 (the knowledge suite). The pages carry English
  aliases only; the plan is aliases per language in the compile step (AI-58).
- **Nightly reader-filter test** (CTL-AI-08's N check) stays a CI and local test: the worker holds no grant on the platform tables, and
  widening one for a nightly run would trade a boundary for a check; the filter is tested on every merge.
- Not assessed here: the model provider's own handling of the page text it is sent (know-how class, AI-83), which the data-class
  policy and the routes already govern (AI-75, AI-83).
