import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowLeft, ChevronRight, CircleCheck, CircleX, Download, History, Info, Link2, Lock, Search, ShieldCheck, Sparkles, TriangleAlert } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { date, dateTime, timeZone } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { MyExports } from "../reports/AccountantExports";

type Summary = components["schemas"]["AuditEventSummary"];
type Actor = components["schemas"]["AuditActor"];
type Change = components["schemas"]["AuditFieldChange"];
type Verification = components["schemas"]["AuditVerification"];
type Recovery = components["schemas"]["AuditRecovery"];
type Purge = components["schemas"]["AuditPurge"];

interface Filters {
  action: string;
  recordType: string;
  recordId: string;
  field: string;
  actor: string;
  ai: boolean;
  approvalId: string;
  from: string;
  to: string;
}

const none: Filters = { action: "", recordType: "", recordId: "", field: "", actor: "", ai: false, approvalId: "", from: "", to: "" };

// The kinds of actor, in words.
const actors: Record<Actor["type"], string> = { user: "Person", service: "Service", integration: "Integration", scheduler: "Schedule", ai: "AI", system: "System" };

// The proofs of a sensitive value (AUD-02), in words.
const proofs: Record<NonNullable<Change["proof"]>, string> = { changed: "Changed", changed_back: "Changed back", unchanged: "Unchanged" };

function today(): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone, year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date());
}

function who(a: Actor | undefined): string {
  return a ? `${actors[a.type] ?? a.type} ${a.id}` : "";
}

// value shows a field's value as the event recorded it; an absent one is a dash.
function value(v: unknown): string {
  if (v === null || v === undefined || v === "") return "—";
  if (typeof v === "string") return v;
  if (typeof v === "number" || typeof v === "boolean") return String(v);
  return JSON.stringify(v);
}

// AuditTrail is Compliance › Audit trail (docs/design/screens AuditTrail; FR-CTL-03, AUD-02, AUD-03, AUD-05, AUD-07,
// AUD-09, AUD-10; Expert): verify the chain for a range of days, see the restores behind the anchors (the Owner
// acknowledges each) and the purges after retention, search the events and open one's before and after, and export.
export function AuditTrail() {
  const id = useId();
  const h = helpText("compliance.audit-trail");
  return (
    <div className="fy-page fy-enter">
      <nav className="fy-breadcrumb" aria-label="Breadcrumb">
        <Link to="/compliance">Compliance</Link>
        <Icon of={ChevronRight} />
        <span>Audit trail</span>
      </nav>
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">{h.label}</h1>
          <p className="fy-secondary">{h.tooltip}</p>
        </div>
      </div>
      <Recoveries />
      <Verify id={`${id}-verify`} />
      <Explorer id={`${id}-explore`} />
      <Kept id={`${id}-kept`} />
    </div>
  );
}

