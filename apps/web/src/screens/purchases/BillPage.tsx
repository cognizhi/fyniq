import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useLocation, useNavigate, useParams } from "@tanstack/react-router";
import { Banknote, CalendarClock, CircleCheck, CircleX, ClockAlert, Link2, Pencil, Save, Send, TriangleAlert, Undo2, UserCheck, X } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date, month } from "../../format/date";
import { typed } from "../../format/decimal";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { listAccounts, postable, type Account } from "../accounting/data";
import { billAttachers, EvidencePanel } from "../accounting/JournalEvidence";
import { who } from "../accounting/Journals";
import { preparers, StateChips, total, type Bill } from "./Bills";
import { BillLines, lineKey, newLine, type Errors, type Line } from "./BillLines";
import { factsOf, InputTaxCard, taxOf, usualTax, type Tax } from "./InputTax";
import { ApplyCredit, Payments, RecordPayment } from "./Payments";
import { PaymentPlanCard } from "./BillsBoard";
import { natures, nonResident, residenceOf, WithholdingCard } from "./Withholding";

type Draft = components["schemas"]["BillDraft"];
type Check = components["schemas"]["BillCheck"];
type EntryLine = components["schemas"]["EntryLine"];
type Release = components["schemas"]["BillRelease"];

// Who posts, voids and edits bills: P on "Bills, vendor credits" (Appendix D), the Accountant. The API decides.
const posters = ["accountant"];
// The code the API answers when a bill's total is above the self-approval threshold (FM-2): ask for approval.
const overThreshold = "BILL-09";

interface Form {
  supplier_id: string;
  supplier_ref: string;
  date: string;
  due_date: string;
  currency: string;
  fx_rate: string;
  notes: string;
  lines: Line[];
  tax: Tax; // what its input tax rests on (FR-AP-20)
  nature: string; // what a non-resident supplier's payment is for (FR-AP-19); blank for a resident one
}


function today(): string {
  return new Date().toLocaleDateString("en-CA");
}

// formOf is a saved bill as the form shows it.
function formOf(b: Bill): Form {
  return {
    supplier_id: b.supplier?.id ?? "",
    supplier_ref: b.supplier_ref ?? "",
    date: b.date ?? "",
    due_date: "",
    currency: b.currency,
    fx_rate: b.fx_rate ?? "",
    notes: b.notes ?? "",
    tax: taxOf(b.input_tax),
    nature: b.nature_of_payment ?? "",
    lines: (b.lines ?? []).map((l) => ({
      key: lineKey(),
      account_id: l.account_id,
      description: l.description ?? "",
      gst_code: l.gst_code,
      net: l.net.amount,
      gst: l.gst.amount,
      stated_gst: l.stated_gst?.amount ?? "",
      spread_months: l.spread_months ? String(l.spread_months) : "",
      spread_from: l.spread_from?.slice(0, 7) ?? "",
      order: l.order,
      business_share: l.business_share ?? "",
    })),
  };
}

const blank = (l: Line) => !l.account_id && !l.description.trim() && !l.net.trim() && !l.gst.trim();

