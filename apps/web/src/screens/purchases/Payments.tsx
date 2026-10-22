import { Banknote, Undo2, UserCheck } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { typed } from "../../format/decimal";
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
): { payment?: PaymentDraft; errors: Errors } {
  const errors: Errors = {};
  const amount = typed(f.amount);
  const foreign = bill.currency !== base;
  const fromBase = foreign && f.accountCurrency === base;
  const rate = f.rate.trim();
  const paid = typed(f.paid);
  if (!f.date) errors.date = "Enter the day the money left the account.";
  if (!f.account) errors.account = "Choose the account the money left.";
  if (amount === null || amount === "" || /^-|^0(\.0*)?$/.test(amount)) errors.amount = `Enter what was paid of the bill in ${bill.currency}, such as 400.00.`;
  if (foreign && !fromBase && rate && !/^(0|[1-9]\d{0,9})(\.\d{1,10})?$/.test(rate)) errors.rate = `Enter the rate as ${base} for 1 ${bill.currency}, such as 1.3472, or leave it blank to take the day's rate.`;
  if (fromBase && (paid === null || paid === "")) errors.paid = `Enter what the account paid in ${base}, as the statement shows it.`;
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

// RecordPayment records a payment the company made of a bill, by hand (FR-AP-02; the owner's decision of 22 Oct 2026):
// the date, the money account it left and what it paid of the bill. It posts at once: the money has left already.
export function RecordPayment({
  open,
  bill,
  base,
  accounts,
  onClose,
  onDone,
}: {
  open: boolean;
  bill: Bill;
  base: string;
  accounts: readonly Account[];
  onClose: () => void;
  onDone: (message: string) => void;
}) {
  const confirm = useConfirm();
  const id = useId();
  const choices = payable(accounts, bill, base);
  const [f, setF] = useState({ date: today(), account: choices.length === 1 ? (choices[0]?.id ?? "") : "", amount: bill.open?.amount ?? "", rate: "", paid: "", reference: "" });
  const [errors, setErrors] = useState<Errors>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText("bill.record-payment");
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
    const { payment, errors: errs } = paymentOf(bill, base, { ...f, accountCurrency });
    setErrors(errs);
    if (!payment) return;
    setBusy(true);
    setProblem(null);
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
      title="Record a payment of this bill"
      description={`For money that has already left the bank. It posts at once: what the company owes ${bill.supplier?.name ?? "the supplier"} goes down by what was paid.`}
    >
      {problem && <Refusal problem={problem} title="The payment was not recorded" />}
      <form className="fy-stack fy-stack--3" onSubmit={(e) => void run(e)} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-date`}>
            Date paid
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
            Paid from
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
            Amount paid of the bill ({bill.currency})
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
              {bill.open ? `${formatAmount(bill.open.amount)} ${bill.open.currency} is open. ` : ""}Pay part of it, or all that is open.
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
              What the account paid ({base})
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
    const read = await api.GET("/v1/bill-payments/{payment_id}", { params: { path: { payment_id: payment.payment_id } } });
    if (!read.data) {
      setBusy(false);
      return setProblem(problemOf(read.error));
    }
    const params = { path: { payment_id: payment.payment_id }, header: { "If-Match": read.response.headers.get("ETag") ?? "", "Idempotency-Key": newKey() } };
    const body = { date: on, reason: reason.trim() };
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
            {formatAmount(bill.open.amount)} {bill.open.currency} open
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
                    Recorded
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
