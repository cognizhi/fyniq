import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowLeft, ChevronRight, CircleCheck, CircleDashed, CircleX, ClockAlert, Info, Minus, ShieldCheck, type LucideIcon } from "lucide-react";
import { useId, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf } from "../../ui";
import { IncidentsTab } from "./Incidents";
import { HeartbeatDrillsTab } from "./HeartbeatDrills";
import { RestoreDrillsTab } from "./RestoreDrills";

type Control = components["schemas"]["Control"];
type Check = components["schemas"]["ControlCheck"];
type Status = Control["status"];

// The statuses of CMP-01, as icon, chip and words; colour never carries the meaning alone.
const statuses: Record<Status, { label: string; chip: string; icon: LucideIcon }> = {
  fail: { label: "Fail", chip: "fy-chip--critical", icon: CircleX },
  overdue: { label: "Overdue", chip: "fy-chip--serious", icon: ClockAlert },
  not_assessed: { label: "Not assessed", chip: "fy-chip--warning", icon: CircleDashed },
  pass: { label: "Pass", chip: "fy-chip--good", icon: CircleCheck },
  not_applicable: { label: "Not applicable", chip: "fy-chip--neutral", icon: Minus },
};

// Problems first: fail, overdue, not assessed, pass, not applicable; then the registry's order.
const rank: Record<Status, number> = { fail: 0, overdue: 1, not_assessed: 2, pass: 3, not_applicable: 4 };

// The roles, as the dashboard names a control's owner.
const roles: Record<Control["owner"], string> = {
  owner: "Owner",
  administrator: "Administrator",
  controller: "Controller",
  accountant: "Accountant",
  dpo: "DPO",
  auditor: "Auditor",
};

// The roles that may read the Compliance Center (Appendix D "Compliance Center": V or C/A).
const readers = ["owner", "administrator", "controller", "accountant", "auditor", "dpo"];

// What runs a check, in words.
const kinds: Record<Check["kind"], string> = {
  ci: "In CI",
  job: "Nightly job",
  check: "Check",
  verify: "Verification",
  attestation: "Review",
  cloud: "In the cloud",
  none: "Not built yet",
};

export function StatusChip({ status }: { status: Status }) {
  const s = statuses[status];
  return (
    <span className={`fy-chip ${s.chip}`}>
      <Icon of={s.icon} />
      {s.label}
    </span>
  );
}

async function controls() {
  const { data, error } = await api.GET("/v1/compliance/controls");
  if (error) throw error;
  return data;
}

