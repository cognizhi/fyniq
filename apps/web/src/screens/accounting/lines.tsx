import { CircleCheck, CircleX, Plus, Trash2 } from "lucide-react";
import type { components } from "../../api/schema";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { isZero, minus, sign, sum, typed } from "../../format/decimal";
import { Icon } from "../../ui";
import type { Account } from "./data";

type Draft = components["schemas"]["JournalDraft"];

export interface Line {
  key: string;
  account_id: string;
  description: string;
  debit: string;
  credit: string;
  currency: string;
  fx_rate: string;
}

let keys = 0;
export const lineKey = () => `line-${++keys}`;
export const newLine = (currency: string): Line => ({ key: lineKey(), account_id: "", description: "", debit: "", credit: "", currency, fx_rate: "" });

// toLines are saved lines as the editor shows them: a positive amount is a debit, a negative one a credit.
export function toLines(saved: readonly { account_id: string; description?: string; amount: { amount: string; currency: string }; fx_rate?: string }[]): Line[] {
  return saved.map((l) => {
    const credit = l.amount.amount.startsWith("-");
    return {
      key: lineKey(),
      account_id: l.account_id,
      description: l.description ?? "",
      debit: credit ? "" : l.amount.amount,
      credit: credit ? l.amount.amount.slice(1) : "",
      currency: l.amount.currency,
      fx_rate: l.fx_rate ?? "",
    };
  });
}

export const blank = (l: Line) => !l.account_id && !l.description.trim() && !l.debit.trim() && !l.credit.trim();

export type Errors = Record<string, string>; // by "date", "lines", or "<line key>.<field>"

// checkLines turns the typed lines into the lines to send, or says what to fix under each field in errors. Blank
// lines are left out. The API checks the rest when the journal posts (ENG-24).
export function checkLines(typedLines: Line[], base: string, errors: Errors): NonNullable<Draft["lines"]> {
  const lines: NonNullable<Draft["lines"]> = [];
  for (const l of typedLines) {
    if (blank(l)) continue;
    const debit = typed(l.debit);
    const credit = typed(l.credit);
    if (!l.account_id) errors[`${l.key}.account`] = "Choose the account.";
    if (debit === null) errors[`${l.key}.debit`] = "Enter an amount, such as 1,200.00.";
    if (credit === null) errors[`${l.key}.credit`] = "Enter an amount, such as 1,200.00.";
    let amount = "";
    if (debit !== null && credit !== null) {
      const d = debit !== "" && !isZero(debit);
      const c = credit !== "" && !isZero(credit);
      if (sign(debit || "0") < 0 || sign(credit || "0") < 0) errors[`${l.key}.debit`] = "Enter the amount without a minus sign: a credit goes under Credit.";
      else if (d && c) errors[`${l.key}.debit`] = "Enter a debit or a credit, not both.";
      else if (!d && !c) errors[`${l.key}.debit`] = "Enter a debit or a credit.";
      else amount = d ? debit : `-${credit}`;
    }
    const currency = (l.currency.trim() || base).toUpperCase();
    const rate = l.fx_rate.trim();
    if (!/^[A-Z]{3}$/.test(currency)) errors[`${l.key}.currency`] = "Enter a currency code, such as USD.";
    else if (currency !== base && !/^(0|[1-9]\d{0,9})(\.\d{1,10})?$/.test(rate)) errors[`${l.key}.fx_rate`] = `Enter the rate: ${base} for 1 ${currency}, such as 1.3472.`;
    lines.push({ account_id: l.account_id, amount: { amount, currency }, description: l.description.trim() || undefined, fx_rate: currency !== base ? rate : undefined });
  }
  return lines;
}

export interface Balance {
  currency: string;
  debits: string;
  credits: string;
  difference: string; // debits − credits
}

