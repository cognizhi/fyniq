import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Archive, ChevronRight, ClockAlert, Pencil, Plus, ShieldCheck, TriangleAlert } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { DPOContact } from "../settings/Organisation";

type Inventory = components["schemas"]["DataInventory"];
type Entry = components["schemas"]["InventoryEntryView"];
type EntryInput = components["schemas"]["InventoryEntryInput"];

async function inventory() {
  const { data, error } = await api.GET("/v1/compliance/data-inventory");
  if (error) throw error;
  return data;
}

async function profile() {
  const { data, error } = await api.GET("/v1/organisation/profile");
  if (error) throw error;
  return data;
}

type Tab = "inventory" | "dpias" | "requests" | "breaches" | "transfers" | "dpo";
type Register = components["schemas"]["TransferRegister"];
type Transfer = components["schemas"]["TransferView"];
type TransferInput = components["schemas"]["TransferInput"];
type Flow = Register["flows"][number];

async function transfers() {
  const { data, error } = await api.GET("/v1/compliance/transfers");
  if (error) throw error;
  return data;
}
type Breach = components["schemas"]["BreachView"];
type BreachClass = Breach["classes"][number];

async function breaches() {
  const { data, error } = await api.GET("/v1/compliance/breaches");
  if (error) throw error;
  return data.items;
}

async function exercises() {
  const { data, error } = await api.GET("/v1/compliance/breach-exercises");
  if (error) throw error;
  return data.items;
}
type Request = components["schemas"]["PDRequestView"];
type Report = components["schemas"]["PersonalDataReport"];

async function requests() {
  const { data, error } = await api.GET("/v1/compliance/personal-data-requests");
  if (error) throw error;
  return data.items;
}
type DPIA = components["schemas"]["DPIAView"];

async function dpias() {
  const { data, error } = await api.GET("/v1/compliance/dpias");
  if (error) throw error;
  return data.items;
}

async function dpia(key: string) {
  const { data, error } = await api.GET("/v1/compliance/dpias/{key}", { params: { path: { key } } });
  if (error) throw error;
  return data;
}

// DataProtection is Compliance › Data protection (CMP-05, CMP-PD-01, CMP-PD-02, CMP-PD-09; rev 0.244): the PDPA programme's
// registers. The inventory: what Fyniq holds and never holds, what the organisation adds, the retention schedule and the
// quarterly review (CTL-DP-05). The DPO: their contact, the text for the website, the published policies and the penalty.
export function DataProtection() {
  const h = helpText("compliance.data-protection");
  const [tab, setTab] = useState<Tab>("inventory");
  return (
    <div className="fy-page fy-enter">
      <nav className="fy-breadcrumb" aria-label="Breadcrumb">
        <Link to="/compliance">Compliance</Link>
        <Icon of={ChevronRight} />
        <span>{h.label}</span>
      </nav>
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">{h.label}</h1>
          <p className="fy-secondary">{h.tooltip}</p>
        </div>
      </div>
      <div className="fy-tabs" role="tablist" aria-label="Data protection registers">
        <button type="button" role="tab" aria-selected={tab === "inventory"} onClick={() => setTab("inventory")} title={helpText("data-protection.inventory").tooltip}>
          Inventory
        </button>
        <button type="button" role="tab" aria-selected={tab === "dpias"} onClick={() => setTab("dpias")} title={helpText("data-protection.dpias").tooltip}>
          Impact assessments
        </button>
        <button type="button" role="tab" aria-selected={tab === "requests"} onClick={() => setTab("requests")} title={helpText("data-protection.requests").tooltip}>
          Requests
        </button>
        <button type="button" role="tab" aria-selected={tab === "breaches"} onClick={() => setTab("breaches")} title={helpText("data-protection.breaches").tooltip}>
          Breaches
        </button>
        <button type="button" role="tab" aria-selected={tab === "transfers"} onClick={() => setTab("transfers")} title={helpText("data-protection.transfers").tooltip}>
          Transfers
        </button>
        <button type="button" role="tab" aria-selected={tab === "dpo"} onClick={() => setTab("dpo")} title={helpText("organisation.dpo").tooltip}>
          Data Protection Officer
        </button>
      </div>
      {tab === "inventory" && <InventoryTab />}
      {tab === "dpias" && <DPIAsTab />}
      {tab === "requests" && <RequestsTab />}
      {tab === "breaches" && <BreachesTab />}
      {tab === "transfers" && <TransfersTab />}
      {tab === "dpo" && <DPOTab />}
    </div>
  );
}

