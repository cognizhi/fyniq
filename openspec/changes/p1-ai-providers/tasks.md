## 1. The port and local mode

Each slice is its own pull request, stacked in order; each passes `make check` and `make test` on its own.

- [x] 1.1 `ports.ModelProvider` (design D1) and the `replay` provider: cassettes in `evals/cassettes/` keyed by the
  hash of the prompt, tools and model version, a miss an error naming its key; live mode only on `FYNIQ_AI_LIVE=1`
  with a developer's key from the environment, synthetic data only. Tests `// req: AI-70`,
  `// req: platform/local-mode Local AI in replay and live mode`.
- [x] 1.2 The `Screening` port and its local stub flagging the red-team corpus patterns (design D12); the real Model
  Armor adapter `[live-verify]`. Test `// req: platform/local-mode Local AI in replay and live mode`.
- [ ] 1.3 DEV-09's local-only adapters: the developer's `claude` or `codex` command-line agent as a subprocess, its
  sign-in never read, its model named from its configuration, what it lacks declared; absent from release images and
  refused outside `FYNIQ_ENV=local` (archtest and the image check). Tests `// req: DEV-09`.

## 2. The adapters

- [ ] 2.1 The connector kind `model-provider` and the four manifests (`connectors/anthropic`, `openai`, `gemini`,
  `openai-compatible`) with their capabilities, regions and data; the manifest schema and catalogue tests. Test
  `// req: AI-71`.
- [ ] 2.2 The Anthropic Messages API adapter on the egress guard: calls, streaming, strict tools and output, caching
  usage, PDF and image input, reasoning controls, the reported model; Agent Platform through the `gcp` adapter's
  credentials `[live-verify]`. Its conformance suite against a fake. Tests `// req: AI-71`, `// req: AI-27`,
  `// req: AI-28`.
- [ ] 2.3 The OpenAI adapter (the Responses API) with its conformance suite. Test `// req: AI-71`.
- [ ] 2.4 The Gemini on Agent Platform adapter with its conformance suite `[live-verify]` for the live endpoint. Test
  `// req: AI-71`.
- [ ] 2.5 The OpenAI-compatible adapter with presets for Moonshot Kimi, xAI Grok, OpenRouter, a LiteLLM gateway, vLLM,
  Ollama and LM Studio; capabilities it cannot prove count as absent. Test `// req: AI-71`.

## 3. Connections

- [ ] 3.1 Migrations for `ai_connection` and its versions (RLS, `organisation_id` leading every index); the approval
  object `ai_connection` on "AI providers and model routes" (create, change, enable, add a model: the Owner and a
  second approver; disable at once); the credential through the connector secret store, shown once, masked, rotated,
  only in headers, scrubbed from errors; each connection's egress its host only, a self-hosted one a registered
  private endpoint; consumer-app tokens, sessions, cookies and command-line seats refused; billing modes and quotas.
  The API's `/v1/ai/connections` operations, the matrix rows. Tests `// req: AI-72`, `// req: AI-73`,
  `// req: AI-79`.
- [ ] 3.2 The data-handling declaration with dated evidence per model, endpoint and feature; the transfer assessment
  for a connection outside Singapore (CMP-PD-06) and its data inventory row; `ai_data_policy` with the default that
  personal and payroll data go only to evidenced Singapore processing or self-hosted Singapore models; the presets'
  Singapore endpoints chosen against each cloud's availability. Tests `// req: AI-75`, `// req: AI-88`,
  `// req: platform/portability-and-cost Claude in Singapore tie`.

## 4. Model lists and prices

- [ ] 4.1 Model lists: `ai_model_list_read` and `ai_model`; reads on a stored credential, a test, an enable, daily at
  06:00 SGT, a not-found error and Refresh, page by page to a cap, complete, partial or failed; states; a picked
  model probed before the allow-list; a retired or unlisted model leaving after a complete read; the last good list
  kept on failure; no list, no connection (`AI_MODEL_LIST_UNAVAILABLE`); automatic qualification off by default.
  Tests `// req: AI-80`, `// req: AI-81`, `// req: AI-84`.
- [ ] 4.2 Prices: `ai_price_catalogue` and `ai_price`; the provider's list, Google's catalogue `[live-verify]`,
  LiteLLM's catalogue read daily (digest, licence, decimal text, units, tiers), an Owner's price with a second
  approver; normalised matches confirmed, suspicious drops held, Owner prices kept; no price, no usage-based route.
  Tests `// req: AI-82`, `// req: AI-85`.

