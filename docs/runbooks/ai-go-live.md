# Runbook: the assistant's go-live check

**When:** once before P1 go-live, with the assistant switched on in the dev cloud and then in production; again whenever
a model ID, the endpoint or the chat agent's route changes.
**Requirements:** PRD-001 AI-05, AI-26, ADR-T2 (§12.5), G4-087; `p1-ai-assistant` design D13, task 9.1.
**Who:** the Owner, with whoever holds the project's AI settings.

At go-live the assistant's chat agent runs on preset A: Claude on Google Cloud's Agent Platform in Singapore
(`asia-southeast1`), with the service's own account, under Google Cloud's terms. The check below is G4-087: it proves
the configured models and endpoint before a person asks a question. Every step is `[live-verify]`; nothing here can be
checked in local mode.

## The check (G4-087)

1. **Dated captures of Google's pages.** For each configured model, capture the Agent Platform pages that show:
   - the model is offered in `asia-southeast1`;
   - the terms the calls run under.
   Save each capture with its date as evidence in the Compliance Center. The page addresses are the ones the AI
   connection's data-handling entry cites `[verify]`.
2. **A synthetic call to `asia-southeast1` for each configured model.** In Settings › AI › Providers, run the probe on
   the Singapore connection's model (synthetic text only, AI-80). Then qualify it for the chat agent, which runs the
   chat agent's evals there (AI-78). Each must succeed; a failure stops the go-live.
3. **The chat agent's route from preset A.** In Settings › AI › Routes, set the chat agent's route from preset A
   (AI-26). The route must:
   - name the Singapore connection only;
   - pin the newest model of the main line that passed its evals;
   - carry personal data and payroll.
   A second person approves the change.
4. **The global endpoint is blocked.** From the production project, a call to Agent Platform's global endpoint must
   be refused. The organisation's location policy (`gcp.resourceLocations`, `in:asia-southeast1-locations`, from
   `infra/gcp/modules/org-policy`) keeps resources in Singapore. `[verify]` whether it also refuses inference on the
   global endpoint; if it does not, record what does before go-live.
5. **The data inventory.** Compliance › Data inventory shows the flow `ai-models`: model calls only, in Singapore, not
   crossing a border, from the class `ai-conversations` (`compliance/data-inventory.yaml`). A route outside Singapore
   would need a transfer assessment first (CMP-PD-06).
6. **The release gate.** The release workflow's `FYNIQ_EVALS_RELEASE=1 make evals` passed on the release: every case of
   the assistant's golden and red-team suites recorded and right (QA-AI-01, QA-AI-03).

Record the outcome of each step, with its evidence, in the release record (the pull request that releases P1).
