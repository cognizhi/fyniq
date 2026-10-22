import { useQuery } from "@tanstack/react-query";
import { Banknote, Link2, Undo2, UserCheck } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { minus, sign, typed } from "../../format/decimal";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { postable, type Account } from "../accounting/data";
import type { Bill } from "./Bills";

type PaymentDraft = components["schemas"]["BillPaymentDraft"];
type PaidBy = components["schemas"]["BillPaidBy"];

function today(): string {
  return new Date().toLocaleDateString("en-CA");
}

// payable is what a bill can be paid from: an active money account in its currency, or in the base currency.
export function payable(accounts: readonly Account[], bill: Bill, base: string): Account[] {
  return postable(accounts).filter((a) => a.money_account && [bill.currency, base].includes(a.currency || base));
}

type Errors = Record<string, string>;

// paymentOf turns what was typed into the payment to send, or says what to fix under each field. The API checks the
// rest: the account, that the amount is at most what is open (BILLPAY-05), and the rate.
export function paymentOf(
  bill: Bill,
  base: string,
  f: { date: string; account: string; accountCurrency: string; amount: string; rate: string; paid: string; reference: string },
  refund = false,
): { payment?: PaymentDraft; errors: Errors } {
  const errors: Errors = {};
  const amount = typed(f.amount);
  const foreign = bill.currency !== base;
  const fromBase = foreign && f.accountCurrency === base;
  const rate = f.rate.trim();
  const paid = typed(f.paid);
  if (!f.date) errors.date = refund ? "Enter the day the money came into the account." : "Enter the day the money left the account.";
  if (!f.account) errors.account = refund ? "Choose the account the money came into." : "Choose the account the money left.";
  if (amount === null || amount === "" || /^-|^0(\.0*)?$/.test(amount))
    errors.amount = refund ? `Enter what the supplier paid back in ${bill.currency}, such as 100.00.` : `Enter what was paid of the bill in ${bill.currency}, such as 400.00.`;
  if (foreign && !fromBase && rate && !/^(0|[1-9]\d{0,9})(\.\d{1,10})?$/.test(rate)) errors.rate = `Enter the rate as ${base} for 1 ${bill.currency}, such as 1.3472, or leave it blank to take the day's rate.`;
  if (fromBase && (paid === null || paid === "")) errors.paid = `Enter what the account ${refund ? "received" : "paid"} in ${base}, as the statement shows it.`;
  if (Object.keys(errors).length > 0) return { errors };
  return {
    errors,
    payment: {
      date: f.date,
      money_account_id: f.account,
      currency: bill.currency,
      fx_rate: foreign && !fromBase && rate ? rate : undefined,
      paid: fromBase && paid ? { amount: paid, currency: base } : undefined,
      reference: f.reference.trim() || undefined,
      supplier_id: bill.supplier?.id,
      bills: [{ bill_id: bill.id, amount: { amount: amount ?? "0", currency: bill.currency } }],
    },
  };
}

// The words of a payment of a bill, and of a refund of a credit note: the same form, money going the other way.
const paymentWords = {
  help: "bill.record-payment",
  title: "Record a payment of this bill",
  description: (who: string) => `For money that has already left the bank. It posts at once: what the company owes ${who} goes down by what was paid.`,
  refused: "The payment was not recorded",
  date: "Date paid",
  account: "Paid from",
  amount: "Amount paid of the bill",
  amountHelp: "Pay part of it, or all that is open.",
  received: "What the account paid",
};
const refundWords = {
  help: "credit.refund",
  title: "Record a refund of this credit note",
  description: (who: string) => `For money ${who} has already paid back. It posts at once: the credit with them goes down by what came in.`,
  refused: "The refund was not recorded",
  date: "Date received",
  account: "Paid into",
  amount: "Amount refunded",
  amountHelp: "Record part of it, or all that is left.",
  received: "What the account received",
};