function InventoryTab() {
  const id = useId();
  const { session, mode } = useSignedIn();
  const officer = (session.roles ?? []).includes("dpo");
  const inv = useQuery({ queryKey: ["data-inventory"], queryFn: inventory });
  const [editing, setEditing] = useState<Entry | "new" | null>(null);
  const [reviewing, setReviewing] = useState(false);
  if (inv.isError) return <Refusal problem={problemOf(inv.error)} title="The inventory could not be shown" />;
  if (!inv.data) return <p className="fy-small">Loading the inventory…</p>;
  const d: Inventory = inv.data;
  return (
    <>
      {d.review.quarter && (
        <div className={`fy-banner ${d.review.overdue ? "fy-banner--warning" : "fy-banner--info"}`} role="status">
          <Icon of={ClockAlert} size={20} />
          <div className="fy-banner__text">
            <strong>
              The inventory's review of {d.review.label} is {d.review.overdue ? "overdue" : "due"}, by {date(d.review.due_by)}
            </strong>
            <span className="fy-small">The Compliance officer checks that the inventory still says what the company holds, then records the review.</span>
          </div>
          {officer && (
            <Button size="sm" onClick={() => setReviewing(true)} title={helpText("data-protection.review").tooltip}>
              <Icon of={ShieldCheck} />
              Record the review
            </Button>
          )}
        </div>
      )}
      <section className="fy-card" aria-labelledby={`${id}-classes`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-classes`}>
            What Fyniq holds
          </h2>
          <span className="fy-small fy-muted">
            {d.review.last_quarter ? `Last reviewed for the quarter from ${date(d.review.last_quarter)}` : "Not reviewed yet"}
          </span>
        </div>
        <ul className="fy-list" aria-labelledby={`${id}-classes`}>
          {d.classes.map((c) => (
            <li key={c.key} className="fy-list__item items-start">
              <span className="fy-list__main">
                <span className="fy-list__title whitespace-normal">{c.what}</span>
                <span className="fy-list__meta whitespace-normal">Why: {c.why}</span>
                <span className="fy-list__meta whitespace-normal">Kept: {c.retention}</span>
                {mode === "expert" && (
                  <span className="fy-list__meta whitespace-normal">
                    Where: {c.where} · Who: {c.who} · Protection: {c.protection} · Abroad: {c.transfers}
                  </span>
                )}
              </span>
            </li>
          ))}
        </ul>
      </section>
      <section className="fy-card" aria-labelledby={`${id}-notheld`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-notheld`}>
            What Fyniq never holds
          </h2>
        </div>
        <ul className="fy-list" aria-labelledby={`${id}-notheld`}>
          {d.not_held.map((n) => (
            <li key={n.key} className="fy-list__item">
              <span className="fy-list__main">
                <span className="fy-list__title">{n.what}</span>
                <span className="fy-list__meta whitespace-normal">{n.why}</span>
              </span>
            </li>
          ))}
        </ul>
      </section>
      <section className="fy-card" aria-labelledby={`${id}-entries`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-entries`} title={helpText("data-protection.entry").tooltip}>
            What the company holds outside Fyniq
          </h2>
          {officer && (
            <Button size="sm" onClick={() => setEditing("new")}>
              <Icon of={Plus} />
              Add an entry
            </Button>
          )}
        </div>
        {d.entries.length === 0 ? (
          <p className="fy-card__body fy-small fy-secondary">Nothing added. Add what the company keeps elsewhere, such as job applicants' CVs or a mailing list.</p>
        ) : (
          <ul className="fy-list" aria-labelledby={`${id}-entries`}>
            {d.entries.map((e) => (
              <li key={e.entry_id} className={`fy-list__item${e.state === "retired" ? " fy-muted" : ""}`}>
                <span className="fy-list__main">
                  <span className="fy-list__title whitespace-normal">
                    {e.what}
                    {e.state === "retired" && " · retired"}
                  </span>
                  <span className="fy-list__meta whitespace-normal">
                    {e.why} · kept {e.retention} · {e.where} · abroad: {e.transfers}
                    {e.needs_dpia && " · needs a DPIA"}
                  </span>
                </span>
                {officer && (
                  <Button size="sm" variant="ghost" onClick={() => setEditing(e)} aria-label={`Change ${e.what}`}>
                    <Icon of={Pencil} />
                    Change
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>
      <section className="fy-card" aria-labelledby={`${id}-schedule`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-schedule`}>
            Retention schedule
          </h2>
          <span className="fy-small fy-muted">From the settings in force today</span>
        </div>
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--cards" aria-labelledby={`${id}-schedule`}>
            <thead>
              <tr>
                <th scope="col">Data</th>
                <th scope="col">Kept for</th>
                <th scope="col">Then</th>
              </tr>
            </thead>
            <tbody>
              {d.schedule.map((r) => (
                <tr key={r.data}>
                  <td data-label="Data">{r.data}</td>
                  <td data-label="Kept for">{r.retention}</td>
                  <td data-label="Then">{r.deletion}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
      {editing && <EntryForm entry={editing === "new" ? null : editing} onClose={() => setEditing(null)} />}
      {reviewing && d.review.quarter && <ReviewForm quarter={d.review.quarter} label={d.review.label ?? ""} onClose={() => setReviewing(false)} />}
    </>
  );
}

const blank: EntryInput = { what: "", why: "", where: "", who: "", retention: "", transfers: "None", needs_dpia: false };

// EntryForm adds an entry to the inventory, or saves its next version, or retires it (CMP-PD-02).
function EntryForm({ entry, onClose }: { entry: Entry | null; onClose: () => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [form, setForm] = useState<EntryInput>(entry ? { what: entry.what, why: entry.why, where: entry.where, who: entry.who, retention: entry.retention,
    transfers: entry.transfers, needs_dpia: entry.needs_dpia, state: entry.state } : blank);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const save = useMutation({
    mutationFn: async (body: EntryInput) => {
      const out = await guarded(confirm, () =>
        entry
          ? api.PUT("/v1/compliance/data-inventory/entries/{entry_id}", { params: { path: { entry_id: entry.entry_id }, header: { "Idempotency-Key": newKey(), "If-Match": `"v${entry.version}"` } }, body })
          : api.POST("/v1/compliance/data-inventory/entries", { params: { header: { "Idempotency-Key": newKey() } }, body }),
      );
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      await queries.invalidateQueries({ queryKey: ["data-inventory"] });
      onClose();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate(form);
  };
  const field = (key: "what" | "why" | "where" | "who" | "retention" | "transfers", label: string) => (
    <div className="fy-field">
      <label className="fy-label" htmlFor={`${id}-${key}`}>
        {label}
      </label>
      <input className="fy-input" id={`${id}-${key}`} value={form[key]} onChange={(e) => setForm({ ...form, [key]: e.target.value })} />
    </div>
  );
  return (
    <Dialog open onOpenChange={(o) => !o && !save.isPending && onClose()} dismissible={!save.isPending} eyebrow="Data inventory"
      title={entry ? "Change the entry" : "Add an entry"} description="Personal data the company processes outside Fyniq. Each change is kept as a version.">
      {problem && <Refusal problem={problem} title="The entry was not saved" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        {field("what", "What personal data")}
        {field("why", "Why the company holds it")}
        {field("where", "Where it is held")}
        {field("who", "Who reads it")}
        {field("retention", "How long it is kept")}
        {field("transfers", "Where it goes abroad")}
        <label className="fy-check">
          <input type="checkbox" checked={form.needs_dpia} onChange={(e) => setForm({ ...form, needs_dpia: e.target.checked })} />
          It needs a data protection impact assessment
        </label>
        <div className="fy-row justify-end gap-2">
          {entry && entry.state === "open" && (
            <Button variant="ghost" type="button" onClick={() => save.mutate({ ...form, state: "retired" })} disabled={save.isPending}>
              <Icon of={Archive} />
              Retire it
            </Button>
          )}
          <Button variant="ghost" type="button" onClick={onClose} disabled={save.isPending}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={save.isPending}>
            {entry ? "Save the change" : "Add the entry"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// ReviewForm records the inventory's review of a quarter (CTL-DP-05).
function ReviewForm({ quarter, label, onClose }: { quarter: string; label: string; onClose: () => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [note, setNote] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const save = useMutation({
    mutationFn: async () => {
      const out = await guarded(confirm, () =>
        api.POST("/v1/compliance/data-inventory/reviews", { params: { header: { "Idempotency-Key": newKey() } }, body: { quarter, note: note.trim() || undefined } }),
      );
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      await Promise.all(["data-inventory", "compliance-reviews"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
      onClose();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  return (
    <Dialog open onOpenChange={(o) => !o && !save.isPending && onClose()} dismissible={!save.isPending} eyebrow="Data inventory" title={`Review ${label}`}
      description="Confirm the inventory still says what the company holds, why, where and for how long.">
      {problem && <Refusal problem={problem} title="The review was not recorded" />}
      <div className="fy-stack fy-stack--3">
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-note`}>
            Note (optional)
          </label>
          <textarea className="fy-textarea" id={`${id}-note`} value={note} onChange={(e) => setNote(e.target.value)} />
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={save.isPending}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => save.mutate()} disabled={save.isPending}>
            Record the review
          </Button>
        </div>
      </div>
    </Dialog>
  );
}

// DPIAsTab lists the data protection impact assessments (CMP-PD-08): Fyniq's for the payroll link and the AI assistant,
// required once switched on, and the organisation's own; the Compliance officer edits, writes and approves them.
function DPIAsTab() {
  const id = useId();
  const { session } = useSignedIn();
  const officer = (session.roles ?? []).includes("dpo");
  const list = useQuery({ queryKey: ["dpias"], queryFn: dpias });
  const [open, setOpen] = useState<string | "new" | null>(null);
  if (list.isError) return <Refusal problem={problemOf(list.error)} title="The impact assessments could not be shown" />;
  if (!list.data) return <p className="fy-small">Loading the impact assessments…</p>;
  return (
    <section className="fy-card" aria-labelledby={`${id}-dpias`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-dpias`}>
          Impact assessments
        </h2>
        {officer && (
          <Button size="sm" onClick={() => setOpen("new")}>
            <Icon of={Plus} />
            Write one
          </Button>
        )}
      </div>
      <ul className="fy-list" aria-labelledby={`${id}-dpias`}>
        {list.data.map((d: DPIA) => (
          <li key={d.key} className="fy-list__item">
            <span className="fy-list__main">
              <span className="fy-list__title whitespace-normal">{d.title}</span>
              <span className="fy-list__meta whitespace-normal">
                {d.why} · {d.version === 0 ? "Fyniq's draft" : `version ${d.version}`}
                {d.approved && ` · version ${d.approved.version} approved ${date(d.approved.at)}`}
              </span>
            </span>
            {d.required && d.unapproved ? (
              <span className="fy-chip fy-chip--serious">
                <Icon of={ClockAlert} />
                Needs approving
              </span>
            ) : d.approved && !d.unapproved ? (
              <span className="fy-chip fy-chip--good">
                <Icon of={ShieldCheck} />
                Approved
              </span>
            ) : null}
            <Button size="sm" variant="ghost" onClick={() => setOpen(d.key)} aria-label={`Open ${d.title}`}>
              <Icon of={Pencil} />
              Open
            </Button>
          </li>
        ))}
      </ul>
      {open && <DPIAForm dpiaKey={open === "new" ? null : open} officer={officer} onClose={() => setOpen(null)} />}
    </section>
  );
}

// DPIAForm shows an impact assessment's text, and lets the Compliance officer save its next version, approve it stepped
// up (approving Fyniq's draft adopts it), or write a new one.
function DPIAForm({ dpiaKey, officer, onClose }: { dpiaKey: string | null; officer: boolean; onClose: () => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const one = useQuery({ queryKey: ["dpia", dpiaKey], queryFn: () => dpia(dpiaKey!), enabled: dpiaKey !== null });
  const [title, setTitle] = useState("");
  const [body, setBody] = useState<string | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const d = one.data;
  const text = body ?? d?.body ?? "";
  const done = async () => {
    await Promise.all(["dpias", "dpia", "compliance-reviews"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
  };
  const save = useMutation({
    mutationFn: async () => {
      const out = await guarded(confirm, () =>
        dpiaKey === null
          ? api.POST("/v1/compliance/dpias", { params: { header: { "Idempotency-Key": newKey() } }, body: { title: title.trim(), body: text } })
          : api.PUT("/v1/compliance/dpias/{key}", { params: { path: { key: dpiaKey }, header: { "Idempotency-Key": newKey(), "If-Match": `"v${d!.version}"` } }, body: { body: text } }),
      );
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      setBody(null);
      await done();
      if (dpiaKey === null) onClose();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  const approve = useMutation({
    mutationFn: async () => {
      const out = await guarded(confirm, () =>
        api.POST("/v1/compliance/dpias/{key}/approvals", { params: { path: { key: dpiaKey! }, header: { "Idempotency-Key": newKey(), "If-Match": `"v${d!.version}"` } } }),
      );
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      await done();
      onClose();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  const busy = save.isPending || approve.isPending;
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && onClose()} dismissible={!busy} wide eyebrow="Impact assessment" title={d?.title ?? "Write an impact assessment"}
      description={d ? d.why : "For a new processing of personal data, such as an inventory entry that needs one."}>
      {problem && <Refusal problem={problem} title="The assessment was not saved or approved" />}
      {one.isError && <Refusal problem={problemOf(one.error)} title="The assessment could not be shown" />}
      {(d || dpiaKey === null) && (
        <div className="fy-stack fy-stack--3">
          {dpiaKey === null && (
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-title`}>
                Title
              </label>
              <input className="fy-input" id={`${id}-title`} value={title} onChange={(e) => setTitle(e.target.value)} />
            </div>
          )}
          {officer ? (
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-body`}>
                Text (Markdown)
              </label>
              <textarea className="fy-textarea" id={`${id}-body`} rows={16} value={text} onChange={(e) => setBody(e.target.value)} />
              <span className="fy-help">Clauses marked [counsel to confirm] bear on the law: have counsel confirm them before you approve.</span>
            </div>
          ) : (
            <div className="fy-small whitespace-pre-wrap" role="document" aria-label={d?.title} style={{ maxHeight: "50vh", overflow: "auto" }}>
              {text}
            </div>
          )}
          {officer && (
            <div className="fy-row justify-end gap-2">
              <Button variant="secondary" onClick={() => save.mutate()} disabled={busy || (dpiaKey !== null && (body === null || body === d?.body))}>
                <Icon of={Pencil} />
                {dpiaKey === null ? "Save it" : "Save the next version"}
              </Button>
              {dpiaKey !== null && (
                <Button variant="primary" onClick={() => approve.mutate()} disabled={busy || body !== null || !d?.unapproved} title={helpText("data-protection.dpia-approve").tooltip}>
                  <Icon of={ShieldCheck} />
                  Approve {d?.version === 0 ? "Fyniq's draft" : `version ${d?.version}`}
                </Button>
              )}
            </div>
          )}
        </div>
      )}
    </Dialog>
  );
}

const nextSteps: Record<Request["status"], Request["steps"][number]["step"][]> = {
  received: ["acknowledged", "extended", "answered"],
  acknowledged: ["extended", "answered"],
  extended: ["answered"],
  answered: ["closed"],
  closed: [],
};
const stepWords: Record<string, string> = { acknowledged: "Acknowledge it", extended: "Extend it, telling them in writing", answered: "Answer it", closed: "Close it" };

// RequestsTab is the register of access and correction requests (CMP-PD-04): each due 30 days after receipt or by its
// written extension, with its steps; the Compliance officer records one from outside, takes each step, and reads the
// personal data held about its subject (an audited read).
function RequestsTab() {
  const id = useId();
  const { session } = useSignedIn();
  const officer = (session.roles ?? []).includes("dpo");
  const list = useQuery({ queryKey: ["pd-requests"], queryFn: requests });
  const [recording, setRecording] = useState(false);
  const [stepping, setStepping] = useState<Request | null>(null);
  const [report, setReport] = useState<{ request: Request; data: Report } | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const readReport = async (r: Request) => {
    const { data, error } = await api.GET("/v1/compliance/personal-data-requests/{request_id}/report", { params: { path: { request_id: r.id } } });
    if (error) return setProblem(problemOf(error));
    setReport({ request: r, data });
  };
  if (list.isError) return <Refusal problem={problemOf(list.error)} title="The requests could not be shown" />;
  if (!list.data) return <p className="fy-small">Loading the requests…</p>;
  return (
    <section className="fy-card" aria-labelledby={`${id}-requests`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-requests`}>
          Access and correction requests
        </h2>
        {officer && (
          <Button size="sm" onClick={() => setRecording(true)}>
            <Icon of={Plus} />
            Record a request
          </Button>
        )}
      </div>
      {problem && <Refusal problem={problem} title="The report could not be read" />}
      {list.data.length === 0 ? (
        <p className="fy-card__body fy-small fy-secondary">No request yet. Members ask in Settings › Profile; record one received by email or letter here.</p>
      ) : (
        <ul className="fy-list" aria-labelledby={`${id}-requests`}>
          {list.data.slice().reverse().map((r) => (
            <li key={r.id} className="fy-list__item">
              <span className="fy-list__main">
                <span className="fy-list__title whitespace-normal">
                  {r.kind === "access" ? "Access" : "Correction"}: {r.details}
                </span>
                <span className="fy-list__meta whitespace-normal">
                  {r.requester || r.subject} · received {date(r.received_on)} · {r.status} · due {date(r.due_on)}
                </span>
              </span>
              {r.overdue && (
                <span className="fy-chip fy-chip--serious">
                  <Icon of={ClockAlert} />
                  Overdue
                </span>
              )}
              {officer && r.subject !== "person" && (
                <Button size="sm" variant="ghost" onClick={() => readReport(r)} aria-label={`Read the data held for ${r.requester || r.subject}`}>
                  Data held
                </Button>
              )}
              {officer && nextSteps[r.status].length > 0 && (
                <Button size="sm" variant="secondary" onClick={() => setStepping(r)} aria-label={`Next step for the request of ${date(r.received_on)}`}>
                  <Icon of={ShieldCheck} />
                  Next step
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      {recording && <RequestForm onClose={() => setRecording(false)} />}
      {stepping && <StepForm request={stepping} onClose={() => setStepping(null)} />}
      {report && (
        <Dialog open onOpenChange={(o) => !o && setReport(null)} eyebrow="Data held" title={report.request.requester || report.request.subject} description={report.data.note}>
          {report.data.items.length === 0 ? (
            <p className="fy-small">Fyniq holds no value about them in the inventory's classes.</p>
          ) : (
            <dl className="fy-dl">
              {report.data.items.map((i) => (
                <div key={i.class + i.field}>
                  <dt>{i.field}</dt>
                  <dd>{i.value}</dd>
                </div>
              ))}
            </dl>
          )}
        </Dialog>
      )}
    </section>
  );
}

// RequestForm records a request received from a contact or anyone else (CMP-PD-04).
function RequestForm({ onClose }: { onClose: () => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [form, setForm] = useState({ kind: "access" as Request["kind"], requester: "", details: "", received_on: "" });
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const save = useMutation({
    mutationFn: async () => {
      const out = await guarded(confirm, () =>
        api.POST("/v1/compliance/personal-data-requests", { params: { header: { "Idempotency-Key": newKey() } },
          body: { kind: form.kind, subject: "person", requester: form.requester.trim(), details: form.details.trim(), received_on: form.received_on || undefined } }),
      );
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      await Promise.all(["pd-requests", "compliance-reviews"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
      onClose();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !save.isPending && onClose()} dismissible={!save.isPending} eyebrow="Requests" title="Record a request"
      description="A request received by email, letter or in person. It is due 30 days after it was received.">
      {problem && <Refusal problem={problem} title="The request was not recorded" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-kind`}>
            They ask to
          </label>
          <select className="fy-select" id={`${id}-kind`} value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value as Request["kind"] })}>
            <option value="access">See their personal data</option>
            <option value="correction">Correct it</option>
          </select>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-requester`}>
            Who asked, and how to reach them
          </label>
          <input className="fy-input" id={`${id}-requester`} value={form.requester} onChange={(e) => setForm({ ...form, requester: e.target.value })} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-details`}>
            What they ask for
          </label>
          <textarea className="fy-textarea" id={`${id}-details`} value={form.details} onChange={(e) => setForm({ ...form, details: e.target.value })} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-received`}>
            Received on (leave empty for today)
          </label>
          <input className="fy-input" type="date" id={`${id}-received`} value={form.received_on} onChange={(e) => setForm({ ...form, received_on: e.target.value })} />
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={save.isPending}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={save.isPending}>
            Record it
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// StepForm adds a request's next step: acknowledged, extended with the day told in writing, answered with its outcome, or
// closed after the answer.
function StepForm({ request, onClose }: { request: Request; onClose: () => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const steps = nextSteps[request.status];
  const [form, setForm] = useState({ step: steps[0]!, note: "", new_due_on: "", outcome: "provided" as NonNullable<Request["steps"][number]["outcome"]> });
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const save = useMutation({
    mutationFn: async () => {
      const body = { step: form.step, note: form.note.trim() || undefined, new_due_on: form.step === "extended" ? form.new_due_on : undefined,
        outcome: form.step === "answered" ? form.outcome : undefined };
      const out = await guarded(confirm, () =>
        api.POST("/v1/compliance/personal-data-requests/{request_id}/steps", {
          params: { path: { request_id: request.id }, header: { "Idempotency-Key": newKey(), "If-Match": `"s${request.steps.length}"` } }, body }),
      );
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      await Promise.all(["pd-requests", "compliance-reviews"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
      onClose();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !save.isPending && onClose()} dismissible={!save.isPending} eyebrow="Requests" title="Next step"
      description={`Due by ${date(request.due_on)}. An extension is told to the requester in writing within the first 30 days.`}>
      {problem && <Refusal problem={problem} title="The step was not recorded" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-step`}>
            Step
          </label>
          <select className="fy-select" id={`${id}-step`} value={form.step} onChange={(e) => setForm({ ...form, step: e.target.value as typeof form.step })}>
            {steps.map((s) => (
              <option key={s} value={s}>
                {stepWords[s]}
              </option>
            ))}
          </select>
        </div>
        {form.step === "extended" && (
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-due`}>
              The day you told them
            </label>
            <input className="fy-input" type="date" id={`${id}-due`} value={form.new_due_on} onChange={(e) => setForm({ ...form, new_due_on: e.target.value })} />
          </div>
        )}
        {form.step === "answered" && (
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-outcome`}>
              Outcome
            </label>
            <select className="fy-select" id={`${id}-outcome`} value={form.outcome} onChange={(e) => setForm({ ...form, outcome: e.target.value as typeof form.outcome })}>
              <option value="provided">Their data was provided</option>
              <option value="corrected">It was corrected</option>
              <option value="partly">Partly met</option>
              <option value="refused">Refused, with the reason given</option>
            </select>
          </div>
        )}
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-note`}>
            {form.step === "extended" ? "What you told them" : "Note (optional)"}
          </label>
          <textarea className="fy-textarea" id={`${id}-note`} value={form.note} onChange={(e) => setForm({ ...form, note: e.target.value })} />
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={save.isPending}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={save.isPending}>
            Record the step
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

const classWords: Record<BreachClass, string> = {
  names: "Names", identification_numbers: "NRIC, FIN or other identification numbers", pay: "Pay", bank_accounts: "Bank account numbers",
  card_numbers: "Card numbers", credentials: "Sign-in identifiers with passwords", contact_details: "Contact details", other: "Other",
};
// breachNext are the steps the database accepts next (FY315): each once, nothing after closing, the decision after the
// assessment, the PDPC only for a breach decided notifiable, and closing after the decision and any PDPC report.
function breachNext(b: Breach): string[] {
  const done = new Set<string>(b.steps.map((s) => s.step));
  if (done.has("closed")) return [];
  const notifiable = b.notifiable === true;
  return ["assessed", "decided", "pdpc_notified", "individuals_notified", "contained", "closed"].filter(
    (s) =>
      !done.has(s) &&
      (s !== "decided" || done.has("assessed")) &&
      (s !== "pdpc_notified" || notifiable) &&
      (s !== "closed" || (done.has("decided") && (!notifiable || done.has("pdpc_notified")))),
  );
}
const breachStepWords: Record<string, string> = {
  assessed: "Assessed", decided: "Decided whether it is notifiable", pdpc_notified: "Notified the PDPC", individuals_notified: "Told the people affected",
  contained: "Contained", closed: "Closed",
};

// BreachesTab is the breach register with its PDPA timers (CMP-PD-05), and the tabletop exercises (CTL-DP-06): the
// assessment due 30 days after awareness, and, once decided notifiable, the PDPC's 3 calendar days.
function BreachesTab() {
  const id = useId();
  const { session } = useSignedIn();
  const officer = (session.roles ?? []).includes("dpo");
  const list = useQuery({ queryKey: ["breaches"], queryFn: breaches });
  const ex = useQuery({ queryKey: ["breach-exercises"], queryFn: exercises });
  const [recording, setRecording] = useState(false);
  const [stepping, setStepping] = useState<Breach | null>(null);
  const [exercising, setExercising] = useState(false);
  if (list.isError) return <Refusal problem={problemOf(list.error)} title="The breach register could not be shown" />;
  if (!list.data) return <p className="fy-small">Loading the breach register…</p>;
  return (
    <>
      <section className="fy-card" aria-labelledby={`${id}-breaches`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-breaches`}>
            Breach register
          </h2>
          {officer && (
            <Button size="sm" onClick={() => setRecording(true)}>
              <Icon of={Plus} />
              Record a breach
            </Button>
          )}
        </div>
        {list.data.length === 0 ? (
          <p className="fy-card__body fy-small fy-secondary">No breach recorded. Record one, or a suspected one, as soon as the company becomes aware of it: its 30 days run from then.</p>
        ) : (
          <ul className="fy-list" aria-labelledby={`${id}-breaches`}>
            {list.data.slice().reverse().map((b) => (
              <li key={b.id} className="fy-list__item">
                <span className="fy-list__main">
                  <span className="fy-list__title whitespace-normal">{b.title}</span>
                  <span className="fy-list__meta whitespace-normal">
                    {b.individuals} {b.individuals === 1 ? "person" : "people"} · {b.classes.map((c) => classWords[c]).join(", ")} · {breachStepWords[b.status] ?? "Recorded"}
                  </span>
                  <span className="fy-list__meta whitespace-normal">
                    {!b.steps.some((s) => s.step === "assessed") ? `Assess by ${date(b.assessment_due_on)}` : ""}
                    {b.pdpc_due_on && !b.steps.some((s) => s.step === "pdpc_notified") ? `Notify the PDPC by ${date(b.pdpc_due_on)}` : ""}
                    {b.notifiable === false ? "Decided not notifiable" : ""}
                  </span>
                </span>
                {b.prescribed && (
                  <span className="fy-chip fy-chip--warning" title="A name or identification number with pay, card or bank account numbers, or credentials: deemed to cause significant harm">
                    <Icon of={TriangleAlert} />
                    Prescribed data
                  </span>
                )}
                {(b.assessment_overdue || b.pdpc_overdue) && (
                  <span className="fy-chip fy-chip--critical">
                    <Icon of={ClockAlert} />
                    {b.pdpc_overdue ? "PDPC overdue" : "Assessment overdue"}
                  </span>
                )}
                {officer && breachNext(b).length > 0 && (
                  <Button size="sm" variant="secondary" onClick={() => setStepping(b)} aria-label={`Next step for ${b.title}`}>
                    <Icon of={ShieldCheck} />
                    Next step
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>
      <section className="fy-card" aria-labelledby={`${id}-exercises`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-exercises`} title={helpText("data-protection.tabletop").tooltip}>
            Tabletop exercises
          </h2>
          {officer && (
            <Button size="sm" onClick={() => setExercising(true)}>
              <Icon of={Plus} />
              Record an exercise
            </Button>
          )}
        </div>
        {(ex.data ?? []).length === 0 ? (
          <p className="fy-card__body fy-small fy-secondary">No exercise recorded. Rehearse the breach runbook twice a year.</p>
        ) : (
          <ul className="fy-list" aria-labelledby={`${id}-exercises`}>
            {(ex.data ?? []).slice().reverse().map((x) => (
              <li key={x.id} className="fy-list__item">
                <span className="fy-list__main">
                  <span className="fy-list__title whitespace-normal">
                    {date(x.held_on)}: {x.scenario}
                  </span>
                  <span className="fy-list__meta whitespace-normal">{x.findings}</span>
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>
      {recording && <BreachForm onClose={() => setRecording(false)} />}
      {stepping && <BreachStepForm breach={stepping} onClose={() => setStepping(null)} />}
      {exercising && <ExerciseForm onClose={() => setExercising(false)} />}
    </>
  );
}

// BreachForm records a breach, or a suspected one, from when the company became aware of it (CMP-PD-05).
function BreachForm({ onClose }: { onClose: () => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [form, setForm] = useState({ title: "", description: "", individuals: "0", classes: [] as BreachClass[] });
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const save = useMutation({
    mutationFn: async () => {
      const out = await guarded(confirm, () =>
        api.POST("/v1/compliance/breaches", { params: { header: { "Idempotency-Key": newKey() } },
          body: { title: form.title.trim(), description: form.description.trim(), individuals: Number(form.individuals) || 0, classes: form.classes } }),
      );
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      await Promise.all(["breaches", "compliance-reviews"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
      onClose();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  const toggle = (c: BreachClass) => setForm({ ...form, classes: form.classes.includes(c) ? form.classes.filter((x) => x !== c) : [...form.classes, c] });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !save.isPending && onClose()} dismissible={!save.isPending} eyebrow="Breach register" title="Record a breach"
      description="Record it as soon as the company becomes aware of it: the assessment is due 30 days from now. The Compliance officer, the Owner and the Auditor are told at once.">
      {problem && <Refusal problem={problem} title="The breach was not recorded" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-title`}>
            What happened, in a few words
          </label>
          <input className="fy-input" id={`${id}-title`} value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-description`}>
            What is known
          </label>
          <textarea className="fy-textarea" id={`${id}-description`} value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-individuals`}>
            How many people, as best known
          </label>
          <input className="fy-input" type="number" min={0} id={`${id}-individuals`} value={form.individuals} onChange={(e) => setForm({ ...form, individuals: e.target.value })} />
        </div>
        <fieldset className="fy-field">
          <legend className="fy-label">The data involved</legend>
          {(Object.keys(classWords) as BreachClass[]).map((c) => (
            <label key={c} className="fy-check">
              <input type="checkbox" checked={form.classes.includes(c)} onChange={() => toggle(c)} />
              {classWords[c]}
            </label>
          ))}
        </fieldset>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={save.isPending}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={save.isPending || form.classes.length === 0}>
            Record it
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// BreachStepForm adds a breach's next step (CMP-PD-05): assessed; decided notifiable or not (a breach of prescribed data,
// or of 500 or more people, is notifiable); the PDPC notified with its reference; the people told; contained; closed.
function BreachStepForm({ breach, onClose }: { breach: Breach; onClose: () => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const steps = breachNext(breach);
  const forced = breach.prescribed || breach.individuals >= 500;
  const [form, setForm] = useState({ step: steps[0]!, note: "", notifiable: true, reference: "" });
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const save = useMutation({
    mutationFn: async () => {
      const body = { step: form.step as "assessed", note: form.note.trim() || undefined, notifiable: form.step === "decided" ? form.notifiable : undefined,
        reference: form.step === "pdpc_notified" ? form.reference.trim() : undefined };
      const out = await guarded(confirm, () =>
        api.POST("/v1/compliance/breaches/{breach_id}/steps", { params: { path: { breach_id: breach.id }, header: { "Idempotency-Key": newKey(), "If-Match": `"s${breach.steps.length}"` } }, body }),
      );
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      await Promise.all(["breaches", "compliance-reviews"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
      onClose();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !save.isPending && onClose()} dismissible={!save.isPending} eyebrow={breach.title} title="Next step"
      description={!breach.steps.some((s) => s.step === "assessed") ? `Assess it by ${date(breach.assessment_due_on)}.` : breach.pdpc_due_on ? `Notify the PDPC by ${date(breach.pdpc_due_on)}.` : undefined}>
      {problem && <Refusal problem={problem} title="The step was not recorded" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-step`}>
            Step
          </label>
          <select className="fy-select" id={`${id}-step`} value={form.step} onChange={(e) => setForm({ ...form, step: e.target.value })}>
            {steps.map((s) => (
              <option key={s} value={s}>
                {breachStepWords[s]}
              </option>
            ))}
          </select>
        </div>
        {form.step === "decided" && (
          <fieldset className="fy-field" aria-describedby={`${id}-forced`}>
            <legend className="fy-label">Is it notifiable?</legend>
            <label className="fy-check">
              <input type="radio" name={`${id}-n`} checked={form.notifiable} onChange={() => setForm({ ...form, notifiable: true })} />
              Yes: likely to cause significant harm, or 500 or more people
            </label>
            <label className="fy-check">
              <input type="radio" name={`${id}-n`} checked={!form.notifiable} disabled={forced} onChange={() => setForm({ ...form, notifiable: false })} />
              No
            </label>
            {forced && (
              <span className="fy-help" id={`${id}-forced`}>
                Its data is prescribed, or it affects 500 or more people: the law deems it notifiable.
              </span>
            )}
          </fieldset>
        )}
        {form.step === "pdpc_notified" && (
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-ref`}>
              The PDPC's reference
            </label>
            <input className="fy-input" id={`${id}-ref`} value={form.reference} onChange={(e) => setForm({ ...form, reference: e.target.value })} />
          </div>
        )}
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-note`}>
            Note (optional)
          </label>
          <textarea className="fy-textarea" id={`${id}-note`} value={form.note} onChange={(e) => setForm({ ...form, note: e.target.value })} />
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={save.isPending}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={save.isPending || (form.step === "pdpc_notified" && form.reference.trim() === "")}>
            Record the step
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// ExerciseForm records a tabletop exercise of the breach runbook (CTL-DP-06).
function ExerciseForm({ onClose }: { onClose: () => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [form, setForm] = useState({ held_on: "", scenario: "", findings: "" });
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const save = useMutation({
    mutationFn: async () => {
      const out = await guarded(confirm, () =>
        api.POST("/v1/compliance/breach-exercises", { params: { header: { "Idempotency-Key": newKey() } },
          body: { held_on: form.held_on, scenario: form.scenario.trim(), findings: form.findings.trim() } }),
      );
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      await Promise.all(["breach-exercises", "compliance-reviews"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
      onClose();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !save.isPending && onClose()} dismissible={!save.isPending} eyebrow="Breach register" title="Record a tabletop exercise"
      description="A rehearsal of the breach runbook: the next is due 6 months after it.">
      {problem && <Refusal problem={problem} title="The exercise was not recorded" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-held`}>
            Held on
          </label>
          <input className="fy-input" type="date" id={`${id}-held`} value={form.held_on} onChange={(e) => setForm({ ...form, held_on: e.target.value })} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-scenario`}>
            The breach rehearsed
          </label>
          <textarea className="fy-textarea" id={`${id}-scenario`} value={form.scenario} onChange={(e) => setForm({ ...form, scenario: e.target.value })} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-findings`}>
            What it found and what changed
          </label>
          <textarea className="fy-textarea" id={`${id}-findings`} value={form.findings} onChange={(e) => setForm({ ...form, findings: e.target.value })} />
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={save.isPending}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={save.isPending || !form.held_on}>
            Record it
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

const basisWords: Record<Transfer["basis"], string> = {
  contract: "A contract with the recipient", binding_corporate_rules: "Binding corporate rules", other_instrument: "Another law or binding instrument",
  certification: "A certification such as APEC CBPR", consent: "The person's consent",
};

// TransfersTab is the register of overseas transfers (CMP-PD-06): Fyniq's flows that leave Singapore, each with the
// assessment that covers it, and the company's own transfers, each reviewed by its next review day.
function TransfersTab() {
  const id = useId();
  const { session } = useSignedIn();
  const officer = (session.roles ?? []).includes("dpo");
  const reg = useQuery({ queryKey: ["transfers"], queryFn: transfers });
  const [editing, setEditing] = useState<{ transfer: Transfer | null; flow: Flow | null } | null>(null);
  if (reg.isError) return <Refusal problem={problemOf(reg.error)} title="The transfers could not be shown" />;
  if (!reg.data) return <p className="fy-small">Loading the transfers…</p>;
  const abroad = reg.data.flows.filter((f) => f.cross_border);
  return (
    <>
      <section className="fy-card" aria-labelledby={`${id}-flows`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-flows`}>
            Fyniq's flows abroad
          </h2>
        </div>
        <ul className="fy-list" aria-labelledby={`${id}-flows`}>
          {abroad.map((f) => (
            <li key={f.key} className="fy-list__item">
              <span className="fy-list__main">
                <span className="fy-list__title whitespace-normal">{f.what}</span>
                <span className="fy-list__meta whitespace-normal">
                  {f.data} · {f.note}
                </span>
              </span>
              {f.covered_by ? (
                <span className="fy-chip fy-chip--good">
                  <Icon of={ShieldCheck} />
                  Assessed
                </span>
              ) : (
                <>
                  <span className="fy-chip fy-chip--warning">
                    <Icon of={TriangleAlert} />
                    Needs an assessment
                  </span>
                  {officer && (
                    <Button size="sm" variant="secondary" onClick={() => setEditing({ transfer: null, flow: f })} aria-label={`Record the assessment of ${f.what}`}>
                      <Icon of={Plus} />
                      Record the assessment
                    </Button>
                  )}
                </>
              )}
            </li>
          ))}
        </ul>
      </section>
      <section className="fy-card" aria-labelledby={`${id}-transfers`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-transfers`}>
            Transfers
          </h2>
          {officer && (
            <Button size="sm" onClick={() => setEditing({ transfer: null, flow: null })}>
              <Icon of={Plus} />
              Record a transfer
            </Button>
          )}
        </div>
        {reg.data.transfers.length === 0 ? (
          <p className="fy-card__body fy-small fy-secondary">No transfer recorded. Record the assessment of each flow above, and of any transfer abroad the company makes outside Fyniq.</p>
        ) : (
          <ul className="fy-list" aria-labelledby={`${id}-transfers`}>
            {reg.data.transfers.map((t) => (
              <li key={t.transfer_id} className="fy-list__item">
                <span className="fy-list__main">
                  <span className="fy-list__title whitespace-normal">
                    {t.recipient}, {t.countries}
                  </span>
                  <span className="fy-list__meta whitespace-normal">
                    {t.data} · {basisWords[t.basis]} · {t.state === "retired" ? "Retired" : `Review by ${date(t.next_review_on)}`}
                  </span>
                </span>
                {t.overdue ? (
                  <span className="fy-chip fy-chip--serious">
                    <Icon of={ClockAlert} />
                    Review overdue
                  </span>
                ) : (
                  t.review_due && (
                    <span className="fy-chip fy-chip--warning">
                      <Icon of={ClockAlert} />
                      Review due
                    </span>
                  )
                )}
                {officer && t.state === "open" && (
                  <Button size="sm" variant="secondary" onClick={() => setEditing({ transfer: t, flow: null })} aria-label={`Change ${t.recipient}`}>
                    <Icon of={Pencil} />
                    Change
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>
      {editing && <TransferForm transfer={editing.transfer} flow={editing.flow} onClose={() => setEditing(null)} />}
    </>
  );
}

// TransferForm records a transfer's assessment, for one of Fyniq's flows or the company's own, or saves its next
// version: a review with its new day, or its retirement (CMP-PD-06).
function TransferForm({ transfer, flow, onClose }: { transfer: Transfer | null; flow: Flow | null; onClose: () => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [form, setForm] = useState<TransferInput>(
    transfer
      ? { flow: transfer.flow, recipient: transfer.recipient, countries: transfer.countries, data: transfer.data, purpose: transfer.purpose, basis: transfer.basis,
          assessed_on: transfer.assessed_on, assessed_by: transfer.assessed_by, evidence: transfer.evidence, next_review_on: transfer.next_review_on, state: transfer.state }
      : { flow: flow?.key, recipient: "", countries: "", data: flow?.data ?? "", purpose: "", basis: "contract", assessed_on: "", assessed_by: "", evidence: "", next_review_on: "" },
  );
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const save = useMutation({
    mutationFn: async (body: TransferInput) => {
      const out = await guarded(confirm, () =>
        transfer
          ? api.PUT("/v1/compliance/transfers/{transfer_id}", { params: { path: { transfer_id: transfer.transfer_id }, header: { "Idempotency-Key": newKey(), "If-Match": `"v${transfer.version}"` } }, body })
          : api.POST("/v1/compliance/transfers", { params: { header: { "Idempotency-Key": newKey() } }, body }),
      );
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      await Promise.all(["transfers", "compliance-reviews"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
      onClose();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate(form);
  };
  const field = (key: "recipient" | "countries" | "data" | "purpose" | "assessed_by" | "evidence", label: string) => (
    <div className="fy-field">
      <label className="fy-label" htmlFor={`${id}-${key}`}>
        {label}
      </label>
      <input className="fy-input" id={`${id}-${key}`} value={form[key]} onChange={(e) => setForm({ ...form, [key]: e.target.value })} />
    </div>
  );
  const day = (key: "assessed_on" | "next_review_on", label: string) => (
    <div className="fy-field">
      <label className="fy-label" htmlFor={`${id}-${key}`}>
        {label}
      </label>
      <input className="fy-input" type="date" id={`${id}-${key}`} value={form[key]} onChange={(e) => setForm({ ...form, [key]: e.target.value })} />
    </div>
  );
  return (
    <Dialog open onOpenChange={(o) => !o && !save.isPending && onClose()} dismissible={!save.isPending} eyebrow={flow?.what ?? transfer?.recipient ?? "Transfers"}
      title={transfer ? "Change the transfer" : "Record a transfer"}
      description="Personal data that leaves Singapore must stay protected to the PDPA's standard: say what protects it there. Each change is kept as a version.">
      {problem && <Refusal problem={problem} title="The transfer was not saved" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        {field("recipient", "Who receives the data")}
        {field("countries", "Where they process it")}
        {field("data", "The personal data")}
        {field("purpose", "Why")}
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-basis`}>
            What protects it abroad
          </label>
          <select className="fy-select" id={`${id}-basis`} value={form.basis} onChange={(e) => setForm({ ...form, basis: e.target.value as Transfer["basis"] })}>
            {(Object.keys(basisWords) as Transfer["basis"][]).map((b) => (
              <option key={b} value={b}>
                {basisWords[b]}
              </option>
            ))}
          </select>
        </div>
        {day("assessed_on", "Assessed on")}
        {field("assessed_by", "Assessed by")}
        {field("evidence", "Where the contract, rules or certificate are kept")}
        {day("next_review_on", "Next review")}
        <div className="fy-row justify-end gap-2">
          {transfer && transfer.state === "open" && (
            <Button variant="ghost" type="button" onClick={() => save.mutate({ ...form, state: "retired" })} disabled={save.isPending}>
              <Icon of={Archive} />
              Retire it
            </Button>
          )}
          <Button variant="ghost" type="button" onClick={onClose} disabled={save.isPending}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={save.isPending}>
            {transfer ? "Save the change" : "Record it"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function DPOTab() {
  const id = useId();
  const p = useQuery({ queryKey: ["profile", "dpo"], queryFn: profile });
  const [status, setStatus] = useState("");
  return (
    <>
      <p className="fy-small" role="status">
        {status}
      </p>
      {p.isError && <Refusal problem={problemOf(p.error)} title="The DPO's contact could not be shown" />}
      {p.data && <DPOContact profile={p.data} onCopied={setStatus} />}
      <section className="fy-card" aria-labelledby={`${id}-published`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-published`}>
            Published policies and notices
          </h2>
        </div>
        <ul className="fy-list" aria-labelledby={`${id}-published`}>
          <li>
            <Link className="fy-list__item" to="/compliance/risks">
              <span className="fy-list__main">
                <span className="fy-list__title">The data protection policy</span>
                <span className="fy-list__meta">In the policy library, under Risks and policies</span>
              </span>
              <Icon of={ChevronRight} />
            </Link>
          </li>
          <li>
            <Link className="fy-list__item" to="/compliance/privacy/notices">
              <span className="fy-list__main">
                <span className="fy-list__title">The privacy notices</span>
                <span className="fy-list__meta">For members and for contacts, under Privacy notices and consent</span>
              </span>
              <Icon of={ChevronRight} />
            </Link>
          </li>
        </ul>
      </section>
      <section className="fy-card" aria-labelledby={`${id}-penalty`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-penalty`}>
            The penalty
          </h2>
        </div>
        <p className="fy-card__body fy-small">
          The PDPC may fine an organisation that breaches the PDPA's data protection provisions up to 10% of its annual turnover in Singapore when that is above S$10
          million, otherwise up to S$1 million (PDPA s48J(3)). Keep a data protection risk in the{" "}
          <Link to="/compliance/risks">risk register</Link>.
        </p>
      </section>
    </>
  );
}
