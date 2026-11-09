import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Building, CalendarClock, CalendarDays, ChevronLeft, ChevronRight, CircleCheck, CircleDashed, ClockAlert, ExternalLink, List, Receipt, Stamp, TriangleAlert, Undo2, Wallet } from "lucide-react";
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
import { EvidencePanel } from "../accounting/JournalEvidence";
import { today } from "../purchases/BillsBoard";

export type Item = components["schemas"]["StatutoryItem"];
type Calendar = components["schemas"]["StatutoryCalendarView"];
type Filing = components["schemas"]["StatutoryFilingView"];
type FilingDraft = components["schemas"]["StatutoryFilingDraft"];

// Who records an item and withdraws a record: C on the Statutory calendar row (Appendix D; the owner's decision of 7 Nov
// 2026).
export const recorders = ["owner", "controller", "accountant", "tax_agent"];

export async function calendar(from?: string, to?: string): Promise<Calendar> {
  const query: { from?: string; to?: string } = {};
  if (from) query.from = from;
  if (to) query.to = to;
  const { data, error } = await api.GET("/v1/compliance/calendar", { params: { query } });
  if (error) throw error;
  return data;
}

// The statuses of an item, in plain words, as a chip.
export const itemWords: Record<Item["status"], [string, string, typeof CircleCheck]> = {
  overdue: ["Overdue", "fy-chip--serious", ClockAlert],
  due_soon: ["Due soon", "fy-chip--serious", ClockAlert],
  open: ["Open", "fy-chip--neutral", CalendarClock],
  filed: ["Filed", "fy-chip--good", CircleCheck],
  not_required: ["Not required", "fy-chip--good", CircleCheck],
  not_assessed: ["Not assessed", "fy-chip--warning", CircleDashed],
};

const areas = [
  ["", "All areas", null],
  ["tax", "Tax", Receipt],
  ["payroll", "Payroll", Wallet],
  ["corporate", "Corporate", Building],
] as const;

export function ItemChip({ it }: { it: Item }) {
  const [label, tone, icon] = itemWords[it.status];
  return (
    <span className={`fy-chip ${tone}`} title={helpText(`calendar.${it.status}`).tooltip}>
      <Icon of={icon} />
      {label}
    </span>
  );
}

const monthOf = (iso: string) => new Date(`${iso.slice(0, 7)}-01T00:00:00`).toLocaleDateString("en-SG", { month: "long", year: "numeric" });

