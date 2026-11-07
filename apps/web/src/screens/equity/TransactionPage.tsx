import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { Check, CircleDashed, FileSymlink, Hourglass, PencilLine, Send, TriangleAlert, Undo2 } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { EvidencePanel } from "../accounting/JournalEvidence";
import { EquityTabs } from "./Holders";
import { bodyOf, TransactionFields, type TransactionDraft } from "./TransactionForm";
import { drafters, kindWords, recorders, reviewText, sharesText, StatusChip, stepsOf, transactionOf, type EquityTransaction } from "./transactionWords";

// TransactionPage is Equity › Transactions › a transaction (docs/design/screens EquityRegister's steps and
// EquityTransaction; FR-EQ-17, FR-EQ-09, FR-DOC-06): what was drafted, its evidence and approval, its ACRA steps with
// what they raise, the day ACRA's register shows it, and once effective the holdings it moved, or its reversal.
export function TransactionPage() {
  const { transactionId } = useParams({ strict: false }) as { transactionId: string };
  const { session } = useSignedIn();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const q = useQuery({ queryKey: ["equity-transaction", transactionId], queryFn: () => transactionOf(transactionId) });
  const [editing, setEditing] = useState(false);
  const [status, setStatus] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const roles = session.roles ?? [];
  const mayDraft = roles.some((r) => drafters.includes(r));
  const mayRecord = roles.some((r) => recorders.includes(r));
  const askH = helpText("equity-transaction.ask");
  const refresh = async (text: string) => {
    setStatus(text);
    await queries.invalidateQueries({ queryKey: ["equity-transaction", transactionId] });
    await queries.invalidateQueries({ queryKey: ["equity-transactions"] });
    await queries.invalidateQueries({ queryKey: ["holders"] });
  };
  const act = async (call: (etag: string) => Promise<{ data?: unknown; error?: unknown }>, done: string) => {
    if (!q.data) return;
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () => call(q.data.etag));
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await refresh(done);
  };
  if (q.isError) return <Refusal problem={problemOf(q.error)} title="The equity transaction could not be shown" />;
  if (!q.data) return null;
  const { t, etag } = q.data;
  const convertH = helpText("equity-transaction.convert-safes");
  const convert = async () => {
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/equity-transactions/{transaction_id}/conversions", { params: { path: { transaction_id: t.id }, header: { "If-Match": etag, "Idempotency-Key": newKey() } } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    const n = out.data.items.length;
    await refresh(n === 0 ? "No SAFE or note waits to convert at this round." : `Drafted ${n} conversion${n === 1 ? "" : "s"}: open each in Transactions, attach the resolutions and ask for approval.`);
  };
  const ask = () =>
    act((m) => api.POST("/v1/equity-transactions/{transaction_id}/approval", { params: { path: { transaction_id: t.id }, header: { "If-Match": m, "Idempotency-Key": newKey() } } }),
      "Asked for approval. Another Owner or the Controller approves it.");
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/equity/transactions">Transactions</Link>
            <span aria-hidden="true">›</span>
            <span>{t.number || "Draft"}</span>
          </nav>
          <h1 className="fy-h1">
            {t.number ? `${t.number}: ` : ""}
            {kindWords[t.details.kind]}
          </h1>
          <StatusChip t={t} />
        </div>
        <div className="fy-pagehead__actions">
          {t.status === "draft" && mayDraft && !t.approval_id && (
            <>
              <Button variant="secondary" onClick={() => setEditing(true)}>
                <Icon of={PencilLine} />
                Change
              </Button>
              <Button onClick={() => void ask()} disabled={busy || t.evidence === 0} title={askH.tooltip}>
                <Icon of={Send} />
                {askH.label}
              </Button>
            </>
          )}
          {t.approval_id && (
            <Link className="fy-btn fy-btn--secondary" to="/approvals/$approvalId" params={{ approvalId: t.approval_id }}>
              <Icon of={Hourglass} />
              Waiting for approval
            </Link>
          )}
          {t.details.kind === "issue_cash" && (t.status === "approved" || t.status === "effective") && mayDraft && (
            <Button variant="secondary" onClick={() => void convert()} disabled={busy} title={convertH.tooltip}>
              <Icon of={FileSymlink} />
              {convertH.label}
            </Button>
          )}
          {t.status === "effective" && mayDraft && t.details.kind !== "reversal" && (
            <Link className="fy-btn fy-btn--secondary" to="/equity/transactions/new" search={{ reverses: t.id }} title={helpText("equity-transaction.reverse").tooltip}>
              <Icon of={Undo2} />
              {helpText("equity-transaction.reverse").label}
            </Link>
          )}
        </div>
      </div>
      <EquityTabs current="transactions" />
      <p className="fy-small" role="status">
        {status}
      </p>
      {problem && <Refusal problem={problem} title="That was not done" />}
      <div className="fy-split">
        <div className="fy-stack fy-stack--4">
          <Details t={t} />
          {t.details.review && (
            <p className="fy-card fy-card--pad fy-small" role="note">
              <Icon of={TriangleAlert} /> {reviewText(t.details.review)} Check the shares and the conversion price against the terms before asking.
            </p>
          )}
          {t.checks.length > 0 && (
            <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-label="What stops it">
              {t.checks.map((c) => (
                <p key={`${c.code}-${c.field}`} className="fy-small fy-secondary">
                  {c.message}. {c.fix}.
                </p>
              ))}
            </section>
          )}
          <EvidencePanel record={{ kind: "equity-transaction", id: t.id, etag }} draft={t.status === "draft"} mayAttach={mayDraft && t.status === "draft" && !t.approval_id}
            onChanged={(m) => void refresh(m)} />
          {t.movements.length > 0 && <Movements t={t} />}
        </div>
        <div className="fy-stack fy-stack--4">
          {t.acra && t.details.kind !== "reversal" && <Steps t={t} etag={etag} mayRecord={mayRecord && t.status === "approved"} onDone={(m) => void refresh(m)} />}
          {t.status === "approved" && mayRecord && <Effective t={t} etag={etag} onDone={(m) => void refresh(m)} />}
        </div>
      </div>
      {editing && <ChangeDraft t={t} etag={etag} onClose={() => setEditing(false)} onDone={(m) => void refresh(m)} />}
    </div>
  );
}