// RecordPayment records a payment the company made of a bill, by hand (FR-AP-02; the owner's decision of 22 Oct 2026):
// the date, the money account it left and what it paid of the bill. It posts at once: the money has left already. With
// refund, it records money the supplier paid back against a credit note the same way (FR-AP-03).
export function RecordPayment({
  open,
  bill,
  base,
  accounts,
  refund = false,
  onClose,
  onDone,
}: {
  open: boolean;
  bill: Bill;
  base: string;
  accounts: readonly Account[];
  refund?: boolean;
  onClose: () => void;
  onDone: (message: string) => void;
}) {
  const w = refund ? refundWords : paymentWords;
  const confirm = useConfirm();
  const id = useId();
  const choices = payable(accounts, bill, base);
  const [f, setF] = useState({ date: today(), account: choices.length === 1 ? (choices[0]?.id ?? "") : "", amount: bill.open?.amount ?? "", rate: "", paid: "", reference: "" });
  const [errors, setErrors] = useState<Errors>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText(w.help);
  const accountCurrency = accounts.find((a) => a.id === f.account)?.currency || base;
  const foreign = bill.currency !== base;
  const fromBase = foreign && Boolean(f.account) && accountCurrency === base;
  const set = (patch: Partial<typeof f>) => setF({ ...f, ...patch });
  const err = (k: string) =>
    errors[k] ? (
      <span className="fy-error" id={`${id}-${k}-error`}>
        {errors[k]}
      </span>
    ) : null;
  const described = (k: string, help?: string) => (errors[k] ? `${id}-${k}-error` : help);

  const run = async (e: FormEvent) => {
    e.preventDefault();
    const { payment, errors: errs } = paymentOf(bill, base, { ...f, accountCurrency }, refund);
    setErrors(errs);
    if (!payment) return;
    setBusy(true);
    setProblem(null);
    if (refund) {
      const body = { credit_id: bill.id, date: payment.date, money_account_id: payment.money_account_id, fx_rate: payment.fx_rate, received: payment.paid,
        reference: payment.reference, amount: payment.bills?.[0]?.amount ?? { amount: "0", currency: bill.currency } };
      const out = await guarded(confirm, () => api.POST("/v1/supplier-refunds", { params: { header: { "Idempotency-Key": newKey() } }, body }));
      setBusy(false);
      if (!out.data) return setProblem(problemOf(out.error));
      const r = out.data.refund;
      return onDone(`Recorded ${r.number ?? "the refund"}: ${formatAmount(r.amount.amount)} ${r.amount.currency} refunded.`);
    }
    const out = await guarded(confirm, () => api.POST("/v1/bill-payments", { params: { header: { "Idempotency-Key": newKey() } }, body: payment }));
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    const p = out.data.payment;
    onDone(`Recorded ${p.number ?? "the payment"}: ${formatAmount(p.total.amount)} ${p.total.currency} paid.`);
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      eyebrow={bill.number}
      title={w.title}
      description={w.description(bill.supplier?.name ?? "the supplier")}
    >
      {problem && <Refusal problem={problem} title={w.refused} />}
      <form className="fy-stack fy-stack--3" onSubmit={(e) => void run(e)} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-date`}>
            {w.date}
          </label>
          <input
            className="fy-input"
            id={`${id}-date`}
            type="date"
            value={f.date}
            min={bill.date}
            onChange={(e) => set({ date: e.target.value })}
            aria-invalid={Boolean(errors.date) || undefined}
            aria-describedby={described("date")}
          />
          {err("date")}
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-account`}>
            {w.account}
          </label>
          <select
            className="fy-select"
            id={`${id}-account`}
            value={f.account}
            onChange={(e) => set({ account: e.target.value })}
            aria-invalid={Boolean(errors.account) || undefined}
            aria-describedby={described("account", `${id}-account-help`)}
          >
            <option value="">Choose the account</option>
            {choices.map((a) => (
              <option key={a.id} value={a.id}>
                {a.code} {a.name}
                {a.currency && a.currency !== base ? ` (${a.currency})` : ""}
              </option>
            ))}
          </select>
          {err("account") ?? (
            <span className="fy-help" id={`${id}-account-help`}>
              A bank, card or wallet account in {foreign ? `${bill.currency} or ${base}` : base}.
            </span>
          )}
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-amount`}>
            {w.amount} ({bill.currency})
          </label>
          <input
            className="fy-input fy-num"
            id={`${id}-amount`}
            inputMode="decimal"
            value={f.amount}
            onChange={(e) => set({ amount: e.target.value })}
            aria-invalid={Boolean(errors.amount) || undefined}
            aria-describedby={described("amount", `${id}-amount-help`)}
          />
          {err("amount") ?? (
            <span className="fy-help" id={`${id}-amount-help`}>
              {bill.open ? `${formatAmount(bill.open.amount)} ${bill.open.currency} is ${refund ? "left" : "open"}. ` : ""}
              {w.amountHelp}
            </span>
          )}
        </div>
        {foreign && !fromBase && (
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-rate`}>
              Rate ({base} for 1 {bill.currency}, optional)
            </label>
            <input
              className="fy-input fy-num"
              id={`${id}-rate`}
              inputMode="decimal"
              value={f.rate}
              onChange={(e) => set({ rate: e.target.value })}
              aria-invalid={Boolean(errors.rate) || undefined}
              aria-describedby={described("rate", `${id}-rate-help`)}
            />
            {err("rate") ?? (
              <span className="fy-help" id={`${id}-rate-help`}>
                As on the bank's advice; leave it blank to take the rate for the date paid.
              </span>
            )}
          </div>
        )}
        {fromBase && (
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-paid`}>
              {w.received} ({base})
            </label>
            <input
              className="fy-input fy-num"
              id={`${id}-paid`}
              inputMode="decimal"
              value={f.paid}
              onChange={(e) => set({ paid: e.target.value })}
              aria-invalid={Boolean(errors.paid) || undefined}
              aria-describedby={described("paid", `${id}-paid-help`)}
            />
            {err("paid") ?? (
              <span className="fy-help" id={`${id}-paid-help`}>
                As the statement shows it. The difference from what the bill carried is the exchange gain or loss.
              </span>
            )}
          </div>
        )}
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-reference`}>
            Bank reference (optional)
          </label>
          <input className="fy-input" id={`${id}-reference`} value={f.reference} maxLength={140} onChange={(e) => set({ reference: e.target.value })} />
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy} title={h.tooltip}>
            <Icon of={Banknote} />
            {h.label}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// ApplyCredit applies a credit note to one of its supplier's open bills in its currency (FR-AP-03): both go down by the
