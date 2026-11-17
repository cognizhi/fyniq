# Threat model: AI providers, routes, budgets and qualification

| | |
|---|---|
| Feature and requirement IDs | The model port, provider connectors and connections, the data-class policy and data handling, routes and failover, model lists, prices, budgets, qualification, the AI switch, the activity log and the AI inventory: AI-12, AI-13, AI-26, AI-27, AI-70 to AI-88, CMP-AI-01 to CMP-AI-03, CTL-AI-05, C.2 #55, DEV-09, SEC-15, SEC-36, SEC-37 (PRD-001 rev 0.274 to 0.276; `p1-ai-providers`) |
| Author, reviewer, date | Claude Code for the owner, 14 Nov 2026, for security review #4; second review waived before production (AGENTS.md), held with `needs-human-review` if wanted |
| Data and its class | Prompts and answers that may carry know-how, business figures, personal data or payroll (confidential to restricted); providers' API keys (secret); prices, budgets and usage (internal); the activity log (confidential, payroll entries restricted) |
| Who acts | Administrators and Owners on "AI providers" (connections, keys, models, routes, the data-class policy: C; an Owner or a second approver approves); Owners alone on budgets and Owner prices; the worker (model lists, prices, the daily qualification); the router on behalf of a person or a job; each member reading their own AI activity, the Owner and the DPO everyone's; the providers; the AI and MCP clients (none of these operations) |

## 1. What we are building

**One model port** (AI-70). Every call to a model goes through `ports.ModelProvider`, implemented by plain-HTTP
adapters for Anthropic (its own API and Agent Platform), OpenAI, Gemini and any OpenAI-compatible endpoint, and in local
mode by recorded answers (replay) or a developer's own command-line agent (DEV-09). Each adapter is a connector of the
catalogue, with a manifest naming what it carries, how it signs in and its presets.

**Connections** (AI-71 to AI-73, AI-79). An organisation connects a provider with a name, a sign-in (an API key, the
service's own cloud account, or none for a server it hosts), a billing mode (usage-based, or a subscription with a
quota) and the provider's settings. The key goes straight to the secret store. Adding, changing, re-keying and
switching a connection on are each asked by one person and approved by another; switching it off is immediate. Every
connection starts off.

**Where data may go** (AI-75, AI-83, AI-88). The data-class policy says which classes (know-how, business figures,
personal data, payroll) each connection may receive; personal data and payroll only where processing in Singapore is
evidenced in the connection's data-handling declaration. Restricted values never leave.

**Routes and failover** (AI-74, AI-76, AI-26). Each agent has a route of one to five steps, each a connection that is on
and a model on its allow-list. The router checks each step against the call's classes and needs before it sends
anything, fails over only to an eligible step, and stops with a plain message and a code otherwise. A route change that
widens where data goes waits for the Owner or a second approver.

**Models, prices and qualification** (AI-78, AI-80 to AI-82, AI-84, AI-85, AI-87). Model names come only from each
provider's own list, read by the worker; a model joins a connection's allow-list after a passing probe and a second
approver, and serves an agent only while its latest run of that agent's evals passed. Prices come from the provider's
list, Google's billing catalogue or LiteLLM's catalogue, in that order, as exact decimals, or from an Owner with a second
approver.

**Budgets, the switch and the log** (AI-12, AI-13, AI-77, AI-86). Monthly budgets in the base currency and in tokens,
per organisation, agent, connection and person, are reserved by a database function before each attempt and settled
after it. The Owner can switch the AI off at once. Every attempt is in the activity log.

Trust boundaries: browser ↔ API (configuration); API and worker ↔ database (grants, RLS, the budget functions); API,
worker and router ↔ secret store (keys); router and worker ↔ providers over the internet (prompts, answers, model
lists); worker ↔ price sources (catalogues); local mode ↔ a developer's own agent.

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | API ↔ secret store; router ↔ provider | Information disclosure: a provider's key shown in an answer, an approval, a log, a provider's error text or a prompt | A key misused to run up cost or read what the account holds | Medium |
| T2 | Browser → API (connect) | Spoofing / elevation: a person's consumer chat plan, a browser session or a cookie used as the organisation's credential (AI-73) | Terms broken; a person's account carries the organisation's data | Medium |
| T3 | Router and worker → provider | Tampering / disclosure (SSRF): a base URL or preset aimed at a private address, the cloud's metadata service, another port, or a host that redirects or re-resolves | Internal services reached; credentials of the platform read | Medium |
| T4 | Router (failover) | Information disclosure: a call carrying personal data or payroll falls back to a step that may not receive it, or to a model whose qualification went out of date | Personal data processed outside Singapore, or by an unproven model | Medium |
| T5 | Connection's declaration | Information disclosure: data handling declared without evidence, or unclear evidence counted as Singapore | Transfers outside Singapore without their assessment (PDPA) | Medium |
| T6 | Router → budgets | Denial of service (financial): runaway spend, concurrent calls racing past a budget, a probe or an eval run outside any budget, a subscription silently switched to per-token billing | Unbudgeted cost | Medium |
| T7 | Provider → router | Tampering: a moving alias or a model ID that now answers with another version, unqualified | Answers from a model that never passed the evals | Medium |
| T8 | Price source → worker | Tampering: a poisoned or broken catalogue (a rate cut to near zero, a changed currency, a licence change, a document altered after download) | Budgets and costs understated; reservations admit too much | Low |
| T9 | Browser → API (models) | Tampering: a typed model name, or one the provider no longer lists, put on a route | Calls to a model nobody vetted, or that fails | Low |
| T10 | API (approvals) | Elevation: one person connects a provider, allows a model, widens a route or the data-class policy, or sets an Owner price alone | Data sent where no second person agreed | Medium |
| T11 | AI / MCP → API and database | Elevation: an agent changes its own routes, connections, budgets, prices or switch, or the AI's database role writes | Golden rules 5 and 6 broken; the AI widens its own reach | Low |
| T12 | API (activity log) | Information disclosure: a member reads others' AI activity, or payroll details through it | Personal and payroll data exposed | Medium |
| T13 | Owner → router | Denial of control: the AI cannot be stopped quickly when it misbehaves | Harm continues while a fix is found | Low |
| T14 | Local mode → production | Elevation: a developer's own command-line agent or plan serves a deployed organisation (DEV-09) | Organisation data sent to a person's account | Low |
| T15 | Eval runner → qualification | Tampering / repudiation: a qualification recorded without a run, edited afterwards, or recorded for a run that never answered | A model serves without having passed | Low |

