import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { CirclePause, CirclePlay, Eye, Save, UserCheck } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { date, dateTime, timeZone } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { listAccounts, postable } from "../accounting/data";
import { Checks } from "../accounting/JournalPage";
import { CheckChip, who } from "../accounting/Journals";
import { RecurringStatusChip } from "../accounting/RecurringJournals";
import { InvoiceLines, lineKey, newLine, type Errors, type Line } from "./InvoiceLines";
import { check } from "./InvoicePage";
import { preparers } from "./Invoices";
import { ModeChip, repeatsOf, type RecurringInvoice } from "./RecurringInvoices";

type Input = components["schemas"]["RecurringInvoiceInput"];

// The schedules a recurring invoice takes (FR-AR-07): weekly to yearly.
const every = [
  ["w1", "Every week"],
  ["w2", "Every two weeks"],
  ["m1", "Every month"],
  ["m3", "Every 3 months"],
  ["m6", "Every 6 months"],
  ["m12", "Every year"],
] as const;
type Every = (typeof every)[number][0];

export interface RecurringInvoiceForm {
  name: string;
  every: Every;
  day_of_month: string;
  starts_on: string;
  ends_on: string; // empty: no end
  customer_id: string;
  reference: string;
  currency: string;
  notes: string;
  mode: "draft" | "issue_and_send";
  lines: Line[];
}

// formOf is a saved recurring invoice as the form shows it.
function formOf(r: RecurringInvoice): RecurringInvoiceForm {
  const s = r.schedule;
  return {
    name: r.name,
    every: (s.every_weeks ? `w${s.every_weeks}` : `m${s.every_months ?? 1}`) as Every,
    day_of_month: String(s.day_of_month ?? 1),
    starts_on: s.starts_on,
    ends_on: s.ends_on ?? "",
    customer_id: r.customer?.id ?? "",
    reference: r.reference ?? "",
    currency: r.currency,
    notes: r.notes ?? "",
    mode: r.mode,
    lines: (r.lines ?? []).map((l) => ({
      key: lineKey(),
      item_id: l.item_id ?? "",
      description: l.description ?? "",
      quantity: l.quantity,
      unit_price: l.unit_price,
      discount: l.discount_percent ?? "",
      account_id: l.account_id,
      gst_code: l.gst_code,
      spread_months: l.spread_months ? String(l.spread_months) : "",
      spread_from: "",
    })),
  };
}

// checkRecurringInvoice turns what was typed into the recurring invoice to send, or says what to fix under each field:
// the schedule here, the lines as an invoice's (check). The API checks the content as the invoice rules would, in its
// statuses.
export function checkRecurringInvoice(f: RecurringInvoiceForm, base: string): { input?: Input; errors: Errors } {
  const { draft, errors } = check({ customer_id: f.customer_id, reference: f.reference, date: "", due_date: "", currency: f.currency, fx_rate: "", notes: f.notes, invoice_id: "", lines: f.lines }, base);
  if (!f.name.trim()) errors.name = "Name it, such as Analytics subscription, Acme.";
  const weekly = f.every.startsWith("w");
  const day = Number(f.day_of_month);
  if (!weekly && (!Number.isInteger(day) || day < 1 || day > 31)) errors.day_of_month = "Enter a day from 1 to 31.";
  if (!f.starts_on) errors.starts_on = "Enter the date it starts from.";
  if (f.ends_on && f.starts_on && f.ends_on < f.starts_on) errors.ends_on = "End it on or after its start, or leave the end empty.";
  if (!draft || Object.keys(errors).length > 0) return { errors };
  const n = Number(f.every.slice(1));
  return {
    input: {
      name: f.name.trim(),
      schedule: weekly
        ? { every_weeks: n as 1 | 2, starts_on: f.starts_on, ends_on: f.ends_on || undefined }
        : { every_months: n as 1 | 3 | 6 | 12, day_of_month: day, starts_on: f.starts_on, ends_on: f.ends_on || undefined },
      customer_id: draft.customer_id,
      reference: draft.reference,
      currency: draft.currency,
      lines: draft.lines ?? [],
      notes: draft.notes,
      mode: f.mode,
    },
    errors,
  };
}

