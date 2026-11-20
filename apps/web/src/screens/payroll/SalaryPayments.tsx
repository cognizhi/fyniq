import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { Ban, CircleCheck, CircleX, Download, Hourglass, Send, TriangleAlert, UserCheck, Wallet } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { BankMatch } from "./BankMatch";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";

export type SalaryBatch = components["schemas"]["SalaryBatch"];
type Line = NonNullable<SalaryBatch["lines"]>[number];

// Who acts on a salary batch (Appendix D's "Pay runs, employee data"): the Payroll administrator prepares and asks (C), the Owner approves (A), the Payment releaser releases (R). The API decides.
export const salaryPreparers = ["payroll_administrator"];
export const salaryReleasers = ["payment_releaser"];

const monthWords = (m: string) => date(`${m}-01`).replace(/^\d+ /, "");

// SalaryState is a batch's state as a chip: icon and words, never colour alone.
export function SalaryState({ batch }: { batch: SalaryBatch }) {
  const words: Record<SalaryBatch["state"], [string, string, typeof Hourglass]> = {
    received: ["Arrived, not prepared", "neutral", Hourglass],
    prepared: ["Prepared, not approved", "info", UserCheck],
    approved: ["Approved, ready to release", "good", CircleCheck],
    released_in_part: ["Released in part", "serious", Send],
    released: ["Released, not yet paid", "serious", Send],
    paid_in_part: ["Paid in part", "info", Wallet],
    paid: ["Paid", "good", Wallet],
    cancelled: ["Cancelled", "neutral", Ban],
  };
  if (batch.status === "approved" && batch.reapproval_needed)
    return (
      <span className="fy-chip fy-chip--critical">
        <Icon of={TriangleAlert} />
        Needs approving again
      </span>
    );
  const [label, tone, icon] = words[batch.state];
  return (
    <span className={`fy-chip fy-chip--${tone}`}>
      <Icon of={icon} />
      {label}
    </span>
  );
}

const waitingWords: Record<NonNullable<Line["waiting"]>, string> = {
  held: "Held for tax clearance",
  hold_unknown: "Nemura has not said whether they are held",
  bank_details: "Bank details changed: waits for the new line",
  unknown_outcome: "An unknown outcome blocks a new payment",
};

function LineState({ line }: { line: Line }) {
  if (line.waiting)
    return (
      <span className="fy-chip fy-chip--warning">
        <Icon of={Ban} />
        {waitingWords[line.waiting]}
      </span>
    );
  const done = line.state === "paid";
  const bad = line.state === "rejected" || line.state === "returned" || line.state === "unknown";
  return (
    <span className={`fy-chip fy-chip--${done ? "good" : bad ? "critical" : line.state === "scheduled" ? "neutral" : "serious"}`}>
      <Icon of={done ? CircleCheck : bad ? CircleX : Hourglass} />
      {line.state === "scheduled" ? "Scheduled" : line.state.charAt(0).toUpperCase() + line.state.slice(1)}
    </span>
  );
}

