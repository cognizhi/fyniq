import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useLocation, useNavigate, useParams } from "@tanstack/react-router";
import { Ban, CircleCheck, FileCheck, Mail, Save, Send, XCircle } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date } from "../../format/date";
import { typed } from "../../format/decimal";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { listAccounts, postable } from "../accounting/data";
import { who } from "../accounting/Journals";
import { InvoiceLines, lineKey, newLine, type Errors, type Line } from "./InvoiceLines";
import { issuers, preparers } from "./Invoices";
import { Checks, EmailDialog, Sends, Totals } from "./parts";
import { QuoteChips, type Quote } from "./Quotes";

type Draft = components["schemas"]["QuoteDraft"];

interface Form {
  customer_id: string;
  reference: string;
  date: string;
  valid_until: string;
  currency: string;
  fx_rate: string;
  notes: string;
  lines: Line[];
}

function today(): string {
  return new Date().toLocaleDateString("en-CA");
}

// in30 is the day 30 days from today, a new quote's validity by default.
function in30(): string {
  const d = new Date();
  d.setDate(d.getDate() + 30);
  return d.toLocaleDateString("en-CA");
}

function formOf(q: Quote): Form {
  return {
    customer_id: q.customer?.id ?? "",
    reference: q.reference ?? "",
    date: q.date ?? "",
    valid_until: q.valid_until ?? "",
    currency: q.currency,
    fx_rate: q.fx_rate ?? "",
    notes: q.notes ?? "",
    lines: (q.lines ?? []).map((l) => ({
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
    })),
  };
}

const blank = (l: Line) => !l.item_id && !l.account_id && !l.description.trim() && !l.unit_price.trim();
const exact = /^(0|[1-9]\d{0,13})(\.\d{1,6})?$/;

// check turns what was typed into the quote to send, or says what to fix under each field. A quote names its validity
// date; an order may name none (QUOTE-03). The API checks the rest when it is sent.
export function check(f: Form, base: string, order = false): { draft?: Draft; errors: Errors } {
  const errors: Errors = {};
  const currency = (f.currency.trim() || base).toUpperCase();
  if (!/^[A-Z]{3}$/.test(currency)) errors.currency = "Enter a currency code, such as SGD.";
  if (!order && !f.valid_until) errors.valid_until = "Enter the last day the quote is valid.";
  if (f.valid_until && f.date && f.valid_until < f.date) errors.valid_until = "Enter a day on or after the quote's date.";
  const rate = f.fx_rate.trim();
  if (currency !== base && rate && !/^(0|[1-9]\d{0,9})(\.\d{1,10})?$/.test(rate)) errors.fx_rate = `Enter the rate as ${base} for 1 ${currency}, or leave it blank to take the day's rate.`;
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
    lines.push({
      item_id: l.item_id || undefined,
      description: l.description.trim() || undefined,
      quantity: quantity ?? "1",
      unit_price: price ?? "0",
      discount_percent: discount || undefined,
      account_id: l.account_id,
      gst_code: l.gst_code,
    });
  }
  if (lines.length === 0) errors.lines = "Add a line for what is offered.";
  if (Object.keys(errors).length > 0) return { errors };
  return {
    errors,
    draft: {
      customer_id: f.customer_id || undefined,
      reference: f.reference.trim() || undefined,
      date: f.date || undefined,
      valid_until: f.valid_until || undefined,
      currency,
      fx_rate: currency !== base && rate ? rate : undefined,
      notes: f.notes.trim() || undefined,
      lines,
    },
  };
}