// balances adds up what was typed, per currency, exactly; amounts that are not amounts yet count as 0.
export function balances(f: { lines: Line[] }, base: string): Balance[] {
  const by = new Map<string, { d: string[]; c: string[] }>();
  for (const l of f.lines) {
    const currency = (l.currency.trim() || base).toUpperCase();
    const t = by.get(currency) ?? { d: [], c: [] };
    const d = typed(l.debit);
    const c = typed(l.credit);
    if (d) t.d.push(d);
    if (c) t.c.push(c);
    by.set(currency, t);
  }
  return [...by.entries()]
    .filter(([, t]) => t.d.length + t.c.length > 0)
    .map(([currency, t]) => {
      const debits = sum(t.d);
      const credits = sum(t.c);
      return { currency, debits, credits, difference: minus(debits, credits) };
    });
}

export function BalanceStatus({ list, base }: { list: Balance[]; base: string }) {
  if (list.length === 0) return <span className="fy-small fy-muted">Enter the lines to see whether they balance.</span>;
  // A journal in several currencies balances in the base currency, once each line is converted and rounded: only
  // the server's check can say so, because the page never rounds (RND-FX, FR-UX-22).
  if (list.length > 1)
    return (
      <span className="fy-small fy-muted">
        In {list.map((b) => b.currency).join(" and ")}: select Check to see whether it balances in {base}.
      </span>
    );
  return (
    <span className="fy-row fy-row--wrap gap-2">
      {list.map((b) =>
        isZero(b.difference) ? (
          <span key={b.currency} className="fy-chip fy-chip--good">
            <Icon of={CircleCheck} />
            Balanced in {b.currency}: {formatAmount(b.debits)} each side
          </span>
        ) : (
          <span key={b.currency} className="fy-chip fy-chip--critical">
            <Icon of={CircleX} />
            {sign(b.difference) > 0 ? "Debits exceed credits" : "Credits exceed debits"} by {formatAmount(b.difference.replace(/^-/, ""))} {b.currency}
          </span>
        ),
      )}
      {list[0]?.currency !== base && <span className="fy-small fy-muted">Check it to see it in {base}: rounding can leave a difference to post to 7030.</span>}
    </span>
  );
}

export function accountLabel(accounts: Map<string, Account>, id: string): string {
  const a = accounts.get(id);
  return a ? `${a.code} ${a.name}` : "An account not in the chart";
}

