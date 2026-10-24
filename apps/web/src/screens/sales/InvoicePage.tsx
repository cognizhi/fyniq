import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useLocation, useNavigate, useParams } from "@tanstack/react-router";
import { CircleX, Mail, Pencil, Save, Send, TriangleAlert, Undo2, UserCheck, X } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date, dateTime } from "../../format/date";
import { typed } from "../../format/decimal";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { listAccounts, postable, type Account } from "../accounting/data";
import { EvidencePanel, invoiceAttachers } from "../accounting/JournalEvidence";
import { who } from "../accounting/Journals";
import { InvoiceLines, lineKey, newLine, type Errors, type Line } from "./InvoiceLines";
import { issuers, preparers, StateChips, total, type Invoice } from "./Invoices";

type Draft = components["schemas"]["InvoiceDraft"];
type Check = components["schemas"]["InvoiceCheck"];
type EntryLine = components["schemas"]["EntryLine"];
type Send = components["schemas"]["DocumentSendOut"];

// The code the API answers when an invoice's total is above the self-approval threshold (FM-2): ask for approval.
const overThreshold = "INVOICE-10";

interface Form {
  customer_id: string;
  reference: string;
  date: string;
  due_date: string;
  currency: string;
  fx_rate: string;
  notes: string;
  invoice_id: string; // a credit note: the issued invoice it credits; blank for a standalone one
  lines: Line[];
}

function today(): string {
  return new Date().toLocaleDateString("en-CA");
}

// formOf is a saved invoice as the form shows it.
function formOf(i: Invoice): Form {
  return {
    customer_id: i.customer?.id ?? "",
    reference: i.reference ?? "",
    date: i.date ?? "",
    due_date: "",
    currency: i.currency,
    fx_rate: i.fx_rate ?? "",
    notes: i.notes ?? "",
    invoice_id: i.credits?.id ?? "",
    lines: (i.lines ?? []).map((l) => ({
      key: lineKey(),
      item_id: l.item_id ?? "",
      description: l.description ?? "",
      quantity: l.quantity,
      unit_price: l.unit_price,
      discount: l.discount_percent ?? "",
      account_id: l.account_id,
      gst_code: l.gst_code,
      spread_months: l.spread_months ? String(l.spread_months) : "",
      spread_from: l.spread_from?.slice(0, 7) ?? "",
      quote: l.quote,
    })),
  };
}

const blank = (l: Line) => !l.item_id && !l.account_id && !l.description.trim() && !l.unit_price.trim();
const exact = /^(0|[1-9]\d{0,13})(\.\d{1,6})?$/;