SOD: connections, keys, switching on, the allow-list, route widening, the data-class policy and Owner prices are each
asked by one person and approved by another (FM-1); switching off, removing a model and narrowing a route are
immediate. RLS: every AI table has `organisation_id` and `FORCE ROW LEVEL SECURITY`.

## 3. What we do about it

| Threat | Control | Test that proves it |
|---|---|---|
| T1 | The key is written to the secret store as it arrives and the connection keeps only its reference; no answer, approval or audit event carries it; the router reads it per piece of work through the cache, by reference; a provider's error text is scrubbed of the credential, tokens, keys and NRIC numbers and cut short before it is logged or shown (SEC-15); replacing a key is asked and approved, and a refused or expired request destroys the new version | `TestAIConnectionsAreDualControlled`, `TestErrorsAreScrubbed`, `TestAModelConnectionReachesOnlyItsOwnHost` (read by reference) |
| T2 | What is not an API key a provider issues to an application is refused before anything is stored: a chat app's sign-in token, a session or cookie, a plan's OAuth token; the database refuses a person's own plan; a person's own plan (AI-73) is not offered | `TestCredentialsThatAreNotAPIKeys`, `TestAttackTheAIConnectionKeepsItsRules`, `TestAIConnectionsAreDualControlled` |
| T3 | A connection's endpoint must be https with no credentials, query or fragment, and a public host unless it is a self-hosted preset; its calls go through the egress guard to that one host on 443 only; the guard dials the address it checked (no DNS rebinding), follows no redirect, bounds the answer and refuses private, loopback and link-local addresses; a self-hosted server is reached only at a private endpoint a Super admin registered for that connection, over TLS with its pinned key (SEC-37) | `TestConnectionDefinitions`, `TestAModelConnectionReachesOnlyItsOwnHost`, `TestEgressRefusals`, `TestEgressDialsTheAddressItChecked`, `TestEgressFollowsNoRedirectAndBoundsTheAnswer`, `TestARegisteredPrivateEndpointIsReachedOnlyWithItsPinnedKey`, `TestEveryOutboundRequestGoesThroughEgress` |
| T4 | Before each attempt the router checks the step against every class the call carries and the capabilities it needs, and skips one that fails without sending it anything; it fails over only to an eligible step and otherwise stops with a code; a step whose qualification is out of date carries no personal data or payroll; a route can name only steps the data-class policy lets every class reach | `TestTheRouterFailsOverOnlyToEligibleSteps`, `TestDataGoesWhereThePolicyAllows`, `TestEveryAgentRunsOnItsRoute`, `TestModelsServeOnlyOnceQualified` |
| T5 | A connection is switched on only with its data-handling declaration, each entry with evidence, its data inventory row, and, when it processes outside Singapore, its transfer assessment; an entry marked unclear never counts as Singapore; changing the declaration is asked and approved; the AI inventory shows each connection's declaration and whether its DPIA is current (CMP-AI-02) | `TestWhereTheAIMaySendData`, `TestDeclarations`, `TestSingaporeEndpoints`, `TestTheAIInventory` |
| T6 | One database function reserves each attempt's most, in the base currency and in tokens, against every budget that applies, under row locks, before anything is sent; it settles to the exact cost, and keeps an unknown outcome whole until reconciled; a cost that cannot be converted is refused where a budget applies; probes and eval runs reserve like any call; the Owner is alerted at 80%; a subscription never changes its billing mode, and when the provider refuses for its quota the call fails over or stops; C.2 #55 warns at 80% of a budget or quota | `TestAttackAIBudgetsAdmitOnlyWhatTheyCover` (ten concurrent calls against five), `TestBudgetsAdmitOnlyWhatTheyCover`, `TestTokenBudgetsBoundEveryCall`, `TestTheAIsRoutesAreWatched` |
| T7 | Every attempt records the model and version the provider reported; another version answering puts the model's pass out of date, and a moving alias's pass lasts a week; out of date, it carries no personal data or payroll, shows it, and the daily job qualifies it again; widening a route needs every step qualified | `TestAStandingComesFromTheLatestRun`, `TestModelsServeOnlyOnceQualified`, `TestTheRouterFailsOverOnlyToEligibleSteps` |
| T8 | Each download is kept with its SHA-256 and a licence check; a document changed after its download is never used; rates are read as exact decimals, and a zero, negative or over-precise rate leaves the model unpriced; a fall of more than half or a change of currency waits for the Owner; normalised matches wait for the Owner; an Owner's price is never overwritten by a read; a model with no price cannot serve a usage-based route | `TestPricesComeFromTheirSource`, `TestTheCatalogueIsReadWithItsLicence`, `TestCataloguePricesAreExactDecimals`, `TestSuspiciousDropsWaitForTheOwner`, `TestModelsMatchExactlyOrNormalised`, `TestNoPriceNoUsageBasedRoute`, `TestAttackAIPricesKeepTheirRules` |
| T9 | Model names come only from the provider's list, read whole page by page; only a complete read marks a model no longer listed, and such a model leaves the allow-list and every route by itself; a model joins the allow-list only after a passing probe and a second approver; a route step must name an allowed model | `TestModelListsComeFromTheProvider`, `TestModelsJoinTheAllowListProbedAndApproved`, `TestReadsAreCompleteOrNot`, `TestTheAllowListKeepsOnlyCallableModels`, `TestEveryAgentRunsOnItsRoute` |
| T10 | Each of these is an approval request the requester cannot approve; a route change is classified as narrowing or widening by the domain, and only narrowing is saved at once; an Owner's price is approved by another Owner; budgets are the Owner's alone | `TestAIConnectionsAreDualControlled`, `TestWideningNeedsASecondApprover`, `TestModelsJoinTheAllowListProbedAndApproved`, `TestAnOwnersPriceTakesASecondApprover`, `TestWhereTheAIMaySendData`, the authorisation matrix |
| T11 | Every operation here is on the never-list for the AI and MCP (§9.6); Appendix D's AI column grants none of them; the AI's database role holds no write on these tables, and calls and reservations are appended only by the API's and worker's roles; the AI tool registry is read-only (CMP-AI-01) | The authorisation matrix (AI column), `TestAttackTheAIRoleCannotReadOrWrite`, `TestEachRoleHoldsExactlyItsGrants`, `TestAttackAIBudgetsAdmitOnlyWhatTheyCover` (the AI reserving and settling), `TestTheAIToolsAreReadOnly` |
| T12 | Each member reads only their own entries, and asking for another's is refused; the Owner and the DPO read everyone's; someone else's entry that carried payroll is masked, its detail withheld, for a reader who may not see payroll unmasked, and an unmasked read is audited (AUD-05) | `TestTheAIActivityLog` |
| T13 | The AI switch is off by default; switching it off needs no one else, refuses new calls at once, and stops a running stream at its next check, well inside a minute; switching it on is asked and approved | `TestTheAISwitchStopsTheAI` |
| T14 | The developer's own command-line agent exists only in the local build, runs in an empty directory with no tools, no MCP servers and no saved session, and never sees Fyniq's variables; release images carry no local adapter | `TestDevelopersOwnAgentAnswersInLocalModeOnly`, `make images` (no local adapter in release) |
| T15 | A qualification is recorded only from a run that answered, with its suite, version, threshold, counts and the run's calls in the log; the database keeps it as it ran and adds only its going out of date, once; the suites ship inside the binary, synthetic, and are checked when they load (CTL-AI-05) | `TestAnUnansweredRunRecordsNothing`, `TestAttackQualificationsAreKeptAsTheyRan`, `TestTheSuitesLoad`, `TestASuiteJudgesExactly`, `TestEvalsReplayTheRecordedRuns` |

## 4. What remains

- **Live providers.** Every adapter is proved against its fake and in replay; the live APIs, the Agent Platform
  endpoint in Singapore and its workload identity, and Model Armor are `[live-verify]` in the dev cloud with real keys.
- **Prompt screening is not wired yet.** Model Armor's adapter and the local stub exist, but screening prompts and
  answers (AI-18, AI-20), prompt injection through tool results and the assistant's own guardrails belong to
  `p1-ai-assistant` and its threat model.
- **A subscription's quota is not checked before a call.** Fyniq counts its use for display and C.2 #55, and learns it
  ran out from the provider's refusal; the call then fails over or stops. Its token budgets still bound it.
- **A declaration is only as true as its evidence.** Fyniq cannot see where a provider processes data; it relies on the
  official page or contract a person records, and an unclear one never counts as Singapore.
- **ADR-T2's revisit.** Which preset the founders' routes start on, and whether Option A's model is still listed near
  go-live, is the owner's decision at enablement `[verify]`.
- **Rate limits and circuit breakers** are fixed in each connector's manifest, not per connection.