// check turns what was typed into the bill to send, or says what to fix under each field. The API checks the rest when
// the bill posts (FR-AP-02): the supplier, the codes, the accounts and the total.
export function check(f: Form, base: string): { draft?: Draft; errors: Errors } {
  const errors: Errors = {};
  const currency = (f.currency.trim() || base).toUpperCase();
  if (!/^[A-Z]{3}$/.test(currency)) errors.currency = "Enter a currency code, such as SGD.";
  const rate = f.fx_rate.trim();
  if (currency !== base && rate && !/^(0|[1-9]\d{0,9})(\.\d{1,10})?$/.test(rate)) errors.fx_rate = `Enter the rate as ${base} for 1 ${currency}, such as 1.3472, or leave it blank to take the day's rate.`;
  const lines: Draft["lines"] = [];
  for (const l of f.lines) {
    if (blank(l)) continue;
    const net = typed(l.net);
    const gst = typed(l.gst || "0");
    const stated = typed(l.stated_gst);
    if (!l.account_id) errors[`${l.key}.account`] = "Choose what the line is for.";
    if (!l.gst_code) errors[`${l.key}.gst_code`] = "Choose the GST code on the supplier's invoice.";
    if (net === null || net === "") errors[`${l.key}.net`] = "Enter the amount before GST, such as 1,200.00.";
    if (gst === null) errors[`${l.key}.gst`] = "Enter the GST as the invoice states it, or 0.";
    if (stated === null) errors[`${l.key}.stated_gst`] = `Enter the GST in ${base} as the invoice states it, or leave it blank.`;
    const months = l.spread_months.trim();
    const from = l.spread_from.trim();
    if ((months && !/^([1-9]\d?|1[01]\d|120)$/.test(months)) || (!months && from)) errors[`${l.key}.spread_months`] = "Enter how many months to spread it over, 1 to 120, or leave both spread fields blank.";
    if (from && !/^\d{4}-(0[1-9]|1[0-2])$/.test(from)) errors[`${l.key}.spread_from`] = "Enter the month the service starts, such as 2026-10, or leave it blank for the invoice's month.";
    const share = (l.business_share ?? "").trim().replace(/%$/, "").trim();
    if (share && !/^(100(\.0{1,2})?|\d{1,2}(\.\d{1,2})?)$/.test(share)) errors[`${l.key}.business_share`] = "Enter the share used for the business, 0 to 100, such as 60, or leave it blank for all of it.";
    lines.push({
      account_id: l.account_id,
      description: l.description.trim() || undefined,
      gst_code: l.gst_code,
      net: { amount: net ?? "0", currency },
      gst: { amount: gst || "0", currency },
      stated_gst: currency !== base && stated ? { amount: stated, currency: base } : undefined,
      spread_months: months ? Number(months) : undefined,
      spread_from: months && from ? `${from}-01` : undefined,
      order: l.order,
      business_share: share && !/^100(\.0{1,2})?$/.test(share) ? share : undefined,
    });
  }
  if (lines.length === 0) errors.lines = "Add a line for what the bill is for.";
  if (Object.keys(errors).length > 0) return { errors };
  return {
    errors,
    draft: {
      supplier_id: f.supplier_id || undefined,
      supplier_ref: f.supplier_ref.trim() || undefined,
      date: f.date || undefined,
      due_date: f.due_date || undefined,
      currency,
      fx_rate: currency !== base && rate ? rate : undefined,
      notes: f.notes.trim() || undefined,
      lines,
      input_tax: factsOf(f.tax),
      nature_of_payment: (f.nature || undefined) as Draft["nature_of_payment"],
    },
  };
}

