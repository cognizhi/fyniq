# Cognizhi Finance Hub Cloud: Claude Code notes

The shared coding instructions for every agent and contributor are in `AGENTS.md`, imported here:

@AGENTS.md

## Claude Code specifics

- **Use the skills.** Before you change something, load the matching `fh-*` skill (table in
  AGENTS.md). Load `fh-design-system` for any UI work. It explains when and how to consult
  **uipro** (`ui-ux-pro-max`): `python3 .claude/skills/ui-ux-pro-max/scripts/search.py "<query>"
  --domain ux|style|color|typography|chart|icons` or `--stack shadcn|react`.
- **Before you finish a task:** run `make check` (and from P0 `make test`). Write the commit as a
  Conventional Commit. Say what you did not verify.
- **PRD edits:** add new IDs and never renumber; tag changes `(rev 0.N)`; add a row to the
  revision history; mark superseded decisions instead of deleting them (`fh-release` skill).
- **When unsure about a statutory rule, a vendor capability or a spec revision**, keep the
  `[verify]` tag and say so. Do not state it as fact.
- **The MCP server and AI tools are security boundaries.** Changes there go through
  `fh-mcp-server` / `fh-ai-proposals` and the red-team suites. Never "temporarily" widen a scope
  or a grant.
