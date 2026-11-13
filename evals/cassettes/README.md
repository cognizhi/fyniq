# Replay cassettes

Recorded model answers for local mode and the evals' replay runs (PRD-001 §12.8, "Local AI in replay and live mode";
p1-ai-providers design D12). Each file is `<key>.json`, where the key is the SHA-256 of the request's canonical JSON:
the model, the system prompt, the conversation, the tools, the output schema and the limits. `models.json` is a
recorded model list.

- **Replay** (the default) answers a request from its cassette; a request with none fails, naming its key.
- **Live** (`FYNIQ_AI_LIVE=1`) calls a provider with a developer's key, on the synthetic sample company only (DEV-08),
  and records each answer here. CI never calls a provider. With `FYNIQ_AI_AGENT=claude` or `codex` it runs the
  developer's own command-line agent instead (DEV-09, local build only), which serves text only: a request with tools,
  a strict schema, images or PDFs needs a key.

Never record anything from real organisation data.
