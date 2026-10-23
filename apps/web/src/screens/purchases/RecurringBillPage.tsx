import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { CirclePause, CirclePlay, Eye, Save } from "lucide-react";
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
import { RecurringStatusChip, repeats } from "../accounting/RecurringJournals";
import { BillLines, lineKey, newLine, type Errors, type Line } from "./BillLines";
import { check } from "./BillPage";
import { preparers } from "./Bills";
import { usualTax } from "./InputTax";
import type { RecurringBill } from "./RecurringBills";

type Input = components["schemas"]["RecurringBillInput"];

export interface RecurringBillForm {
  name: string;
  every_months: string; // "1", "3", "6" or "12"
  day_of_month: string;
  starts_on: string;
  ends_on: string; // empty: no end
  supplier_id: string;
  currency: string;
  notes: string;
  lines: Line[];
}

// recurringBillFormOf is a saved recurring bill as the form shows it.
export function recurringBillFormOf(r: RecurringBill): RecurringBillForm {
  return {
    name: r.name,
    every_months: String(r.schedule.every_months),
    day_of_month: String(r.schedule.day_of_month),
    starts_on: r.schedule.starts_on,
    ends_on: r.schedule.ends_on ?? "",
    supplier_id: r.supplier?.id ?? "",
    currency: r.currency,
    notes: r.notes ?? "",
    lines: (r.lines ?? []).map((l) => ({
      key: lineKey(),
      account_id: l.account_id,
      description: l.description ?? "",
      gst_code: l.gst_code,
      net: l.net.amount,
      gst: l.gst.amount,
      stated_gst: "",
      spread_months: "",
      spread_from: "",
    })),
  };
}

// checkRecurringBill turns what was typed into the recurring bill to send, or says what to fix under each field: the
// schedule here, the lines as a bill's (check). The API checks the content as the bill rules would, in its statuses.
export function checkRecurringBill(f: RecurringBillForm, base: string): { input?: Input; errors: Errors } {
  const { draft, errors } = check({ supplier_id: f.supplier_id, supplier_ref: "", date: "", due_date: "", currency: f.currency, fx_rate: "", notes: f.notes, lines: f.lines, tax: usualTax(), nature: "" }, base);
  if (!f.name.trim()) errors.name = "Name it, such as Office rent.";
  const day = Number(f.day_of_month);
  if (!Number.isInteger(day) || day < 1 || day > 31) errors.day_of_month = "Enter a day from 1 to 31.";
  if (!f.starts_on) errors.starts_on = "Enter the date it starts from.";
  if (f.ends_on && f.starts_on && f.ends_on < f.starts_on) errors.ends_on = "End it on or after its start, or leave the end empty.";
  if (!draft || Object.keys(errors).length > 0) return { errors };
  return {
    input: {
      name: f.name.trim(),
      schedule: { every_months: Number(f.every_months) as 1 | 3 | 6 | 12, day_of_month: day, starts_on: f.starts_on, ends_on: f.ends_on || undefined },
      supplier_id: draft.supplier_id,
      currency: draft.currency,
      lines: draft.lines ?? [],
      notes: draft.notes,
    },
    errors,
  };
}

function today(): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone, year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date());
}

