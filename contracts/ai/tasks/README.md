# The proposal catalogue

One `<id>.json` per task type the AI may propose (PRD-001 §9.6, Appendix E; `p1-ai-proposals-core` design D1): its ID
and version, its name, the description the model and the approver read, a strict input JSON Schema, the domain command
it runs, its risk tier, its limits, the roles that approve it and its expiry. `internal/proposal` refuses a definition
that is not strict and bounded, a tier it does not allow, and anything on the never-list, whatever the file says.

A changed definition is a new version, pinned in `../tasks.lock.json`, reviewed with its evals (CMP-AI-03). Each
organisation turns a task type on with the setting `ai.task_types`, under dual control; every type starts off (AI-32).
Each task type is added by the change that builds what it acts on.