## 5. Routes, the router, budgets and qualification

- [ ] 5.1 `ai_agent` and `ai_route` versions with ordered steps; presets A, B and C pinning the newest active,
  qualified model of a line from the discovered list; the approval object `ai_route` for a route that widens where
  data goes. The API's `/v1/ai/routes` and `/v1/ai/presets`. Tests `// req: AI-74`, `// req: AI-26`.
- [ ] 5.2 The router (`internal/ai`): admission, the union of classes checked before each call and fallback, failover
  only to an eligible step, a plain stop with a code, strict schemas, thinking, caching and streaming where proven, the
  call recorded with the model requested and reported (`ai_call`, append-only, through `ai_record_call`). Tests
  `// req: AI-76`, `// req: AI-83`, `// req: AI-87`, `// req: AI-27`.
- [ ] 5.3 Budgets: `ai_budget`, `ai_reservation`, `ai_reserve` and `ai_settle` under row locks (the concurrent-spend
  scenario: S$5 left, ten calls of S$1, at most five admitted); unknown outcomes kept; probes, evals and retries
  counted; alerts at 80%, a hard stop at 100%; metering by connection, model, agent, person and organisation; costs
  exact, in their currency and SGD. `fyniq_ai`'s grants exactly design D11's. Tests `// req: AI-77`, `// req: AI-86`,
  QA-ENG-05's attack on the AI role.
- [ ] 5.4 Qualification: `ai_qualification` per agent, connection and model version; the `probe` agent and its
  synthetic suite; moving aliases re-qualified weekly; a route, model or prompt change applied only with a passing eval
  run on its approval request; CTL-AI-05's control file and CI gate. Tests `// req: AI-78`, `// req: CMP-AI-03`,
  `// req: CTL-AI-05`.

## 6. The activity log and the switch

- [ ] 6.1 The AI activity log over `ai_call` (and the entry kinds later changes add): own for each person, all for the
  Owner and compliance, restricted classes masked, reading them audited (AUD-05), retained as audit data. The API's
  `/v1/ai/activity`. Tests `// req: AI-12`.
- [ ] 6.2 The switch `ai.enabled` (off at once by the Owner; on with a second approver), refusing new calls at once and
  stopping streams within 60 seconds; organisation and per-person token budgets with alerts. The API's
  `/v1/ai/settings`. Tests `// req: AI-13`.

## 7. Checks and compliance

- [ ] 7.1 C.2 #55, with a fixture that triggers each of its six conditions. Tests `// req: C.2 #55`.
- [ ] 7.2 The AI inventory in the Compliance Center (connections, models, routes; prompts and tool versions as later
  changes add them; owners and data categories) beside the AI DPIA in the DPIA register. Tests `// req: CMP-AI-02`.

## 8. Screens, help and the guide

- [ ] 8.1 `ai-settings` (the switch, budgets) and `ai-usage`, from their fragments; help keys; guide
  `docs/user-guide/ai-settings.md` compiled into `kb/`. Tests `// req: AI-13`, `// req: AI-77`.
- [ ] 8.2 `ai-providers` and `ai-provider-connection` (add from an adapter or preset, credential, models from the list,
  probe, billing, data handling, enable with a second approver); help keys; guide `docs/user-guide/ai-providers.md`.
  Tests `// req: AI-72`, `// req: AI-80`.
- [ ] 8.3 `ai-model-routes` and `ai-route-editor`; help keys; the guide's routes section. Tests `// req: AI-74`.
- [ ] 8.4 `ai-activity-log`, and `my-ai-plan` showing P2's own-plan option as not offered; help keys; guides compiled.
  Tests `// req: AI-12`.

## 9. The finish

- [ ] 9.1 `docs/security/threat-models/ai-providers.md` (credentials, SSRF through a base URL, classes leaking on
  fallback, budget exhaustion, alias drift, a poisoned catalogue, consumer tokens) for security review #4; the build
  plan's paragraph on what was built; `make check`, `make test`, `make test-e2e` and `make evals` pass.

## Workflow follow-up

- `/opsx:verify p1-ai-providers`, then `/opsx:archive p1-ai-providers --skip-specs`.
- `p1-ai-assistant` follows, then `p1-scheduled-agents` and `p1-company-secretary`, then `p1-launch-readiness`.
