# AI evals

The AI golden, proposal and red-team suites (PRD-001 §16.2, QA-AI-*) live here, one directory per
suite, run by `make evals` in replay mode by default (recorded cassettes; no model call and no
credential, DEV-02). `FYNIQ_AI_LIVE=1` runs them live on synthetic data only. The suites arrive in
P1 with the AI service.
