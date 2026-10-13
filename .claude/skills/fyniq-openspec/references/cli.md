# OpenSpec CLI 1.14.1: every command

Installed globally (`npm i -g @fission-ai/openspec@1.14.1`); CI uses
`npx -y @fission-ai/openspec@1.14.1`. Set `OPENSPEC_TELEMETRY=0` in scripts and CI. Most commands
take `--json` (for agents) and `--store <id>` (a registered standalone store instead of this repo;
Fyniq does not use stores). Item names: a change's directory name, or a spec's path under
`openspec/specs/` (`organisation/settings`). When a change and a spec share a name, pass
`--type change|spec`.

## Read

| Command | Does | Useful flags |
|---|---|---|
| `openspec list` | Active changes | `--specs` (the capability inventory), `--archived`, `--all`, `--sort recent\|name`, `--json` |
| `openspec show <item>` | A change or spec, as markdown | `--json`, `--type`, `--no-interactive`; spec JSON: `--requirements` (no scenarios), `--no-scenarios`, `-r <n>` (one requirement, 1-based); change JSON: `--deltas-only`, `--diff` (per-requirement diff of each delta) |
| `openspec view` | Interactive dashboard of specs and changes (a terminal UI; not for agents) | |
| `openspec status --change <id>` | Which artifacts of a change exist and which are blocked | `--all`, `--schema`, `--json` |
| `openspec instructions <artifact> --change <id>` | The enriched instruction for writing `proposal`, `specs`, `design` or `tasks` (or `apply`, `archive`), with `planningHome.root` and output paths | `--json`, `--schema` |
| `openspec templates` | Paths of the templates of a schema | `--schema`, `--json` |
| `openspec schemas` | Available workflow schemas (`spec-driven` is the default: proposal → specs → design → tasks) | `--json` |
| `openspec context` | The working context (resolved OpenSpec root, store, worksets) as an agent brief | `--json`, `--code-workspace <path>` |
| `openspec doctor` | Relationship health of the resolved root | `--json` |
| `openspec version` | Installed version | `--check` (registry), `--json` |

## Validate

| Command | Does |
|---|---|
| `openspec validate --specs --strict` | Every main spec; strict fails on warnings (description > 500 characters, no SHALL/MUST, Purpose < 50 characters) |
| `openspec validate <item> --strict` | One change or spec |
| `openspec validate --changes` / `--all` | Every change / everything |
| `openspec validate --archived` | Archived changes have every task ticked (pre-commit lint) |
| Flags | `--json`, `--report full\|findings`, `--concurrency <n>`, `--no-interactive`, `--type` |

What the validator checks: a `## Purpose` and `## Requirements` section in main specs; each
`### Requirement:` has at least one `#### Scenario:` (exactly four `#`; error); the description
(every non-blank line from the header to the first scenario, except `**Key**:` lines) contains
uppercase SHALL or MUST and is at most 500 characters (warnings, fatal with `--strict`); deltas
use `## ADDED|MODIFIED|REMOVED|RENAMED Requirements`; REMOVED carries `**Reason**` and
`**Migration**`; a change has at least one delta unless its `.openspec.yaml` says
`skip_specs: true`.

## Create and change

| Command | Does |
|---|---|
| `openspec new change <name>` | Creates `openspec/changes/<name>/` with `.openspec.yaml` and README; `--description`, `--goal`, `--schema`, `--json` |
| `openspec archive <change>` | Applies the deltas to main specs and moves the change to `changes/archive/YYYY-MM-DD-<name>/`; `-y` (no prompts), `--skip-specs` (no spec update: build or tooling changes), `--no-validate` (not recommended), `--json` |
| `openspec change show\|validate <name>` | Older noun-first forms of `show`/`validate` (`change list` is deprecated; use `openspec list`) |
| `openspec spec show\|list\|validate <id>` | Older noun-first forms for specs (deprecated warnings; prefer `show`/`list --specs`/`validate --specs`) |

## Set up and configure