// Verify runs Verify integrity for a range of days (AUD-03): passed, failed with where, or explained by a purge or a
// restore (AUD-09).
function Verify({ id }: { id: string }) {
  const h = helpText("audit.verify");
  const [range, setRange] = useState({ from: `${today().slice(0, 8)}01`, to: today() });
  const run = useMutation({
    mutationFn: async (r: { from: string; to: string }) => {
      const { data, error } = await api.GET("/v1/audit/verification", { params: { query: r } });
      if (error) throw error;
      return data;
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    run.mutate(range);
  };
  return (
    <section className="fy-card" aria-labelledby={id}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={id}>
          Verify integrity
        </h2>
      </div>
      <form className="fy-card__body fy-row fy-row--wrap gap-3" onSubmit={submit}>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-from`}>
            Events from
          </label>
          <input className="fy-input" id={`${id}-from`} type="date" required value={range.from} onChange={(e) => setRange({ ...range, from: e.target.value })} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-to`}>
            to
          </label>
          <input className="fy-input" id={`${id}-to`} type="date" required value={range.to} onChange={(e) => setRange({ ...range, to: e.target.value })} />
        </div>
        <Button type="submit" variant="primary" className="self-end" disabled={run.isPending} title={h.tooltip}>
          <Icon of={ShieldCheck} />
          {h.label}
        </Button>
      </form>
      <div className="fy-card__body" role="status">
        {run.isPending && <p className="fy-small">Verifying the chain…</p>}
        {run.data && <Outcome v={run.data} />}
      </div>
      {run.isError && <Refusal problem={problemOf(run.error)} title="The audit trail could not be verified" />}
    </section>
  );
}

function Outcome({ v }: { v: Verification }) {
  const days = v.from === v.to ? date(v.from) : `${date(v.from)} to ${date(v.to)}`;
  const counts = `${v.events.toLocaleString("en-SG")} events recomputed against ${v.anchors.toLocaleString("en-SG")} signed anchors and ${v.close_anchors.toLocaleString("en-SG")} close snapshots; ${v.details.toLocaleString("en-SG")} security events' request details checked.`;
  const unchecked = v.details_checked ? "" : " This installation holds no fingerprint key, so the request details were not checked.";
  switch (v.outcome) {
    case "failed":
      return (
        <div className="fy-banner fy-banner--critical">
          <Icon of={CircleX} size={20} />
          <div className="fy-banner__text">
            <strong>The audit trail was altered: {days}</strong>
            <span className="fy-small">
              {v.problem}
              {v.failed_seq ? ` (event ${v.failed_seq})` : ""}. Treat it as tampering: open an incident and keep everything as it is.
            </span>
          </div>
        </div>
      );
    case "explained":
      return (
        <div className="fy-banner fy-banner--info">
          <Icon of={Info} size={20} />
          <div className="fy-banner__text">
            <strong>Verified, with an explained gap: {days}</strong>
            <span className="fy-small">
              {counts} The gap: {v.problem}.{unchecked}
            </span>
          </div>
        </div>
      );
    default:
      return (
        <div className="fy-banner fy-banner--good">
          <Icon of={ShieldCheck} size={20} />
          <div className="fy-banner__text">
            <strong>Chain verified: {days}</strong>
            <span className="fy-small">
              {counts}
              {v.events > 0 && v.anchored_through < v.last_seq ? ` Events after ${v.anchored_through} are anchored within the hour.` : ""}
              {unchecked}
            </span>
          </div>
        </div>
      );
  }
}

// Recoveries shows each restore behind the anchors that waits for the Owner's acknowledgement (AUD-09, design D8).
function Recoveries() {
  const { session } = useSignedIn();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const h = helpText("audit.acknowledge-recovery");
  const owner = (session.roles ?? []).includes("owner");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const list = useQuery({
    queryKey: ["audit-recoveries"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/audit/recoveries");
      if (error) throw error;
      return data.items;
    },
  });
  const ack = useMutation({
    mutationFn: async (r: Recovery) => {
      const key = newKey();
      const out = await guarded(confirm, () =>
        api.POST("/v1/audit/recoveries/{recovery_id}/acknowledge", {
          params: { path: { recovery_id: r.id }, header: { "Idempotency-Key": key, "If-Match": `"${r.version}"` } },
        }),
      );
      if (out.error) throw out.error;
      return out.data;
    },
    onMutate: () => setProblem(null),
    onError: (e) => setProblem(problemOf(e)),
    onSettled: () => queries.invalidateQueries({ queryKey: ["audit-recoveries"] }),
  });
  const waiting = (list.data ?? []).filter((r) => !r.acknowledged_at);
  if (waiting.length === 0 && !problem) return null;
  return (
    <div className="fy-stack fy-stack--2">
      {problem && <Refusal problem={problem} title="The recovery could not be acknowledged" />}
      {waiting.map((r) => (
        <div key={r.id} className="fy-banner fy-banner--warning" role="status">
          <Icon of={TriangleAlert} size={20} />
          <div className="fy-banner__text">
            <strong>The audit trail was restored behind its anchors</strong>
            <span className="fy-small">
              A restore found on {dateTime(r.detected_at)} left the events {r.missing_from} to {r.missing_to}, which the anchors signed, no longer proven; the chain continues from event{" "}
              {r.restored_seq}. Verification explains the gap. Follow the restore runbook ({r.runbook}), record what was lost, then the Owner acknowledges it. Until then the check of the audit
              trail fails.
            </span>
          </div>
          {owner && (
            <div className="fy-banner__action">
              <Button size="sm" variant="secondary" disabled={ack.isPending} onClick={() => ack.mutate(r)} title={h.tooltip} aria-label={`${h.label}: the restore found on ${dateTime(r.detected_at)}`}>
                <Icon of={CircleCheck} />
                {h.label}
              </Button>
            </div>
          )}
        </div>
      ))}
    </div>
  );
}

// Explorer searches the events (FR-CTL-03), newest first, and opens one in the detail beside the list.
function Explorer({ id }: { id: string }) {
  const [filters, setFilters] = useState<Filters>(none);
  const [applied, setApplied] = useState<Filters>(none);
  const [open, setOpen] = useState<string | null>(null);
  const [exporting, setExporting] = useState(false);
  const query = (f: Filters) => ({
    action: f.action.trim() || undefined,
    record_type: f.recordType.trim() || undefined,
    record_id: f.recordId.trim() || undefined,
    field: f.field.trim() || undefined,
    actor: f.actor.trim() || undefined,
    ai: f.ai ? "any" : undefined,
    approval_id: f.approvalId.trim() || undefined,
  });
  const events = useInfiniteQuery({
    queryKey: ["audit-events", applied],
    initialPageParam: "",
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET("/v1/audit/events", {
        params: { query: { ...query(applied), from: applied.from || undefined, to: applied.to || undefined, page_size: 100, cursor: pageParam || undefined } },
      });
      if (error) throw error;
      return data;
    },
    getNextPageParam: (last) => last.next_cursor || undefined,
  });
  const items = events.data?.pages.flatMap((p) => p.items) ?? [];
  const set = (k: keyof Filters, v: string | boolean) => setFilters({ ...filters, [k]: v });
  const text = (k: Exclude<keyof Filters, "ai">, label: string, placeholder: string) => (
    <div className="fy-field">
      <label className="fy-label" htmlFor={`${id}-${k}`}>
        {label}
      </label>
      <input className="fy-input" id={`${id}-${k}`} placeholder={placeholder} value={filters[k]} onChange={(e) => set(k, e.target.value)} />
    </div>
  );
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setOpen(null);
    setApplied(filters);
  };
  const exportHelp = helpText("audit.export");
  return (
    <section className="fy-card" aria-labelledby={id}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={id}>
          Events
        </h2>
        <Button size="sm" variant="secondary" onClick={() => setExporting(!exporting)} aria-expanded={exporting} title={exportHelp.tooltip}>
          <Icon of={Download} />
          {exportHelp.label}
        </Button>
      </div>
      {exporting && <Export filters={applied} query={query(applied)} />}
      <form className="fy-card__body fy-stack fy-stack--3" onSubmit={submit}>
        <div className="fy-row fy-row--wrap gap-3">
          {text("action", "Action", "document.")}
          {text("recordId", "Record ID", "")}
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-from`}>
              From
            </label>
            <input className="fy-input" id={`${id}-from`} type="date" value={filters.from} onChange={(e) => set("from", e.target.value)} />
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-to`}>
              to
            </label>
            <input className="fy-input" id={`${id}-to`} type="date" value={filters.to} onChange={(e) => set("to", e.target.value)} />
          </div>
          <label className="fy-check self-end">
            <input type="checkbox" checked={filters.ai} onChange={(e) => set("ai", e.target.checked)} />
            Only what AI proposals changed
          </label>
        </div>
        <details>
          <summary className="fy-small">More filters</summary>
          <div className="fy-row fy-row--wrap gap-3 mt-3">
            {text("actor", "Actor ID", "")}
            {text("recordType", "Record type", "document")}
            {text("field", "Field", "status")}
            {text("approvalId", "Approval ID", "")}
          </div>
        </details>
        <div>
          <Button type="submit" variant="secondary">
            <Icon of={Search} />
            Find
          </Button>
        </div>
      </form>
      {events.isError && <Refusal problem={problemOf(events.error)} title="The audit trail could not be searched" />}
      {events.data && items.length === 0 && <p className="fy-card__body fy-small fy-secondary">No event matches.</p>}
      {items.length > 0 && (
        <div className="fy-listdetail" data-show={open ? "detail" : "list"}>
          <div className="fy-listdetail__list">
            <ul className="fy-list" aria-label="Audit events">
              {items.map((e) => (
                <li key={e.id}>
                  <button type="button" className="fy-list__item w-full text-left" aria-current={open === e.id ? "true" : undefined} onClick={() => setOpen(e.id)}>
                    <span className={e.originator ? "fy-list__icon fy-list__icon--ai" : "fy-list__icon"}>
                      <Icon of={e.originator ? Sparkles : History} size={20} />
                    </span>
                    <span className="fy-list__main">
                      <span className="fy-list__title">{e.action}</span>
                      <span className="fy-list__meta">
                        {dateTime(e.occurred_at)} · {who(e.actor)}
                        {e.on_behalf_of ? ` for ${who(e.on_behalf_of)}` : ""} · {e.record.type}
                      </span>
                    </span>
                  </button>
                </li>
              ))}
            </ul>
            {events.hasNextPage && (
              <div className="fy-card__body">
                <Button size="sm" variant="secondary" disabled={events.isFetchingNextPage} onClick={() => void events.fetchNextPage()}>
                  Show more
                </Button>
              </div>
            )}
          </div>
          <div className="fy-listdetail__detail">{open ? <Event eventId={open} onBack={() => setOpen(null)} /> : <p className="fy-card__body fy-small fy-secondary">Choose an event to see its before and after.</p>}</div>
        </div>
      )}
    </section>
  );
}

