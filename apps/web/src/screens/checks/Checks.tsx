import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChevronDown, ChevronRight, Info, RefreshCw, UserRoundPen } from "lucide-react";
import { Fragment, useId, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { roleLabel } from "../approvals/words";
import { itemLink, needsAction, readChecks, scores, statuses, type Check, type Checks as ChecksOut } from "./data";

type Member = components["schemas"]["Member"];

// Checks is Books health and checks (docs/design/screens Checks; FR-CTL-01, FR-CTL-02, FR-CTL-05, FR-UX-09; p1-checks
// task 1.7, design D9): books health with its counts, each area's checks with their status chip (icon and words), what
// they found in plain words, their items with links (FR-UX-04), their owner and when they ran; the nightly ledger
// verification beside them; and Run now. Basic and Expert show the same statuses; Expert adds each check's Appendix C
// row and rule IDs. What a person sees, and whose items open, the API decides (design D7).
export function Checks() {
  const qc = useQueryClient();
  const checks = useQuery({
    queryKey: ["checks"],
    queryFn: readChecks,
    retry: false,
    refetchInterval: (q) => (q.state.data?.run_requested ? 3000 : false),
  });
  const [busy, setBusy] = useState(false);
  const [said, setSaid] = useState("");
  const [refused, setRefused] = useState<ApiProblem | null>(null);
  const run = helpText("checks.run");
  const c = checks.data;

  const runNow = async () => {
    setBusy(true);
    setRefused(null);
    const { data, error } = await api.POST("/v1/checks/runs");
    setBusy(false);
    if (error) {
      setRefused(problemOf(error));
      return;
    }
    setSaid(data.already_waiting ? "A run asked for earlier is still waiting: the checks update once it has run." : "Every check is running now. This page updates once they have run.");
    await qc.invalidateQueries({ queryKey: ["checks"] });
  };

  return (
    <div className="fy-page fy-enter">
      <nav className="fy-breadcrumb" aria-label="Breadcrumb">
        <Link to="/">Home</Link>
        <Icon of={ChevronRight} />
        <span>Books health</span>
      </nav>
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">Books health and checks</h1>
          <p className="fy-secondary">{c ? intro(c) : "The checks run when something they read changes, each night, and when you ask."}</p>
        </div>
        {c && (
          <div className="fy-pagehead__actions">
            <Button variant="secondary" onClick={() => void runNow()} disabled={busy || c.run_requested} title={run.tooltip}>
              <Icon of={RefreshCw} />
              {c.run_requested ? "Running…" : run.label}
            </Button>
          </div>
        )}
      </div>
      <p className="fy-sr" role="status">
        {said}
      </p>
      {refused && <Refusal problem={refused} title="The checks did not run" />}
      {checks.error && <Refusal problem={problemOf(checks.error)} title="The checks could not be read" />}
      {checks.isPending && <div className="fy-skeleton h-40" aria-hidden="true" />}
      {c && (
        <>
          <Summary c={c} />
          <div className="fy-split">
            <Areas c={c} />
            <Nightly c={c} />
          </div>
        </>
      )}
    </div>
  );
}

function intro(c: ChecksOut): string {
  const n = c.checks.length;
  const blocking = c.health.problems;
  const ran = c.last_run ? ` They last ran ${dateTime(c.last_run.finished_at)}.` : " They have not run yet.";
  return `${n} check${n === 1 ? "" : "s"} run when something they read changes, each night and when you ask.${ran}${blocking > 0 ? ` ${blocking} stop${blocking === 1 ? "s" : ""} the close.` : ""}`;
}

// Summary is books health (FR-CTL-02): the score and the count of each status, the checks not assessed beside it.
function Summary({ c }: { c: ChecksOut }) {
  const h = c.health;
  const score = scores[h.score];
  const cell = (status: keyof typeof statuses, count: number, meta: string) => {
    const s = statuses[status];
    return (
      <div className="fy-stack fy-stack--2 bg-surface-1 p-4" data-status={status}>
        <span className={`fy-chip ${s.tone} self-start`}>
          <Icon of={s.icon} />
          {s.label}
        </span>
        <span className="fy-tile__value fy-num">{count}</span>
        <span className="fy-small fy-muted">{meta}</span>
      </div>
    );
  };
  return (
    <section className="fy-card" aria-labelledby="health-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="health-title">
          Books health: {score.label.toLowerCase()}
        </h2>
        <span className={`fy-chip ${score.tone}`}>
          <Icon of={score.icon} />
          {score.label}
        </span>
      </div>
      <div className="fy-grid fy-grid--tiles gap-px overflow-hidden rounded-b-[inherit] bg-border">
        {cell("error", h.problems, "stop the close, or could not run")}
        {cell("warning", h.warnings, "post, but need a look")}
        {cell("due", h.due, "deadlines due or past")}
        {cell("ok", h.ok, `of ${c.checks.length} checks`)}
      </div>
      {h.not_assessed > 0 && (
        <div className="fy-card__foot fy-small fy-secondary">
          <Icon of={Info} />
          {h.not_assessed} check{h.not_assessed === 1 ? " is" : "s are"} not assessed: each says what it needs. They do not change the score.
        </div>
      )}
    </section>
  );
}

// Areas are the checks by area, in the catalogue's order: those that found something, then a toggle for the rest.
function Areas({ c }: { c: ChecksOut }) {
  const [only, setOnly] = useState(false);
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const [owning, setOwning] = useState<Check | null>(null);
  const id = useId();
  const areas: string[] = [];
  for (const x of c.checks) if (!areas.includes(x.area)) areas.push(x.area);
  return (
    <section className="fy-card" aria-labelledby={`${id}-title`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-title`}>
          Checks by area
        </h2>
        <div className="fy-segmented" role="radiogroup" aria-label="Show">
          <button type="button" role="radio" aria-checked={!only} onClick={() => setOnly(false)}>
            All
          </button>
          <button type="button" role="radio" aria-checked={only} onClick={() => setOnly(true)}>
            Needs action
          </button>
        </div>
      </div>
      {c.checks.length === 0 ? (
        <p className="fy-card__body fy-secondary">None of the checks so far are of your area. They join as the modules they guard are built.</p>
      ) : (
        <table className="fy-table fy-table--cards">
          <caption className="fy-sr">Each check, by area, with its status, owner and last run</caption>
          <thead>
            <tr>
              <th scope="col">Check</th>
              <th scope="col">Status</th>
              <th scope="col">Owner</th>
              <th scope="col">Last run</th>
            </tr>
          </thead>
          <tbody>
            {areas.map((area) => {
              const all = c.checks.filter((x) => x.area === area);
              const acting = all.filter(needsAction);
              const fine = all.filter((x) => !needsAction(x));
              const showFine = !only && (acting.length === 0 || open[area]);
              if (only && acting.length === 0) return null;
              return (
                <Fragment key={area}>
                  <tr className="fy-group">
                    <td data-card="title" colSpan={4}>
                      {area} · {summaryOf(all)}
                    </td>
                  </tr>
                  {acting.map((x) => (
                    <Row key={x.key} c={x} canName={c.may_name_owners} onName={() => setOwning(x)} />
                  ))}
                  {showFine && fine.map((x) => <Row key={x.key} c={x} canName={c.may_name_owners} onName={() => setOwning(x)} />)}
                  {!only && acting.length > 0 && fine.length > 0 && (
                    <tr>
                      <td data-card="title" colSpan={4}>
                        <Button variant="ghost" size="sm" aria-expanded={!!open[area]} onClick={() => setOpen({ ...open, [area]: !open[area] })}>
                          <Icon of={ChevronDown} />
                          {open[area] ? `Hide the OK checks in ${area}` : `Show ${fine.length} more OK in ${area}`}
                        </Button>
                      </td>
                    </tr>
                  )}
                </Fragment>
              );
            })}
          </tbody>
        </table>
      )}
      <div className="fy-card__foot fy-small fy-muted">
        <Icon of={Info} />
        Blocks the close stops the month closing until it is fixed. To review posts, but stays here until someone looks at it.
      </div>
      {owning && <OwnerDialog c={owning} onClose={() => setOwning(null)} />}
    </section>
  );
}

function summaryOf(all: Check[]): string {
  const counts = new Map<string, number>();
  for (const x of all) counts.set(statuses[x.status].label, (counts.get(statuses[x.status].label) ?? 0) + 1);
  return [...counts].map(([label, n]) => `${n} ${label === "OK" ? "OK" : label.toLowerCase()}`).join(", ");
}

function ownerName(c: Check): string {
  return c.owner.person?.name || (c.owner.person ? "A former member" : `The ${roleLabel(c.owner.role).toLowerCase()}`);
}

// Row is one check: what it checks and found, its items, the fix, and in Expert its references.
function Row({ c, canName, onName }: { c: Check; canName: boolean; onName: () => void }) {
  const s = statuses[c.status];
  const more = c.count - c.items.length;
  return (
    <tr data-check={c.key}>
      <td data-card="title">
        <span className="fy-stack fy-stack--2 gap-0.5 py-2">
          <span>{c.title}</span>
          <span className={`fy-small font-normal ${c.status === "ok" ? "fy-muted" : "fy-secondary"}`}>{c.detail}</span>
          {c.status === "not_assessed" && c.needs !== "its first run" && (
            <span className="fy-small fy-secondary font-normal">
              It needs {c.needs}
              {c.setting ? `, set in ${c.setting}` : ""}.
            </span>
          )}
          {c.items.length > 0 && (
            <ul className="fy-stack fy-stack--1 fy-small font-normal" aria-label={`What ${c.title.toLowerCase()} found`}>
              {c.items.map((it, i) => {
                const to = itemLink(it);
                return (
                  <li key={`${it.kind}-${it.id ?? i}`}>
                    {to ? (
                      <Link to={to.to} params={to.params}>
                        {it.label}
                      </Link>
                    ) : (
                      <span className="fy-strong">{it.label}</span>
                    )}
                    {it.detail ? `: ${it.detail}` : ""}
                  </li>
                );
              })}
              {more > 0 && <li className="fy-muted">and {more} more</li>}
            </ul>
          )}
          {c.items_hidden && (
            <span className="fy-small fy-secondary font-normal">
              {c.count} item{c.count === 1 ? "" : "s"} in {c.items_area}: your roles do not open them.
            </span>
          )}
          {c.fix && c.status !== "ok" && <span className="fy-small fy-secondary font-normal">To fix it: {c.fix}.</span>}
          <span className="fy-small fy-mono fy-muted fy-expert-only font-normal">
            {c.ref} · {c.rules}
          </span>
        </span>
      </td>
      <td data-card="status">
        <span className={`fy-chip ${s.tone}`}>
          <Icon of={s.icon} />
          {s.label}
        </span>
      </td>
      <td data-card="meta" className="whitespace-nowrap">
        <span className="fy-row gap-1">
          {ownerName(c)}
          {canName && (
            <Button variant="ghost" size="icon" aria-label={`Name the owner of ${c.title.toLowerCase()}`} title={helpText("checks.owner").tooltip} onClick={onName}>
              <Icon of={UserRoundPen} />
            </Button>
          )}
        </span>
      </td>
      <td data-card="meta" className="whitespace-nowrap">
        {c.run_at ? dateTime(c.run_at) : "Not run yet"}
      </td>
    </tr>
  );
}

// Nightly is FR-CTL-05 on Checks: the checks of the nightly ledger verification, and when the nightly run last ran.
function Nightly({ c }: { c: ChecksOut }) {
  const group = c.checks.filter((x) => x.nightly);
  if (group.length === 0) return null;
  const failed = group.some((x) => x.status === "error" || x.status === "failed");
  const waiting = group.some((x) => x.status === "not_assessed");
  const chip = failed ? statuses.error : waiting ? statuses.not_assessed : statuses.ok;
  return (
    <aside aria-label="Ledger verification">
      <section className="fy-card" aria-labelledby="nightly-title">
        <div className="fy-card__head">
          <h2 className="fy-h3" id="nightly-title">
            Nightly ledger verification
          </h2>
          <span className={`fy-chip ${chip.tone}`}>
            <Icon of={chip.icon} />
            {failed ? "Failed" : waiting ? "Waiting" : "Passed"}
          </span>
        </div>
        <div className="fy-card__body fy-stack fy-stack--3">
          <p className="fy-small fy-secondary">
            {c.last_nightly_run
              ? `Last night's run finished ${dateTime(c.last_nightly_run.finished_at)}: Fyniq worked the books out again from every entry and compared them with what you see.`
              : "The nightly run has not run yet: each night at 04:15 Fyniq works the books out again from every entry and compares them with what you see."}
          </p>
          <ul className="fy-stack fy-stack--3">
            {group.map((x) => {
              const s = statuses[x.status];
              return (
                <li key={x.key} className="fy-row fy-row--top fy-small gap-2">
                  <span className={`fy-chip ${s.tone} shrink-0`}>
                    <Icon of={s.icon} />
                    <span className="fy-sr">{s.label}</span>
                  </span>
                  <span>
                    <span className="fy-strong">{x.title}</span>: {x.detail}
                  </span>
                </li>
              );
            })}
          </ul>
        </div>
      </section>
    </aside>
  );
}

// OwnerDialog names a person as a check's owner, or gives it back to its role (FR-CTL-01, design D8): the members whose
// roles open its items, as the API decides (CHECK-02 otherwise), with If-Match on the owner's version.
function OwnerDialog({ c, onClose }: { c: Check; onClose: () => void }) {
  const qc = useQueryClient();
  const id = useId();
  const h = helpText("checks.owner");
  const members = useQuery({
    queryKey: ["members", "check-owner"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/members", { params: { query: { page_size: 500 } } });
      if (error) throw error;
      return data.items as Member[];
    },
    retry: false,
  });
  const [person, setPerson] = useState(c.owner.person?.id ?? "");
  const [busy, setBusy] = useState(false);
  const [refused, setRefused] = useState<ApiProblem | null>(null);
  const save = async () => {
    setBusy(true);
    setRefused(null);
    const { error } = await api.PUT("/v1/checks/{key}/owner", {
      params: { path: { key: c.key }, header: { "If-Match": `"${c.owner.version}"` } },
      body: { person_user_id: person || null },
    });
    setBusy(false);
    if (error) {
      setRefused(problemOf(error));
      return;
    }
    await qc.invalidateQueries({ queryKey: ["checks"] });
    onClose();
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title={`Who owns "${c.title}"?`}
      description={`Its owner looks into what it finds. Without a person named, it is the ${roleLabel(c.owner.role).toLowerCase()}'s.`}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => void save()} disabled={busy || members.isPending} title={h.tooltip}>
            Save
          </Button>
        </>
      }
    >
      {refused && <Refusal problem={refused} title="The owner was not changed" />}
      {members.error && <Refusal problem={problemOf(members.error)} title="The members could not be read" />}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-person`}>
          Owner
        </label>
        <select className="fy-select" id={`${id}-person`} value={person} onChange={(e) => setPerson(e.target.value)} aria-describedby={`${id}-hint`}>
          <option value="">The {roleLabel(c.owner.role).toLowerCase()} (the catalogue's choice)</option>
          {(members.data ?? []).map((m) => (
            <option key={m.user_id} value={m.user_id}>
              {m.display_name}
            </option>
          ))}
        </select>
        <span className="fy-help" id={`${id}-hint`}>
          Only someone whose roles open the check's items can own it.
        </span>
      </div>
    </Dialog>
  );
}