// RecurringBillPage is a recurring bill (FR-AP-09, FR-AUT-01; PRD-001 rev 0.84): its name, schedule, supplier and
// lines. Fyniq makes the draft bill of each occurrence on its date; a person adds the supplier's number and posts it
// under the usual approvals. Whoever writes the lines does not approve the posting of the drafts made from them
// (SOD-3). Check shows the next draft's date and what would stop the drafts posting, without saving (FR-INT-14).
export function RecurringBillPage() {
  const { recurringBillId } = useParams({ strict: false }) as { recurringBillId?: string };
  const isNew = !recurringBillId;
  const { session, organisation } = useSignedIn();
  const base = organisation?.base_currency ?? "SGD";
  const queries = useQueryClient();
  const navigate = useNavigate();
  const confirm = useConfirm();
  const id = useId();
  const mayPrepare = (session.roles ?? []).some((r) => preparers.includes(r));

  const accounts = useQuery({ queryKey: ["accounts"], queryFn: listAccounts });
  const vendors = useQuery({
    queryKey: ["contacts", "vendor"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/contacts", { params: { query: { role: "vendor", page_size: 500 } } });
      if (error) throw error;
      return (data.items ?? []).filter((c) => c.active);
    },
  });
  const codes = useQuery({
    queryKey: ["gst-codes", ""],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/purchases/gst-codes", { params: { query: {} } });
      if (error) throw error;
      return data;
    },
  });
  const one = useQuery({
    queryKey: ["recurring-bill", recurringBillId],
    enabled: !isNew,
    queryFn: async () => {
      const r = await api.GET("/v1/recurring-bills/{recurring_bill_id}", { params: { path: { recurring_bill_id: recurringBillId ?? "" } } });
      if (r.error) throw r.error;
      return { recurring: r.data, etag: etagOf(r.response) };
    },
  });
  const r = one.data?.recurring;
  const [form, setForm] = useState<RecurringBillForm | null>(null);
  const loaded: RecurringBillForm | null = isNew
    ? { name: "", every_months: "1", day_of_month: "1", starts_on: `${today().slice(0, 8)}01`, ends_on: "", supplier_id: "", currency: base, notes: "", lines: [newLine()] }
    : r
      ? recurringBillFormOf(r)
      : null;
  const f = form ?? loaded;
  const dirty = form !== null;
  const [errors, setErrors] = useState<Errors>({});
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [failed, setFailed] = useState("");
  const [preview, setPreview] = useState<RecurringBill | null>(null);
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState("");
  const byId = new Map((accounts.data ?? []).map((a) => [a.id, a]));
  const choices = postable(accounts.data ?? []).filter((a) => a.type === "expense" || a.type === "asset");
  const saveHelp = helpText("recurring-bill.save");
  const checkHelp = helpText("recurring-bill.check");
  const pauseHelp = helpText("recurring-bill.pause");
  const resumeHelp = helpText("recurring-bill.resume");

  const change = (next: RecurringBillForm) => {
    setForm(next);
    setPreview(null);
  };
  const fieldError = (key: string) =>
    errors[key] ? (
      <span className="fy-error" id={`${id}-${key}-error`}>
        {errors[key]}
      </span>
    ) : null;
  const described = (key: string) => (errors[key] ? `${id}-${key}-error` : undefined);

  // send saves the recurring bill, or with dryRun checks it and saves nothing (Prefer: return=preview).
  const send = async (dryRun: boolean, e?: FormEvent) => {
    e?.preventDefault();
    if (!f) return;
    const { input, errors: errs } = checkRecurringBill(f, base);
    setErrors(errs);
    if (!input) return setStatus(`Fix the fields marked, then ${dryRun ? "check" : "save"} again.`);
    setBusy(true);
    setProblem(null);
    const prefer = dryRun ? { Prefer: "return=preview" } : {};
    const out = isNew
      ? await guarded(confirm, () => api.POST("/v1/recurring-bills", { params: { header: { "Idempotency-Key": newKey(), ...prefer } }, body: input }))
      : await guarded(confirm, () =>
          api.PUT("/v1/recurring-bills/{recurring_bill_id}", { params: { path: { recurring_bill_id: recurringBillId ?? "" }, header: { "If-Match": one.data?.etag ?? "", ...prefer } }, body: input }),
        );
    setBusy(false);
    setFailed(dryRun ? "The recurring bill could not be checked" : "The recurring bill was not saved");
    if (!out.data) {
      setPreview(null);
      return setProblem(problemOf(out.error));
    }
    if (dryRun) {
      setPreview(out.data);
      return setStatus(out.data.next_on ? `Its next draft would be made on ${date(out.data.next_on)}. Nothing was saved.` : "It would make no draft. Nothing was saved.");
    }
    await queries.invalidateQueries({ queryKey: ["recurring-bills"] });
    setForm(null);
    setPreview(null);
    if (isNew) return navigate({ to: "/purchases/bills/recurring/$recurringBillId", params: { recurringBillId: out.data.id } });
    await queries.invalidateQueries({ queryKey: ["recurring-bill", recurringBillId] });
    setStatus(out.data.next_on ? `Saved. Its next draft is made on ${date(out.data.next_on)}.` : "Saved.");
  };

  const pauseOrResume = async (to: "pause" | "resume") => {
    if (!one.data) return;
    setBusy(true);
    setProblem(null);
    const params = { path: { recurring_bill_id: one.data.recurring.id }, header: { "If-Match": one.data.etag, "Idempotency-Key": newKey() } };
    const out = await guarded(confirm, () => (to === "pause" ? api.POST("/v1/recurring-bills/{recurring_bill_id}/pause", { params }) : api.POST("/v1/recurring-bills/{recurring_bill_id}/resume", { params })));
    setBusy(false);
    setFailed(to === "pause" ? "The recurring bill was not paused" : "The recurring bill was not resumed");
    if (!out.data) return setProblem(problemOf(out.error));
    await Promise.all([queries.invalidateQueries({ queryKey: ["recurring-bills"] }), queries.invalidateQueries({ queryKey: ["recurring-bill", recurringBillId] })]);
    setStatus(to === "pause" ? "Paused: it makes no drafts until you resume it." : out.data.next_on ? `Resumed: its next draft is made on ${date(out.data.next_on)}.` : "Resumed.");
  };

  const actions = (
    <>
      <Button type="button" onClick={() => void send(true)} disabled={busy} title={checkHelp.tooltip}>
        <Icon of={Eye} />
        {checkHelp.label}
      </Button>
      {r && !dirty && r.status === "active" && (
        <Button type="button" onClick={() => void pauseOrResume("pause")} disabled={busy} title={pauseHelp.tooltip}>
          <Icon of={CirclePause} />
          {pauseHelp.label}
        </Button>
      )}
      {r && !dirty && r.status === "paused" && (
        <Button type="button" onClick={() => void pauseOrResume("resume")} disabled={busy} title={resumeHelp.tooltip}>
          <Icon of={CirclePlay} />
          {resumeHelp.label}
        </Button>
      )}
      <Button variant="primary" type="submit" form={`${id}-form`} disabled={busy || (!isNew && !dirty)} title={saveHelp.tooltip}>
        <Icon of={Save} />
        {saveHelp.label}
      </Button>
    </>
  );

  const shown = preview ?? r;
  const title = isNew ? "New recurring bill" : (r?.name ?? "Recurring bill");

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <span>Purchases</span>
            <span aria-hidden="true">›</span>
            <Link to="/purchases/bills/recurring">Recurring bills</Link>
          </nav>
          <div className="fy-row fy-row--wrap gap-2">
            <h1 className="fy-h1">{title}</h1>
            {r && <RecurringStatusChip status={r.status} />}
            {r && <CheckChip checks={r.checks.filter((c) => c.severity !== "ok")} />}
          </div>
          {r && (
            <p className="fy-secondary">
              {repeats(r.schedule)}
              {r.next_on ? ` · next draft on ${date(r.next_on)}` : " · no more drafts"}
              {r.edited_by ? ` · lines by ${who(r.edited_by)} on ${dateTime(r.edited_at)}` : ""}
            </p>
          )}
        </div>
        {mayPrepare && <div className="fy-pagehead__actions fy-from-medium">{actions}</div>}
      </div>
      <ReviewBanner screen="bill-editor" />
      <p className="fy-small" role="status">
        {status}
      </p>
      {one.isError && <Refusal problem={problemOf(one.error)} title="The recurring bill could not be shown" />}
      {problem && <Refusal problem={problem} title={failed} />}
      {!isNew && one.isPending && <p className="fy-secondary">Loading the recurring bill…</p>}
      {shown && <Checks checks={shown.checks} heading={(n) => (n === 1 ? "1 thing to look at before its drafts can post" : `${n} things to look at before its drafts can post`)} />}
      {r?.last_bill_id && (
        <p className="fy-small">
          Its last draft, for {r.last_on ? date(r.last_on) : "its last date"}:{" "}
          <Link to="/purchases/bills/$billId" params={{ billId: r.last_bill_id }}>
            open the bill
          </Link>
          . Add the supplier's number from their invoice and post it; whoever wrote these lines does not approve it.
        </p>
      )}
      {isNew && !mayPrepare && <p className="fy-secondary">A Bookkeeper, an AP specialist or an Accountant sets up recurring bills.</p>}

      {f && (
        <form id={`${id}-form`} className="fy-stack fy-stack--6" onSubmit={(e) => void send(false, e)} noValidate>
          <fieldset className="fy-stack fy-stack--6 m-0 min-w-0 border-0 p-0" disabled={!mayPrepare}>
            <section className="fy-card" aria-labelledby={`${id}-head`}>
              <div className="fy-card__head">
                <h2 className="fy-h3" id={`${id}-head`}>
                  Recurring bill
                </h2>
              </div>
              <div className="fy-card__body fy-form fy-form--2">
                <div className="fy-field">
                  <label className="fy-label" htmlFor={`${id}-name`}>
                    Name
                  </label>
                  <input className="fy-input" id={`${id}-name`} value={f.name} maxLength={120} placeholder="Office rent" onChange={(e) => change({ ...f, name: e.target.value })} aria-invalid={errors.name ? true : undefined} aria-describedby={described("name")} />
                  {fieldError("name")}
                </div>
                <div className="fy-field">
                  <label className="fy-label" htmlFor={`${id}-supplier`}>
                    Supplier
                  </label>
                  <select className="fy-select" id={`${id}-supplier`} value={f.supplier_id} onChange={(e) => change({ ...f, supplier_id: e.target.value })}>
                    <option value="">Choose the supplier</option>
                    {(vendors.data ?? []).map((c) => (
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
                  <select className="fy-select" id={`${id}-every`} value={f.every_months} onChange={(e) => change({ ...f, every_months: e.target.value })}>
                    <option value="1">Every month</option>
                    <option value="3">Every 3 months</option>
                    <option value="6">Every 6 months</option>
                    <option value="12">Every year</option>
                  </select>
                </div>
                <div className="fy-field">
                  <label className="fy-label" htmlFor={`${id}-day`}>
                    On day
                  </label>
                  <input className="fy-input fy-num" id={`${id}-day`} inputMode="numeric" value={f.day_of_month} onChange={(e) => change({ ...f, day_of_month: e.target.value })} aria-invalid={errors.day_of_month ? true : undefined} aria-describedby={described("day_of_month") ?? `${id}-day-hint`} />
                  <span className="fy-small fy-muted" id={`${id}-day-hint`}>
                    The 29th to the 31st fall on the last day of a shorter month.
                  </span>
                  {fieldError("day_of_month")}
                </div>
                <div className="fy-field">
                  <label className="fy-label" htmlFor={`${id}-starts`}>
                    Starts on
                  </label>
                  <input className="fy-input" id={`${id}-starts`} type="date" value={f.starts_on} onChange={(e) => change({ ...f, starts_on: e.target.value })} aria-invalid={errors.starts_on ? true : undefined} aria-describedby={described("starts_on") ?? `${id}-starts-hint`} />
                  <span className="fy-small fy-muted" id={`${id}-starts-hint`}>
                    A start in the past makes the drafts of the dates already passed.
                  </span>
                  {fieldError("starts_on")}
                </div>
                <div className="fy-field">
                  <label className="fy-label" htmlFor={`${id}-ends`}>
                    Ends on (optional)
                  </label>
                  <input className="fy-input" id={`${id}-ends`} type="date" value={f.ends_on} onChange={(e) => change({ ...f, ends_on: e.target.value })} aria-invalid={errors.ends_on ? true : undefined} aria-describedby={described("ends_on")} />
                  {fieldError("ends_on")}
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
                    aria-describedby={described("currency") ?? `${id}-currency-hint`}
                  />
                  {fieldError("currency") ?? (
                    <span className="fy-small fy-muted" id={`${id}-currency-hint`}>
                      A draft in another currency takes the rate for its date when it posts.
                    </span>
                  )}
                </div>
              </div>
            </section>
            <BillLines
              id={id}
              lines={f.lines}
              onChange={(lines) => change({ ...f, lines })}
              editable={mayPrepare}
              errors={errors}
              currency={f.currency || base}
              base={base}
              stated={false}
              spreads={false}
              codes={codes.data?.codes ?? []}
              choices={choices}
              byId={byId}
              subtitle="The lines of each draft"
              footer={r && <p className="fy-small">Each draft's total {formatAmount(r.total.amount)} {r.total.currency}</p>}
            />
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-notes`}>
                Notes of each draft
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
