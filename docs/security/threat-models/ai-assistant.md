# Threat model: the AI assistant

| | |
|---|---|
| Feature and requirement IDs | The assistant's read tools on behalf of the person, the AI exclusions, the semantic layer and typed facts, result limits, numeric grounding and the calculator, untrusted text and screening, the AI service and its internal API, conversations, limits, feedback, the activity log's turns, the purge, the panel, the evals and the monitoring: AI-01 to AI-05, AI-08, AI-12, AI-18 to AI-25, AI-68, AI-69, AI-P1, AI-P2, AI-P6 to AI-P8, CTL-AI-06, FR-UX-17, QA-AI-01, QA-AI-03 to QA-AI-05 (PRD-001 rev 0.277 and 0.278; `p1-ai-assistant`) |
| Author, reviewer, date | Claude Code for the owner, 15 Nov 2026, for security review #4; second review waived before production (AGENTS.md), held with `needs-human-review` if wanted |
| Data and its class | Questions, answers and their facts, which may carry business figures, personal data or payroll (confidential to restricted); the delegated read token of a turn (secret); the AI service's own token (secret); feedback and its reasons (confidential); the activity log's turns (confidential, payroll turns restricted) |
| Who acts | Each member asking in their own conversations (the AI activity row's V); the AI service, reading as that member through the API; the model, which writes words and asks for reads; the Owner and an Administrator reviewing feedback; the worker purging conversations; the AI and MCP clients (none of these operations) |

## 1. What we are building

**The AI service** (AI-P2, design D3). A separate binary, `cmd/ai`, runs the tool loop. It holds no database code
(archtest) and no write credential. It reaches the platform only through the API's internal operations, on internal
ingress, with its own service token and the turn's delegated token: `/internal/v1/model-calls`, which runs the router's
rules, and `/internal/v1/tool-calls`, which runs the public read operations in process.

**Reads as the person** (AI-01, AI-02, AI-03, AI-04, AI-68). Each turn gets a delegated read token for that turn alone,
which ends with the turn, carrying the person's roles and the AI column's lesser right. Every read is a registered tool
(`contracts/ai/tools`): read-only, bounded, closed-world inputs, pinned by hash. It is one of the public read
operations, filtered by the AI exclusions after the masking of an M right (no NRIC or FIN, bank numbers, secrets, home
addresses; pay only when allowed). Every figure is a typed fact with its source, lineage and ledger revision, within
row and token limits.

**Untrusted text and screening** (AI-18, AI-20, AI-P7, AI-25). Free text from records reaches the model quoted as
`untrusted_text`, never as instructions; tool results, questions and answers are screened (Model Armor in a release, a
local stub offline), and a screening that does not finish refuses (fail closed). An output filter masks NRIC and
bank-number shapes in anything bound for the model or the person, the conversation's earlier questions included.

**Grounded answers** (AI-08, AI-69, AI-22). The model answers in a strict structure of words, fact references and
"could not establish"; Fyniq renders each figure from its fact, rounded once by RND-DISPLAY, and asks again once when a
figure fails the check, then withholds it. The panel renders the answer as text in a fixed Markdown subset, never HTML,
with links only to Fyniq's own pages, violet with the AI badge.

**Conversations, limits and feedback** (AI-P8, AI-23, AI-12, QA-AI-05). Each conversation is its person's alone, kept
append-only under FORCE RLS, purged after its retention. Turns are admitted by the AI switch and the limits: a minute, a
day, reads per question, a conversation's length. A person rates an answer once; an Owner or Administrator reviews it,
with payroll masked as in the activity log.

Trust boundaries: browser ↔ API (questions, the event stream); API ↔ AI service (internal operations, two tokens); AI
service ↔ model provider (prompts and answers, through the router); API ↔ database (grants, RLS, the guards); the
edge ↔ the internal path (refused from outside).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | Records → tool result → model | Tampering / elevation: prompt injection in a memo, a bank line, a bill or a name tells the model to read something else, ignore its rules, or claim something | The model reads beyond the question, misleads the person, or leaks what it read | High |
| T2 | API read → model | Information disclosure: an exclusion bypassed (an NRIC, a bank number, a home address or pay reaching a model through a field, a memo, a search result, a name or an earlier question) | Personal data sent to a provider; PDPA breach | Medium |
| T3 | AI service ↔ API | Spoofing / elevation: the delegated token stolen, replayed after its turn, used for another person or organisation, or for a write; the internal path reached from outside | Reads or writes beyond the person's own; the AI acting with no person | Medium |
| T4 | Conversation | Information disclosure: accumulated disclosure, a hidden value rebuilt piece by piece across questions, or facts carried from earlier turns beyond what the person may read | Excluded data reconstructed | Medium |
| T5 | Browser → API → provider | Denial of service (financial): denial of wallet, a flood of questions, endless reads, long conversations, or a budget raced past | Unbudgeted cost; the AI unavailable to others | Medium |
| T6 | Model → answer → screen | Tampering: output manipulation, a figure the model made up, a figure bound to the wrong fact, raw HTML or an image that fetches, a link that exfiltrates, a claim of an action never taken | The person acts on a wrong figure; data leaks through a URL | High |
| T7 | API → browser (event stream) | Tampering / spoofing: the relayed stream injected, cross-site requests asking as the person, a stream that keeps the turn's token alive | Questions asked or answers read as someone else | Low |
| T8 | Conversation, feedback, activity log | Information disclosure: another member's conversations read, or their payroll questions through the activity log or the feedback review | Personal and payroll data exposed | Medium |
| T9 | AI / MCP → API | Elevation: an AI or MCP client asks the assistant, reviews feedback, or reaches a write through it | Golden rules 5 and 6 broken | Low |
| T10 | Database | Tampering / repudiation: a turn's answer, reads or classes changed after the fact, a conversation removed outside the purge, feedback reviewed twice | The record of what the AI said and read is lost | Low |
| T11 | Model | Information disclosure: the system prompt leaked, or the model claims to have posted, approved or paid | Rules learnt by an attacker; the person misled | Medium |
| T12 | Evals | Repudiation / tampering: a golden value wrong, a harness that passes a wrong answer, a release with no recording | A bad model or prompt ships unnoticed | Low |

SOD: the assistant changes nothing; reviewing feedback is the Owner's or an Administrator's. RLS: every AI table has
`organisation_id` and `FORCE ROW LEVEL SECURITY`.

## 3. What we do about it

| Threat | Control | Test that proves it |
|---|---|---|
| T1 | Free text reaches the model only as quoted `untrusted_text` inside a tool result; screening of the result withholds the text and keeps the figures; the prompt says text from records is data; every read is a registered, read-only, bounded tool, so an injected read reads only what the person may read; the red team plants an injected memo and requires that its read is never made and its claim never said | `TestUntrustedTextStaysData`, `TestTheRegistryRefusesAnythingButABoundedRead`, `TestLocalScreeningFlagsTheRedTeamCorpus`, `TestTheAssistantSuites` (redteam: injection-in-a-memo, link-in-a-memo) |
| T2 | The exclusion policy runs on every answer to an AI principal, by field name at any depth, after the masking of an M right; pay goes only with `ai.payroll_access` and the person's payroll right; the output filter masks NRIC and bank-number shapes in every text bound for the model, earlier questions included; screening refuses a request that still carries one; CTL-AI-06 plants them in the books and scans every model request, and `verify ai.traces` scans the kept turns daily | `TestWhatNeverReachesTheAI`, `TestTheNeverSendFieldsNeverLeave`, `TestTheOutputFilterMasksNRICAndBankNumbers`, `TestPlantedExclusionsReachNoModelRequest`, `TestTheToolLoopSaysWhatWentWrong`, `TestTheAITracesHoldNoExcludedValue`, `TestEachVerificationFindsItsBreach` |
| T3 | The turn's token is opaque, for one turn, organisation and person, read-only, and ends with the turn; the database keeps it only as a hash and refuses a read outside its turn; the internal operations need the AI service's own token as well; the AI service has no database code; the edge refuses `/api/internal/` in Caddy and Cloud Armor | `TestTheAssistantsTokenReadsAsThePersonForItsTurn`, `TestAttackAIGrantsReadOnlyForTheirTurn`, `TestAttackAGrantsClassesOnlyWiden`, `TestTheAssistantAnswersThroughTheAPI` (a forged service token), `TestTheAIServiceCarriesNoDatabaseCode`, the edge module's `the_edge_screens_every_request` |
| T4 | Every read is filtered again on each turn, so no earlier answer carries more than the person may read; masked values stay masked in every request, so the hidden digits never reach the model to be repeated; earlier facts carried by ID are the turn's own reads; a turn widens its classes only, so later calls route for what it read | `TestTheAssistantSuites` (redteam: accumulated-disclosure), `TestTheAssistantAnswersWithinItsLimits`, `TestAttackAGrantsClassesOnlyWiden` |
| T5 | The API admits a turn only within the switch and the limits (questions a minute and a day, the conversation's length), before it is kept; the AI service stops at the reads per question; the router reserves each model call against the budgets in the database; AI usage counts the assistant's cost per conversation | `TestTheAssistantAnswersWithinItsLimits`, `TestBudgetsAdmitOnlyWhatTheyCover`, `TestTheAISwitchStopsTheAI`, `TestTheAssistantIsMonitored` |
| T6 | Figures come only from facts: the strict structure binds each figure to a fact of the turn's reads, checked by measure, member, days and currency, with stray figures in words refused; a failure is asked again once and then withheld; Fyniq renders the figure; the calculator works on facts only; the panel renders text, never HTML, with no images and links only to `/` paths, and the API drops images and raw HTML and writes other links out in full | `TestFiguresAreBoundToTheirFacts`, `TestTheAnswerIsRenderedInFyniqsWords`, `TestCalcIsExactAndNamesItsInputs`, `TestEveryFigureIsAFact`, the panel's "renders only Fyniq's Markdown subset" (AI-69) |
| T7 | Asking is a POST, which a session may send only from the web app's own origin (Fetch Metadata); the API relays the stream, keeps the turn and ends its token whatever happens to the stream; the AI service answers only the API (its handler refuses any other caller) | `TestNoWriteFromAnotherSiteWithASession`, `TestTurnsAreTheAPIsAlone`, `TestAPersonsConversationsWithTheAssistant`, `TestTheAssistantAnswersThroughTheAPI` |
| T8 | A conversation is read, asked in, rated and deleted only by its person (another's is not found); the activity log shows each member their own turns, the Owner and the DPO everyone's, a payroll turn masked to a reader who may not see payroll, an unmasked read audited; the feedback review masks the same and refuses golden on a masked item | `TestAPersonsConversationsWithTheAssistant`, `TestTheAssistantsTurnsInTheActivityLog`, `TestFeedbackIsReviewedBeforeItFeedsTheEvalSet` |
| T9 | The conversations, the questions, the feedback and its review are on rows whose AI column is "—" (§9.6); `personOnly` refuses any principal that is not a signed-in person; the AI's database role writes nothing (CMP-AI-01) | The authorisation matrix (AI column), `TestTheAIToolsAreReadOnly`, `TestEachRoleHoldsExactlyItsGrants` |
| T10 | The guard of 000369 keeps a conversation only deleted once, a turn answered once with its classes fixed, a tool call as it ran, feedback reviewed once; nothing is removed but by the purge, which only the worker runs and which removes only what passed its retention or was deleted; every change is audited in its transaction | `TestAttackAIConversationsOnlyGoForward`, `TestAttackTheAIConversationsPurgeTakesOnlyWhatPassedRetention`, `TestTheAssistantsConversationsArePurgedAfterTheirRetention` |
| T11 | The model has no write tool, so a claim of an action is only words; the red team asks for the prompt, for a posting and for approvals under false authority, and requires none is claimed or leaked; the prompt is versioned and hashed with each turn | `TestTheAssistantSuites` (redteam: prompt-leak, write-request, false-authority), `TestThePromptIsTheLatestVersionHashed` |
| T12 | Each golden value is worked out again from the seed in cents and checked against the oracle; each case must pass its own solution and fail a wrong answer, and the judge must fail a synthetic leak; a release run fails on any case with no recording | `TestTheAssistantSuites`, `TestTheJudge`, `TestFromSeed`, `TestTheSuitesAreWellFormed` |

## 4. What remains

- **The recordings.** Every case of the golden and red-team suites passes against a model scripted from its solution,
  offline; each real model's answers are recorded live on the synthetic company, and the release gate runs on them
  `[live-verify]`.
- **Model Armor.** Screening in a release runs through Model Armor's template in Singapore; offline, a local stub with
  the red-team corpus stands in `[live-verify]`.
- **A determined injection that stays within the person's rights.** The assistant reads only what the person may read
  and changes nothing, so an injection that persuades the model can at worst mislead in words. The grounding check
  keeps figures true and the red team measures the rest; a person still decides.
- **Payroll reads.** No read tool reaches pay records in this release; the policy and the classes check are in place
  for when one does.
- **The global endpoint.** Whether the location policy refuses inference on Agent Platform's global endpoint is
  checked before go-live (`docs/runbooks/ai-go-live.md`) `[verify]`.