| Command | Does |
|---|---|
| `openspec init --tools claude --no-animation .` | Creates `openspec/` and the agent skills and commands for the chosen tools (`--tools all\|none\|claude,codex,cursor,github-copilot,…`; `--profile core\|custom`; `--language`; `--force` cleans legacy files) |
| `openspec update [--force]` | Regenerates the skills and commands after a CLI upgrade or a profile change |
| `openspec config list` / `get <key>` / `set <key> <value>` / `unset <key>` / `path` / `edit` / `reset --all -y` | The **global** configuration (`~/.config/openspec/config.json`): `profile` (`core` or `custom`), `workflows` (the list), `delivery` (`skills`, `commands` or `both`), `telemetry.enabled`, `featureFlags` |
| `openspec config profile [core]` | Interactive workflow picker; `core` resets to the six core workflows |
| `openspec schema init <name>` / `fork <source> [name]` / `validate [name]` / `which [name]` | Project-local workflow schemas (experimental): `--artifacts proposal,specs,design,tasks`, `--default` |
| `openspec store setup\|register\|unregister\|remove\|list\|doctor` | Standalone OpenSpec repositories registered on this machine (not used here) |
| `openspec workset create\|list\|open\|remove` | Personal, local working views over several folders (not used here) |
| `openspec completion generate\|install\|uninstall [shell]` | Shell completion |
| `openspec feedback <message>` | Sends feedback to the OpenSpec project (outward: ask before using) |

Fyniq's profile (13 Oct 2026): `profile: custom`, `delivery: both`, all twelve workflows. To check:
`openspec config list`. To restore after a reset:

```bash
openspec config set profile custom
openspec config set workflows '["propose","explore","new","continue","apply","update","ff","sync","archive","bulk-archive","verify","onboard"]'
openspec update
```

## The twelve OPSX workflows

| Command | Skill | Use for | Writes |
|---|---|---|---|
| `/opsx:explore` | `openspec-explore` | Thinking partner: investigate, compare, clarify before committing | Nothing (unless asked) |
| `/opsx:propose` | `openspec-propose` | A change with all artifacts in one step | proposal, specs, design, tasks |
| `/opsx:new` | `openspec-new-change` | Start a change, then stop for review | the change directory |
| `/opsx:continue` | `openspec-continue-change` | The next missing artifact of a change | one artifact |
| `/opsx:ff` | `openspec-ff-change` | Fast-forward: every remaining artifact | the rest |
| `/opsx:update` | `openspec-update-change` | Revise artifacts after a decision; keep them coherent | the affected artifacts |
| `/opsx:apply` | `openspec-apply-change` | Implement the tasks, ticking them off | code, tests, `tasks.md` |
| `/opsx:verify` | `openspec-verify-change` | Completeness, correctness and coherence before archive | a report |
| `/opsx:sync` | `openspec-sync-specs` | Merge delta specs into main specs, keep the change open | main specs |
| `/opsx:archive` | `openspec-archive-change` | Merge deltas and archive a finished change | main specs, archive |
| `/opsx:bulk-archive` | `openspec-bulk-archive-change` | Archive several finished changes, resolving overlaps | main specs, archive |
| `/opsx:onboard` | `openspec-onboard` | A guided first cycle on real work | a small change |

Planning workflows (`propose`, `new`, `continue`, `ff`, `update`) never edit code; implementation
starts only with `/opsx:apply` on a new request.

## Files

```
openspec/
  config.yaml                 schema, project context and per-artifact rules (read by every workflow)
  specs/<domain>/<cap>/spec.md main specs: "# <path> Specification", "## Purpose", "## Requirements"
  changes/<name>/             .openspec.yaml, proposal.md, specs/**/spec.md (deltas), design.md, tasks.md
  changes/archive/            archived changes, dated
  sources/<prd>.json          Fyniq: the PRD → capability map (prdspec.py)
  sources/<prd>.lock.json     Fyniq: hashes of the PRD rows the specs were converted from (stamp only)
```
