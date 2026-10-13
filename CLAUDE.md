# Fyniq: Claude Code notes

The shared coding instructions for every agent and contributor are in `AGENTS.md`, imported here:

@AGENTS.md

## Claude Code specifics

- **Use the skills.** Before you change something, load the matching `fyniq-*` skill (table in
  AGENTS.md). Load `fyniq-design-system` for any UI work. It explains when and how to consult
  **uipro** (`ui-ux-pro-max`): `python3 .claude/skills/ui-ux-pro-max/scripts/search.py "<query>"
  --domain ux|style|color|typography|chart|icons` or `--stack shadcn|react`.
- **Few subagents.** Use the Agent and Workflow tools only for a self-contained task that ends in
  under about five minutes; do long work in the main thread, even when it could run in parallel
  ("Working as an AI agent" in AGENTS.md).
- **Before you finish a task:** run `make check` (and from P0 `make test`). Write the commit as a
  Conventional Commit. Say what you did not verify.
- **PRD edits:** add new IDs and never renumber; tag changes `(rev 0.N)`; add a row to the
  revision history; mark superseded decisions instead of deleting them (`fyniq-release` skill).
- **Changed the PRD, or adding a feature?** Load `fyniq-openspec`: update the affected specs in
  `openspec/specs/` (then `python3 scripts/prdspec.py stamp …`) and the screens in
  `docs/design/screens/` in the same change. Plan and build with the `/opsx` workflows.
- **Changed a knowledge source** (anything in `docs/user-guide/`)? Load `fyniq-knowledge-base`,
  recompile the affected pages in `kb/pages/`, then `make kb-index` and `make kb-lint`.
- **When unsure about a statutory rule, a vendor capability or a spec revision**, keep the
  `[verify]` tag and say so. Do not state it as fact.
- **The MCP server and AI tools are security boundaries.** Changes there go through
  `fyniq-mcp-server` / `fyniq-ai-proposals` and the red-team suites. Never "temporarily" widen a scope
  or a grant.
