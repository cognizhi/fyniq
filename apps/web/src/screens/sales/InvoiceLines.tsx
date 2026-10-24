import { Plus, Trash2 } from "lucide-react";
import type { ReactNode } from "react";
import type { components } from "../../api/schema";
import { Button } from "../../components/ui/button";
import { helpText } from "../../i18n";
import { Icon } from "../../ui";
import type { Account } from "../accounting/data";

type GSTCode = components["schemas"]["SalesGSTCodes"]["codes"][number];
export type Item = components["schemas"]["Item"];
export type QuoteRef = components["schemas"]["InvoiceLineQuote"];

// Line is one line of an invoice as the form keeps it (FR-AR-03): what was typed, before it is checked.
export interface Line {
  key: string;
  item_id: string;
  description: string;
  quantity: string;
  unit_price: string;
  discount: string; // a percentage off the line; blank for none
  account_id: string;
  gst_code: string;
  spread_months: string; // revenue spread over months on 2400 Deferred revenue (FR-GL-03); blank for none
  spread_from: string; // the spread's first month, "2026-10"; blank for the invoice's month
  quote?: QuoteRef; // the quote or sales order line it invoices (FR-AR-02), kept as the quote made it
}

let keys = 0;
export const newLine = (): Line => ({ key: `il-${++keys}`, item_id: "", description: "", quantity: "1", unit_price: "", discount: "", account_id: "", gst_code: "", spread_months: "", spread_from: "" });

// lineKey is a fresh key for a line read from a saved invoice.
export const lineKey = (): string => `il-${++keys}`;

export type Errors = Record<string, string>;

// fromItem fills a line from the item it sells: its description, its price in the currency, its revenue account and
// GST code. What was typed already stays (FR-AR-03: the line keeps its own description, price and account).
export function fromItem(l: Line, item: Item | undefined, currency: string): Line {
  if (!item) return { ...l, item_id: "" };
  const price = (item.prices ?? []).find((p) => p.currency === currency)?.unit_price ?? "";
  return {
    ...l,
    item_id: item.id,
    description: l.description || item.description || item.name,
    unit_price: l.unit_price || price,
    account_id: l.account_id || item.revenue_account_id || "",
    gst_code: l.gst_code || item.gst_code || "",
  };
}