// Answer records the customer's answer to a sent quote, as a person heard it: accepted with who said so, or declined.
function Answer({ open, quote, etag, outcome, onClose, onDone }: { open: boolean; quote: Quote; etag: string; outcome: "accept" | "decline"; onClose: () => void; onDone: (message: string) => void }) {
  const confirm = useConfirm();
  const id = useId();
  const [on, setOn] = useState(today);
  const [name, setName] = useState("");
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText(outcome === "accept" ? "quote.accept" : "quote.decline");
  const run = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const params = { path: { quote_id: quote.id }, header: { "If-Match": etag, "Idempotency-Key": newKey() } };
    const body = { date: on, name: name.trim() || undefined, note: note.trim() || undefined };
    const out =
      outcome === "accept"
        ? await guarded(confirm, () => api.POST("/v1/quotes/{quote_id}/accept", { params, body }))
        : await guarded(confirm, () => api.POST("/v1/quotes/{quote_id}/decline", { params, body }));
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone(outcome === "accept" ? "Recorded as accepted: it can be invoiced now." : "Recorded as declined.");
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      eyebrow={quote.number}
      title={outcome === "accept" ? "Record that the customer accepted it?" : "Record that the customer declined it?"}
      description="Record the answer the customer gave you, such as by email or phone; the audit trail keeps who recorded it. A customer who answers through the secure link records it themselves."
    >
      {problem && <Refusal problem={problem} title="The answer was not recorded" />}
      <form className="fy-stack fy-stack--3" onSubmit={run} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-date`}>
            The day they answered
          </label>
          <input className="fy-input" id={`${id}-date`} type="date" value={on} onChange={(e) => setOn(e.target.value)} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-name`}>
            Who answered (optional)
          </label>
          <input className="fy-input" id={`${id}-name`} value={name} maxLength={200} onChange={(e) => setName(e.target.value)} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-note`}>
            Note (optional)
          </label>
          <textarea className="fy-textarea" id={`${id}-note`} value={note} maxLength={2000} onChange={(e) => setNote(e.target.value)} />
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy} title={h.tooltip}>
            <Icon of={outcome === "accept" ? CircleCheck : XCircle} />
            {h.label}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// CancelQuote cancels a sent quote or order, with a reason; one that issued invoices take is not cancelled (QUOTE-12).
function CancelQuote({ open, quote, etag, onClose, onDone }: { open: boolean; quote: Quote; etag: string; onClose: () => void; onDone: (message: string) => void }) {
  const confirm = useConfirm();
  const id = useId();
  const [reason, setReason] = useState("");
  const [missing, setMissing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText("quote.cancel");
  const run = async (e: FormEvent) => {
    e.preventDefault();
    if (!reason.trim()) return setMissing(true);
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/quotes/{quote_id}/cancel", { params: { path: { quote_id: quote.id }, header: { "If-Match": etag, "Idempotency-Key": newKey() } }, body: { reason: reason.trim() } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone("Cancelled: it no longer counts in the pipeline.");
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      eyebrow={quote.number}
      title="Cancel it?"
      description="A cancelled quote or order stays in the records, marked cancelled, and no longer counts in the pipeline. Nothing posts."
    >
      {problem && <Refusal problem={problem} title="It was not cancelled" />}
      <form className="fy-stack fy-stack--3" onSubmit={run} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-reason`}>
            Why it is cancelled
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
              Say why: the audit trail keeps the reason.
            </span>
          )}
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Keep it
          </Button>
          <Button variant="primary" type="submit" disabled={busy} title={h.tooltip}>
            <Icon of={Ban} />
            {h.label}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// Invoicing is what a sent quote's lines have been invoiced (FR-AR-02), and the invoices made from it.