// RecordForm drafts what was done about an item (FR-TAX-07, design D6): filed with its day and reference, then given
// its acknowledgement and recorded; or not required with the reason, recorded at once.
function RecordForm({ it, outcome, onClose, onDone }: { it: Item; outcome: FilingDraft["outcome"]; onClose: () => void; onDone: (text: string) => void }) {
  const id = useId();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const [form, setForm] = useState({ filed_on: today(), reference: "", reason: "", note: "" });
  const [draft, setDraft] = useState<Filing | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);
  const record = async (f: Filing) => {
    const out = await guarded(confirm, () =>
      api.POST("/v1/compliance/filings/{filing_id}/record", { params: { path: { filing_id: f.id }, header: { "If-Match": `"${f.version}-draft"`, "Idempotency-Key": newKey() } } }),
    );
    if (!out.data) {
      setProblem(problemOf(out.error));
      return false;
    }
    await queries.invalidateQueries({ queryKey: ["calendar"] });
    onDone(`${it.title}: recorded as ${outcome === "filed" ? "filed" : "not required"}.`);
    onClose();
    return true;
  };
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const body: FilingDraft = { key: it.key, outcome };
    if (outcome === "filed") {
      body.filed_on = form.filed_on;
      body.reference = form.reference.trim();
    } else {
      body.reason = form.reason.trim();
    }
    if (form.note.trim()) body.note = form.note.trim();
    const out = await guarded(confirm, () => api.POST("/v1/compliance/filings", { params: { header: { "Idempotency-Key": key } }, body }));
    if (!out.data) {
      setBusy(false);
      return setProblem(problemOf(out.error));
    }
    if (outcome === "not_required") {
      await record(out.data);
    } else {
      setDraft(out.data);
    }
    setBusy(false);
  };
  const refresh = async () => {
    if (!draft) return;
    const { data } = await api.GET("/v1/compliance/filings/{filing_id}", { params: { path: { filing_id: draft.id } } });
    if (data) setDraft(data);
  };
  const h = helpText(outcome === "filed" ? "statutory-filing.record" : "statutory-filing.not-required");
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && onClose()} dismissible={!busy} eyebrow="Statutory calendar" title={h.label} description={it.title}>
      {problem && <Refusal problem={problem} title="The item was not recorded" />}
      {!draft ? (
        <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
          {outcome === "filed" ? (
            <>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-on`}>
                  Filed or paid on
                </label>
                <input className="fy-input" id={`${id}-on`} type="date" value={form.filed_on} max={today()} onChange={(e) => setForm({ ...form, filed_on: e.target.value })} />
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-ref`}>
                  Acknowledgement or transaction number
                </label>
                <input className="fy-input" id={`${id}-ref`} maxLength={100} value={form.reference} onChange={(e) => setForm({ ...form, reference: e.target.value })} />
              </div>
            </>
          ) : (
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-reason`}>
                Why it is not required
              </label>
              <textarea className="fy-textarea" id={`${id}-reason`} maxLength={500} value={form.reason} onChange={(e) => setForm({ ...form, reason: e.target.value })}
                aria-describedby={`${id}-reason-help`} />
              <span className="fy-help" id={`${id}-reason-help`}>
                For example: revenue of $5 million or below and ECI nil, so ECI is waived
              </span>
            </div>
          )}
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-note`}>
              Note (optional)
            </label>
            <textarea className="fy-textarea" id={`${id}-note`} maxLength={1000} value={form.note} onChange={(e) => setForm({ ...form, note: e.target.value })} />
          </div>
          <div className="fy-row justify-end gap-2">
            <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
              Cancel
            </Button>
            <Button variant="primary" type="submit" disabled={busy}>
              {outcome === "filed" ? "Next: the acknowledgement" : "Record it"}
            </Button>
          </div>
        </form>
      ) : (
        <div className="fy-stack fy-stack--3">
          <p className="fy-small">
            Filed on {date(draft.filed_on)}, {draft.reference}. Attach the acknowledgement, then record it.
          </p>
          {draft.checks.map((c) => (
            <p key={c.code} className="fy-small fy-secondary">
              {c.message}. {c.fix}.
            </p>
          ))}
          <EvidencePanel record={{ kind: "statutory-filing", id: draft.id, etag: `"${draft.version}-draft"` }} draft mayAttach onChanged={() => void refresh()} />
          <div className="fy-row justify-end gap-2">
            <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
              Later
            </Button>
            <Button variant="primary" type="button" disabled={busy || draft.checks.length > 0} onClick={() => void record(draft)}>
              <Icon of={Stamp} />
              Record
            </Button>
          </div>
        </div>
      )}
    </Dialog>
  );
}