// Checks lists what stops a draft posting (✖) and what it posts with (⚠).
function Checks({ checks }: { checks: Check[] }) {
  const id = useId();
  if (checks.length === 0) return null;
  const errors = checks.filter((c) => c.severity === "error").length;
  return (
    <section className="fy-card" aria-labelledby={id}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={id}>
          {errors > 0 ? (errors === 1 ? "1 thing to fix before it can post" : `${errors} things to fix before it can post`) : "To check before it posts"}
        </h2>
      </div>
      <ul className="fy-list">
        {checks.map((c, i) => (
          <li key={`${c.code}-${c.field ?? ""}-${i}`} className="fy-list__item">
            <span className={`fy-list__icon ${c.severity === "error" ? "fy-list__icon--critical" : "fy-list__icon--warning"}`}>
              <Icon of={c.severity === "error" ? CircleX : TriangleAlert} size={20} />
            </span>
            <span className="fy-list__main">
              <span className="fy-list__title whitespace-normal">
                {c.message} <span className="fy-small fy-muted fy-mono">{c.code}</span>
              </span>
              {c.fix && <span className="fy-list__meta whitespace-normal">{c.fix}.</span>}
            </span>
            <span className="fy-sr">{c.severity === "error" ? "Stops it posting" : "Review it"}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}

// Impact is the ledger impact in Expert mode (debits and credits by account), and in Basic mode one plain sentence.
function Impact({ bill, lines, accounts, base }: { bill: Bill; lines: EntryLine[]; accounts: Map<string, Account>; base: string }) {
  const id = useId();
  if (!bill.payable) return null;
  const owed = `${base} ${formatAmount(bill.payable.amount)}`;
  const posted = bill.status !== "draft";
  const credit = bill.kind === "vendor_credit";
  return (
    <>
      <p className="fy-plain fy-basic-only fy-small">
        {credit ? (posted ? "This credit note took " : "This credit note takes ") : posted ? "This bill added " : "This bill adds "}
        {owed} {credit ? "off" : "to"} what we owe {bill.supplier?.name ?? "the supplier"}
        {bill.currency !== base ? ` (${total(bill)})` : ""}.
      </p>
      {lines.length > 0 && (
        <section className="fy-card fy-expert-only" aria-labelledby={id}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={id}>
              Ledger impact
            </h2>
            <span className="fy-small fy-muted">{posted ? "As posted" : "What posting writes"}</span>
          </div>
          <table className="fy-table fy-table--compact fy-ledger">
            <thead>
              <tr>
                <th scope="col">Account</th>
                <th scope="col" className="fy-num">
                  Debit ({base})
                </th>
                <th scope="col" className="fy-num">
                  Credit ({base})
                </th>
              </tr>
            </thead>
            <tbody>
              {lines.map((l) => {
                const a = accounts.get(l.account_id);
                const credit = l.base_amount.amount.startsWith("-");
                const amount = formatAmount(l.base_amount.amount.replace(/^-/, ""));
                return (
                  <tr key={l.line_no}>
                    <td>
                      <span className="fy-acct">{a?.code ?? "—"}</span>
                      {a?.name ?? "An account not in the chart"}
                      {l.amount.currency !== base ? ` · ${l.amount.currency} ${formatAmount(l.amount.amount.replace(/^-/, ""))}` : ""}
                    </td>
                    <td className="fy-num">{credit ? "" : amount}</td>
                    <td className="fy-num">{credit ? amount : ""}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </section>
      )}
    </>
  );
}

// releaseChip is a release's state (ENG-05): only posted ones are in the books; projected ones are not, yet.
function releaseChip(r: Release) {
  switch (r.status) {
    case "posted":
      return (
        <span className="fy-chip fy-chip--good">
          <Icon of={CircleCheck} />
          Posted
        </span>
      );
    case "waiting":
      return (
        <span className="fy-chip fy-chip--warning">
          <Icon of={ClockAlert} />
          Due, not posted
        </span>
      );
    case "reversed":
      return (
        <span className="fy-chip fy-chip--neutral">
          <Icon of={Undo2} />
          Reversed
        </span>
      );
    default:
      return (
        <span className="fy-chip fy-chip--neutral">
          <Icon of={CalendarClock} />
          Projected
        </span>
      );
  }
}

// Releases is a spread bill's schedule (FR-GL-03): each month's part, moved from Prepayments to the cost as it posts.
// Months to come are projections, never shown as if they were in the books (ENG-05).
function Releases({ bill, base }: { bill: Bill; base: string }) {
  const id = useId();
  const releases = bill.releases ?? [];
  if (releases.length === 0) return null;
  const lines = new Set(releases.map((r) => r.line)).size;
  return (
    <section className="fy-card" aria-labelledby={id}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={id}>
          {helpText("bill.releases").label}
        </h2>
        <span className="fy-small fy-muted">{bill.status === "draft" ? "What posting starts" : "Released month by month"}</span>
      </div>
      <p className="fy-card__body fy-small whitespace-normal">
        {bill.status === "draft"
          ? "Posting puts the spread cost in Prepayments; each month's part then moves to the cost as its month comes."
          : "Each month's part moves from Prepayments to the cost on its date. Projected months are not in the books yet."}
      </p>
      <table className="fy-table fy-table--compact fy-table--cards" aria-labelledby={id}>
        <thead>
          <tr>
            <th scope="col">Month</th>
            {lines > 1 && <th scope="col">Line</th>}
            <th scope="col">Posts on</th>
            <th scope="col" className="fy-num">
              Amount ({base})
            </th>
            <th scope="col">Status</th>
          </tr>
        </thead>
        <tbody>
          {releases.map((r) => (
            <tr key={`${r.line}-${r.month}`}>
              <td data-card="title">
                {month(r.month)}{" "}
                <span className="fy-small fy-muted">
                  ({r.part} of {r.of})
                </span>
              </td>
              {lines > 1 && <td data-card="meta">Line {r.line}</td>}
              <td data-card="meta">{date(r.date)}</td>
              <td data-card="amount" className="fy-num">
                {formatAmount(r.amount.amount)}
              </td>
              <td data-card="status">{releaseChip(r)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}

// Void voids a posted bill with a date and a reason; in Founder mode it asks another founder first (FM-1).
function Void({ open, bill, etag, ask, onClose, onDone }: { open: boolean; bill: Bill; etag: string; ask: boolean; onClose: () => void; onDone: (message: string) => void }) {
  const confirm = useConfirm();
  const id = useId();
  const [on, setOn] = useState(today);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [missing, setMissing] = useState(false);
  const h = helpText(ask ? "bill.ask-void" : "bill.void");
  const run = async (e: FormEvent) => {
    e.preventDefault();
    if (!reason.trim()) return setMissing(true);
    setBusy(true);
    setProblem(null);
    const params = { path: { bill_id: bill.id }, header: { "If-Match": etag, "Idempotency-Key": newKey() } };
    const body = { date: on, reason: reason.trim() };
    if (ask) {
      const out = await guarded(confirm, () => api.POST("/v1/bills/{bill_id}/void-approval", { params, body }));
      setBusy(false);
      if (!out.data) return setProblem(problemOf(out.error));
      return onDone("Asked for approval: the bill is voided when another person approves it.");
    }
    const out = await guarded(confirm, () => api.POST("/v1/bills/{bill_id}/void", { params, body }));
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone("Voided: a reversing entry posted.");
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      eyebrow={bill.number}
      title={ask ? "Ask to void this bill?" : "Void this bill?"}
      description={
        ask
          ? "In Founder mode another founder approves a void first. Then a reversing entry posts on the date you choose; the bill stays in the books, marked voided."
          : "A reversing entry with every line the other way round posts on the date you choose. The bill stays in the books, marked voided; nothing is deleted."
      }
    >
      {problem && <Refusal problem={problem} title={ask ? "The request was not made" : "The bill was not voided"} />}
      <form className="fy-stack fy-stack--3" onSubmit={run} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-date`}>
            Date of the reversing entry
          </label>
          <input className="fy-input" id={`${id}-date`} type="date" value={on} min={bill.date} onChange={(e) => setOn(e.target.value)} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-reason`}>
            Why it is voided
          </label>
          <textarea
            className="fy-textarea"
            id={`${id}-reason`}
            value={reason}
            maxLength={500}
            onChange={(e) => {
              setReason(e.target.value);
              setMissing(false);
            }}
            aria-invalid={missing || undefined}
            aria-describedby={missing ? `${id}-reason-error` : undefined}
          />
          {missing && (
            <span className="fy-error" id={`${id}-reason-error`}>
              Say why: the audit trail keeps the reason with the reversal.
            </span>
          )}
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy} title={h.tooltip}>
            <Icon of={ask ? UserCheck : Undo2} />
            {h.label}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// BillPage is a bill (docs/design/screens BillEditor and BillRecord; FR-AP-02, FR-GL-12): a new one, a draft its
// preparer changes and posts, or a posted bill, read-only, which an Accountant voids or edits with a reason. The lines
// are typed as the supplier's invoice states them, GST included; the server's check gives what posting writes and the
// warnings it posts with. A bill above the self-approval threshold, and in Founder mode a void or an edit, is asked for
// and approved by another person (FM-1, FM-2, SOD-3).
export function BillPage() {
  const { billId } = useParams({ strict: false }) as { billId?: string };
  const { pathname } = useLocation();
  const isNew = !billId;
  const { session, organisation } = useSignedIn();
  const base = organisation?.base_currency ?? "SGD";
  const founder = organisation?.team_mode === "founder";
  const queries = useQueryClient();
  const navigate = useNavigate();
  const confirm = useConfirm();
  const id = useId();
  const roles = session.roles ?? [];
  const mayPrepare = roles.some((r) => preparers.includes(r));
  const mayPost = roles.some((r) => posters.includes(r));

  const accounts = useQuery({ queryKey: ["accounts"], queryFn: listAccounts });
  const vendors = useQuery({
    queryKey: ["contacts", "vendor"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/contacts", { params: { query: { role: "vendor", page_size: 500 } } });
      if (error) throw error;
      return (data.items ?? []).filter((c) => c.active);
    },
  });
  const one = useQuery({
    queryKey: ["bill", billId],
    enabled: !isNew,
    queryFn: async () => {
      const r = await api.GET("/v1/bills/{bill_id}", { params: { path: { bill_id: billId ?? "" } } });
      if (r.error) throw r.error;
      return { bill: r.data, etag: r.response.headers.get("ETag") ?? "" };
    },
  });
  const bill = one.data?.bill;
  // A supplier's credit note (FR-AP-03) is entered and posted as a bill is, and applied to bills or refunded once posted.
  const credit = bill ? bill.kind === "vendor_credit" : pathname.endsWith("/purchases/credits/new");
  const [form, setForm] = useState<Form | null>(null);
  const [editing, setEditing] = useState(false);
  const [reason, setReason] = useState("");
  const [reasonMissing, setReasonMissing] = useState(false);
  const [errors, setErrors] = useState<Errors>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [failed, setFailed] = useState("");
  const [status, setStatus] = useState("");
  const [voiding, setVoiding] = useState(false);
  const [paying, setPaying] = useState(false);
  const [applying, setApplying] = useState(false);
  const [refunding, setRefunding] = useState(false);

  const editable = isNew || (bill?.status === "draft" && mayPrepare) || editing;
  const f: Form | null =
    form ?? (isNew ? { supplier_id: "", supplier_ref: "", date: today(), due_date: "", currency: base, fx_rate: "", notes: "", lines: [newLine()], tax: usualTax(), nature: "" } : bill ? formOf(bill) : null);
  const codes = useQuery({
    queryKey: ["gst-codes", f?.date ?? ""],
    enabled: Boolean(f),
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/purchases/gst-codes", { params: { query: { on: f?.date || undefined } } });
      if (error) throw error;
      return data;
    },
  });
  const byId = new Map((accounts.data ?? []).map((a) => [a.id, a]));
  const choices = postable(accounts.data ?? []).filter((a) => a.type === "expense" || a.type === "asset");
  const set = (patch: Partial<Form>) => f && setForm({ ...f, ...patch });
  const setLine = (key: string, patch: Partial<Line>) => f && setForm({ ...f, lines: f.lines.map((l) => (l.key === key ? { ...l, ...patch } : l)) });

  const refresh = async () => {
    setForm(null);
    await queries.invalidateQueries({ queryKey: ["bill", billId] });
    await queries.invalidateQueries({ queryKey: ["bills"] });
  };

  const save = async (e?: FormEvent) => {
    e?.preventDefault();
    if (!f) return;
    const { draft, errors: errs } = check(f, base);
    setErrors(errs);
    if (!draft) return setStatus("Fix the fields marked, then save again.");
    setBusy(true);
    setProblem(null);
    setFailed("The bill was not saved");
    if (isNew) {
      const out = await api.POST("/v1/bills", { params: { header: { "Idempotency-Key": newKey() } }, body: credit ? { ...draft, kind: "vendor_credit" } : draft });
      setBusy(false);
      if (!out.data) return setProblem(problemOf(out.error));
      await queries.invalidateQueries({ queryKey: ["bills"] });
      void navigate({ to: "/purchases/bills/$billId", params: { billId: out.data.id } });
      return;
    }
    if (!one.data) return;
    const out = await api.PUT("/v1/bills/{bill_id}", { params: { path: { bill_id: one.data.bill.id }, header: { "If-Match": one.data.etag } }, body: draft });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await refresh();
    setStatus("Saved.");
  };

  const post = async () => {
    if (!one.data) return;
    setBusy(true);
    setProblem(null);
    setFailed("The bill was not posted");
    const out = await guarded(confirm, () =>
      api.POST("/v1/bills/{bill_id}/post", { params: { path: { bill_id: one.data.bill.id }, header: { "If-Match": one.data.etag, "Idempotency-Key": newKey() } } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await refresh();
    setStatus(`Posted as ${out.data.bill.number ?? "a bill"}.`);
  };

  const askPost = async () => {
    if (!one.data) return;
    setBusy(true);
    setProblem(null);
    setFailed("The request was not made");
    const out = await guarded(confirm, () =>
      api.POST("/v1/bills/{bill_id}/post-approval", { params: { path: { bill_id: one.data.bill.id }, header: { "If-Match": one.data.etag, "Idempotency-Key": newKey() } } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await refresh();
    setStatus("Asked for approval: the bill posts when another person approves it.");
  };

  const sendEdit = async (ask: boolean) => {
    if (!f || !one.data) return;
    const { draft, errors: errs } = check(f, base);
    const why = reason.trim();
    setErrors(errs);
    setReasonMissing(!why);
    if (!draft || !why) return setStatus("Fix the fields marked, then try again.");
    setBusy(true);
    setProblem(null);
    setFailed(ask ? "The request was not made" : "The bill was not edited");
    const params = { path: { bill_id: one.data.bill.id }, header: { "If-Match": one.data.etag, "Idempotency-Key": newKey() } };
    const body = { ...draft, reason: why };
    const out = ask ? await guarded(confirm, () => api.POST("/v1/bills/{bill_id}/edit-approval", { params, body })) : await guarded(confirm, () => api.POST("/v1/bills/{bill_id}/edit", { params, body }));
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    setEditing(false);
    setReason("");
    await refresh();
    setStatus(ask ? "Asked for approval: the edit posts when another person approves it." : "Edited: the old entry is reversed and the new one posted.");
  };

  if (!isNew && one.isPending) return <p className="fy-secondary">Loading the bill…</p>;
  if (!isNew && one.isError) return <Refusal problem={problemOf(one.error)} title="The bill could not be read" />;
  if (!f) return null;

  const h = (k: string) => helpText(k);
  const title = isNew ? (credit ? "New credit note" : "New bill") : `${bill?.supplier?.name ?? (credit ? "Credit note" : "Bill")}${bill?.supplier_ref ? ` · ${bill.supplier_ref}` : ""}`;
  const foreign = (f.currency.trim() || base).toUpperCase() !== base;
  // A supplier resident outside Singapore: its bill says what the payment is for, so its withholding can be decided (FR-AP-19).
  const supplier = (vendors.data ?? []).find((c) => c.id === f.supplier_id);
  const askNature = !credit && (nonResident(supplier) || f.nature !== "");
  const needsApproval = problem?.code === overThreshold && bill?.status === "draft";
  // A posted bill with something still open is paid by recording the payment (FR-AP-02); the server refuses more than is open.
  const open = bill?.status === "posted" && bill.open && !bill.open.amount.startsWith("-") && !/^0(\.0*)?$/.test(bill.open.amount) ? bill.open : undefined;
  // A document payments, applications or refunds settle is voided or edited only once they are voided (BILL-13, CREDIT-08).
  const settled = Boolean(bill?.open && bill.open.amount !== bill.total.amount);
  const err = (k: string) =>
    errors[k] ? (
      <span className="fy-error" id={`${id}-${k}-error`}>
        {errors[k]}
      </span>
    ) : null;
  const described = (k: string) => (errors[k] ? `${id}-${k}-error` : undefined);

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <span>Purchases</span>
            <span aria-hidden="true">›</span>
            <Link to="/purchases/bills">Bills</Link>
          </nav>
          <div className="fy-row fy-row--wrap">
            <h1 className="fy-h1">{title}</h1>
            {bill && <StateChips states={bill.states} credit={credit} />}
          </div>
          {bill && (
            <p className="fy-secondary">
              {bill.number ? `${bill.number} · ` : ""}
              {bill.date ? `Dated ${date(bill.date)}` : "No date"}
              {bill.due_date && !credit ? ` · due ${date(bill.due_date)}` : ""}
              {bill.prepared_by ? ` · prepared by ${who(bill.prepared_by)}` : ""}
              {bill.posted_by ? ` · posted by ${who(bill.posted_by)}` : ""}
              {open ? ` · ${formatAmount(open.amount)} ${open.currency} ${credit ? "left" : "open"}` : ""}
            </p>
          )}
        </div>
      </div>
      <ReviewBanner screen={isNew || bill?.status === "draft" ? "bill-editor" : "bill-record"} />
      {needsApproval && problem && (
        <div className="fy-banner fy-banner--info" role="note">
          <Icon of={UserCheck} size={20} />
          <div className="fy-banner__text">
            <strong>Another person approves this bill</strong>
            <span className="fy-small">{problem.detail} The one who prepared it never approves it.</span>
          </div>
          <div className="fy-banner__action">
            <Button size="sm" variant="primary" type="button" onClick={() => void askPost()} disabled={busy} title={h("bill.ask-approval").tooltip}>
              <Icon of={UserCheck} />
              {h("bill.ask-approval").label}
            </Button>
          </div>
        </div>
      )}
      {problem && !needsApproval && <Refusal problem={problem} title={failed} />}
      {status && (
        <p className="fy-small" role="status">
          {status}
        </p>
      )}
      {bill?.approval_asked && (
        <div className="fy-banner fy-banner--info" role="status">
          <Icon of={UserCheck} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">Awaiting approval</span>
            <span className="fy-small">
              Another person approves it in the <Link to="/inbox">Inbox</Link>; it posts as they approve it.
            </span>
          </div>
        </div>
      )}
      {bill && !form && !editing && <Checks checks={bill.checks ?? []} />}
      {bill && bill.status !== "draft" && !form && !editing && (
        <PaymentPlanCard
          bill={bill}
          mayPlan={mayPrepare}
          onDone={(message) => {
            void refresh().then(() => setStatus(message));
          }}
        />
      )}
      {bill && !form && !editing && (
        <WithholdingCard
          bill={bill}
          base={base}
          mayRecord={mayPrepare}
          onDone={(message) => {
            void refresh().then(() => setStatus(message));
          }}
        />
      )}
      {bill && bill.status !== "draft" && !editing && (
        <Payments
          bill={bill}
          mayVoid={mayPost}
          ask={founder}
          onDone={(message) => {
            void refresh().then(() => setStatus(message));
          }}
        />
      )}
      <form className="fy-stack fy-stack--6" onSubmit={(e) => void save(e)} noValidate>
        <section className="fy-card" aria-labelledby={`${id}-supp`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-supp`}>
              Supplier and dates
            </h2>
          </div>
          <div className="fy-card__body fy-form fy-form--2">
            <div className="fy-field fy-field--full">
              <label className="fy-label" htmlFor={`${id}-supplier`}>
                Supplier
              </label>
              <select className="fy-select" id={`${id}-supplier`} value={f.supplier_id} disabled={!editable} onChange={(e) => set({ supplier_id: e.target.value })}>
                <option value="">Choose the supplier</option>
                {(vendors.data ?? []).map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.legal_name}
                  </option>
                ))}
              </select>
              <span className="fy-help">A contact marked as a vendor. Add one under Contacts.</span>
            </div>
            {askNature && (
              <div className="fy-field fy-field--full">
                <label className="fy-label" htmlFor={`${id}-nature`}>
                  {h("bill.nature").label}
                </label>
                <select
                  className="fy-select"
                  id={`${id}-nature`}
                  value={f.nature}
                  disabled={!editable}
                  onChange={(e) => set({ nature: e.target.value })}
                  aria-describedby={`${id}-nature-help`}
                >
                  <option value="">Choose what the payment is for</option>
                  {natures.map(([v, label]) => (
                    <option key={v} value={v}>
                      {label}
                    </option>
                  ))}
                </select>
                <span className="fy-help" id={`${id}-nature-help`}>
                  {supplier && nonResident(supplier) ? `${supplier.legal_name} is resident for tax in ${residenceOf(supplier)}. ` : ""}
                  {h("bill.nature").tooltip}
                </span>
              </div>
            )}
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-ref`}>
                {credit ? "Their credit note number" : "Their invoice number"}
              </label>
              <input className="fy-input" id={`${id}-ref`} value={f.supplier_ref} maxLength={60} disabled={!editable} onChange={(e) => set({ supplier_ref: e.target.value })} />
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-date`}>
                {credit ? "Credit note date" : "Invoice date"}
              </label>
              <input className="fy-input" id={`${id}-date`} type="date" value={f.date} disabled={!editable} onChange={(e) => set({ date: e.target.value })} />
            </div>
            {!credit && (
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-due`}>
                  Due date
                </label>
                <input className="fy-input" id={`${id}-due`} type="date" value={f.due_date} disabled={!editable} onChange={(e) => set({ due_date: e.target.value })} aria-describedby={`${id}-due-help`} />
                <span className="fy-help" id={`${id}-due-help`}>
                  {bill?.due_date && !f.due_date ? `Due ${date(bill.due_date)} by the payment terms. ` : ""}Leave it blank for the supplier's terms, or the organisation's.
                </span>
              </div>
            )}
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-currency`}>
                Currency
              </label>
              <input
                className="fy-input fy-mono"
                id={`${id}-currency`}
                value={f.currency}
                maxLength={3}
                disabled={!editable}
                onChange={(e) => set({ currency: e.target.value.toUpperCase() })}
                aria-invalid={Boolean(errors.currency) || undefined}
                aria-describedby={described("currency")}
              />
              {err("currency")}
            </div>
            {foreign && (
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-rate`}>
                  Rate ({base} for 1 {f.currency}, optional)
                </label>
                <input
                  className="fy-input fy-num"
                  id={`${id}-rate`}
                  inputMode="decimal"
                  value={f.fx_rate}
                  disabled={!editable}
                  onChange={(e) => set({ fx_rate: e.target.value })}
                  aria-invalid={Boolean(errors.fx_rate) || undefined}
                  aria-describedby={errors.fx_rate ? described("fx_rate") : `${id}-rate-help`}
                />
                {err("fx_rate") ?? (
                  <span className="fy-help" id={`${id}-rate-help`}>
                    Leave it blank to take the rate for the invoice date.
                  </span>
                )}
              </div>
            )}
          </div>
        </section>

        <InputTaxCard id={id} tax={f.tax} onChange={(tax) => set({ tax })} editable={editable} decisions={bill?.input_tax_decisions ?? []} dirty={Boolean(form)} />

        <BillLines
          id={id}
          lines={f.lines}
          onChange={(lines) => set({ lines })}
          editable={editable}
          errors={errors}
          currency={f.currency || base}
          base={base}
          stated={foreign}
          spreads={!credit}
          shares
          codes={codes.data?.codes ?? []}
          choices={choices}
          byId={byId}
          subtitle={`As the supplier's ${credit ? "credit note" : "invoice"} states them`}
          footer={bill && <p className="fy-small">Total {total(bill)}{bill.currency !== base && bill.payable ? ` · ${base} ${formatAmount(bill.payable.amount)} payable` : ""}</p>}
        />

        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-notes`}>
            Notes
          </label>
          <textarea className="fy-textarea" id={`${id}-notes`} value={f.notes} maxLength={2000} disabled={!editable} onChange={(e) => set({ notes: e.target.value })} />
        </div>

        {editing && (
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-reason`}>
              Why it is edited
            </label>
            <textarea
              className="fy-textarea"
              id={`${id}-reason`}
              value={reason}
              maxLength={500}
              onChange={(e) => {
                setReason(e.target.value);
                setReasonMissing(false);
              }}
              aria-invalid={reasonMissing || undefined}
              aria-describedby={reasonMissing ? `${id}-reason-error` : `${id}-reason-help`}
            />
            {reasonMissing ? (
              <span className="fy-error" id={`${id}-reason-error`}>
                Say why: the audit trail keeps the reason with the edit.
              </span>
            ) : (
              <span className="fy-help" id={`${id}-reason-help`}>
                The entry it had is reversed and the new one posted, in one go. {founder ? "In Founder mode another founder approves the edit first." : ""}
              </span>
            )}
          </div>
        )}

        <div className="fy-row fy-row--wrap gap-2">
          {editable && !editing && (
            <Button type="submit" disabled={busy} title={h("bill.save").tooltip}>
              <Icon of={Save} />
              {h("bill.save").label}
            </Button>
          )}
          {bill?.status === "draft" && !form && mayPost && !bill.approval_asked && (
            <Button variant="primary" type="button" onClick={() => void post()} disabled={busy || (bill.checks ?? []).some((c) => c.severity === "error")} title={h("bill.post").tooltip}>
              <Icon of={Send} />
              {h("bill.post").label}
            </Button>
          )}
          {bill?.status === "posted" && mayPost && !editing && (
            <>
              {open && !credit && (
                <Button variant="primary" type="button" onClick={() => setPaying(true)} title={h("bill.record-payment").tooltip}>
                  <Icon of={Banknote} />
                  {h("bill.record-payment").label}…
                </Button>
              )}
              {open && credit && (
                <>
                  <Button variant="primary" type="button" onClick={() => setApplying(true)} title={h("credit.apply").tooltip}>
                    <Icon of={Link2} />
                    {h("credit.apply").label}…
                  </Button>
                  <Button type="button" onClick={() => setRefunding(true)} title={h("credit.refund").tooltip}>
                    <Icon of={Banknote} />
                    {h("credit.refund").label}…
                  </Button>
                </>
              )}
              {!settled && (
                <>
                  <Button type="button" onClick={() => setEditing(true)} title={h("bill.edit").tooltip}>
                    <Icon of={Pencil} />
                    {h("bill.edit").label}
                  </Button>
                  <Button type="button" onClick={() => setVoiding(true)} title={h(founder ? "bill.ask-void" : "bill.void").tooltip}>
                    <Icon of={founder ? UserCheck : Undo2} />
                    {h(founder ? "bill.ask-void" : "bill.void").label}…
                  </Button>
                </>
              )}
            </>
          )}
          {editing && (
            <>
              <Button variant="primary" type="button" onClick={() => void sendEdit(founder)} disabled={busy} title={h(founder ? "bill.ask-edit" : "bill.post-edit").tooltip}>
                <Icon of={founder ? UserCheck : Send} />
                {h(founder ? "bill.ask-edit" : "bill.post-edit").label}
              </Button>
              <Button
                variant="ghost"
                type="button"
                onClick={() => {
                  setEditing(false);
                  setForm(null);
                  setReason("");
                }}
                disabled={busy}
              >
                <Icon of={X} />
                Cancel
              </Button>
            </>
          )}
          {!isNew && bill?.status === "draft" && form && <span className="fy-small fy-muted">Save the changes to see what posting writes.</span>}
        </div>
      </form>

      {bill && !editing && !form && <Releases bill={bill} base={base} />}
      {bill && !editing && !form && <Impact bill={bill} lines={bill.ledger_impact ?? []} accounts={byId} base={base} />}
      {bill && one.data && !editing && (
        <EvidencePanel record={{ kind: "bill", id: bill.id, etag: one.data.etag }} draft={bill.status === "draft"} mayAttach={roles.some((r) => billAttachers.includes(r))} onChanged={setStatus} />
      )}
      {bill && applying && (
        <ApplyCredit
          open={applying}
          credit={bill}
          onClose={() => setApplying(false)}
          onDone={(message) => {
            setApplying(false);
            void refresh().then(() => setStatus(message));
          }}
        />
      )}
      {bill && refunding && (
        <RecordPayment
          open={refunding}
          bill={bill}
          base={base}
          accounts={accounts.data ?? []}
          refund
          onClose={() => setRefunding(false)}
          onDone={(message) => {
            setRefunding(false);
            void refresh().then(() => setStatus(message));
          }}
        />
      )}
      {bill && paying && (
        <RecordPayment
          open={paying}
          bill={bill}
          base={base}
          accounts={accounts.data ?? []}
          onClose={() => setPaying(false)}
          onDone={(message) => {
            setPaying(false);
            void refresh().then(() => setStatus(message));
          }}
        />
      )}
      {bill && voiding && one.data && (
        <Void
          open={voiding}
          bill={bill}
          etag={one.data.etag}
          ask={founder}
          onClose={() => setVoiding(false)}
          onDone={(message) => {
            setVoiding(false);
            void refresh().then(() => setStatus(message));
          }}
        />
      )}
    </div>
  );
}