// InvoiceLines is the card of an invoice's lines (FR-AR-03), shared by an invoice, a credit note and a recurring
// invoice: each line's item, description, quantity, unit price before GST, discount, revenue account and GST code; with
// spreads, the spread of its revenue over months (FR-GL-03). id is the form's, for each error's element.
export function InvoiceLines({ id, lines, onChange, editable, errors, currency, spreads, codes, choices, items, subtitle, footer }: {
  id: string;
  lines: Line[];
  onChange: (lines: Line[]) => void;
  editable: boolean;
  errors: Errors;
  currency: string;
  spreads: boolean;
  codes: GSTCode[];
  choices: Account[];
  items: Item[];
  subtitle: string;
  footer?: ReactNode;
}) {
  const setLine = (key: string, patch: Partial<Line>) => onChange(lines.map((l) => (l.key === key ? { ...l, ...patch } : l)));
  const err = (k: string) =>
    errors[k] ? (
      <span className="fy-error" id={`${id}-${k}-error`}>
        {errors[k]}
      </span>
    ) : null;
  const described = (k: string, help?: string) => (errors[k] ? `${id}-${k}-error` : help);
  return (
    <section className="fy-card" aria-labelledby={`${id}-lines`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-lines`}>
          Lines
        </h2>
        <span className="fy-small fy-muted">{subtitle}</span>
      </div>
      <div className="fy-card__body fy-stack fy-stack--4">
        {lines.map((l, i) => (
          <fieldset key={l.key} className="fy-fieldset">
            <legend className="fy-small fy-strong">Line {i + 1}</legend>
            {l.quote && (
              <p className="fy-small fy-muted">
                Invoices {l.quantity} of line {l.quote.line} of an accepted quote.
              </p>
            )}
            <div className="fy-form fy-form--2">
              {items.length > 0 && (
                <div className="fy-field">
                  <label className="fy-label" htmlFor={`${l.key}-item`}>
                    Item (optional)
                  </label>
                  <select
                    className="fy-select"
                    id={`${l.key}-item`}
                    value={l.item_id}
                    disabled={!editable}
                    onChange={(e) => onChange(lines.map((x) => (x.key === l.key ? fromItem(x, items.find((it) => it.id === e.target.value), currency) : x)))}
                  >
                    <option value="">No item</option>
                    {items.map((it) => (
                      <option key={it.id} value={it.id}>
                        {it.code} {it.name}
                      </option>
                    ))}
                  </select>
                </div>
              )}
              <div className={`fy-field${items.length > 0 ? "" : " fy-field--full"}`}>
                <label className="fy-label" htmlFor={`${l.key}-description`}>
                  Description
                </label>
                <input
                  className="fy-input"
                  id={`${l.key}-description`}
                  value={l.description}
                  maxLength={500}
                  disabled={!editable}
                  onChange={(e) => setLine(l.key, { description: e.target.value })}
                  aria-invalid={Boolean(errors[`${l.key}.description`]) || undefined}
                  aria-describedby={described(`${l.key}.description`)}
                />
                {err(`${l.key}.description`)}
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${l.key}-quantity`}>
                  Quantity
                </label>
                <input
                  className="fy-input fy-num"
                  id={`${l.key}-quantity`}
                  inputMode="decimal"
                  value={l.quantity}
                  disabled={!editable}
                  onChange={(e) => setLine(l.key, { quantity: e.target.value })}
                  aria-invalid={Boolean(errors[`${l.key}.quantity`]) || undefined}
                  aria-describedby={described(`${l.key}.quantity`)}
                />
                {err(`${l.key}.quantity`)}
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${l.key}-price`}>
                  Unit price before GST ({currency})
                </label>
                <input
                  className="fy-input fy-num"
                  id={`${l.key}-price`}
                  inputMode="decimal"
                  value={l.unit_price}
                  disabled={!editable}
                  onChange={(e) => setLine(l.key, { unit_price: e.target.value })}
                  aria-invalid={Boolean(errors[`${l.key}.unit_price`]) || undefined}
                  aria-describedby={described(`${l.key}.unit_price`)}
                />
                {err(`${l.key}.unit_price`)}
              </div>
              <div className={`fy-field${l.discount ? "" : " fy-expert-only"}`}>
                <label className="fy-label" htmlFor={`${l.key}-discount`}>
                  Discount (%, optional)
                </label>
                <input
                  className="fy-input fy-num"
                  id={`${l.key}-discount`}
                  inputMode="decimal"
                  value={l.discount}
                  disabled={!editable}
                  onChange={(e) => setLine(l.key, { discount: e.target.value })}
                  aria-invalid={Boolean(errors[`${l.key}.discount`]) || undefined}
                  aria-describedby={described(`${l.key}.discount`)}
                />
                {err(`${l.key}.discount`)}
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${l.key}-account`}>
                  Revenue account
                </label>
                <select
                  className="fy-select"
                  id={`${l.key}-account`}
                  value={l.account_id}
                  disabled={!editable}
                  onChange={(e) => setLine(l.key, { account_id: e.target.value })}
                  aria-invalid={Boolean(errors[`${l.key}.account`]) || undefined}
                  aria-describedby={described(`${l.key}.account`)}
                >
                  <option value="">Choose an account</option>
                  {choices.map((a) => (
                    <option key={a.id} value={a.id}>
                      {a.code} {a.name}
                    </option>
                  ))}
                </select>
                {err(`${l.key}.account`)}
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${l.key}-code`}>
                  GST code
                </label>
                <select
                  className="fy-select"
                  id={`${l.key}-code`}
                  value={l.gst_code}
                  disabled={!editable}
                  onChange={(e) => setLine(l.key, { gst_code: e.target.value })}
                  aria-invalid={Boolean(errors[`${l.key}.gst_code`]) || undefined}
                  aria-describedby={described(`${l.key}.gst_code`)}
                >
                  <option value="">Choose a code</option>
                  {codes.map((c) => (
                    <option key={c.code} value={c.code}>
                      {c.code}: {c.description}
                    </option>
                  ))}
                </select>
                {err(`${l.key}.gst_code`)}
              </div>
              {spreads && (
                // A spread is an Expert field (§7.8), shown in Basic too once a line has one, so nothing is hidden.
                <>
                  <div className={`fy-field${l.spread_months || l.spread_from ? "" : " fy-expert-only"}`}>
                    <label className="fy-label" htmlFor={`${l.key}-spread`}>
                      Spread over (months, optional)
                    </label>
                    <input
                      className="fy-input fy-num"
                      id={`${l.key}-spread`}
                      inputMode="numeric"
                      value={l.spread_months}
                      disabled={!editable}
                      onChange={(e) => setLine(l.key, { spread_months: e.target.value })}
                      aria-invalid={Boolean(errors[`${l.key}.spread_months`]) || undefined}
                      aria-describedby={described(`${l.key}.spread_months`, `${l.key}-spread-help`)}
                    />
                    {err(`${l.key}.spread_months`) ?? (
                      <span className="fy-help" id={`${l.key}-spread-help`}>
                        {helpText("invoice.spread").tooltip}
                      </span>
                    )}
                  </div>
                  <div className={`fy-field${l.spread_months || l.spread_from ? "" : " fy-expert-only"}`}>
                    <label className="fy-label" htmlFor={`${l.key}-spread-from`}>
                      Spread from
                    </label>
                    <input
                      className="fy-input"
                      id={`${l.key}-spread-from`}
                      type="month"
                      placeholder="2026-10"
                      value={l.spread_from}
                      disabled={!editable}
                      onChange={(e) => setLine(l.key, { spread_from: e.target.value })}
                      aria-invalid={Boolean(errors[`${l.key}.spread_from`]) || undefined}
                      aria-describedby={described(`${l.key}.spread_from`, `${l.key}-spread-from-help`)}
                    />
                    {err(`${l.key}.spread_from`) ?? (
                      <span className="fy-help" id={`${l.key}-spread-from-help`}>
                        The month the service starts; blank for the invoice's month.
                      </span>
                    )}
                  </div>
                </>
              )}
              {editable && lines.length > 1 && (
                <div className="fy-field fy-field--full">
                  <Button variant="ghost" size="sm" type="button" aria-label={`Remove line ${i + 1}`} onClick={() => onChange(lines.filter((x) => x.key !== l.key))}>
                    <Icon of={Trash2} />
                    Remove
                  </Button>
                </div>
              )}
            </div>
          </fieldset>
        ))}
        {err("lines")}
        {editable && (
          <div>
            <Button variant="ghost" type="button" onClick={() => onChange([...lines, newLine()])}>
              <Icon of={Plus} />
              Add a line
            </Button>
          </div>
        )}
        {footer}
      </div>
    </section>
  );
}