// check turns what was typed into the invoice to send, or says what to fix under each field. The API checks the rest
// when it is issued (FR-AR-03): the customer, the codes, the accounts and the GST.
export function check(f: Form, base: string, credit = false): { draft?: Draft; errors: Errors } {
  const errors: Errors = {};
  const currency = (f.currency.trim() || base).toUpperCase();
  if (!/^[A-Z]{3}$/.test(currency)) errors.currency = "Enter a currency code, such as SGD.";
  const rate = f.fx_rate.trim();
  if (currency !== base && rate && !/^(0|[1-9]\d{0,9})(\.\d{1,10})?$/.test(rate)) errors.fx_rate = `Enter the rate as ${base} for 1 ${currency}, such as 1.3472, or leave it blank to take the day's rate.`;
  const lines: Draft["lines"] = [];
  for (const l of f.lines) {
    if (blank(l)) continue;
    const quantity = typed(l.quantity);
    const price = typed(l.unit_price);
    const discount = l.discount.trim().replace(/%$/, "").trim();
    if (!l.account_id) errors[`${l.key}.account`] = "Choose the revenue account the line earns.";
    if (!l.gst_code) errors[`${l.key}.gst_code`] = "Choose the line's GST code, such as SR for a standard-rated supply.";
    if (quantity === null || !exact.test(quantity)) errors[`${l.key}.quantity`] = "Enter how many, such as 1 or 2.5, with up to 6 decimals.";
    if (price === null || price === "" || !exact.test(price)) errors[`${l.key}.unit_price`] = "Enter the price of one before GST, such as 1,500.00.";
    if (discount && !/^(100(\.0{1,2})?|\d{1,2}(\.\d{1,2})?)$/.test(discount)) errors[`${l.key}.discount`] = "Enter a percentage from 0 to 100, such as 10, or leave it blank.";
    const months = l.spread_months.trim();
    const from = l.spread_from.trim();
    if ((months && !/^([1-9]\d?|1[01]\d|120)$/.test(months)) || (!months && from)) errors[`${l.key}.spread_months`] = "Enter how many months to spread it over, 1 to 120, or leave both spread fields blank.";
    if (from && !/^\d{4}-(0[1-9]|1[0-2])$/.test(from)) errors[`${l.key}.spread_from`] = "Enter the month the service starts, such as 2026-10, or leave it blank for the invoice's month.";
    lines.push({
      item_id: l.item_id || undefined,
      description: l.description.trim() || undefined,
      quantity: quantity ?? "1",
      unit_price: price ?? "0",
      discount_percent: discount || undefined,
      account_id: l.account_id,
      gst_code: l.gst_code,
      spread_months: months ? Number(months) : undefined,
      spread_from: months && from ? `${from}-01` : undefined,
      quote: l.quote,
    });
  }
  if (lines.length === 0) errors.lines = credit ? "Add a line for what the credit note gives back." : "Add a line for what the invoice is for.";
  if (Object.keys(errors).length > 0) return { errors };
  return {
    errors,
    draft: {
      customer_id: f.customer_id || undefined,
      reference: f.reference.trim() || undefined,
      date: f.date || undefined,
      due_date: credit ? undefined : f.due_date || undefined,
      currency,
      fx_rate: currency !== base && rate ? rate : undefined,
      notes: f.notes.trim() || undefined,
      invoice_id: credit ? f.invoice_id || undefined : undefined,
      lines,
    },
  };
}