function today(): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone, year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date());
}

// RecurringInvoicePage is a recurring invoice (FR-AR-07, FR-AUT-01; PRD-001 rev 0.135, rev 0.136): its name, schedule,
// customer and lines. Fyniq makes the draft invoice of each occurrence on its date, which a person issues under the
// usual approvals; whoever wrote the lines does not approve their issue (SOD-3). Set to issue and send, once another
// person approves its content, the scheduler issues and emails each one itself, and lists it for the weekly review; a
// change of content needs the approval again.
export function RecurringInvoicePage() {
  const { recurringInvoiceId } = useParams({ strict: false }) as { recurringInvoiceId?: string };
  const isNew = !recurringInvoiceId;
  const { session, organisation } = useSignedIn();
  const base = organisation?.base_currency ?? "SGD";
  const queries = useQueryClient();
  const navigate = useNavigate();
  const confirm = useConfirm();
  const id = useId();
  const mayPrepare = (session.roles ?? []).some((r) => preparers.includes(r));

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
  const codes = useQuery({
    queryKey: ["sales-gst-codes", ""],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/sales/gst-codes", { params: { query: {} } });
      if (error) throw error;
      return data;
    },
  });
  const one = useQuery({
    queryKey: ["recurring-invoice", recurringInvoiceId],
    enabled: !isNew,
    queryFn: async () => {
      const r = await api.GET("/v1/recurring-invoices/{recurring_invoice_id}", { params: { path: { recurring_invoice_id: recurringInvoiceId ?? "" } } });
      if (r.error) throw r.error;
      return { recurring: r.data, etag: etagOf(r.response) };
    },
  });
  const r = one.data?.recurring;
  const [form, setForm] = useState<RecurringInvoiceForm | null>(null);
  const loaded: RecurringInvoiceForm | null = isNew
    ? { name: "", every: "m1", day_of_month: "1", starts_on: `${today().slice(0, 8)}01`, ends_on: "", customer_id: "", reference: "", currency: base, notes: "", mode: "draft", lines: [newLine()] }
    : r
      ? formOf(r)
      : null;
  const f = form ?? loaded;
  const dirty = form !== null;
  const [errors, setErrors] = useState<Errors>({});
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [failed, setFailed] = useState("");
  const [preview, setPreview] = useState<RecurringInvoice | null>(null);
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState("");
  const choices = postable(accounts.data ?? []).filter((a) => a.type === "revenue");
  const h = (k: string) => helpText(k);

  const change = (next: RecurringInvoiceForm) => {
    setForm(next);
    setPreview(null);
  };
  const fieldError = (key: string) =>
    errors[key] ? (
      <span className="fy-error" id={`${id}-${key}-error`}>
        {errors[key]}
      </span>
    ) : null;
  const described = (key: string, help?: string) => (errors[key] ? `${id}-${key}-error` : help);

  // send saves the recurring invoice, or with dryRun checks it and saves nothing (Prefer: return=preview).
  const send = async (dryRun: boolean, e?: FormEvent) => {
    e?.preventDefault();
    if (!f) return;
    const { input, errors: errs } = checkRecurringInvoice(f, base);
    setErrors(errs);
    if (!input) return setStatus(`Fix the fields marked, then ${dryRun ? "check" : "save"} again.`);
    setBusy(true);
    setProblem(null);
    const prefer = dryRun ? { Prefer: "return=preview" } : {};
    const out = isNew
      ? await guarded(confirm, () => api.POST("/v1/recurring-invoices", { params: { header: { "Idempotency-Key": newKey(), ...prefer } }, body: input }))
      : await guarded(confirm, () =>
          api.PUT("/v1/recurring-invoices/{recurring_invoice_id}", { params: { path: { recurring_invoice_id: recurringInvoiceId ?? "" }, header: { "If-Match": one.data?.etag ?? "", ...prefer } }, body: input }),
        );
    setBusy(false);
    setFailed(dryRun ? "The recurring invoice could not be checked" : "The recurring invoice was not saved");
    if (!out.data) {
      setPreview(null);
      return setProblem(problemOf(out.error));
    }
    if (dryRun) {
      setPreview(out.data);
      return setStatus(out.data.next_on ? `Its next invoice would be made on ${date(out.data.next_on)}. Nothing was saved.` : "It would make no invoice. Nothing was saved.");
    }
    await queries.invalidateQueries({ queryKey: ["recurring-invoices"] });
    setForm(null);
    setPreview(null);
    if (isNew) return navigate({ to: "/sales/recurring/$recurringInvoiceId", params: { recurringInvoiceId: out.data.id } });
    await queries.invalidateQueries({ queryKey: ["recurring-invoice", recurringInvoiceId] });
    setStatus(out.data.next_on ? `Saved. Its next invoice is made on ${date(out.data.next_on)}.` : "Saved.");
  };

  const pauseOrResume = async (to: "pause" | "resume") => {
    if (!one.data) return;
    setBusy(true);
    setProblem(null);
    const params = { path: { recurring_invoice_id: one.data.recurring.id }, header: { "If-Match": one.data.etag, "Idempotency-Key": newKey() } };
    const out = await guarded(confirm, () =>
      to === "pause" ? api.POST("/v1/recurring-invoices/{recurring_invoice_id}/pause", { params }) : api.POST("/v1/recurring-invoices/{recurring_invoice_id}/resume", { params }),
    );
    setBusy(false);
    setFailed(to === "pause" ? "The recurring invoice was not paused" : "The recurring invoice was not resumed");
    if (!out.data) return setProblem(problemOf(out.error));
    await Promise.all([queries.invalidateQueries({ queryKey: ["recurring-invoices"] }), queries.invalidateQueries({ queryKey: ["recurring-invoice", recurringInvoiceId] })]);
    setStatus(to === "pause" ? "Paused: it makes no invoices until you resume it." : out.data.next_on ? `Resumed: its next invoice is made on ${date(out.data.next_on)}.` : "Resumed.");
  };

  const askApproval = async () => {
    if (!one.data) return;
    setBusy(true);
    setProblem(null);
    setFailed("The request was not made");
    const out = await guarded(confirm, () =>
      api.POST("/v1/recurring-invoices/{recurring_invoice_id}/approval", { params: { path: { recurring_invoice_id: one.data.recurring.id }, header: { "If-Match": one.data.etag, "Idempotency-Key": newKey() } } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    setStatus("Asked for approval: once another person approves this content, the scheduler issues and sends its invoices.");
  };

  const actions = (
    <>
      <Button type="button" onClick={() => void send(true)} disabled={busy} title={h("recurring-invoice.check").tooltip}>
        <Icon of={Eye} />
        {h("recurring-invoice.check").label}
      </Button>
      {r && !dirty && r.mode === "issue_and_send" && !r.issues_and_sends && (
        <Button type="button" onClick={() => void askApproval()} disabled={busy} title={h("recurring-invoice.ask-approval").tooltip}>
          <Icon of={UserCheck} />
          {h("recurring-invoice.ask-approval").label}
        </Button>
      )}
      {r && !dirty && r.status === "active" && (
        <Button type="button" onClick={() => void pauseOrResume("pause")} disabled={busy} title={h("recurring-invoice.pause").tooltip}>
          <Icon of={CirclePause} />
          {h("recurring-invoice.pause").label}
        </Button>
      )}
      {r && !dirty && r.status === "paused" && (
        <Button type="button" onClick={() => void pauseOrResume("resume")} disabled={busy} title={h("recurring-invoice.resume").tooltip}>
          <Icon of={CirclePlay} />
          {h("recurring-invoice.resume").label}
        </Button>
      )}
      <Button variant="primary" type="submit" form={`${id}-form`} disabled={busy || (!isNew && !dirty)} title={h("recurring-invoice.save").tooltip}>
        <Icon of={Save} />
        {h("recurring-invoice.save").label}
      </Button>
    </>
  );

  const shown = preview ?? r;
  const title = isNew ? "New recurring invoice" : (r?.name ?? "Recurring invoice");
  const weekly = f?.every.startsWith("w");

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <span>Sales</span>
            <span aria-hidden="true">›</span>
            <Link to="/sales/recurring">Recurring invoices</Link>
          </nav>
          <div className="fy-row fy-row--wrap gap-2">
            <h1 className="fy-h1">{title}</h1>
            {r && <RecurringStatusChip status={r.status} />}
            {r && <ModeChip r={r} />}
            {r && <CheckChip checks={r.checks.filter((c) => c.severity !== "ok")} />}
          </div>
          {r && (
            <p className="fy-secondary">
              {repeatsOf(r.schedule)}
              {r.next_on ? ` · next on ${date(r.next_on)}` : " · no more invoices"}
              {r.edited_by ? ` · lines by ${who(r.edited_by)} on ${dateTime(r.edited_at)}` : ""}
              {r.approved_by ? ` · approved by ${who(r.approved_by)}` : ""}
            </p>
          )}
        </div>
        {mayPrepare && <div className="fy-pagehead__actions fy-from-medium">{actions}</div>}
      </div>
      <ReviewBanner screen="recurring-documents" />
      <p className="fy-small" role="status">
        {status}
      </p>
      {one.isError && <Refusal problem={problemOf(one.error)} title="The recurring invoice could not be shown" />}
      {problem && <Refusal problem={problem} title={failed} />}
      {!isNew && one.isPending && <p className="fy-secondary">Loading the recurring invoice…</p>}
      {shown && <Checks checks={shown.checks} heading={(n) => (n === 1 ? "1 thing to look at before its invoices can be issued" : `${n} things to look at before its invoices can be issued`)} />}
      {r?.last_invoice_id && (
        <p className="fy-small">
          Its last invoice, for {r.last_on ? date(r.last_on) : "its last date"}:{" "}
          <Link to="/sales/invoices/$invoiceId" params={{ invoiceId: r.last_invoice_id }}>
            open the invoice
          </Link>
          {r.issues_and_sends ? ". The scheduler issued and emailed it." : ". Check it and issue it; whoever wrote these lines does not approve it."}
        </p>
      )}
      {isNew && !mayPrepare && <p className="fy-secondary">A Bookkeeper, an AR specialist or an Accountant sets up recurring invoices.</p>}

      {f && (
        <form id={`${id}-form`} className="fy-stack fy-stack--6" onSubmit={(e) => void send(false, e)} noValidate>
          <fieldset className="fy-stack fy-stack--6 m-0 min-w-0 border-0 p-0" disabled={!mayPrepare}>
            <section className="fy-card" aria-labelledby={`${id}-head`}>
              <div className="fy-card__head">
                <h2 className="fy-h3" id={`${id}-head`}>
                  Recurring invoice
                </h2>
              </div>
              <div className="fy-card__body fy-form fy-form--2">
                <div className="fy-field">
                  <label className="fy-label" htmlFor={`${id}-name`}>
                    Name
                  </label>
                  <input className="fy-input" id={`${id}-name`} value={f.name} maxLength={120} placeholder="Analytics subscription, Acme" onChange={(e) => change({ ...f, name: e.target.value })} aria-invalid={errors.name ? true : undefined} aria-describedby={described("name")} />
                  {fieldError("name")}
                </div>
                <div className="fy-field">
                  <label className="fy-label" htmlFor={`${id}-customer`}>
                    Customer
                  </label>
                  <select className="fy-select" id={`${id}-customer`} value={f.customer_id} onChange={(e) => change({ ...f, customer_id: e.target.value })}>
                    <option value="">Choose the customer</option>
                    {(customers.data ?? []).map((c) => (
                      <option key={c.id} value={c.id}>
                        {c.legal_name}
                      </option>
                    ))}
                  </select>
                </div>
                <div className="fy-field">
                  <label className="fy-label" htmlFor={`${id}-every`}>
                    Repeats
                  </label>
                  <select className="fy-select" id={`${id}-every`} value={f.every} onChange={(e) => change({ ...f, every: e.target.value as Every })}>
                    {every.map(([v, label]) => (
                      <option key={v} value={v}>
                        {label}
                      </option>
                    ))}
                  </select>
                </div>
                {!weekly && (
                  <div className="fy-field">
                    <label className="fy-label" htmlFor={`${id}-day`}>
                      On day
                    </label>
                    <input className="fy-input fy-num" id={`${id}-day`} inputMode="numeric" value={f.day_of_month} onChange={(e) => change({ ...f, day_of_month: e.target.value })} aria-invalid={errors.day_of_month ? true : undefined} aria-describedby={described("day_of_month", `${id}-day-hint`)} />
                    {fieldError("day_of_month") ?? (
                      <span className="fy-small fy-muted" id={`${id}-day-hint`}>
                        The 29th to the 31st fall on the last day of a shorter month.
                      </span>
                    )}
                  </div>
                )}
                <div className="fy-field">
                  <label className="fy-label" htmlFor={`${id}-starts`}>
                    Starts on
                  </label>
                  <input className="fy-input" id={`${id}-starts`} type="date" value={f.starts_on} onChange={(e) => change({ ...f, starts_on: e.target.value })} aria-invalid={errors.starts_on ? true : undefined} aria-describedby={described("starts_on", `${id}-starts-hint`)} />
                  {fieldError("starts_on") ?? (
                    <span className="fy-small fy-muted" id={`${id}-starts-hint`}>
                      {weekly ? "Each week falls on the start's weekday. " : ""}A start in the past makes the invoices of the dates already passed.
                    </span>
                  )}
                </div>
                <div className="fy-field">
                  <label className="fy-label" htmlFor={`${id}-ends`}>
                    Ends on (optional)
                  </label>
                  <input className="fy-input" id={`${id}-ends`} type="date" value={f.ends_on} onChange={(e) => change({ ...f, ends_on: e.target.value })} aria-invalid={errors.ends_on ? true : undefined} aria-describedby={described("ends_on")} />
                  {fieldError("ends_on")}
                </div>
                <div className="fy-field">
                  <label className="fy-label" htmlFor={`${id}-ref`}>
                    Customer's reference
                  </label>
                  <input className="fy-input" id={`${id}-ref`} value={f.reference} maxLength={60} onChange={(e) => change({ ...f, reference: e.target.value })} />
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
                    onChange={(e) => change({ ...f, currency: e.target.value.toUpperCase() })}
                    aria-invalid={errors.currency ? true : undefined}
                    aria-describedby={described("currency", `${id}-currency-hint`)}
                  />
                  {fieldError("currency") ?? (
                    <span className="fy-small fy-muted" id={`${id}-currency-hint`}>
                      An invoice in another currency takes the rate for its date when it is issued.
                    </span>
                  )}
                </div>
                <div className="fy-field fy-field--full">
                  <label className="fy-label" htmlFor={`${id}-mode`}>
                    {h("recurring-invoice.mode").label}
                  </label>
                  <select className="fy-select" id={`${id}-mode`} value={f.mode} onChange={(e) => change({ ...f, mode: e.target.value as RecurringInvoiceForm["mode"] })} aria-describedby={`${id}-mode-help`}>
                    <option value="draft">Make a draft of each, for a person to issue</option>
                    <option value="issue_and_send">Issue and send each, once another person approves the content</option>
                  </select>
                  <span className="fy-help" id={`${id}-mode-help`}>
                    {h("recurring-invoice.mode").tooltip}
                  </span>
                </div>
              </div>
            </section>
            <InvoiceLines
              id={id}
              lines={f.lines}
              onChange={(lines) => change({ ...f, lines })}
              editable={mayPrepare}
              errors={errors}
              currency={f.currency || base}
              spreads={false}
              codes={codes.data?.codes ?? []}
              choices={choices}
              items={items.data ?? []}
              subtitle="The lines of each invoice"
              footer={r && <p className="fy-small">Each invoice's lines come to {formatAmount(r.net.amount)} {r.net.currency} before GST; the GST is worked out on each invoice's date.</p>}
            />
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-notes`}>
                Notes of each invoice
              </label>
              <textarea className="fy-textarea" id={`${id}-notes`} value={f.notes} maxLength={2000} onChange={(e) => change({ ...f, notes: e.target.value })} />
            </div>
          </fieldset>
        </form>
      )}
      {mayPrepare && <div className="fy-actionbar fy-only-compact">{actions}</div>}
    </div>
  );
}