// WithdrawForm takes back a live record with the reason: the item is open again.
function WithdrawForm({ it, onClose, onDone }: { it: Item; onClose: () => void; onDone: (text: string) => void }) {
  const id = useId();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const filing = it.filing!;
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/compliance/filings/{filing_id}/withdraw", {
        params: { path: { filing_id: filing.id }, header: { "If-Match": `"${filing.version}-posted"`, "Idempotency-Key": newKey() } },
        body: { reason: reason.trim() },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["calendar"] });
    onDone(`${it.title}: the record is withdrawn, and the item is open again.`);
    onClose();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && onClose()} dismissible={!busy} eyebrow="Statutory calendar" title="Withdraw the record" description={it.title}>
      {problem && <Refusal problem={problem} title="The record was not withdrawn" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-reason`}>
            Why it was wrong
          </label>
          <textarea className="fy-textarea" id={`${id}-reason`} maxLength={500} value={reason} onChange={(e) => setReason(e.target.value)} />
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy}>
            Withdraw it
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// Row is one item: what is due, by when and why, and what can be done about it.
function Row({ it, mayRecord, expert, onRecord, onWithdraw }: { it: Item; mayRecord: boolean; expert: boolean; onRecord: (o: FilingDraft["outcome"]) => void; onWithdraw: () => void }) {
  const live = it.status === "filed" || it.status === "not_required";
  return (
    <li className="fy-list__item" id={it.key}>
      <span className="fy-list__main">
        <span className="fy-list__title whitespace-normal">{it.title}</span>
        <span className="fy-list__meta whitespace-normal">
          {it.due_on ? `Due ${date(it.due_on)}` : it.reason}
          {it.extended ? ", moved by IRAS's extension" : ""}
          {it.rolled ? ", moved to the next working day" : ""}
          {it.filing?.outcome === "filed" ? `. Filed on ${date(it.filing.filed_on)}, ${it.filing.reference}` : ""}
          {it.filing?.outcome === "not_required" ? `. Not required: ${it.filing.reason}` : ""}
        </span>
        {expert && it.why && <span className="fy-small fy-muted whitespace-normal">{it.why}{it.source ? ` (${it.source})` : ""}</span>}
      </span>
      <span className="fy-row fy-row--wrap gap-2">
        <ItemChip it={it} />
        {it.register && it.link && (
          <a className="fy-btn fy-btn--ghost fy-btn--sm" href={it.link} title={helpText("calendar.register").tooltip}>
            <Icon of={ExternalLink} />
            Open its record
          </a>
        )}
        {mayRecord && !it.register && it.status !== "not_assessed" && !live && (
          <>
            <Button size="sm" onClick={() => onRecord("filed")} title={helpText("statutory-filing.record").tooltip}>
              <Icon of={Stamp} />
              Record as filed
            </Button>
            <Button size="sm" variant="ghost" onClick={() => onRecord("not_required")} title={helpText("statutory-filing.not-required").tooltip}>
              Not required
            </Button>
          </>
        )}
        {mayRecord && live && it.filing && (
          <Button size="sm" variant="ghost" onClick={onWithdraw} title={helpText("statutory-filing.withdraw").tooltip}>
            <Icon of={Undo2} />
            Withdraw
          </Button>
        )}
      </span>
    </li>
  );
}

// Month is the calendar of one month, Monday first, with each item on its due day.
function Month({ items, month, onMove }: { items: Item[]; month: string; onMove: (by: number) => void }) {
  const first = new Date(`${month}-01T00:00:00`);
  const lead = (first.getDay() + 6) % 7;
  const daysIn = new Date(first.getFullYear(), first.getMonth() + 1, 0).getDate();
  const now = today();
  const cells: (string | null)[] = [...Array(lead).fill(null), ...Array.from({ length: daysIn }, (_, i) => `${month}-${String(i + 1).padStart(2, "0")}`)];
  while (cells.length % 7 !== 0) cells.push(null);
  return (
    <section className="fy-card" aria-labelledby="month-title">
      <div className="fy-card__head">
        <button className="fy-iconbtn" type="button" aria-label="Previous month" onClick={() => onMove(-1)}>
          <Icon of={ChevronLeft} />
        </button>
        <h2 className="fy-h3" id="month-title">
          {monthOf(month)}
        </h2>
        <button className="fy-iconbtn" type="button" aria-label="Next month" onClick={() => onMove(1)}>
          <Icon of={ChevronRight} />
        </button>
      </div>
      <div className="fy-card__body">
        <div className="fy-cal" role="table" aria-label={monthOf(month)}>
          {["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"].map((d) => (
            <div key={d} className="fy-cal__dow" role="columnheader">
              {d}
            </div>
          ))}
          {cells.map((d, i) =>
            d ? (
              <div key={d} role="cell" className={`fy-cal__day${d === now ? " fy-cal__day--today" : ""}`}>
                <span className="fy-cal__n">{Number(d.slice(8))}</span>
                {items
                  .filter((it) => it.due_on === d)
                  .map((it) => (
                    <a key={it.key} href={`#${it.key}`} className={`fy-cal__ev${it.status === "filed" || it.status === "not_required" ? " fy-cal__ev--done" : " fy-cal__ev--due"}`}
                      title={`${it.title}: ${itemWords[it.status][0]}`}>
                      {it.title}
                    </a>
                  ))}
              </div>
            ) : (
              <div key={`blank-${i}`} role="cell" className="fy-cal__day fy-cal__day--muted" />
            ),
          )}
        </div>
      </div>
    </section>
  );
}