// amount, at most what each has open (the server refuses more: CREDIT-05). In the base currency it posts nothing; across
// rates the realised FX.
export function ApplyCredit({ open, credit, onClose, onDone }: { open: boolean; credit: Bill; onClose: () => void; onDone: (message: string) => void }) {
  const confirm = useConfirm();
  const id = useId();
  const h = helpText("credit.apply");
  const left = credit.open?.amount ?? "0";
  const bills = useQuery({
    queryKey: ["bills", "open", credit.supplier?.id, credit.currency],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/bills", { params: { query: { status: "posted", page_size: 500 } } });
      if (error) throw error;
      return (data.items ?? []).filter((b) => b.supplier?.id === credit.supplier?.id && b.currency === credit.currency && b.open && sign(b.open.amount) > 0);
    },
  });
  const [billId, setBillId] = useState("");
  const [amount, setAmount] = useState(left);
  const [on, setOn] = useState(today);
  const [errors, setErrors] = useState<Errors>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const choose = (bid: string) => {
    setBillId(bid);
    const b = (bills.data ?? []).find((x) => x.id === bid);
    if (b?.open) setAmount(sign(minus(b.open.amount, left)) < 0 ? b.open.amount : left); // the lesser of the two
  };
  const run = async (e: FormEvent) => {
    e.preventDefault();
    const typedAmount = typed(amount);
    const errs: Errors = {};
    if (!billId) errs.bill = "Choose the bill the credit settles.";
    if (typedAmount === null || typedAmount === "" || sign(typedAmount) <= 0) errs.amount = `Enter what to apply in ${credit.currency}, such as 100.00.`;
    if (!on) errs.date = "Enter the day the credit is applied.";
    setErrors(errs);
    if (Object.keys(errs).length > 0) return;
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/credit-applications", {
        params: { header: { "Idempotency-Key": newKey() } },
        body: { credit_id: credit.id, bill_id: billId, date: on, amount: { amount: typedAmount ?? "0", currency: credit.currency } },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    const x = out.data.application;
    onDone(`Applied ${formatAmount(x.amount.amount)} ${x.amount.currency} to ${x.bill.number ?? "the bill"}.`);
  };
  const err = (k: string) =>
    errors[k] ? (
      <span className="fy-error" id={`${id}-${k}-error`}>
        {errors[k]}
      </span>
    ) : null;
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      eyebrow={credit.number}
      title="Apply this credit to a bill"
      description={`The bill and the credit both go down by the amount. ${formatAmount(left)} ${credit.currency} of the credit is left.`}
    >
      {problem && <Refusal problem={problem} title="The credit was not applied" />}
      <form className="fy-stack fy-stack--3" onSubmit={(e) => void run(e)} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-bill`}>
            Bill
          </label>
          <select
            className="fy-select"
            id={`${id}-bill`}
            value={billId}
            onChange={(e) => choose(e.target.value)}
            aria-invalid={Boolean(errors.bill) || undefined}
            aria-describedby={errors.bill ? `${id}-bill-error` : `${id}-bill-help`}
          >
            <option value="">Choose a bill</option>
            {(bills.data ?? []).map((b) => (
              <option key={b.id} value={b.id}>
                {b.number ?? "A bill"}
                {b.supplier_ref ? ` (${b.supplier_ref})` : ""} · {formatAmount(b.open?.amount ?? "0")} {b.currency} open
              </option>
            ))}
          </select>
          {err("bill") ?? (
            <span className="fy-help" id={`${id}-bill-help`}>
              {bills.data && bills.data.length === 0 ? `${credit.supplier?.name ?? "The supplier"} has no open bill in ${credit.currency}.` : `${credit.supplier?.name ?? "The supplier"}'s posted bills in ${credit.currency} with something open.`}
            </span>
          )}
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-amount`}>
            Amount to apply ({credit.currency})
          </label>
          <input
            className="fy-input fy-num"
            id={`${id}-amount`}
            inputMode="decimal"
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            aria-invalid={Boolean(errors.amount) || undefined}
            aria-describedby={errors.amount ? `${id}-amount-error` : undefined}
          />
          {err("amount")}
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-date`}>
            Date applied
          </label>
          <input className="fy-input" id={`${id}-date`} type="date" value={on} min={credit.date} onChange={(e) => setOn(e.target.value)} aria-describedby={errors.date ? `${id}-date-error` : undefined} />
          {err("date")}
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy} title={h.tooltip}>
            <Icon of={Link2} />
            {h.label}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// VoidPayment voids a recorded payment with a date and a reason, which reopens the bill; in Founder mode it asks