// SalaryPayments lists the salary batches (docs/design/screens SalaryBatch; FR-PAY-11, FR-PPL-17): each with its run, state, total and counts, and never a name, account or line.
export function SalaryPayments() {
  useSignedIn();
  const q = useQuery({
    queryKey: ["salary-batches"],
    queryFn: async () => {
      const r = await api.GET("/v1/salary-batches", {});
      if (r.error) throw r.error;
      return r.data.items;
    },
  });
  if (q.isError) return <Refusal problem={problemOf(q.error)} title="The salary payments could not be shown" />;
  const items = q.data;
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <span>Payroll</span>
          </nav>
          <h1 className="fy-h1">Salary payments</h1>
          <p className="fy-secondary">Salary lines arrive from Nemura with each approved pay run. They are approved and released here like any payment batch.</p>
        </div>
      </div>
      <ReviewBanner screen="salary-payments" />
      {!items && <p className="fy-secondary">Loading the salary payments…</p>}
      {items && items.length === 0 && <p className="fy-secondary">No pay run has sent its salary lines yet. They arrive when a pay run is approved in Nemura and posted here.</p>}
      {items && items.length > 0 && (
        <table className="fy-table" aria-label="Salary batches">
          <thead>
            <tr>
              <th scope="col">Batch</th>
              <th scope="col">Wage month</th>
              <th scope="col">State</th>
              <th scope="col">People</th>
              <th scope="col" className="fy-num">
                Total (SGD)
              </th>
            </tr>
          </thead>
          <tbody>
            {items.map((b) => (
              <tr key={b.id}>
                <th scope="row">
                  <Link to="/payroll/salary-payments/$batchId" params={{ batchId: b.id }}>
                    {b.number}
                  </Link>
                </th>
                <td>{monthWords(b.wage_month)}</td>
                <td>
                  <SalaryState batch={b} />
                </td>
                <td>
                  {b.line_count}
                  {b.waiting > 0 ? `, ${b.waiting} waiting` : ""}
                </td>
                <td className="fy-num">{formatAmount(b.total.amount)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

// Prepare names where and when a salary batch is paid from.
function Prepare({ batch, etag, onClose, onDone }: { batch: SalaryBatch; etag: string; onClose: () => void; onDone: (what: string) => void }) {
  const id = useId();
  const h = helpText("salary.prepare");
  const accounts = useQuery({
    queryKey: ["money-accounts"],
    queryFn: async () => {
      const r = await api.GET("/v1/money-accounts", {});
      if (r.error) throw r.error;
      return r.data.items.filter((m) => m.kind === "bank" && m.currency === "SGD");
    },
  });
  const [account, setAccount] = useState(batch.money_account_id ?? "");
  const [day, setDay] = useState(batch.payment_date ?? "");
  const [method, setMethod] = useState<"fast" | "giro">(batch.method ?? "giro");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  // A bank whose BIC Fyniq does not know from its code: a person fills it in from the bank's own advice (design D26).
  const lacking = (batch.lines ?? []).filter((l) => l.needs_bic);
  const [bics, setBics] = useState<Record<string, string>>({});
  const filled = Object.fromEntries(Object.entries(bics).filter(([, v]) => v.trim() !== ""));
  const run = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/salary-batches/{batch_id}/prepare", {
      params: { path: { batch_id: batch.id }, header: { "If-Match": etag, "Idempotency-Key": newKey() } },
      body: { money_account_id: account || accounts.data?.[0]?.account_id || "", payment_date: day, method, ...(Object.keys(filled).length > 0 ? { bank_bics: filled } : {}) },
    });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone(`Prepared ${batch.number}: paid ${date(day)} by ${method.toUpperCase()}.`);
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} dismissible={!busy} eyebrow={batch.number} title={h.label} description={h.tooltip}>
      {problem && <Refusal problem={problem} title="The batch was not prepared" />}
      <form className="fy-stack fy-stack--3" onSubmit={(e) => void run(e)} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-account`}>
            Paid from
          </label>
          <select className="fy-select" id={`${id}-account`} value={account} onChange={(e) => setAccount(e.target.value)}>
            {(accounts.data ?? []).map((m) => (
              <option key={m.id} value={m.account_id}>
                {m.name}
              </option>
            ))}
          </select>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-day`}>
            Payment date
          </label>
          <input className="fy-input" id={`${id}-day`} type="date" value={day} onChange={(e) => setDay(e.target.value)} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-method`}>
            Method
          </label>
          <select className="fy-select" id={`${id}-method`} value={method} onChange={(e) => setMethod(e.target.value as "fast" | "giro")}>
            <option value="giro">GIRO: the next working days</option>
            <option value="fast">FAST: within minutes, up to the bank's limit</option>
          </select>
        </div>
        {lacking.length > 0 && (
          <fieldset className="fy-field">
            <legend className="fy-label">Banks Fyniq does not know</legend>
            <span className="fy-hint">Fyniq looks a bank's BIC up from the code Nemura sends. For these people it has none: fill it in from the bank's own advice, 8 or 11 letters and digits.</span>
            {lacking.map((l) => (
              <label key={l.line_id} className="fy-row gap-2">
                <span>{l.person_name ?? l.person_id}</span>
                <input className="fy-input" aria-label={`BIC for ${l.person_name ?? l.person_id}`} value={bics[l.line_id] ?? ""} onChange={(e) => setBics({ ...bics, [l.line_id]: e.target.value.toUpperCase() })} maxLength={11} />
              </label>
            ))}
          </fieldset>
        )}
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Not now
          </Button>
          <Button variant="primary" type="submit" disabled={busy || !day}>
            {h.label}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// Record says what became of one payment, with a file of evidence (FR-PPL-12): a bank feed pays a line by itself, and nothing else does.
function Record({ batch, line, onClose, onDone }: { batch: SalaryBatch; line: Line; onClose: () => void; onDone: (what: string) => void }) {
  const id = useId();
  const h = helpText("salary.record");
  const confirm = useConfirm();
  const [state, setState] = useState<"sent" | "accepted" | "paid" | "rejected" | "returned" | "unknown">("paid");
  const [evidence, setEvidence] = useState("");
  const [day, setDay] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const run = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/salary-batches/{batch_id}/lines/{line_id}/state", {
        params: { path: { batch_id: batch.id, line_id: line.line_id }, header: { "Idempotency-Key": newKey() } },
        body: { state, evidence_id: evidence.trim(), on: day },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone(`Recorded ${state} for ${line.person_name ?? "the person"}.`);
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} dismissible={!busy} eyebrow={line.person_name ?? batch.number} title={h.label} description={h.tooltip}>
      {problem && <Refusal problem={problem} title="Nothing was recorded" />}
      <form className="fy-stack fy-stack--3" onSubmit={(e) => void run(e)} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-state`}>
            What became of it
          </label>
          <select className="fy-select" id={`${id}-state`} value={state} onChange={(e) => setState(e.target.value as typeof state)}>
            {["sent", "accepted", "paid", "rejected", "returned", "unknown"].map((s) => (
              <option key={s} value={s}>
                {s.charAt(0).toUpperCase() + s.slice(1)}
              </option>
            ))}
          </select>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-evidence`}>
            Evidence file ID
          </label>
          <input className="fy-input" id={`${id}-evidence`} value={evidence} onChange={(e) => setEvidence(e.target.value)} />
          <span className="fy-hint">A bank advice or a statement, attached to a record of the company. Needed for every outcome.</span>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-day`}>
            The day it happened
          </label>
          <input className="fy-input" id={`${id}-day`} type="date" value={day} onChange={(e) => setDay(e.target.value)} />
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Not now
          </Button>
          <Button variant="primary" type="submit" disabled={busy || !evidence.trim() || !day}>
            {h.label}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function save(name: string, type: string, base64: string) {
  const bytes = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0));
  if (typeof URL.createObjectURL !== "function") return;
  const url = URL.createObjectURL(new Blob([bytes], { type }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}

// SalaryPayment is one salary batch: what stops it, who sees the lines (FR-PPL-17), asking for its approval, releasing the lines free to go (FR-PPL-13), and what became of each payment (FR-PPL-12).
export function SalaryPayment() {
  const { batchId } = useParams({ strict: false }) as { batchId: string };
  const { session } = useSignedIn();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const roles = session.roles ?? [];
  const prepares = roles.some((r) => salaryPreparers.includes(r));
  const releases = roles.some((r) => salaryReleasers.includes(r));
  const [status, setStatus] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [failed, setFailed] = useState("");
  const [preparing, setPreparing] = useState(false);
  const [recording, setRecording] = useState<Line | null>(null);
  const [matching, setMatching] = useState(false);
  const ask = helpText("salary.ask");
  const release = helpText("salary.release");
  const cancel = helpText("salary.cancel");
  const prepare = helpText("salary.prepare");
  const q = useQuery({
    queryKey: ["salary-batch", batchId],
    queryFn: async () => {
      const r = await api.GET("/v1/salary-batches/{batch_id}", { params: { path: { batch_id: batchId } } });
      if (r.error) throw r.error;
      return { batch: r.data, etag: etagOf(r.response) };
    },
  });
  const b = q.data?.batch;
  const etag = q.data?.etag ?? "";
  const refresh = () => Promise.all([queries.invalidateQueries({ queryKey: ["salary-batch", batchId] }), queries.invalidateQueries({ queryKey: ["salary-batches"] })]);
  const run = async (what: string, call: () => Promise<{ data?: unknown; error?: unknown }>, done: string) => {
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, call);
    setBusy(false);
    setFailed(what);
    if (!out.data) return setProblem(problemOf(out.error));
    setStatus(done);
    await refresh();
  };
  const header = () => ({ "If-Match": etag, "Idempotency-Key": newKey() });
  const download = async (releaseId: string) => {
    if (!b) return;
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () => api.GET("/v1/salary-batches/{batch_id}/releases/{release_id}/file", { params: { path: { batch_id: b.id, release_id: releaseId } } }));
    setBusy(false);
    setFailed("The payment file was not downloaded");
    if (!out.data) return setProblem(problemOf(out.error));
    save(out.data.file_name, out.data.media_type, out.data.content_base64);
    setStatus(`Downloaded ${out.data.file_name}. It holds names and account numbers: key it into the bank or upload it there, then delete the file.`);
  };
  if (q.isError) return <Refusal problem={problemOf(q.error)} title="The salary batch could not be shown" />;
  if (!b) return <p className="fy-secondary">Loading the salary batch…</p>;
  const errors = b.checks.filter((c) => c.severity === "error");
  const open = b.status !== "cancelled";
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <span>Payroll</span>
            <span aria-hidden="true">›</span>
            <Link to="/payroll/salary-payments">Salary payments</Link>
          </nav>
          <h1 className="fy-h1">
            {b.number} · {monthWords(b.wage_month)} salaries
          </h1>
          <p className="fy-secondary">
            {b.payment_date ? `Pays ${date(b.payment_date)} by ${(b.method ?? "").toUpperCase()}` : "Not prepared yet: choose the bank account, the payment date and the method."}
          </p>
        </div>
        <div className="fy-pagehead__actions">
          <SalaryState batch={b} />
          {b.lines_visible && b.lines?.some((l) => l.state === "released") && (prepares || releases) && (
            <Button variant="secondary" onClick={() => setMatching(true)} disabled={busy} title={helpText("salary.bank-match").tooltip}>
              <Icon of={Wallet} />
              {helpText("salary.bank-match").label}
            </Button>
          )}
          {open && prepares && !b.releases.length && (
            <Button variant="secondary" onClick={() => setPreparing(true)} disabled={busy} title={prepare.tooltip}>
              {prepare.label}
            </Button>
          )}
          {open && prepares && b.payment_date && (b.status === "open" || b.reapproval_needed) && (
            <Button
              variant="primary"
              onClick={() => void run("Approval was not asked for", () => api.POST("/v1/salary-batches/{batch_id}/approval", { params: { path: { batch_id: b.id }, header: header() } }), "Asked for approval: another person, who did not prepare or approve the pay run, approves it before it is released.")}
              disabled={busy || errors.length > 0}
              title={ask.tooltip}
            >
              <Icon of={UserCheck} />
              {ask.label}
            </Button>
          )}
          {b.status === "approved" && !b.reapproval_needed && releases && b.state !== "paid" && b.state !== "released" && (
            <Button
              variant="primary"
              onClick={() => void run("The batch was not released", () => api.POST("/v1/salary-batches/{batch_id}/release", { params: { path: { batch_id: b.id }, header: header() } }), "Released the lines that are free to go. Download the payment file and pay it in the bank; the rest wait.")}
              disabled={busy}
              title={release.tooltip}
            >
              <Icon of={Send} />
              {release.label}
            </Button>
          )}
          {open && prepares && !b.releases.length && (
            <Button
              variant="ghost"
              onClick={() => void run("The batch was not cancelled", () => api.POST("/v1/salary-batches/{batch_id}/cancel", { params: { path: { batch_id: b.id }, header: header() }, body: { reason: "Cancelled before release" } }), `Cancelled ${b.number}.`)}
              disabled={busy}
              title={cancel.tooltip}
            >
              <Icon of={Ban} />
              {cancel.label}
            </Button>
          )}
        </div>
      </div>
      <ReviewBanner screen="salary-payment" />
      {status && (
        <div className="fy-banner fy-banner--good" role="status">
          <Icon of={CircleCheck} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">{status}</span>
          </div>
        </div>
      )}
      {problem && <Refusal problem={problem} title={failed} />}
      {b.checks.length > 0 && (
        <ul className="fy-stack fy-stack--2" aria-label="Checks">
          {b.checks.map((c, i) => (
            <li key={i} className={`fy-banner ${c.severity === "error" ? "fy-banner--critical" : "fy-banner--warning"}`}>
              <Icon of={c.severity === "error" ? CircleX : TriangleAlert} size={20} />
              <div className="fy-banner__text">
                <span className="fy-strong">{c.message}</span>
                {c.fix && <span className="fy-small">{c.fix}</span>}
              </div>
            </li>
          ))}
        </ul>
      )}
      <dl className="fy-facts" aria-label="Totals">
        <div>
          <dt>Total</dt>
          <dd className="fy-num">{formatAmount(b.total.amount)} SGD</dd>
        </div>
        <div>
          <dt>People</dt>
          <dd>{b.line_count}</dd>
        </div>
        <div>
          <dt>Released</dt>
          <dd>{b.released}</dd>
        </div>
        <div>
          <dt>Paid</dt>
          <dd>{b.paid}</dd>
        </div>
        <div>
          <dt>Waiting</dt>
          <dd>{b.waiting}</dd>
        </div>
      </dl>
      {b.lines_visible && b.lines ? (
        <table className="fy-table" aria-label="Salary lines">
          <thead>
            <tr>
              <th scope="col">Person</th>
              <th scope="col">Account</th>
              <th scope="col">Bank details</th>
              <th scope="col">Status</th>
              <th scope="col" className="fy-num">
                Net (SGD)
              </th>
              {prepares && <th scope="col">What became of it</th>}
            </tr>
          </thead>
          <tbody>
            {b.lines.map((l) => (
              <tr key={l.line_id}>
                <th scope="row">{l.person_name ?? l.person_id}</th>
                <td>{l.account ?? ""}</td>
                <td>Version {l.bank_detail_version}</td>
                <td>
                  <LineState line={l} />
                </td>
                <td className="fy-num">{formatAmount(l.amount.amount)}</td>
                {prepares && (
                  <td>
                    {l.instruction_id && (
                      <Button variant="secondary" size="sm" onClick={() => setRecording(l)} aria-label={`Record what became of ${l.person_name ?? "the payment"}`}>
                        Record
                      </Button>
                    )}
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <p className="fy-secondary" role="note">
          You see the batch's total, the number of people and its release state. Names, accounts and amounts are for the roles that hold the salary-payments right.
        </p>
      )}
      {b.releases.length > 0 && releases && (
        <section aria-label="Releases" className="fy-stack fy-stack--2">
          <h2 className="fy-h2">Releases</h2>
          <ul className="fy-stack fy-stack--1">
            {b.releases.map((r) => (
              <li key={r.id} className="fy-row fy-row--wrap gap-2">
                <span>
                  {r.file_name}, released {date(r.released_at.slice(0, 10))}
                </span>
                <Button variant="secondary" size="sm" onClick={() => void download(r.id)} disabled={busy}>
                  <Icon of={Download} />
                  Download the file
                </Button>
              </li>
            ))}
          </ul>
        </section>
      )}
      <p className="fy-secondary">
        Paid means the bank feed shows the debit, matched to the lines it paid, or a person recorded it with evidence. A bank file or a posted journal is never taken as payment. If an outcome is
        unknown, that person cannot be paid again until it is resolved.
      </p>
      {preparing && (
        <Prepare
          batch={b}
          etag={etag}
          onClose={() => setPreparing(false)}
          onDone={(what) => {
            setPreparing(false);
            setStatus(what);
            void refresh();
          }}
        />
      )}
      {matching && (
        <BankMatch
          batch={b}
          etag={etag}
          onClose={() => setMatching(false)}
          onDone={(what) => {
            setMatching(false);
            setStatus(what);
            void refresh();
          }}
        />
      )}
      {recording && (
        <Record
          batch={b}
          line={recording}
          onClose={() => setRecording(null)}
          onDone={(what) => {
            setRecording(null);
            setStatus(what);
            void refresh();
          }}
        />
      )}
    </div>
  );
}