// LineEditor edits a journal's lines: the account, description, debit or credit, currency and rate of each, with
// whether they balance as typed (FR-GL-01). The journal editor and the recurring journal editor share it.
export function LineEditor({ id, lines, onChange, base, choices, byId, errors, title = "Lines" }: { id: string; lines: Line[]; onChange: (lines: Line[]) => void; base: string; choices: Account[]; byId: Map<string, Account>; errors: Errors; title?: string }) {
  const setLine = (key: string, field: keyof Line, value: string) => onChange(lines.map((l) => (l.key === key ? { ...l, [field]: value } : l)));
  const list = balances({ lines }, base);
  const fieldError = (key: string) =>
    errors[key] ? (
      <span className="fy-error" id={`${id}-${key}-error`}>
        {errors[key]}
      </span>
    ) : null;
  const described = (key: string) => (errors[key] ? `${id}-${key}-error` : undefined);
  return (
    <section className="fy-card" aria-labelledby={`${id}-lines`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-lines`}>
          {title}
        </h2>
        <span className="fy-small fy-muted fy-from-medium">Debits are positive, credits negative, once posted.</span>
      </div>
      <ol className="fy-card__body fy-stack fy-stack--3">
        {lines.map((l, i) => {
          const n = i + 1;
          return (
            <li key={l.key} className="grid grid-cols-2 gap-2 border-b border-border pb-3 medium:grid-cols-12 medium:items-start">
              <div className="fy-field col-span-2 medium:col-span-6 expanded:col-span-3">
                <label className="fy-label" htmlFor={`${l.key}-account`}>
                  <span className="fy-sr">Line {n}: </span>
                  Account
                </label>
                <select className="fy-select" id={`${l.key}-account`} value={l.account_id} onChange={(e) => setLine(l.key, "account_id", e.target.value)} aria-invalid={errors[`${l.key}.account`] ? true : undefined} aria-describedby={described(`${l.key}.account`)}>
                  <option value="">Choose an account</option>
                  {choices.map((a) => (
                    <option key={a.id} value={a.id}>
                      {a.code} {a.name}
                    </option>
                  ))}
                  {l.account_id && !choices.some((a) => a.id === l.account_id) && <option value={l.account_id}>{accountLabel(byId, l.account_id)}</option>}
                </select>
                {fieldError(`${l.key}.account`)}
              </div>
              <div className="fy-field col-span-2 medium:col-span-6 expanded:col-span-3">
                <label className="fy-label" htmlFor={`${l.key}-description`}>
                  <span className="fy-sr">Line {n}: </span>
                  Description
                </label>
                <input className="fy-input" id={`${l.key}-description`} value={l.description} maxLength={500} onChange={(e) => setLine(l.key, "description", e.target.value)} />
              </div>
              <div className="fy-field medium:col-span-4 expanded:col-span-2">
                <label className="fy-label" htmlFor={`${l.key}-debit`}>
                  <span className="fy-sr">Line {n}: </span>
                  Debit
                </label>
                <input className="fy-input fy-num" id={`${l.key}-debit`} inputMode="decimal" value={l.debit} onChange={(e) => setLine(l.key, "debit", e.target.value)} aria-invalid={errors[`${l.key}.debit`] ? true : undefined} aria-describedby={described(`${l.key}.debit`)} />
                {fieldError(`${l.key}.debit`)}
              </div>
              <div className="fy-field medium:col-span-4 expanded:col-span-2">
                <label className="fy-label" htmlFor={`${l.key}-credit`}>
                  <span className="fy-sr">Line {n}: </span>
                  Credit
                </label>
                <input className="fy-input fy-num" id={`${l.key}-credit`} inputMode="decimal" value={l.credit} onChange={(e) => setLine(l.key, "credit", e.target.value)} aria-invalid={errors[`${l.key}.credit`] ? true : undefined} aria-describedby={described(`${l.key}.credit`)} />
                {fieldError(`${l.key}.credit`)}
              </div>
              <div className="fy-field medium:col-span-3 expanded:col-span-1">
                <label className="fy-label" htmlFor={`${l.key}-currency`}>
                  <span className="fy-sr">Line {n}: </span>
                  Currency
                </label>
                <input className="fy-input fy-mono" id={`${l.key}-currency`} value={l.currency} maxLength={3} onChange={(e) => setLine(l.key, "currency", e.target.value.toUpperCase())} aria-invalid={errors[`${l.key}.currency`] ? true : undefined} aria-describedby={described(`${l.key}.currency`)} />
                {fieldError(`${l.key}.currency`)}
              </div>
              <div className="flex items-end justify-end self-end medium:col-span-1">
                <Button variant="ghost" size="sm" type="button" aria-label={`Remove line ${n}`} onClick={() => onChange(lines.filter((x) => x.key !== l.key))}>
                  <Icon of={Trash2} />
                </Button>
              </div>
              {(l.currency.trim() || base).toUpperCase() !== base && (
                <div className="fy-field col-span-2 medium:col-span-4 expanded:col-span-3">
                  <label className="fy-label" htmlFor={`${l.key}-rate`}>
                    <span className="fy-sr">Line {n}: </span>
                    Rate ({base} for 1 {l.currency.toUpperCase()})
                  </label>
                  <input className="fy-input fy-num" id={`${l.key}-rate`} inputMode="decimal" value={l.fx_rate} onChange={(e) => setLine(l.key, "fx_rate", e.target.value)} aria-invalid={errors[`${l.key}.fx_rate`] ? true : undefined} aria-describedby={described(`${l.key}.fx_rate`)} />
                  {fieldError(`${l.key}.fx_rate`)}
                </div>
              )}
            </li>
          );
        })}
      </ol>
      <div className="fy-card__foot fy-row fy-row--wrap fy-row--between gap-2">
        <Button variant="ghost" size="sm" type="button" onClick={() => onChange([...lines, newLine(base)])}>
          <Icon of={Plus} />
          Add a line
        </Button>
        <BalanceStatus list={list} base={base} />
      </div>
      {errors.lines && <p className="fy-error px-4 pb-3">{errors.lines}</p>}
    </section>
  );
}