// Controls is Compliance › Controls (docs/design/screens ComplianceCenter; CMP-01, FR-CTL-05; Expert): every control of
// the registry with its status for the organisation, problems first; the counts by status; a filter by framework; a
// control's checks with what each last said and its evidence; the nightly verifications; and each framework's tally. Its
// Incidents tab is the incident register (SEC-32); its Restore drills and Heartbeat drills tabs the drills' registers
// (DR-06, NFR-OBS-02).
export function Controls() {
  const id = useId();
  const h = helpText("compliance.controls");
  const list = useQuery({ queryKey: ["controls"], queryFn: controls });
  const [framework, setFramework] = useState("");
  const [status, setStatus] = useState<Status | "">("");
  const [open, setOpen] = useState<string | null>(null);
  const [tab, setTab] = useState<"controls" | "incidents" | "drills" | "heartbeat">("controls");
  const all = list.data?.items ?? [];
  const shown = all
    .filter((c) => (!framework || c.frameworks.includes(framework as Control["frameworks"][number])) && (!status || c.status === status))
    .sort((a, b) => rank[a.status] - rank[b.status]);
  const count = (s: Status) => all.filter((c) => c.status === s).length;
  const checked = all.map((c) => c.checked_at).filter((x): x is string => !!x).sort().at(-1);
  return (
    <div className="fy-page fy-enter">
      <nav className="fy-breadcrumb" aria-label="Breadcrumb">
        <Link to="/compliance">Compliance</Link>
        <Icon of={ChevronRight} />
        <span>Controls</span>
      </nav>
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">{h.label}</h1>
          <p className="fy-secondary">
            {h.tooltip}
            {list.data && ` ${all.length} controls${checked ? ` · worked out ${dateTime(checked)}` : ""}.`}
          </p>
        </div>
      </div>
      <div className="fy-tabs" role="tablist" aria-label="Controls, incidents and drills">
        <button type="button" role="tab" aria-selected={tab === "controls"} onClick={() => setTab("controls")}>
          Controls
        </button>
        <button type="button" role="tab" aria-selected={tab === "incidents"} onClick={() => setTab("incidents")} title={helpText("compliance.incidents").tooltip}>
          {helpText("compliance.incidents").label}
        </button>
        <button type="button" role="tab" aria-selected={tab === "drills"} onClick={() => setTab("drills")} title={helpText("compliance.restore-drills").tooltip}>
          {helpText("compliance.restore-drills").label}
        </button>
        <button type="button" role="tab" aria-selected={tab === "heartbeat"} onClick={() => setTab("heartbeat")} title={helpText("compliance.heartbeat-drills").tooltip}>
          {helpText("compliance.heartbeat-drills").label}
        </button>
      </div>
      {tab === "incidents" && <IncidentsTab />}
      {tab === "drills" && <RestoreDrillsTab />}
      {tab === "heartbeat" && <HeartbeatDrillsTab />}
      {tab === "controls" && list.isError && <Refusal problem={problemOf(list.error)} title="The controls could not be shown" />}
      {tab === "controls" && list.isPending && <p className="fy-small">Loading the controls…</p>}
      {tab === "controls" && list.data && !list.data.release && (
        <p className="fy-banner fy-banner--info" role="note">
          <Icon of={Info} />
          <span>A development build: the checks that run in CI passed there, but this build carries no release's evidence, so they are not assessed. A release shows them as passed, with links to its evidence.</span>
        </p>
      )}
      {tab === "controls" && list.data && (
        <>
          <div className="fy-grid fy-grid--tiles" role="group" aria-label="Controls by status">
            {(["fail", "overdue", "not_assessed", "pass"] as const).map((s) => (
              <button key={s} type="button" className="fy-tile text-left" aria-pressed={status === s} onClick={() => setStatus(status === s ? "" : s)}>
                <span className="fy-tile__label">
                  <Icon of={statuses[s].icon} />
                  {statuses[s].label}
                </span>
                <span className="fy-tile__value fy-num">{count(s)}</span>
              </button>
            ))}
          </div>
          <div className="fy-chipset" role="group" aria-label={helpText("controls.filter").label} title={helpText("controls.filter").tooltip}>
            <button className="fy-filter" type="button" aria-pressed={framework === ""} onClick={() => setFramework("")}>
              All frameworks
            </button>
            {list.data.frameworks
              .filter((f) => f.controls > 0)
              .map((f) => (
                <button key={f.key} className="fy-filter" type="button" aria-pressed={framework === f.key} onClick={() => setFramework(f.key)}>
                  {f.title}
                </button>
              ))}
          </div>
          <div className="fy-split">
            <section className="fy-card" aria-labelledby={`${id}-list`}>
              <div className="fy-card__head">
                <h2 className="fy-h3" id={`${id}-list`}>
                  Controls
                </h2>
                <span className="fy-small fy-muted">
                  {shown.length} of {all.length} · problems first
                </span>
              </div>
              {shown.length === 0 ? (
                <p className="fy-card__body fy-small fy-secondary">No control matches the filters.</p>
              ) : (
                <div className="fy-listdetail" data-show={open ? "detail" : "list"}>
                  <div className="fy-listdetail__list">
                    <ul className="fy-list" aria-labelledby={`${id}-list`}>
                      {shown.map((c) => (
                        <li key={c.id}>
                          <button type="button" className="fy-list__item w-full text-left" aria-current={open === c.id ? "true" : undefined} onClick={() => setOpen(c.id)}>
                            <span className="fy-list__main">
                              <span className="fy-list__title whitespace-normal">
                                <span className="fy-mono fy-muted">{c.id}</span> {c.control}
                              </span>
                              <span className="fy-list__meta whitespace-normal">
                                {c.status === "pass" ? c.mapping : c.detail} · {roles[c.owner]}
                              </span>
                            </span>
                            <StatusChip status={c.status} />
                          </button>
                        </li>
                      ))}
                    </ul>
                  </div>
                  <div className="fy-listdetail__detail">
                    {open ? (
                      <ControlDetail c={all.find((c) => c.id === open)} onBack={() => setOpen(null)} />
                    ) : (
                      <p className="fy-card__body fy-small fy-secondary">Choose a control to see its checks and their evidence.</p>
                    )}
                  </div>
                </div>
              )}
            </section>
            <aside className="fy-stack fy-stack--6" aria-label="Verification and frameworks">
              <Nightly all={all} />
              <ByFramework list={list.data} />
            </aside>
          </div>
        </>
      )}
    </div>
  );
}

