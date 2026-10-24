import { Banknote } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { minus, sign, typed } from "../../format/decimal";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { postable, type Account } from "../accounting/data";
import type { Invoice } from "./Invoices";

type ReceiptDraft = components["schemas"]["CustomerReceiptDraft"];
type Errors = Record<string, string>;

function today(): string {
  return new Date().toLocaleDateString("en-CA");
}

// receivable is what a receipt of an invoice can come into: an active money account in its currency, or in the base
// currency.
export function receivable(accounts: readonly Account[], invoice: Invoice, base: string): Account[] {
  return postable(accounts).filter((a) => a.money_account && [invoice.currency, base].includes(a.currency || base));
}

// receiptOf turns what was typed into the receipt to record, or says what to fix under each field (FR-AR-18). What the
// customer paid beyond what is open on the invoice stays as a credit with them, never as income; in full settles what
// is left within the settlement tolerance (FR-BNK-08). The API checks the rest: the account, the rate and the tolerance.
export function receiptOf(
  invoice: Invoice,
  base: string,
  f: { date: string; account: string; accountCurrency: string; amount: string; rate: string; received: string; reference: string; inFull: boolean },
): { receipt?: ReceiptDraft; errors: Errors } {
  const errors: Errors = {};
  const amount = typed(f.amount);
  const foreign = invoice.currency !== base;
  const intoBase = foreign && f.accountCurrency === base;
  const rate = f.rate.trim();
  const received = typed(f.received);
  if (!f.date) errors.date = "Enter the day the money came into the account.";
  if (!f.account) errors.account = "Choose the account the money came into.";
  if (amount === null || amount === "" || /^-|^0(\.0*)?$/.test(amount)) errors.amount = `Enter what the customer paid in ${invoice.currency}, such as 400.00.`;
  if (foreign && !intoBase && rate && !/^(0|[1-9]\d{0,9})(\.\d{1,10})?$/.test(rate)) errors.rate = `Enter the rate as ${base} for 1 ${invoice.currency}, such as 1.3472, or leave it blank to take the day's rate.`;
  if (intoBase && (received === null || received === "")) errors.received = `Enter what the account received in ${base}, as the statement shows it.`;
  if (Object.keys(errors).length > 0 || amount === null) return { errors };
  const open = invoice.open?.amount ?? "0";
  // What is open goes to the invoice; anything more is the customer's credit.
  const more = sign(minus(amount, open)) > 0;
  const applied = more ? open : amount;
  const extra = more ? minus(amount, open) : "";
  return {
    errors,
    receipt: {
      customer_id: invoice.customer?.id,
      date: f.date,
      money_account_id: f.account,
      currency: invoice.currency,
      fx_rate: foreign && !intoBase && rate ? rate : undefined,
      received: intoBase && received ? { amount: received, currency: base } : undefined,
      reference: f.reference.trim() || undefined,
      invoices: [{ invoice_id: invoice.id, amount: { amount: applied, currency: invoice.currency }, in_full: f.inFull && !extra ? true : undefined }],
      unapplied: extra ? { amount: extra, currency: invoice.currency } : undefined,
    },
  };
}