function Details({ t }: { t: EquityTransaction }) {
  const d = t.details;
  const rows: [string, string][] = [
    ["Holders", t.parties.map((p) => `${p.name} (${p.role})`).join(", ") || "–"],
    ["Class", t.class_code || "–"],
    ["Shares", d.kind === "class_conversion" ? `${sharesText(d.shares)} out, ${sharesText(d.to_shares)} in` : sharesText(d.shares)],
  ];
  const converting = d.kind === "instrument_conversion";
  if (d.price) rows.push([converting ? "Conversion price per share" : "Price per share", d.price]);
  if (t.amount)
    rows.push([d.kind === "class_conversion" ? "Carrying amount" : converting ? "Converted from 2700" : "Amount", `${formatAmount(t.amount.amount)} ${t.amount.currency}`]);
  if (d.resolution_refs) rows.push(["Resolutions", d.resolution_refs]);
  if (d.reason) rows.push(["Why", d.reason]);
  if (d.note) rows.push(["Note", d.note]);
  if (t.approved_on) rows.push(["Approved", date(t.approved_on)]);
  if (t.effective_on) rows.push(["Effective", date(t.effective_on)]);
  return (
    <section className="fy-card" aria-labelledby="tx-details-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="tx-details-title">
          Details
        </h2>
      </div>
      <dl className="fy-card__body fy-dl">
        {rows.map(([k, v]) => (
          <div key={k}>
            <dt>{k}</dt>
            <dd>{v}</dd>
          </div>
        ))}
      </dl>
    </section>
  );
}

