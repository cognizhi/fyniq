# Handoff, 21 Oct 2026: moving to a new laptop

Development moves to a new laptop. This note is for the next session, human or agent, which will
not have the old session's transcript or its local notes. Read it with `AGENTS.md`, then
`openspec/changes/p1-general-ledger-close/tasks.md`.

## Where things stand

- **Wave 2** is building `p1-general-ledger-close` (the founders said "proceed as recommended").
  #94 to #123 are merged; the PRD is at **revision 0.63**.
- **#123** (merged): confirming the accounting framework and approving a policy ask for a passkey
  or authenticator app while the setting `accounting.signing_step_up` is on (the default; turning
  it off needs a second approver, FM-1).
- **#124** (open, every check green): the accounting framework and policies get a Settings page of
  their own in the screen designs (`docs/design/screens/src/AccountingFramework.html`), and the
  Settings canvas was republished. It also edits the review register,
  `docs/prd/reviews/review-status.json`. The auto-approve workflow leaves any change under
  `docs/prd/reviews/` for a person, so #124 waits for the owner:
  `gh pr merge 124 --squash --admin`. Until it merges, main is still consistent: the inventory lists
  FR-ORG-10 under Organisation settings, and `apps/web/src/screens/settings/Accounting.tsx` names
  `screen="organisation-settings"` (#124 changes it to `accounting-framework`).
- Nothing else is unpushed. The worktree `../fyniq-wt-journals` holds #124's branch.

## Founder decisions already in the PRD

These were asked and answered. They are recorded, so don't ask them again:

- **Revision 0.62, FR-ORG-10:** the Owner, the Controller or the Accountant records the framework
  and each policy. Another member who holds the Accountant role confirms or approves each
  version, never its writer. It has its own Appendix D row.
- **Revision 0.63:**
  - The accountant's signature asks for step-up while a setting is on. The setting is on by
    default and switches off under FM-1.
  - The screen designs were updated.
  - **FR-GL-14** compares only income and expense accounts. A variance needs an explanation when
    it is **both** more than S$1,000 **and** more than 10% of the previous month's total.
  - Balance-sheet accounts rely on the support the close already requires. Budget variances wait
    for budgets.

For anything else, take the recommended option, record it as provisional in the next PRD revision,
say so in the pull request, and keep building. A decision that only the founders can make, such as
a grant, a control or an accounting policy, goes to them as a question. List every good option
with its pros and cons, and put the recommended one first with its reasons.

## Next: flux analysis (FR-GL-14, task 6.3)

This is the plan that was ready to start. Build it as one API pull request, then one web pull
request.

1. Branch from `origin/main`.
2. **Figures** come only from the balance package (ENG-04):
   - Use `balance.Trial(ctx, u, base, periodIDs...)` for the period and for the previous one. The
     previous period is the one that ends the day before the period's `StartsOn`.
   - With no previous period, the check passes with "No earlier month to compare".
   - Use the same sign convention as the general ledger report and the trial balance.
3. **Settings:** add `close.variance_amount` and `close.variance_percent` to
   `internal/domain/settings/settings.go`.
   - The amount is `AmountSGD`, recommended `"1000.00"`; the percent is `Percent`, recommended
     `"10"`.
   - Both have `DualControl: true` and `Requirement: "FR-GL-14"`.
   - Keys stay sorted, and each needs a `## heading` section in
     `docs/user-guide/organisation-settings.md`.
   - Check how `apps/web/src/screens/settings/Organisation.tsx` groups them.
4. **The threshold rule** is exact decimals, no floats:
   - A variance needs an explanation when it is above the amount **and** above the percent of the
     previous month's total.
   - When the previous month is zero, the amount alone decides.
   - Any percent shown goes through a named rounding policy and travels as a string.
   - Test the boundaries in a table.
5. **Explanations:** use migration `000066`, an append-only table that stores the amounts it
   explained.
   - An explanation goes stale when those amounts change (a provisional rule; record it in the
     PRD).
   - The database refuses a new explanation in a closed period, with a new FY code. FY162 looks
     free, but grep `db/migrations` first.
   - The table needs `UNIQUE (organisation_id, id)`.
6. **The close check:** add key `variances` in `internal/domain/closing` (`closing.run`) with
   severity `Failed` (`"error"`) while a variance has no current explanation.
   - Add Appendix C.2 row **#57** (the last is #56).
   - Add the scenario to `openspec/specs/controls/platform-checks/spec.md`.
   - Run `python3 scripts/prdspec.py stamp …` and `make roadmap`.
7. **API:**
   - `GET /v1/periods/{id}/variances` (cursor-paged; Spectral requires `cursor`, `page_size` and
     `next_cursor`).
   - `POST /v1/periods/{id}/variances/{account_id}/explanation` (takes `Idempotency-Key` and the
     preview).
   - Rights: View and Create on the Appendix D "Period close / reopen" row.
   - Run `make openapi`.
8. **Web:** add a Variances section to `apps/web/src/screens/accounting/PeriodClose.tsx`.
   - It is Expert, with a registry action and a help key. Explanations are written by the members
     who run the checks (Accountant, Controller).
   - Update `docs/user-guide/periods-and-close.md`, then recompile `kb/pages/task/accounting-periods.md`.
   - The design already lists FR-GL-14 on the `period-close` screen; check that the fragment shows
     the section.
9. **Tests** (`// req: FR-GL-14`):
   - the domain threshold table;
   - the API flow;
   - database attack tests (closed period, append-only);
   - authorisation-matrix rows;
   - a web test.
10. **Records:**
    - PRD revision **0.65** (0.64 is taken) for the built details and the provisional staleness rule.
    - In `tasks.md` 6.3, note that the AI-drafted explanation and budget variances are out of
      scope.

## Also left in the change

- **3.1:** changing posted opening balances (FM-1, as an edit is).
- **7.2:** the locked-period banner, the checklist template and reconciliations.
- **5.1:** the other automated postings (depreciation, FX revaluation, schedules).
- **6.4 and 7.4:** schedules, with `p1-purchases` bills.
- **8.1:** the oracle's shapes. These belong to the oracle session, written from the rules; never
  read or edit `oracle/` from a build session.
- **9.1 and 9.2:** the finish.

## Next free identifiers (check again before use)

| What | Next |
|---|---|
| Migration | `000066` |
| FY error code | FY162 (FY059 to FY061 are taken; FY160 and FY161 are the accounting guard) |
| Appendix C.2 check | #57 |
| PRD-001 revision | 0.65 (tag `(rev 0.65)`, history row, header Revision line); 0.64 went to the image platforms (NFR-IMG-08) |

## Lessons that cost reruns

- **Contract test and Spectral:**
  - The contract test needs an example on every leaf, booleans included; a `[]string` field
    cannot carry one.
  - Spectral needs cursor paging on every list operation.
- **Database:**
  - Every tenant table needs `UNIQUE (organisation_id, id)`. Version tables get their own id plus
    `UNIQUE (parent, version)`.
  - squawk refuses a non-concurrent unique index on an existing table. For a one-per-organisation
    rule, use a new table keyed by the organisation and add it to `onePerOrganisation` in the
    catalogue test.
  - jsonb rewrites spacing and key order, so hash JSON over its canonical form.
- **Tests:**
  - Test principals are `ViaSession` with `StepUpAt` an hour ago and a unique subject per token.
    A step-up path sets `StepUpAt` to the clock's now.
  - In Founder mode, a test that reverses a journal switches to Standard mode first (`standardMode(e)`).
  - A test tagged with a SEC-* requirement needs an `// asvs:` ID.
- **Approvals:** a new approval object type also needs the CHECK widened (NOT VALID, then validate),
  `authz.ApprovalRows`, the response enums, the web labels in `apps/web/src/screens/approvals/words.tsx` and the policy count
  test.
- **API (Huma):** query arrays are comma-separated unless the tag says `query:"name,explode"`.
- **Pull requests and merging:**
  - A stacked pull request whose base branch is deleted on merge can be closed rather than
    retargeted. Check the dependent PR after its base merges.
  - Two PRs that both append to `kb/log.md` conflict. Rebase the second, keep both entries, and
    rerun `make kb-index` and `make kb-lint`.
  - `gh pr merge N --auto --squash` lets a green PR merge by itself.
  - The "Local mode timings" CI job flakes now and then; re-run it once the workflow finishes.
- **Shell:** `pkill -f` or `pgrep -f` with a pattern in the shell's own command line kills that
  shell. Find processes by port with `ss -ltnp`.
- **Web:** `apps/web` has no Prettier or ESLint config, so don't run them. Browser checks mock
  writes rather than writing to the local database.
- **Local data:** the local sample company (`make seed-load`) has the 2026 periods and posted
  journals but no books start. The accounting and period pages therefore answer PERIOD-01 there.

## Setting up the new laptop

- `make setup` installs the pinned toolchain from `mise.toml` (Go, sqlc, squawk, oasdiff, gosec and
  the rest), the web dependencies and the commit hook. Then run `make up`, `make setup-code` and
  `make seed-load`, as AGENTS.md describes.
- If PostgreSQL already uses host port 5432 there, the stack's 55432 still applies.
- The new laptop is Apple silicon with Colima. Its Homebrew Docker CLI needs the buildx and compose
  plugins (`brew install docker-buildx docker-compose` and `cliPluginsExtraDirs` in
  `~/.docker/config.json`; README). Every image builds and runs natively as `linux/arm64` (NFR-IMG-08).
- If the machine's Go updates itself past the pinned version, set `GOTOOLCHAIN` to the pinned one
  before `make scan`.
- `make test` needs Docker running (Testcontainers).
- Before finishing a change, run `make check` and `make test`, and say which gates were not run.
- Commit with Conventional Commits under 100 characters, as squash-merged pull requests.
- Recompile `kb/` in the same commit as any change to `docs/user-guide/`.
- **Design canvases:** the URLs are in `docs/design/screens/canvases.json`. To republish one:
  1. Read its `project/canvas.json` and file list first.
  2. Build with `build.py canvas <area> --ds <design system URL>`.
  3. Publish from a folder under the session's scratchpad.