function Invoicing({ quote }: { quote: Quote }) {
  const id = useId();
  const rows = quote.invoicing ?? [];
  const invoices = quote.invoices ?? [];
  if (rows.length === 0 && invoices.length === 0) return null;
  return (
    <section className="fy-card" aria-labelledby={id}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={id}>
          Invoicing
        </h2>
      </div>
      {rows.length > 0 && (
        <table className="fy-table fy-table--compact" aria-labelledby={id}>
          <thead>
            <tr>
              <th scope="col">Line</th>
              <th scope="col" className="fy-num">
                Quoted
              </th>
              <th scope="col" className="fy-num">
                Invoiced
              </th>
              <th scope="col" className="fy-num">
                In drafts
              </th>
              <th scope="col" className="fy-num">
                Left
              </th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.line}>
                <td>Line {r.line}</td>
                <td className="fy-num">{r.quoted}</td>
                <td className="fy-num">{r.invoiced}</td>
                <td className="fy-num">{r.drafted}</td>
                <td className="fy-num">{r.left}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {invoices.length > 0 && (
        <ul className="fy-list">
          {invoices.map((i) => (
            <li key={i.id} className="fy-list__item">
              <span className="fy-list__main">
                <Link className="fy-mono" to="/sales/invoices/$invoiceId" params={{ invoiceId: i.id }}>
                  {i.number || "Draft invoice"}
                </Link>
              </span>
              <span className="fy-chip fy-chip--neutral">{i.status === "draft" ? "Draft" : "Issued"}</span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

// QuotePage is a quote or a sales order (docs/design/screens Quotes; FR-AR-01, FR-AR-02, FR-AR-06): a new one, a draft
// its preparer saves and sends, or a sent one, emailed to the customer, recorded as accepted or declined, cancelled
// with a reason, and, once accepted, made into an invoice. It posts nothing.
export function QuotePage() {
  const { quoteId } = useParams({ strict: false }) as { quoteId?: string };
  const { pathname } = useLocation();
  const isNew = !quoteId;
  const { session, organisation } = useSignedIn();
  const base = organisation?.base_currency ?? "SGD";
  const queries = useQueryClient();
  const navigate = useNavigate();
  const confirm = useConfirm();
  const id = useId();
  const roles = session.roles ?? [];
  const mayPrepare = roles.some((r) => preparers.includes(r));
  const maySend = roles.some((r) => issuers.includes(r));

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
    queryKey: ["quote", quoteId],
    enabled: !isNew,
    queryFn: async () => {
      const r = await api.GET("/v1/quotes/{quote_id}", { params: { path: { quote_id: quoteId ?? "" } } });
      if (r.error) throw r.error;
      return { quote: r.data, etag: r.response.headers.get("ETag") ?? "" };
    },
  });
  const quote = one.data?.quote;
  const order = quote ? quote.kind === "sales_order" : pathname.endsWith("/sales/orders/new");
  const noun = order ? "sales order" : "quote";
  const [form, setForm] = useState<Form | null>(null);
  const [errors, setErrors] = useState<Errors>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [failed, setFailed] = useState("");
  const [status, setStatus] = useState("");
  const [emailing, setEmailing] = useState(false);
  const [answering, setAnswering] = useState<"accept" | "decline" | null>(null);
  const [cancelling, setCancelling] = useState(false);

  const editable = isNew || (quote?.status === "draft" && mayPrepare);
  const f: Form | null =
    form ?? (isNew ? { customer_id: "", reference: "", date: today(), valid_until: order ? "" : in30(), currency: base, fx_rate: "", notes: "", lines: [newLine()] } : quote ? formOf(quote) : null);
  const codes = useQuery({
    queryKey: ["sales-gst-codes", f?.date ?? ""],
    enabled: Boolean(f),
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/sales/gst-codes", { params: { query: { on: f?.date || undefined } } });
      if (error) throw error;
      return data;
    },
  });
  const choices = postable(accounts.data ?? []).filter((a) => a.type === "revenue");
  const set = (patch: Partial<Form>) => f && setForm({ ...f, ...patch });

  const refresh = async () => {
    setForm(null);
    await queries.invalidateQueries({ queryKey: ["quote", quoteId] });
    await queries.invalidateQueries({ queryKey: ["quotes"] });
    await queries.invalidateQueries({ queryKey: ["sales-pipeline"] });
  };

  const save = async (e?: FormEvent) => {
    e?.preventDefault();
    if (!f) return;
    const { draft, errors: errs } = check(f, base, order);
    setErrors(errs);
    if (!draft) return setStatus("Fix the fields marked, then save again.");
    setBusy(true);
    setProblem(null);
    setFailed(`The ${noun} was not saved`);
    if (isNew) {
      const out = await api.POST("/v1/quotes", { params: { header: { "Idempotency-Key": newKey() } }, body: { ...draft, kind: order ? "sales_order" : "quote" } });
      setBusy(false);
      if (!out.data) return setProblem(problemOf(out.error));
      await queries.invalidateQueries({ queryKey: ["quotes"] });
      void navigate({ to: "/sales/quotes/$quoteId", params: { quoteId: out.data.id } });
      return;
    }
    if (!one.data) return;
    const out = await api.PUT("/v1/quotes/{quote_id}", { params: { path: { quote_id: one.data.quote.id }, header: { "If-Match": one.data.etag } }, body: draft });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await refresh();
    setStatus("Saved.");
  };

  const send = async () => {
    if (!one.data) return;
    setBusy(true);
    setProblem(null);
    setFailed(`The ${noun} was not sent`);
    const out = await guarded(confirm, () => api.POST("/v1/quotes/{quote_id}/send", { params: { path: { quote_id: one.data.quote.id }, header: { "If-Match": one.data.etag, "Idempotency-Key": newKey() } } }));
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await refresh();
    setStatus(`Sent as ${out.data.number ?? `a ${noun}`}: email it to the customer, or record their answer.`);
  };

  const invoiceIt = async () => {
    if (!one.data) return;
    setBusy(true);
    setProblem(null);
    setFailed("The invoice was not made");
    const out = await guarded(confirm, () =>
      api.POST("/v1/quotes/{quote_id}/invoice", { params: { path: { quote_id: one.data.quote.id }, header: { "If-Match": one.data.etag, "Idempotency-Key": newKey() } } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["invoices"] });
    void navigate({ to: "/sales/invoices/$invoiceId", params: { invoiceId: out.data.id } });
  };

  if (!isNew && one.isPending) return <p className="fy-secondary">Loading the {noun}…</p>;
  if (!isNew && one.isError) return <Refusal problem={problemOf(one.error)} title={`The ${noun} could not be read`} />;
  if (!f) return null;

  const h = (k: string) => helpText(k);
  const title = isNew ? (order ? "New sales order" : "New quote") : `${quote?.number || `Draft ${noun}`}${quote?.customer?.name ? ` · ${quote.customer.name}` : ""}`;
  const foreign = (f.currency.trim() || base).toUpperCase() !== base;
  const sent = quote && quote.status !== "draft" && quote.status !== "cancelled";
  const left = (quote?.invoicing ?? []).some((r) => !/^0(\.0*)?$/.test(r.left));
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
            <Link to="/sales/quotes">Quotes and orders</Link>
          </nav>
          <div className="fy-row fy-row--wrap">
            <h1 className="fy-h1">{title}</h1>
            {quote && <QuoteChips quote={quote} />}
          </div>
          {quote && (
            <p className="fy-secondary">
              {quote.date ? `Dated ${date(quote.date)}` : "No date"}
              {quote.valid_until ? ` · valid until ${date(quote.valid_until)}` : ""}
              {quote.prepared_by ? ` · prepared by ${who(quote.prepared_by)}` : ""}
              {quote.sent_by ? ` · sent by ${who(quote.sent_by)}` : ""}
            </p>
          )}
        </div>
      </div>
      <ReviewBanner screen="quotes-list" />
      {problem && <Refusal problem={problem} title={failed} />}
      {status && (
        <p className="fy-small" role="status">
          {status}
        </p>
      )}
      {quote?.answer && (
        <div className={`fy-banner fy-banner--${quote.answer.outcome === "accepted" ? "info" : "warning"}`} role="note">
          <Icon of={quote.answer.outcome === "accepted" ? CircleCheck : XCircle} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">
              {quote.answer.outcome === "accepted" ? "Accepted" : quote.answer.outcome === "declined" ? "Declined" : "Expired"} on {date(quote.answer.date)}
              {quote.answer.name ? ` by ${quote.answer.name}` : ""}
            </span>
            <span className="fy-small">
              {quote.answer.note ? `“${quote.answer.note}” · ` : ""}
              recorded by {who(quote.answer.recorded_by)}
            </span>
          </div>
        </div>
      )}
      {quote && !form && <Checks checks={quote.checks ?? []} verb="sent" />}
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
              <select className="fy-select" id={`${id}-customer`} value={f.customer_id} disabled={!editable} onChange={(e) => set({ customer_id: e.target.value })}>
                <option value="">Choose the customer</option>
                {(customers.data ?? []).map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.legal_name}
                  </option>
                ))}
              </select>
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-ref`}>
                Customer's reference
              </label>
              <input className="fy-input" id={`${id}-ref`} value={f.reference} maxLength={60} disabled={!editable} onChange={(e) => set({ reference: e.target.value })} />
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-date`}>
                {order ? "Order date" : "Quote date"}
              </label>
              <input className="fy-input" id={`${id}-date`} type="date" value={f.date} disabled={!editable} onChange={(e) => set({ date: e.target.value })} />
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-valid`}>
                {order ? "Valid until (optional)" : "Valid until"}
              </label>
              <input
                className="fy-input"
                id={`${id}-valid`}
                type="date"
                value={f.valid_until}
                disabled={!editable}
                onChange={(e) => set({ valid_until: e.target.value })}
                aria-invalid={Boolean(errors.valid_until) || undefined}
                aria-describedby={described("valid_until")}
              />
              {err("valid_until")}
            </div>
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
                  aria-describedby={described("fx_rate")}
                />
                {err("fx_rate")}
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
          spreads={false}
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

        <div className="fy-row fy-row--wrap gap-2">
          {editable && (
            <Button type="submit" disabled={busy} title={h("quote.save").tooltip}>
              <Icon of={Save} />
              {h("quote.save").label}
            </Button>
          )}
          {quote?.status === "draft" && !form && maySend && (
            <Button variant="primary" type="button" onClick={() => void send()} disabled={busy || (quote.checks ?? []).some((c) => c.severity === "error")} title={h("quote.send").tooltip}>
              <Icon of={Send} />
              {h("quote.send").label}
            </Button>
          )}
          {quote?.status === "accepted" && left && mayPrepare && (
            <Button variant="primary" type="button" onClick={() => void invoiceIt()} disabled={busy} title={h("quote.invoice").tooltip}>
              <Icon of={FileCheck} />
              {h("quote.invoice").label}
            </Button>
          )}
          {sent && maySend && (
            <>
              <Button type="button" onClick={() => setEmailing(true)} title={h("quote.email").tooltip}>
                <Icon of={Mail} />
                {h("quote.email").label}…
              </Button>
              {quote.status === "sent" && (
                <>
                  <Button type="button" onClick={() => setAnswering("accept")} title={h("quote.accept").tooltip}>
                    <Icon of={CircleCheck} />
                    {h("quote.accept").label}…
                  </Button>
                  <Button type="button" onClick={() => setAnswering("decline")} title={h("quote.decline").tooltip}>
                    <Icon of={XCircle} />
                    {h("quote.decline").label}…
                  </Button>
                </>
              )}
              {quote.status !== "invoiced" && (
                <Button type="button" variant="ghost" onClick={() => setCancelling(true)} title={h("quote.cancel").tooltip}>
                  <Icon of={Ban} />
                  {h("quote.cancel").label}…
                </Button>
              )}
            </>
          )}
          {!isNew && quote?.status === "draft" && form && <span className="fy-small fy-muted">Save the changes to see the totals.</span>}
        </div>
      </form>

      {quote && !form && <Totals doc={quote} />}
      {quote && <Invoicing quote={quote} />}
      {quote && <Sends sends={quote.sends ?? []} />}
      {quote && emailing && one.data && (
        <EmailDialog
          open={emailing}
          kind="quote"
          id={quote.id}
          number={quote.number}
          etag={one.data.etag}
          onClose={() => setEmailing(false)}
          onDone={(message) => {
            setEmailing(false);
            void refresh().then(() => setStatus(message));
          }}
        />
      )}
      {quote && answering && one.data && (
        <Answer
          open={Boolean(answering)}
          quote={quote}
          etag={one.data.etag}
          outcome={answering}
          onClose={() => setAnswering(null)}
          onDone={(message) => {
            setAnswering(null);
            void refresh().then(() => setStatus(message));
          }}
        />
      )}
      {quote && cancelling && one.data && (
        <CancelQuote
          open={cancelling}
          quote={quote}
          etag={one.data.etag}
          onClose={() => setCancelling(false)}
          onDone={(message) => {
            setCancelling(false);
            void refresh().then(() => setStatus(message));
          }}
        />
      )}
    </div>
  );
}