// RecordReceipt records money a customer paid against an issued invoice, by hand until bank import matches it (FR-AR-18;
// the owner's decision of 24 Oct 2026): the date, the money account it came into, and what they paid. It posts at once:
// the money is in the bank already. Paid in full within the settlement tolerance, the difference clears; paid more, the
// rest stays as a credit with the customer.
export function RecordReceipt({ open, invoice, base, accounts, onClose, onDone }: { open: boolean; invoice: Invoice; base: string; accounts: readonly Account[]; onClose: () => void; onDone: (message: string) => void }) {
  const confirm = useConfirm();
  const id = useId();
  const choices = receivable(accounts, invoice, base);
  const [f, setF] = useState({ date: today(), account: choices.length === 1 ? (choices[0]?.id ?? "") : "", amount: invoice.open?.amount ?? "", rate: "", received: "", reference: "", inFull: false });
  const [errors, setErrors] = useState<Errors>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText("invoice.record-payment");
  const accountCurrency = accounts.find((a) => a.id === f.account)?.currency || base;
  const foreign = invoice.currency !== base;
  const intoBase = foreign && Boolean(f.account) && accountCurrency === base;
  const amount = typed(f.amount);
  const short = amount && invoice.open && /^\d/.test(amount) && sign(minus(amount, invoice.open.amount)) < 0;
  const over = amount && invoice.open && /^\d/.test(amount) && sign(minus(amount, invoice.open.amount)) > 0 ? minus(amount, invoice.open.amount) : "";
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
    const { receipt, errors: errs } = receiptOf(invoice, base, { ...f, accountCurrency });
    setErrors(errs);
    if (!receipt) return;
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () => api.POST("/v1/customer-receipts", { params: { header: { "Idempotency-Key": newKey() } }, body: receipt }));
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    const r = out.data.receipt;
    onDone(`Recorded ${r.number ?? "the receipt"}: ${formatAmount(r.total.amount)} ${r.total.currency} received${/^0(\.0*)?$/.test(r.unapplied.amount) ? "" : `, ${formatAmount(r.unapplied.amount)} of it kept as the customer's credit`}.`);
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      eyebrow={invoice.number}
      title="Record a payment of this invoice"
      description={`For money ${invoice.customer?.name ?? "the customer"} has already paid into the bank. It posts at once: what they owe goes down by what came in.`}
    >
      {problem && <Refusal problem={problem} title="The payment was not recorded" />}
      <form className="fy-stack fy-stack--3" onSubmit={(e) => void run(e)} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-date`}>
            Date received
          </label>
          <input className="fy-input" id={`${id}-date`} type="date" value={f.date} min={invoice.date} onChange={(e) => set({ date: e.target.value })} aria-invalid={Boolean(errors.date) || undefined} aria-describedby={described("date")} />
          {err("date")}
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-account`}>
            Paid into
          </label>
          <select className="fy-select" id={`${id}-account`} value={f.account} onChange={(e) => set({ account: e.target.value })} aria-invalid={Boolean(errors.account) || undefined} aria-describedby={described("account", `${id}-account-help`)}>
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
              A bank, card or wallet account in {foreign ? `${invoice.currency} or ${base}` : base}.
            </span>
          )}
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-amount`}>
            Amount received ({invoice.currency})
          </label>
          <input className="fy-input fy-num" id={`${id}-amount`} inputMode="decimal" value={f.amount} onChange={(e) => set({ amount: e.target.value })} aria-invalid={Boolean(errors.amount) || undefined} aria-describedby={described("amount", `${id}-amount-help`)} />
          {err("amount") ?? (
            <span className="fy-help" id={`${id}-amount-help`}>
              {invoice.open ? `${formatAmount(invoice.open.amount)} ${invoice.open.currency} is open. ` : ""}
              {over ? `${formatAmount(over)} more than is open stays as the customer's credit, never as income.` : "Record part of it, or all that is open."}
            </span>
          )}
        </div>
        {short && (
          <div className="fy-field">
            <label className="fy-row gap-2 fy-label">
              <input type="checkbox" checked={f.inFull} onChange={(e) => set({ inFull: e.target.checked })} aria-describedby={`${id}-full-help`} />
              {helpText("invoice.settle-in-full").label}
            </label>
            <span className="fy-help" id={`${id}-full-help`}>
              {helpText("invoice.settle-in-full").tooltip}
            </span>
          </div>
        )}
        {foreign && !intoBase && (
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-rate`}>
              Rate ({base} for 1 {invoice.currency}, optional)
            </label>
            <input className="fy-input fy-num" id={`${id}-rate`} inputMode="decimal" value={f.rate} onChange={(e) => set({ rate: e.target.value })} aria-invalid={Boolean(errors.rate) || undefined} aria-describedby={described("rate", `${id}-rate-help`)} />
            {err("rate") ?? (
              <span className="fy-help" id={`${id}-rate-help`}>
                As on the bank's advice; leave it blank to take the rate for the date received.
              </span>
            )}
          </div>
        )}
        {intoBase && (
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-received`}>
              What the account received ({base})
            </label>
            <input className="fy-input fy-num" id={`${id}-received`} inputMode="decimal" value={f.received} onChange={(e) => set({ received: e.target.value })} aria-invalid={Boolean(errors.received) || undefined} aria-describedby={described("received", `${id}-received-help`)} />
            {err("received") ?? (
              <span className="fy-help" id={`${id}-received-help`}>
                As the statement shows it. The difference from what the invoice carried is the exchange gain or loss.
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