function Movements({ t }: { t: EquityTransaction }) {
  return (
    <section className="fy-card" aria-labelledby="tx-moves-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="tx-moves-title">
          Holdings it moved
        </h2>
        <span className="fy-small fy-muted">{t.entries.length === 0 ? "No entry in the company's books" : `${t.entries.length} entry posted`}</span>
      </div>
      <table className="fy-table" aria-labelledby="tx-moves-title">
        <thead>
          <tr>
            <th scope="col">Holder</th>
            <th scope="col">Class</th>
            <th scope="col" className="fy-num">
              Shares
            </th>
            <th scope="col">Effective</th>
          </tr>
        </thead>
        <tbody>
          {t.movements.map((m) => (
            <tr key={m.id}>
              <td>{m.holder_name}</td>
              <td className="fy-mono">{m.class_code}</td>
              <td className="fy-num">{m.shares.startsWith("-") ? `−${sharesText(m.shares.slice(1))}` : sharesText(m.shares)}</td>
              <td>{date(m.effective_on)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}

// Steps are a transaction's ACRA steps in order, done or still to do, with what they raise, and for an Owner or the
// Controller the form to record the next (FR-EQ-09).
function Steps({ t, etag, mayRecord, onDone }: { t: EquityTransaction; etag: string; mayRecord: boolean; onDone: (m: string) => void }) {
  const id = useId();
  const confirm = useConfirm();
  const kinds = stepsOf[t.details.kind];
  const latest = new Map<string, NonNullable<EquityTransaction["acra"]>["steps"][number]>((t.acra?.steps ?? []).map((s) => [s.kind, s]));
  const [form, setForm] = useState({ kind: kinds[0]?.kind ?? "", on: "", reference: "" });
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const stepH = helpText("equity-transaction.step");
  const record = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/equity-transactions/{transaction_id}/acra-steps", {
        params: { path: { transaction_id: t.id }, header: { "If-Match": etag, "Idempotency-Key": newKey() } },
        body: { kind: form.kind as never, on: form.on, ...(form.reference.trim() ? { reference: form.reference.trim() } : {}) },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    setForm({ ...form, on: "", reference: "" });
    onDone(`Recorded: ${kinds.find((k) => k.kind === form.kind)?.label ?? form.kind}.`);
  };
  return (
    <section className="fy-card" aria-labelledby="tx-acra-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="tx-acra-title" title={stepH.tooltip}>
          ACRA steps
        </h2>
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        <p className="fy-small fy-secondary">Fyniq tracks the steps and their evidence; you or your company secretary lodge with ACRA.</p>
        <ol className="fy-timeline" style={{ paddingLeft: 28 }}>
          {kinds.map((k) => {
            const s = latest.get(k.kind);
            return (
              <li key={k.kind} className="fy-timeline__item">
                <span className={`fy-timeline__dot${s ? " fy-timeline__dot--good" : ""}`}>
                  <Icon of={s ? Check : CircleDashed} />
                </span>
                <span className="fy-strong">{k.label}</span>
                <span className="fy-small fy-muted">
                  {s ? `${date(s.on)}${s.reference ? ` · ${s.reference}` : ""}` : k.kind === "agm_due" && t.acra?.agm_due_on ? `By ${date(t.acra.agm_due_on)}, worked out` : "To record"}
                </span>
              </li>
            );
          })}
        </ol>
        {(t.acra?.flags ?? []).map((f) => (
          <p key={f.code} className="fy-small" role="note">
            <span className={`fy-chip ${f.kind === "due" ? "fy-chip--serious" : "fy-chip--warning"}`}>{f.kind === "due" ? "Due" : "Review"}</span> {f.message}. {f.fix}.
          </p>
        ))}
        {mayRecord && (
          <form className="fy-stack fy-stack--3" onSubmit={record} aria-label="Record an ACRA step">
            {problem && <Refusal problem={problem} title="The step was not recorded" />}
            <div className="fy-form fy-form--2">
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-kind`}>
                  Step
                </label>
                <select className="fy-select" id={`${id}-kind`} value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value })}>
                  {kinds.map((k) => (
                    <option key={k.kind} value={k.kind}>
                      {k.label}
                    </option>
                  ))}
                </select>
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-on`}>
                  On
                </label>
                <input className="fy-input" id={`${id}-on`} type="date" value={form.on} onChange={(e) => setForm({ ...form, on: e.target.value })} />
              </div>
              <div className="fy-field fy-field--full">
                <label className="fy-label" htmlFor={`${id}-ref`}>
                  ACRA reference (optional)
                </label>
                <input className="fy-input" id={`${id}-ref`} maxLength={200} value={form.reference} onChange={(e) => setForm({ ...form, reference: e.target.value })} />
              </div>
            </div>
            <div className="fy-row justify-end">
              <Button variant="secondary" type="submit" disabled={busy || !form.on} title={stepH.tooltip}>
                {stepH.label}
              </Button>
            </div>
          </form>
        )}
      </div>
    </section>
  );
}

// Effective records the day ACRA's register of members shows an approved transaction (FR-EQ-17): a cash issue says what
// its subscriber has paid into share application monies first.
function Effective({ t, etag, onDone }: { t: EquityTransaction; etag: string; onDone: (m: string) => void }) {
  const id = useId();
  const confirm = useConfirm();
  const [form, setForm] = useState({ effective_on: "", lodgement_ref: "" });
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const h = helpText("equity-transaction.effective");
  const record = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/equity-transactions/{transaction_id}/effective", {
        params: { path: { transaction_id: t.id }, header: { "If-Match": etag, "Idempotency-Key": newKey() } },
        body: { effective_on: form.effective_on, ...(form.lodgement_ref.trim() ? { lodgement_ref: form.lodgement_ref.trim() } : {}) },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone(`Effective on ${date(form.effective_on)}: the holdings moved.`);
  };
  return (
    <form className="fy-card fy-card--pad fy-stack fy-stack--3" onSubmit={record} aria-labelledby="tx-effective-title">
      <h2 className="fy-h3" id="tx-effective-title" title={h.tooltip}>
        When ACRA's register shows it
      </h2>
      {t.details.kind === "issue_cash" && t.paid && t.amount && (
        <p className="fy-small fy-secondary">
          Paid into share application monies: {formatAmount(t.paid.amount)} of {formatAmount(t.amount.amount)} {t.amount.currency}. It takes effect only once paid.
        </p>
      )}
      {problem && <Refusal problem={problem} title="The effective date was not recorded" />}
      <div className="fy-form fy-form--2">
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-on`}>
            Effective date
          </label>
          <input className="fy-input" id={`${id}-on`} type="date" value={form.effective_on} onChange={(e) => setForm({ ...form, effective_on: e.target.value })} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-ref`}>
            ACRA lodgement reference (optional)
          </label>
          <input className="fy-input" id={`${id}-ref`} maxLength={100} value={form.lodgement_ref} onChange={(e) => setForm({ ...form, lodgement_ref: e.target.value })} />
        </div>
      </div>
      <div className="fy-row justify-end">
        <Button type="submit" disabled={busy || !form.effective_on} title={h.tooltip}>
          {h.label}
        </Button>
      </div>
    </form>
  );
}

function ChangeDraft({ t, etag, onClose, onDone }: { t: EquityTransaction; etag: string; onClose: () => void; onDone: (m: string) => void }) {
  const confirm = useConfirm();
  const [form, setForm] = useState<TransactionDraft>(t.details);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const [key] = useState(newKey);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.PUT("/v1/equity-transactions/{transaction_id}", { params: { path: { transaction_id: t.id }, header: { "If-Match": etag, "Idempotency-Key": key } }, body: bodyOf(form) }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone("The draft is saved.");
    onClose();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && onClose()} dismissible={!busy} wide eyebrow="Equity transaction" title="Change the draft"
      description="A draft changes until approval is asked for; once approved, a reversal corrects it.">
      {problem && <Refusal problem={problem} title="The draft was not saved" />}
      <form className="fy-stack fy-stack--4" onSubmit={submit}>
        <TransactionFields form={form} set={setForm} kindFixed />
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy}>
            Save the draft
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