// Event is one event's detail (FR-CTL-03): who, for whom, why, from where, each field's before and after (a sensitive
// value only as a proof, AUD-02), and its place in the chain.
function Event({ eventId, onBack }: { eventId: string; onBack: () => void }) {
  const id = useId();
  const one = useQuery({
    queryKey: ["audit-event", eventId],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/audit/events/{event_id}", { params: { path: { event_id: eventId } } });
      if (error) throw error;
      return data;
    },
  });
  const e = one.data;
  return (
    <div aria-labelledby={id} role="region">
      <div className="fy-card__head">
        <Button size="sm" variant="ghost" className="fy-only-compact" onClick={onBack}>
          <Icon of={ArrowLeft} />
          Events
        </Button>
        <h3 className="fy-h3" id={id}>
          {e ? `Event ${e.seq} · ${e.action}` : "Event"}
        </h3>
        {e && (
          <span className="fy-chip fy-chip--good">
            <Icon of={Link2} />
            In chain
          </span>
        )}
      </div>
      {one.isError && <Refusal problem={problemOf(one.error)} title="The event could not be shown" />}
      {one.isPending && <p className="fy-card__body fy-small">Loading the event…</p>}
      {e && (
        <div className="fy-card__body fy-stack fy-stack--3">
          <dl className="fy-dl">
            <div>
              <dt>Recorded</dt>
              <dd>{dateTime(e.occurred_at)}</dd>
            </div>
            <div>
              <dt>Record</dt>
              <dd className="fy-mono">
                {e.record.type} {e.record.id}
                {e.record.version ? ` · version ${e.record.version}` : ""}
              </dd>
            </div>
            <div>
              <dt>Actor</dt>
              <dd className="fy-mono">{who(e.actor)}</dd>
            </div>
            {e.on_behalf_of && (
              <div>
                <dt>On behalf of</dt>
                <dd className="fy-mono">{who(e.on_behalf_of)}</dd>
              </div>
            )}
            {e.originator && (
              <div>
                <dt>Originator</dt>
                <dd>
                  <span className="fy-ai-badge">
                    <Icon of={Sparkles} />
                    AI
                  </span>{" "}
                  proposal <span className="fy-mono">{e.originator}</span>
                </dd>
              </div>
            )}
            {e.approval_ids.length > 0 && (
              <div>
                <dt>Approvals</dt>
                <dd className="fy-mono">{e.approval_ids.join(", ")}</dd>
              </div>
            )}
            {e.reason && (
              <div>
                <dt>Reason</dt>
                <dd>{e.reason}</dd>
              </div>
            )}
            <div>
              <dt>Request</dt>
              <dd>
                <Request r={e.request} />
              </dd>
            </div>
          </dl>
          {e.changes.length > 0 ? (
            <div className="overflow-x-auto">
              <table className="fy-table fy-table--compact" aria-label="Before and after">
                <thead>
                  <tr>
                    <th scope="col">Field</th>
                    <th scope="col">Before</th>
                    <th scope="col">After</th>
                  </tr>
                </thead>
                <tbody>
                  {e.changes.map((c) => (
                    <tr key={c.field}>
                      <td>{c.field}</td>
                      {c.proof ? (
                        <td colSpan={2}>
                          <span className="fy-chip fy-chip--neutral">{proofs[c.proof]}</span>
                          {c.hint && <span className="fy-mono ml-2">{c.hint}</span>}
                          <span className="fy-small fy-muted ml-2">A sensitive value: the trail keeps only a proof, never the value.</span>
                        </td>
                      ) : c.sealed === "shredded" ? (
                        <td colSpan={2}>
                          <span className="fy-chip fy-chip--neutral">
                            <Icon of={Lock} />
                            Shredded
                          </span>
                          <span className="fy-small fy-muted ml-2">
                            {c.shredded_on && c.shredded_year
                              ? `A person's value: their key for the year ending ${date(c.shredded_year)} was shredded on ${date(c.shredded_on)}, when the records' retention ended.`
                              : "A person's value: their key was shredded when the records' retention ended."}
                          </span>
                        </td>
                      ) : (
                        <>
                          <td className="fy-mono whitespace-normal">{value(c.before)}</td>
                          <td className="fy-mono whitespace-normal">
                            {value(c.after)}
                            {c.sealed === "opened" && (
                              <span className="fy-chip fy-chip--neutral ml-2" title="A person's value, sealed under their own key and shredded when the records' retention ends">
                                <Icon of={Lock} />
                                Sealed
                              </span>
                            )}
                          </td>
                        </>
                      )}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <p className="fy-small fy-secondary">It changed no field.</p>
          )}
          <p className="fy-small fy-muted fy-mono break-all">
            hash {e.hash} · previous {e.prev_hash}
          </p>
        </div>
      )}
    </div>
  );
}

// Request is where an event came from; a security event's IP address and browser are kept until the security-log
// retention ends, then erased (AUD-06).
function Request({ r }: { r: components["schemas"]["AuditRequest"] }) {
  const parts = [r.ip && `IP ${r.ip}`, r.user_agent, r.session && `session ${r.session}`, r.id && `request ${r.id}`].filter(Boolean).join(" · ");
  if (r.details === "erased") return <>{parts ? `${parts} · ` : ""}its IP address and browser were erased when the security-log retention ended</>;
  return <>{parts || "None recorded"}</>;
}

// Export asks for the events the filters match over a range of days as CSV or Excel (FR-CTL-03, AUD-07); it is
// prepared in the background and recorded in the audit trail (AUD-05).
function Export({ filters, query }: { filters: Filters; query: Record<string, string | undefined> }) {
  const id = useId();
  const queries = useQueryClient();
  const [range, setRange] = useState({ from: filters.from || `${today().slice(0, 8)}01`, to: filters.to || today(), format: "xlsx" as "xlsx" | "csv" });
  const [asked, setAsked] = useState(false);
  const ask = useMutation({
    mutationFn: async () => {
      const f = Object.fromEntries(Object.entries(query).filter(([, v]) => v !== undefined));
      const { data, error } = await api.POST("/v1/audit/exports", {
        params: { header: { "Idempotency-Key": newKey() } },
        body: { from: range.from, to: range.to, format: range.format, ...(Object.keys(f).length > 0 ? { filters: f } : {}) },
      });
      if (error) throw error;
      return data;
    },
    onSuccess: () => {
      setAsked(true);
      void queries.invalidateQueries({ queryKey: ["report-exports"] });
    },
  });
  return (
    <div className="fy-card__body fy-stack fy-stack--3">
      <form
        className="fy-row fy-row--wrap gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          setAsked(false);
          ask.mutate();
        }}
      >
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-from`}>
            Export from
          </label>
          <input className="fy-input" id={`${id}-from`} type="date" required value={range.from} onChange={(e) => setRange({ ...range, from: e.target.value })} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-to`}>
            to
          </label>
          <input className="fy-input" id={`${id}-to`} type="date" required value={range.to} onChange={(e) => setRange({ ...range, to: e.target.value })} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-format`}>
            Format
          </label>
          <select className="fy-select" id={`${id}-format`} value={range.format} onChange={(e) => setRange({ ...range, format: e.target.value as "xlsx" | "csv" })}>
            <option value="xlsx">Excel (XLSX)</option>
            <option value="csv">CSV</option>
          </select>
        </div>
        <Button type="submit" variant="primary" className="self-end" disabled={ask.isPending}>
          <Icon of={Download} />
          Ask for the export
        </Button>
      </form>
      <p className="fy-small fy-muted">With the filters you searched with. Sensitive values stay proofs. The export is prepared in the background, and asking for it is recorded in the audit trail.</p>
      {asked && (
        <p className="fy-small" role="status">
          The export is being prepared. It appears in your exports below.
        </p>
      )}
      {ask.isError && <Refusal problem={problemOf(ask.error)} title="The export could not be asked for" />}
      <MyExports limit={5} title="Your recent exports" />
    </div>
  );
}

// Kept lists the purges after retention behind their signed boundaries (AUD-09, AUD-10) and every restore behind the
// anchors with its acknowledgement (design D8).
function Kept({ id }: { id: string }) {
  const purges = useQuery({
    queryKey: ["audit-purges"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/audit/purges");
      if (error) throw error;
      return data.items;
    },
  });
  const recoveries = useQuery({
    queryKey: ["audit-recoveries"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/audit/recoveries");
      if (error) throw error;
      return data.items;
    },
  });
  const p = purges.data ?? [];
  const r = recoveries.data ?? [];
  return (
    <section className="fy-card" aria-labelledby={id}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={id}>
          Purges and restores
        </h2>
      </div>
      {(purges.isError || recoveries.isError) && <Refusal problem={problemOf(purges.error ?? recoveries.error)} title="The purges and restores could not be listed" />}
      {purges.data && recoveries.data && p.length === 0 && r.length === 0 && (
        <p className="fy-card__body fy-small fy-secondary">Nothing was purged after its retention, and the trail was never restored behind its anchors.</p>
      )}
      {(p.length > 0 || r.length > 0) && (
        <ul className="fy-list" aria-label="Purges and restores">
          {p.map((x) => (
            <li key={x.anchor_key} className="fy-list__item">
              <span className="fy-list__main">
                <span className="fy-list__title whitespace-normal">{purgeTitle(x)}</span>
                <span className="fy-list__meta">
                  Written {dateTime(x.written_at)} under {x.approval} · {counts(x.counts)} · signed copy <span className="fy-mono">{x.anchor_key}</span>
                </span>
              </span>
            </li>
          ))}
          {r.map((x) => (
            <li key={x.id} className="fy-list__item">
              <span className={x.acknowledged_at ? "fy-chip fy-chip--good" : "fy-chip fy-chip--warning"}>
                <Icon of={x.acknowledged_at ? CircleCheck : TriangleAlert} />
                {x.acknowledged_at ? "Acknowledged" : "Waiting"}
              </span>
              <span className="fy-list__main">
                <span className="fy-list__title whitespace-normal">
                  Restored behind the anchors: events {x.missing_from} to {x.missing_to} no longer proven
                </span>
                <span className="fy-list__meta">
                  Found {dateTime(x.detected_at)} · anchor epoch {x.epoch}
                  {x.acknowledged_at ? ` · acknowledged ${dateTime(x.acknowledged_at)} by user ${x.acknowledged_by}` : ""} · signed copy <span className="fy-mono">{x.anchor_key}</span>
                </span>
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function purgeTitle(x: Purge): string {
  if (x.kind === "ledger") return `The ledger before ${date(x.boundary)} carried forward: reports from that day on are unchanged; earlier days are refused`;
  return `The audit trail's events up to ${x.last_seq}, which occurred until ${dateTime(x.last_occurred_at)}, purged after their retention`;
}

const countNames: Record<string, string> = {
  events: "events",
  security_events: "security events",
  entries_purged: "entries purged",
  lines_purged: "lines purged",
  entries_kept: "entries kept",
  lines_kept: "lines kept",
  carry_forward_entries: "carry-forward entries",
};

function counts(c: Record<string, number>): string {
  return Object.entries(c)
    .map(([k, n]) => `${n.toLocaleString("en-SG")} ${countNames[k] ?? k.replaceAll("_", " ")}`)
    .join(", ");
}