// Checks lists what stops a draft being issued (✖) and what it is issued with (⚠).
function Checks({ checks }: { checks: Check[] }) {
  const id = useId();
  if (checks.length === 0) return null;
  const errors = checks.filter((c) => c.severity === "error").length;
  return (
    <section className="fy-card" aria-labelledby={id}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={id}>
          {errors > 0 ? (errors === 1 ? "1 thing to fix before it can be issued" : `${errors} things to fix before it can be issued`) : "To check before it is issued"}
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
            <span className="fy-sr">{c.severity === "error" ? "Stops it being issued" : "Review it"}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}

// Totals are the invoice's net, its GST by code, its total, the deposits it deducts and what is left to pay, as the
// server worked them out: never computed here (FR-UX-22).
function Totals({ invoice }: { invoice: Invoice }) {
  const id = useId();
  const cur = invoice.currency;
  const row = (label: string, m?: components["schemas"]["Money"], strong = false) =>
    m && (
      <div className="fy-row fy-row--between">
        <span className={strong ? "fy-strong" : "fy-secondary"}>{label}</span>
        <span className={`fy-num${strong ? " fy-strong" : ""}`}>
          {formatAmount(m.amount)} {m.currency}
        </span>
      </div>
    );
  return (
    <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-labelledby={id}>
      <h2 className="fy-h3" id={id}>
        Totals
      </h2>
      {row("Before GST", invoice.net)}
      {invoice.gst_charged
        ? (invoice.gst_subtotals ?? []).map((g) => (
            <div key={g.gst_code} className="fy-row fy-row--between">
              <span className="fy-secondary">
                GST {g.gst_code} on {formatAmount(g.net.amount)}
              </span>
              <span className="fy-num">
                {formatAmount(g.gst.amount)} {cur}
              </span>
            </div>
          ))
        : invoice.net && <p className="fy-small fy-muted">No GST: the organisation is not GST-registered on the invoice's date.</p>}
      {row("Total", invoice.total, true)}
      {(invoice.deposits ?? []).map((d) => (
        <div key={d.deposit_id} className="fy-row fy-row--between">
          <span className="fy-secondary">Less deposit {d.number ?? ""}</span>
          <span className="fy-num">
            −{formatAmount(d.amount.amount)} {cur}
          </span>
        </div>
      ))}
      {(invoice.deposits ?? []).length > 0 && row("Left to pay", invoice.balance, true)}
    </section>
  );
}

// Impact is the ledger impact in Expert mode (debits and credits by account), and in Basic mode one plain sentence.
function Impact({ invoice, lines, accounts, base }: { invoice: Invoice; lines: EntryLine[]; accounts: Map<string, Account>; base: string }) {
  const id = useId();
  if (!invoice.receivable) return null;
  const owed = `${base} ${formatAmount(invoice.receivable.amount)}`;
  const issued = invoice.status !== "draft";
  const credit = invoice.kind === "credit_note";
  return (
    <>
      <p className="fy-plain fy-basic-only fy-small">
        {credit ? (issued ? "This credit note took " : "This credit note takes ") : issued ? "This invoice added " : "This invoice adds "}
        {owed} {credit ? "off" : "to"} what {invoice.customer?.name ?? "the customer"} owes{invoice.currency !== base ? ` (${total(invoice)})` : ""}.
      </p>
      {lines.length > 0 && (
        <section className="fy-card fy-expert-only" aria-labelledby={id}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={id}>
              Ledger impact
            </h2>
            <span className="fy-small fy-muted">{issued ? "As posted" : "What issuing writes"}</span>
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
                const isCredit = l.base_amount.amount.startsWith("-");
                const amount = formatAmount(l.base_amount.amount.replace(/^-/, ""));
                return (
                  <tr key={l.line_no}>
                    <td>
                      <span className="fy-acct">{a?.code ?? "—"}</span>
                      {a?.name ?? "An account not in the chart"}
                      {l.amount.currency !== base ? ` · ${l.amount.currency} ${formatAmount(l.amount.amount.replace(/^-/, ""))}` : ""}
                    </td>
                    <td className="fy-num">{isCredit ? "" : amount}</td>
                    <td className="fy-num">{isCredit ? amount : ""}</td>
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

const deliveryWords: Record<Send["delivery"], string> = { sent: "Sent", delivered: "Delivered", viewed: "Viewed", bounced: "Bounced" };

// Sends are each email of the invoice (FR-AR-06): to whom, when, by whom, and its delivery.
function Sends({ sends }: { sends: Send[] }) {
  const id = useId();
  if (sends.length === 0) return null;
  return (
    <section className="fy-card" aria-labelledby={id}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={id}>
          Emails
        </h2>
      </div>
      <ul className="fy-list">
        {sends.map((s) => (
          <li key={s.id} className="fy-list__item">
            <span className="fy-list__icon">
              <Icon of={Mail} size={20} />
            </span>
            <span className="fy-list__main">
              <span className="fy-list__title whitespace-normal">To {s.to}</span>
              <span className="fy-list__meta whitespace-normal">
                {dateTime(s.sent_at)}
                {s.sent_by ? ` · by ${who(s.sent_by)}` : ""} · version {s.version}
              </span>
            </span>
            <span className={`fy-chip fy-chip--${s.delivery === "bounced" ? "critical" : s.delivery === "sent" ? "neutral" : "good"}`}>{deliveryWords[s.delivery]}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}

// EmailDialog emails an issued invoice to the customer's billing email or another address (FR-AR-06).
function EmailDialog({ open, invoice, etag, onClose, onDone }: { open: boolean; invoice: Invoice; etag: string; onClose: () => void; onDone: (message: string) => void }) {
  const confirm = useConfirm();
  const id = useId();
  const [to, setTo] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText("invoice.email");
  const run = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/invoices/{invoice_id}/email", {
        params: { path: { invoice_id: invoice.id }, header: { "If-Match": etag, "Idempotency-Key": newKey() } },
        body: to.trim() ? { to: to.trim() } : undefined,
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone(`Emailed to ${out.data.sends?.[0]?.to ?? "the customer"}, with its PDF and a secure link.`);
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      eyebrow={invoice.number}
      title="Email this invoice?"
      description="It goes from the organisation's invoices sender, with the PDF of this version and a secure link the customer opens without signing in. The PDF is kept with the invoice."
    >
      {problem && <Refusal problem={problem} title="The invoice was not emailed" />}
      <form className="fy-stack fy-stack--3" onSubmit={run} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-to`}>
            Send to (optional)
          </label>
          <input className="fy-input" id={`${id}-to`} type="email" value={to} maxLength={254} onChange={(e) => setTo(e.target.value)} aria-describedby={`${id}-to-help`} />
          <span className="fy-help" id={`${id}-to-help`}>
            Leave it blank for the customer's billing email.
          </span>
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy} title={h.tooltip}>
            <Icon of={Mail} />
            {h.label}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// Void voids an issued invoice with a date and a reason; in Founder mode it asks another founder first (FM-1).
function Void({ open, invoice, etag, ask, onClose, onDone }: { open: boolean; invoice: Invoice; etag: string; ask: boolean; onClose: () => void; onDone: (message: string) => void }) {
  const confirm = useConfirm();
  const id = useId();
  const [on, setOn] = useState(today);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [missing, setMissing] = useState(false);
  const h = helpText(ask ? "invoice.ask-void" : "invoice.void");
  const run = async (e: FormEvent) => {
    e.preventDefault();
    if (!reason.trim()) return setMissing(true);
    setBusy(true);
    setProblem(null);
    const params = { path: { invoice_id: invoice.id }, header: { "If-Match": etag, "Idempotency-Key": newKey() } };
    const body = { date: on, reason: reason.trim() };
    if (ask) {
      const out = await guarded(confirm, () => api.POST("/v1/invoices/{invoice_id}/void-approval", { params, body }));
      setBusy(false);
      if (!out.data) return setProblem(problemOf(out.error));
      return onDone("Asked for approval: the invoice is voided when another person approves it.");
    }
    const out = await guarded(confirm, () => api.POST("/v1/invoices/{invoice_id}/void", { params, body }));
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone("Voided: a reversing entry posted.");
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      eyebrow={invoice.number}
      title={ask ? "Ask to void this invoice?" : "Void this invoice?"}
      description={
        ask
          ? "In Founder mode another founder approves a void first. Then a reversing entry posts on the date you choose; the invoice stays in the books, marked voided."
          : "A reversing entry with every line the other way round posts on the date you choose. The invoice stays in the books, marked voided; nothing is deleted."
      }
    >
      {problem && <Refusal problem={problem} title={ask ? "The request was not made" : "The invoice was not voided"} />}
      <form className="fy-stack fy-stack--3" onSubmit={run} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-date`}>
            Date of the reversing entry
          </label>
          <input className="fy-input" id={`${id}-date`} type="date" value={on} min={invoice.date} onChange={(e) => setOn(e.target.value)} />
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

// InvoicePage is an invoice or a credit note (docs/design/screens InvoiceEditor and InvoiceRecord; FR-AR-03, FR-AR-04,
// FR-AR-06, FR-GL-12): a new one, a draft its preparer changes and issues, or an issued one, read-only, which is emailed
// to the customer, or voided or edited with a reason. The lines are typed as quantity × unit price before GST; the
// server works out each line's net, the GST and the totals, and what issuing writes. An invoice above the self-approval
// threshold, and in Founder mode a void or an edit, is asked for and approved by another person (FM-1, FM-2, SOD-3).
export function InvoicePage() {
  const { invoiceId } = useParams({ strict: false }) as { invoiceId?: string };
  const { pathname } = useLocation();
  const isNew = !invoiceId;
  const { session, organisation } = useSignedIn();
  const base = organisation?.base_currency ?? "SGD";
  const founder = organisation?.team_mode === "founder";
  const queries = useQueryClient();
  const navigate = useNavigate();
  const confirm = useConfirm();
  const id = useId();
  const roles = session.roles ?? [];
  const mayPrepare = roles.some((r) => preparers.includes(r));
  const mayIssue = roles.some((r) => issuers.includes(r));

  const accounts = useQuery({ queryKey: ["accounts"], queryFn: listAccounts });
  const customers = useQuery({
    queryKey: ["contacts", "customer"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/contacts", { params: { query: { role: "customer", page_size: 500 } } });
      if (error) throw error;
      return (data.items ?? []).filter((c) => c.active);
    },
  });
  const items = useQuery({
    queryKey: ["items"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/items", { params: { query: { page_size: 500 } } });
      if (error) throw error;
      return (data.items ?? []).filter((it) => it.active);
    },
  });
  const one = useQuery({
    queryKey: ["invoice", invoiceId],
    enabled: !isNew,
    queryFn: async () => {
      const r = await api.GET("/v1/invoices/{invoice_id}", { params: { path: { invoice_id: invoiceId ?? "" } } });
      if (r.error) throw r.error;
      return { invoice: r.data, etag: r.response.headers.get("ETag") ?? "" };
    },
  });
  const invoice = one.data?.invoice;
  // A credit note (FR-AR-04) is entered and issued as an invoice is, as its reverse.
  const credit = invoice ? invoice.kind === "credit_note" : pathname.endsWith("/sales/credit-notes/new");
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
  const [emailing, setEmailing] = useState(false);

  const editable = isNew || (invoice?.status === "draft" && mayPrepare) || editing;
  const f: Form | null =
    form ?? (isNew ? { customer_id: "", reference: "", date: today(), due_date: "", currency: base, fx_rate: "", notes: "", invoice_id: "", lines: [newLine()] } : invoice ? formOf(invoice) : null);
  const codes = useQuery({
    queryKey: ["sales-gst-codes", f?.date ?? ""],
    enabled: Boolean(f),
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/sales/gst-codes", { params: { query: { on: f?.date || undefined } } });
      if (error) throw error;
      return data;
    },
  });
  // A credit note credits an issued invoice of its customer, or stands alone.
  const credited = useQuery({
    queryKey: ["invoices", "invoice", "posted", f?.customer_id ?? ""],
    enabled: credit && Boolean(f?.customer_id) && editable,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/invoices", { params: { query: { kind: "invoice", status: "posted", page_size: 200 } } });
      if (error) throw error;
      return (data.items ?? []).filter((i) => i.customer?.id === f?.customer_id);
    },
  });
  const byId = new Map((accounts.data ?? []).map((a) => [a.id, a]));
  const choices = postable(accounts.data ?? []).filter((a) => a.type === "revenue");
  const set = (patch: Partial<Form>) => f && setForm({ ...f, ...patch });

  const refresh = async () => {
    setForm(null);
    await queries.invalidateQueries({ queryKey: ["invoice", invoiceId] });
    await queries.invalidateQueries({ queryKey: ["invoices"] });
  };

  const save = async (e?: FormEvent) => {
    e?.preventDefault();
    if (!f) return;
    const { draft, errors: errs } = check(f, base, credit);
    setErrors(errs);
    if (!draft) return setStatus("Fix the fields marked, then save again.");
    setBusy(true);
    setProblem(null);
    setFailed(credit ? "The credit note was not saved" : "The invoice was not saved");
    if (isNew) {
      const out = await api.POST("/v1/invoices", { params: { header: { "Idempotency-Key": newKey() } }, body: credit ? { ...draft, kind: "credit_note" } : draft });
      setBusy(false);
      if (!out.data) return setProblem(problemOf(out.error));
      await queries.invalidateQueries({ queryKey: ["invoices"] });
      void navigate({ to: "/sales/invoices/$invoiceId", params: { invoiceId: out.data.id } });
      return;
    }
    if (!one.data) return;
    const out = await api.PUT("/v1/invoices/{invoice_id}", { params: { path: { invoice_id: one.data.invoice.id }, header: { "If-Match": one.data.etag } }, body: draft });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await refresh();
    setStatus("Saved.");
  };

  const issue = async () => {
    if (!one.data) return;
    setBusy(true);
    setProblem(null);
    setFailed(credit ? "The credit note was not issued" : "The invoice was not issued");
    const out = await guarded(confirm, () =>
      api.POST("/v1/invoices/{invoice_id}/issue", { params: { path: { invoice_id: one.data.invoice.id }, header: { "If-Match": one.data.etag, "Idempotency-Key": newKey() } } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await refresh();
    setStatus(`Issued as ${out.data.invoice.number ?? (credit ? "a credit note" : "an invoice")}.`);
  };

  const askIssue = async () => {
    if (!one.data) return;
    setBusy(true);
    setProblem(null);
    setFailed("The request was not made");
    const out = await guarded(confirm, () =>
      api.POST("/v1/invoices/{invoice_id}/issue-approval", { params: { path: { invoice_id: one.data.invoice.id }, header: { "If-Match": one.data.etag, "Idempotency-Key": newKey() } } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await refresh();
    setStatus("Asked for approval: it is issued when another person approves it.");
  };

  const sendEdit = async (ask: boolean) => {
    if (!f || !one.data) return;
    const { draft, errors: errs } = check(f, base, credit);
    const why = reason.trim();
    setErrors(errs);
    setReasonMissing(!why);
    if (!draft || !why) return setStatus("Fix the fields marked, then try again.");
    setBusy(true);
    setProblem(null);
    setFailed(ask ? "The request was not made" : "The invoice was not edited");
    const params = { path: { invoice_id: one.data.invoice.id }, header: { "If-Match": one.data.etag, "Idempotency-Key": newKey() } };
    const body = { ...draft, reason: why };
    const out = ask
      ? await guarded(confirm, () => api.POST("/v1/invoices/{invoice_id}/edit-approval", { params, body }))
      : await guarded(confirm, () => api.POST("/v1/invoices/{invoice_id}/edit", { params, body }));
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    setEditing(false);
    setReason("");
    await refresh();
    setStatus(ask ? "Asked for approval: the edit posts when another person approves it." : "Edited: the old entry is reversed and the new one posted.");
  };

  if (!isNew && one.isPending) return <p className="fy-secondary">Loading the invoice…</p>;
  if (!isNew && one.isError) return <Refusal problem={problemOf(one.error)} title="The invoice could not be read" />;
  if (!f) return null;

  const h = (k: string) => helpText(k);
  const noun = credit ? "credit note" : "invoice";
  const title = isNew ? (credit ? "New credit note" : "New invoice") : `${invoice?.number || (credit ? "Draft credit note" : "Draft invoice")}${invoice?.customer?.name ? ` · ${invoice.customer.name}` : ""}`;
  const foreign = (f.currency.trim() || base).toUpperCase() !== base;
  const needsApproval = problem?.code === overThreshold && invoice?.status === "draft";
  // A document receipts or credits settle is voided or edited only once they are voided (INVOICE-13).
  const settled = Boolean(invoice?.open && invoice.total && invoice.open.amount !== invoice.total.amount);
  const err = (k: string) =>
    errors[k] ? (
      <span className="fy-error" id={`${id}-${k}-error`}>
        {errors[k]}
      </span>
    ) : null;
  const described = (k: string, help?: string) => (errors[k] ? `${id}-${k}-error` : help);

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <span>Sales</span>
            <span aria-hidden="true">›</span>
            <Link to="/sales/invoices">Invoices</Link>
          </nav>
          <div className="fy-row fy-row--wrap">
            <h1 className="fy-h1">{title}</h1>
            {invoice && <StateChips states={invoice.states} credit={credit} />}
          </div>
          {invoice && (
            <p className="fy-secondary">
              {invoice.date ? `Dated ${date(invoice.date)}` : "No date"}
              {invoice.due_date && !credit ? ` · due ${date(invoice.due_date)}` : ""}
              {invoice.prepared_by ? ` · prepared by ${who(invoice.prepared_by)}` : ""}
              {invoice.posted_by ? ` · issued by ${who(invoice.posted_by)}` : ""}
              {invoice.status === "posted" && invoice.open ? ` · ${formatAmount(invoice.open.amount)} ${invoice.open.currency} ${credit ? "left" : "open"}` : ""}
            </p>
          )}
        </div>
      </div>
      <ReviewBanner screen={isNew || invoice?.status === "draft" ? "invoice-editor" : "invoice-record"} />
      {invoice?.recurring_invoice && (
        <p className="fy-small fy-muted">
          Made by the scheduler from a recurring invoice, version {invoice.recurring_invoice.version} of its content; whoever wrote that version does not approve its issue.
        </p>
      )}
      {needsApproval && problem && (
        <div className="fy-banner fy-banner--info" role="note">
          <Icon of={UserCheck} size={20} />
          <div className="fy-banner__text">
            <strong>Another person approves this {noun}</strong>
            <span className="fy-small">{problem.detail} The one who prepared it never approves it.</span>
          </div>
          <div className="fy-banner__action">
            <Button size="sm" variant="primary" type="button" onClick={() => void askIssue()} disabled={busy} title={h("invoice.ask-approval").tooltip}>
              <Icon of={UserCheck} />
              {h("invoice.ask-approval").label}
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
      {invoice?.approval_asked && (
        <div className="fy-banner fy-banner--info" role="status">
          <Icon of={UserCheck} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">Awaiting approval</span>
            <span className="fy-small">
              Another person approves it in the <Link to="/inbox">Inbox</Link>; it is issued as they approve it.
            </span>
          </div>
        </div>
      )}
      {invoice && !form && !editing && <Checks checks={invoice.checks ?? []} />}
      <form className="fy-stack fy-stack--6" onSubmit={(e) => void save(e)} noValidate>
        <section className="fy-card" aria-labelledby={`${id}-cust`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-cust`}>
              Customer and dates
            </h2>
          </div>
          <div className="fy-card__body fy-form fy-form--2">
            <div className="fy-field fy-field--full">
              <label className="fy-label" htmlFor={`${id}-customer`}>
                Customer
              </label>
              <select className="fy-select" id={`${id}-customer`} value={f.customer_id} disabled={!editable} onChange={(e) => set({ customer_id: e.target.value, invoice_id: "" })}>
                <option value="">Choose the customer</option>
                {(customers.data ?? []).map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.legal_name}
                  </option>
                ))}
              </select>
              <span className="fy-help">A contact marked as a customer. Add one under Contacts.</span>
            </div>
            {credit && (
              <div className="fy-field fy-field--full">
                <label className="fy-label" htmlFor={`${id}-credits`}>
                  Invoice it credits (optional)
                </label>
                <select className="fy-select" id={`${id}-credits`} value={f.invoice_id} disabled={!editable} onChange={(e) => set({ invoice_id: e.target.value })} aria-describedby={`${id}-credits-help`}>
                  <option value="">None: a standalone credit note</option>
                  {invoice?.credits && !(credited.data ?? []).some((i) => i.id === invoice.credits?.id) && <option value={invoice.credits.id}>{invoice.credits.number ?? "The invoice it credits"}</option>}
                  {(credited.data ?? []).map((i) => (
                    <option key={i.id} value={i.id}>
                      {i.number} · {total(i)}
                    </option>
                  ))}
                </select>
                <span className="fy-help" id={`${id}-credits-help`}>
                  {h("credit-note.credits").tooltip}
                </span>
              </div>
            )}
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-ref`}>
                Customer's reference
              </label>
              <input className="fy-input" id={`${id}-ref`} value={f.reference} maxLength={60} disabled={!editable} onChange={(e) => set({ reference: e.target.value })} aria-describedby={`${id}-ref-help`} />
              <span className="fy-help" id={`${id}-ref-help`}>
                Such as their purchase order number.
              </span>
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
                  {invoice?.due_date && !f.due_date ? `Due ${date(invoice.due_date)} by the payment terms. ` : ""}Leave it blank for the customer's terms, or the organisation's.
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
                  aria-describedby={described("fx_rate", `${id}-rate-help`)}
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

        <InvoiceLines
          id={id}
          lines={f.lines}
          onChange={(lines) => set({ lines })}
          editable={editable}
          errors={errors}
          currency={f.currency || base}
          spreads={!credit}
          codes={codes.data?.codes ?? []}
          choices={choices}
          items={items.data ?? []}
          subtitle="Quantity × unit price before GST; the GST and the totals are worked out when saved"
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
            <Button type="submit" disabled={busy} title={h("invoice.save").tooltip}>
              <Icon of={Save} />
              {h("invoice.save").label}
            </Button>
          )}
          {invoice?.status === "draft" && !form && mayIssue && !invoice.approval_asked && (
            <Button variant="primary" type="button" onClick={() => void issue()} disabled={busy || (invoice.checks ?? []).some((c) => c.severity === "error")} title={h("invoice.issue").tooltip}>
              <Icon of={Send} />
              {h("invoice.issue").label}
            </Button>
          )}
          {invoice?.status === "posted" && mayIssue && !editing && (
            <>
              {!credit && (
                <Button variant="primary" type="button" onClick={() => setEmailing(true)} title={h("invoice.email").tooltip}>
                  <Icon of={Mail} />
                  {h("invoice.email").label}…
                </Button>
              )}
              {!settled && (
                <>
                  <Button type="button" onClick={() => setEditing(true)} title={h("invoice.edit").tooltip}>
                    <Icon of={Pencil} />
                    {h("invoice.edit").label}
                  </Button>
                  <Button type="button" onClick={() => setVoiding(true)} title={h(founder ? "invoice.ask-void" : "invoice.void").tooltip}>
                    <Icon of={founder ? UserCheck : Undo2} />
                    {h(founder ? "invoice.ask-void" : "invoice.void").label}…
                  </Button>
                </>
              )}
            </>
          )}
          {editing && (
            <>
              <Button variant="primary" type="button" onClick={() => void sendEdit(founder)} disabled={busy} title={h(founder ? "invoice.ask-edit" : "invoice.post-edit").tooltip}>
                <Icon of={founder ? UserCheck : Send} />
                {h(founder ? "invoice.ask-edit" : "invoice.post-edit").label}
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
          {!isNew && invoice?.status === "draft" && form && <span className="fy-small fy-muted">Save the changes to see the totals and what issuing writes.</span>}
        </div>
      </form>

      {invoice && !editing && !form && <Totals invoice={invoice} />}
      {invoice && !editing && <Sends sends={invoice.sends ?? []} />}
      {invoice && !editing && !form && <Impact invoice={invoice} lines={invoice.ledger_impact ?? []} accounts={byId} base={base} />}
      {invoice && one.data && !editing && (
        <EvidencePanel record={{ kind: "invoice", id: invoice.id, etag: one.data.etag }} draft={invoice.status === "draft"} mayAttach={roles.some((r) => invoiceAttachers.includes(r))} onChanged={setStatus} />
      )}
      {invoice && emailing && one.data && (
        <EmailDialog
          open={emailing}
          invoice={invoice}
          etag={one.data.etag}
          onClose={() => setEmailing(false)}
          onDone={(message) => {
            setEmailing(false);
            void refresh().then(() => setStatus(message));
          }}
        />
      )}
      {invoice && voiding && one.data && (
        <Void
          open={voiding}
          invoice={invoice}
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