// another founder first (FM-1).
function VoidPayment({ open, payment, minDate, ask, onClose, onDone }: { open: boolean; payment: PaidBy; minDate: string; ask: boolean; onClose: () => void; onDone: (message: string) => void }) {
  const confirm = useConfirm();
  const id = useId();
  const [on, setOn] = useState(today);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [missing, setMissing] = useState(false);
  const h = helpText(ask ? "bill-payment.ask-void" : "bill-payment.void");
  const run = async (e: FormEvent) => {
    e.preventDefault();
    if (!reason.trim()) return setMissing(true);
    setBusy(true);
    setProblem(null);
    const body = { date: on, reason: reason.trim() };
    if (payment.via === "application") {
      // A credit applied to the bill (FR-AP-03): voided through the application, which reopens the bill and the credit.
      const path = { application_id: payment.payment_id };
      const read = await api.GET("/v1/credit-applications/{application_id}", { params: { path } });
      if (!read.data) {
        setBusy(false);
        return setProblem(problemOf(read.error));
      }
      const params = { path, header: { "If-Match": read.response.headers.get("ETag") ?? "", "Idempotency-Key": newKey() } };
      const out = ask
        ? await guarded(confirm, () => api.POST("/v1/credit-applications/{application_id}/void-approval", { params, body }))
        : await guarded(confirm, () => api.POST("/v1/credit-applications/{application_id}/void", { params, body }));
      setBusy(false);
      if (!out.data) return setProblem(problemOf(out.error));
      return onDone(ask ? "Asked for approval: the credit is unapplied when another person approves it." : "Voided: the bill and the credit are open again by what was applied.");
    }
    if (payment.via === "refund") {
      // Money the supplier paid back against the credit (FR-AP-03): voided through the refund, which reopens the credit.
      const path = { refund_id: payment.payment_id };
      const read = await api.GET("/v1/supplier-refunds/{refund_id}", { params: { path } });
      if (!read.data) {
        setBusy(false);
        return setProblem(problemOf(read.error));
      }
      const params = { path, header: { "If-Match": read.response.headers.get("ETag") ?? "", "Idempotency-Key": newKey() } };
      const out = ask
        ? await guarded(confirm, () => api.POST("/v1/supplier-refunds/{refund_id}/void-approval", { params, body }))
        : await guarded(confirm, () => api.POST("/v1/supplier-refunds/{refund_id}/void", { params, body }));
      setBusy(false);
      if (!out.data) return setProblem(problemOf(out.error));
      return onDone(ask ? "Asked for approval: the refund is voided when another person approves it." : "Voided: the credit is open again by what was refunded.");
    }
    const read = await api.GET("/v1/bill-payments/{payment_id}", { params: { path: { payment_id: payment.payment_id } } });
    if (!read.data) {
      setBusy(false);
      return setProblem(problemOf(read.error));
    }
    const params = { path: { payment_id: payment.payment_id }, header: { "If-Match": read.response.headers.get("ETag") ?? "", "Idempotency-Key": newKey() } };
    if (ask) {
      const out = await guarded(confirm, () => api.POST("/v1/bill-payments/{payment_id}/void-approval", { params, body }));
      setBusy(false);
      if (!out.data) return setProblem(problemOf(out.error));
      return onDone("Asked for approval: the payment is voided, and the bill reopened, when another person approves it.");
    }
    const out = await guarded(confirm, () => api.POST("/v1/bill-payments/{payment_id}/void", { params, body }));
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone("Voided: a reversing entry posted and the bill is open again by what it paid.");
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      eyebrow={payment.payment_number}
      title={ask ? "Ask to void this payment?" : "Void this payment?"}
      description={
        ask
          ? "In Founder mode another founder approves a void first. Then a reversing entry posts on the date you choose and the bill is open again by what the payment paid."
          : "A reversing entry posts on the date you choose and the bill is open again by what the payment paid. The payment stays in the books, marked voided."
      }
    >
      {problem && <Refusal problem={problem} title={ask ? "The request was not made" : "The payment was not voided"} />}
      <form className="fy-stack fy-stack--3" onSubmit={(e) => void run(e)} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-date`}>
            Date of the reversing entry
          </label>
          <input className="fy-input" id={`${id}-date`} type="date" value={on} min={minDate} onChange={(e) => setOn(e.target.value)} />
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

// Payments lists the payments that settle a bill, earliest first, each with its void (FR-AR-18); a voided one stays
// listed, marked voided.
export function Payments({ bill, mayVoid, ask, onDone }: { bill: Bill; mayVoid: boolean; ask: boolean; onDone: (message: string) => void }) {
  const id = useId();
  const [voiding, setVoiding] = useState<PaidBy | null>(null);
  const h = helpText(ask ? "bill-payment.ask-void" : "bill-payment.void");
  const payments = bill.payments ?? [];
  if (payments.length === 0) return null;
  return (
    <section className="fy-card" aria-labelledby={id}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={id}>
          Payments
        </h2>
        {bill.open && (
          <span className="fy-small fy-muted">
            {formatAmount(bill.open.amount)} {bill.open.currency} {bill.kind === "vendor_credit" ? "left" : "open"}
          </span>
        )}
      </div>
      <table className="fy-table fy-table--compact fy-table--cards" aria-labelledby={id}>
        <thead>
          <tr>
            <th scope="col">Payment</th>
            <th scope="col">Date</th>
            <th scope="col" className="fy-num">
              Amount ({bill.currency})
            </th>
            <th scope="col">Status</th>
            <th scope="col" className="w-px">
              <span className="sr-only">Actions</span>
            </th>
          </tr>
        </thead>
        <tbody>
          {payments.map((p) => (
            <tr key={p.payment_id}>
              <td data-card="title" className="fy-mono">
                {p.payment_number ?? "—"}
              </td>
              <td data-card="meta">{date(p.date)}</td>
              <td data-card="amount" className="fy-num">
                {formatAmount(p.amount.amount)}
              </td>
              <td data-card="status">
                {p.voided ? (
                  <span className="fy-chip fy-chip--neutral">
                    <Icon of={Undo2} />
                    Voided
                  </span>
                ) : (
                  <span className="fy-chip fy-chip--good">
                    <Icon of={Banknote} />
                    {p.via === "application" ? "Applied" : p.via === "refund" ? "Refunded" : "Recorded"}
                  </span>
                )}
              </td>
              <td>
                {mayVoid && !p.voided && (
                  <Button size="sm" variant="ghost" type="button" onClick={() => setVoiding(p)} title={h.tooltip} aria-label={`${h.label} ${p.payment_number ?? "the payment"}`}>
                    <Icon of={ask ? UserCheck : Undo2} />
                    {h.label}…
                  </Button>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {voiding && (
        <VoidPayment
          open
          payment={voiding}
          minDate={voiding.date}
          ask={ask}
          onClose={() => setVoiding(null)}
          onDone={(message) => {
            setVoiding(null);
            onDone(message);
          }}
        />
      )}
    </section>
  );
}