// StatutoryCalendar is Compliance › Calendar (docs/design/screens StatutoryCalendar; FR-TAX-07, CMP-02, design D5, D6 and
// D12, Basic and Expert): every filing and payment deadline of the next 12 months and every earlier one overdue, from the
// sg-deadlines pack and the organisation's years, GST periods and settings; what is not assessed first; recording a filing
// with its acknowledgement, or that it is not required, and withdrawing a record. Expert adds each rule's words.
export function StatutoryCalendar() {
  const { session, mode } = useSignedIn();
  const mayRecord = (session.roles ?? []).some((r) => recorders.includes(r));
  const expert = mode === "expert";
  const c = useQuery({ queryKey: ["calendar"], queryFn: () => calendar() });
  const [view, setView] = useState<"list" | "month">("list");
  const [area, setArea] = useState("");
  const [month, setMonth] = useState(today().slice(0, 7));
  const [recording, setRecording] = useState<{ it: Item; outcome: FilingDraft["outcome"] } | null>(null);
  const [withdrawing, setWithdrawing] = useState<Item | null>(null);
  const [status, setStatus] = useState("");
  const all = c.data?.deadlines ?? [];
  const shown = all.filter((it) => !area || it.area === area);
  const missing = shown.filter((it) => it.status === "not_assessed");
  const dated = shown.filter((it) => it.status !== "not_assessed");
  const soon = all.filter((it) => (it.status === "due_soon" || it.status === "open") && it.due_on && it.due_on <= addDays(today(), 30));
  const overdue = all.filter((it) => it.status === "overdue");
  const done = all.filter((it) => it.status === "filed" || it.status === "not_required");
  const months = [...new Set(dated.map((it) => it.due_on!.slice(0, 7)))];
  const move = (by: number) => {
    const d = new Date(`${month}-01T00:00:00`);
    d.setMonth(d.getMonth() + by);
    setMonth(`${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`);
  };
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Compliance</span>
          <h1 className="fy-h1" title={helpText("compliance.calendar").tooltip}>
            Statutory calendar
          </h1>
          <p className="fy-secondary">
            Every filing and payment deadline, worked out from your financial years, GST periods and settings. Reminders go out 30, 14, 7 and 1 days before.
          </p>
        </div>
        <div className="fy-pagehead__actions">
          <div className="fy-segmented" role="radiogroup" aria-label="View">
            <button type="button" role="radio" aria-checked={view === "month"} onClick={() => setView("month")}>
              <Icon of={CalendarDays} />
              Month
            </button>
            <button type="button" role="radio" aria-checked={view === "list"} onClick={() => setView("list")}>
              <Icon of={List} />
              List
            </button>
          </div>
        </div>
      </div>
      <p className="fy-small" role="status">
        {status}
      </p>
      {c.isError && <Refusal problem={problemOf(c.error)} title="The calendar could not be shown" />}
      {c.isPending && <p className="fy-secondary">Loading the calendar…</p>}
      {c.data && (
        <>
          {missing.map((it) => (
            <div key={it.key} className="fy-banner fy-banner--warning" role="status">
              <Icon of={TriangleAlert} size={20} />
              <div className="fy-banner__text">
                <strong>Not assessed: {it.title}</strong>
                <span className="fy-small">{it.reason}</span>
              </div>
            </div>
          ))}
          <div className="fy-grid fy-grid--tiles">
            <div className="fy-tile" title={helpText("calendar.tile-soon").tooltip}>
              <span className="fy-tile__label">
                <Icon of={CalendarClock} />
                Due in the next 30 days
              </span>
              <span className="fy-tile__value fy-num">{soon.length}</span>
            </div>
            <div className="fy-tile" title={helpText("calendar.tile-overdue").tooltip}>
              <span className="fy-tile__label">
                <Icon of={ClockAlert} />
                Overdue
              </span>
              <span className="fy-tile__value fy-num">{overdue.length}</span>
              <span className="fy-tile__meta">Overdue items also show on Checks</span>
            </div>
            <div className="fy-tile" title={helpText("calendar.tile-done").tooltip}>
              <span className="fy-tile__label">
                <Icon of={CircleCheck} />
                Recorded
              </span>
              <span className="fy-tile__value fy-num">{done.length}</span>
              <span className="fy-tile__meta">Each with its acknowledgement or reason</span>
            </div>
          </div>
          <div className="fy-chipset" role="group" aria-label="Filter by area">
            {areas.map(([key, label, icon]) => (
              <button key={key} className="fy-filter" type="button" aria-pressed={area === key} onClick={() => setArea(key)}>
                {icon && <Icon of={icon} />}
                {label}
              </button>
            ))}
          </div>
          {view === "month" ? (
            <Month items={dated} month={month} onMove={move} />
          ) : dated.length === 0 ? (
            <p className="fy-small fy-muted">Nothing is due in the next 12 months.</p>
          ) : (
            months.map((m) => (
              <section key={m} className="fy-card" aria-labelledby={`m-${m}`}>
                <div className="fy-card__head">
                  <h2 className="fy-h3" id={`m-${m}`}>
                    {monthOf(m)}
                  </h2>
                </div>
                <ul className="fy-list" aria-labelledby={`m-${m}`}>
                  {dated
                    .filter((it) => it.due_on!.startsWith(m))
                    .map((it) => (
                      <Row key={it.key} it={it} mayRecord={mayRecord} expert={expert} onRecord={(outcome) => setRecording({ it, outcome })} onWithdraw={() => setWithdrawing(it)} />
                    ))}
                </ul>
              </section>
            ))
          )}
          {c.data.starts && (
            <p className="fy-small fy-muted">
              The rules come from {c.data.pack}, read from IRAS's, CPF Board's, ACRA's and MOM's pages; nothing due before {date(c.data.starts)} is in the calendar.
            </p>
          )}
        </>
      )}
      {recording && <RecordForm it={recording.it} outcome={recording.outcome} onClose={() => setRecording(null)} onDone={setStatus} />}
      {withdrawing && <WithdrawForm it={withdrawing} onClose={() => setWithdrawing(null)} onDone={setStatus} />}
    </div>
  );
}

export function addDays(iso: string, n: number): string {
  const d = new Date(`${iso}T00:00:00`);
  d.setDate(d.getDate() + n);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}