// ControlDetail is one control: why it holds or not, who answers for it, its frameworks, and each check by frequency.
function ControlDetail({ c, onBack }: { c: Control | undefined; onBack: () => void }) {
  const id = useId();
  if (!c) return null;
  return (
    <div aria-labelledby={id} role="region">
      <div className="fy-card__head">
        <Button size="sm" variant="ghost" className="fy-only-compact" onClick={onBack}>
          <Icon of={ArrowLeft} />
          Controls
        </Button>
        <h3 className="fy-h3" id={id}>
          {c.id}
        </h3>
        <StatusChip status={c.status} />
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        <p>{c.control}</p>
        <p className="fy-small fy-secondary">{c.detail}</p>
        <dl className="fy-dl">
          <div>
            <dt>Owner</dt>
            <dd>{roles[c.owner]}</dd>
          </div>
          <div>
            <dt>Mapping</dt>
            <dd>{c.mapping}</dd>
          </div>
          <div>
            <dt>Frequency</dt>
            <dd>{c.frequency}</dd>
          </div>
          {c.failing_since && (
            <div>
              <dt>Failing since</dt>
              <dd>{dateTime(c.failing_since)}</dd>
            </div>
          )}
          {c.checked_at && (
            <div>
              <dt>Worked out</dt>
              <dd>{dateTime(c.checked_at)}</dd>
            </div>
          )}
        </dl>
        <h4 className="fy-eyebrow">Checks</h4>
        <ul className="fy-list" aria-label={`Checks of ${c.id}`}>
          {c.checks.map((k, i) => (
            <li key={`${k.frequency}-${k.how ?? ""}-${i}`} className="fy-list__item">
              <span className="fy-list__main">
                <span className="fy-list__title whitespace-normal">
                  <span className="fy-mono">{k.frequency}</span> · {kinds[k.kind]}
                  {k.how ? <span className="fy-mono fy-muted"> {k.how}</span> : null}
                </span>
                <span className="fy-list__meta whitespace-normal">
                  {k.detail}
                  {k.arrives ? ` It arrives with ${k.arrives}.` : ""}
                </span>
                {k.keeps && <span className="fy-list__meta whitespace-normal fy-muted">Evidence: {k.keeps}</span>}
                {k.evidence && (
                  <a className="fy-small" href={k.evidence} target="_blank" rel="noreferrer">
                    Open the evidence
                  </a>
                )}
              </span>
              <StatusChip status={k.status} />
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

// Nightly shows the nightly verifications of the ledger and the audit trail (FR-CTL-05), from their controls.
function Nightly({ all }: { all: Control[] }) {
  const id = useId();
  const rows = [
    { control: "CTL-FIN-01", title: "Balances recomputed from their lines" },
    { control: "CTL-FIN-04", title: "Sub-ledgers agree with their control accounts" },
    { control: "CTL-AUD-02", title: "Audit chain intact and anchored" },
  ];
  return (
    <section className="fy-card" aria-labelledby={id}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={id}>
          Nightly verification
        </h2>
      </div>
      <ul className="fy-list">
        {rows.map((r) => {
          const c = all.find((x) => x.id === r.control);
          return (
            <li key={r.control} className="fy-list__item">
              <span className="fy-list__icon">
                <Icon of={ShieldCheck} size={20} />
              </span>
              <span className="fy-list__main">
                <span className="fy-list__title">{r.title}</span>
                <span className="fy-list__meta whitespace-normal">
                  {r.control} · {c?.detail ?? "Not in the registry"}
                </span>
              </span>
              {c && <StatusChip status={c.status} />}
            </li>
          );
        })}
      </ul>
    </section>
  );
}

// ByFramework tallies, for each framework, how many of its controls pass.
function ByFramework({ list }: { list: components["schemas"]["Controls"] }) {
  const id = useId();
  return (
    <section className="fy-card" aria-labelledby={id}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={id}>
          By framework
        </h2>
      </div>
      <ul className="fy-list">
        {list.frameworks
          .filter((f) => f.controls > 0)
          .map((f) => {
            const mine = list.items.filter((c) => c.frameworks.includes(f.key as Control["frameworks"][number]));
            const passing = mine.filter((c) => c.status === "pass").length;
            return (
              <li key={f.key} className="fy-list__item">
                <span className="fy-list__main">
                  <span className="fy-list__title">{f.title}</span>
                </span>
                <span className="fy-num fy-small">
                  {passing} of {mine.length} passing
                </span>
              </li>
            );
          })}
      </ul>
    </section>
  );
}

// ControlsFailing is the Inbox's Compliance section (CMP-01, design D5): the controls that fail or are overdue whose owner
// role the viewer holds, each linking to the dashboard; nothing when none is.
export function ControlsFailing() {
  const { session } = useSignedIn();
  const mine = new Set(session.roles ?? []);
  const list = useQuery({ queryKey: ["controls"], queryFn: controls, enabled: readers.some((r) => mine.has(r)) });
  const failing = (list.data?.items ?? []).filter((c) => (c.status === "fail" || c.status === "overdue") && mine.has(c.owner));
  if (!list.data || failing.length === 0) return null;
  return (
    <section className="fy-card" aria-labelledby="controls-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="controls-title" title={helpText("inbox.controls").tooltip}>
          Compliance
        </h2>
      </div>
      <ul className="fy-list" aria-labelledby="controls-title">
        {failing.map((c) => (
          <li key={c.id}>
            <Link className="fy-list__item" to="/compliance/controls">
              <span className="fy-list__main">
                <span className="fy-list__title whitespace-normal">
                  <span className="fy-mono fy-muted">{c.id}</span> {c.control}
                </span>
                <span className="fy-list__meta whitespace-normal">{c.detail}</span>
              </span>
              <StatusChip status={c.status} />
              <Icon of={ChevronRight} />
            </Link>
          </li>
        ))}
      </ul>
    </section>
  );
}
