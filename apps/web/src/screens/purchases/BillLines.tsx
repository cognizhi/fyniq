import { Link } from "@tanstack/react-router";
import { Plus, Trash2 } from "lucide-react";
import type { ReactNode } from "react";
import type { components } from "../../api/schema";
import { Button } from "../../components/ui/button";
import { helpText } from "../../i18n";
import { Icon } from "../../ui";
import type { Account } from "../accounting/data";

type GSTCode = components["schemas"]["PurchaseGSTCodes"]["codes"][number];
export type LineOrder = components["schemas"]["BillLineOrder"];

// Line is one line of a bill as the form keeps it (FR-AP-02): what was typed, before it is checked.
export interface Line {
  key: string;
  account_id: string;
  description: string;
  gst_code: string;
  net: string;
  gst: string;
  stated_gst: string; // the SGD GST the tax invoice states, for a bill in another currency (ENG-42)
  spread_months: string; // a cost spread over months to Prepayments (FR-GL-03); blank for none
  spread_from: string; // the spread's first month, "2026-10"; blank for the bill's month
  order?: LineOrder; // the purchase order line it bills (FR-AP-01), kept as the order made it
}

let keys = 0;
export const newLine = (): Line => ({ key: `bl-${++keys}`, account_id: "", description: "", gst_code: "", net: "", gst: "", stated_gst: "", spread_months: "", spread_from: "" });

// lineKey is a fresh key for a line read from a saved bill.
export const lineKey = (): string => `bl-${++keys}`;

export type Errors = Record<string, string>;

// BillLines is the card of a bill's lines (FR-AP-02), shared by a bill, a credit note and a recurring bill: each line's
// account, description, GST code, net and GST as stated; with stated, the GST the tax invoice states in the base
// currency (ENG-42); with spreads, the spread over months (FR-GL-03). id is the form's, for each error's element.
export function BillLines({ id, lines, onChange, editable, errors, currency, base, stated, spreads, codes, choices, byId, subtitle, footer }: {
  id: string;
  lines: Line[];
  onChange: (lines: Line[]) => void;
  editable: boolean;
  errors: Errors;
  currency: string;
  base: string;
  stated: boolean;
  spreads: boolean;
  codes: GSTCode[];
  choices: Account[];
  byId: Map<string, Account>;
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
  const described = (k: string) => (errors[k] ? `${id}-${k}-error` : undefined);
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
            {l.order && (
              <p className="fy-small fy-muted">
                Bills {l.order.quantity} of line {l.order.line} of{" "}
                <Link to="/purchases/orders/$orderId" params={{ orderId: l.order.purchase_order_id }}>
                  its purchase order
                </Link>
                .
              </p>
            )}
            <div className="fy-form fy-form--2">
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${l.key}-account`}>
                  What it is for
                </label>
                <select
                  className="fy-select"
                  id={`${l.key}-account`}
                  value={l.account_id}
                  disabled={!editable}
                  onChange={(e) => setLine(l.key, { account_id: e.target.value, gst_code: l.gst_code || byId.get(e.target.value)?.default_gst_code || "" })}
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
                <label className="fy-label" htmlFor={`${l.key}-description`}>
                  Description
                </label>
                <input className="fy-input" id={`${l.key}-description`} value={l.description} maxLength={500} disabled={!editable} onChange={(e) => setLine(l.key, { description: e.target.value })} />
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
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${l.key}-net`}>
                  Amount before GST ({currency})
                </label>
                <input
                  className="fy-input fy-num"
                  id={`${l.key}-net`}
                  inputMode="decimal"
                  value={l.net}
                  disabled={!editable}
                  onChange={(e) => setLine(l.key, { net: e.target.value })}
                  aria-invalid={Boolean(errors[`${l.key}.net`]) || undefined}
                  aria-describedby={described(`${l.key}.net`)}
                />
                {err(`${l.key}.net`)}
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${l.key}-gst`}>
                  GST as stated ({currency})
                </label>
                <input
                  className="fy-input fy-num"
                  id={`${l.key}-gst`}
                  inputMode="decimal"
                  value={l.gst}
                  disabled={!editable}
                  onChange={(e) => setLine(l.key, { gst: e.target.value })}
                  aria-invalid={Boolean(errors[`${l.key}.gst`]) || undefined}
                  aria-describedby={described(`${l.key}.gst`)}
                />
                {err(`${l.key}.gst`)}
              </div>
              {stated && (
                <div className="fy-field">
                  <label className="fy-label" htmlFor={`${l.key}-stated`}>
                    GST stated in {base}
                  </label>
                  <input
                    className="fy-input fy-num"
                    id={`${l.key}-stated`}
                    inputMode="decimal"
                    value={l.stated_gst}
                    disabled={!editable}
                    onChange={(e) => setLine(l.key, { stated_gst: e.target.value })}
                    aria-invalid={Boolean(errors[`${l.key}.stated_gst`]) || undefined}
                    aria-describedby={errors[`${l.key}.stated_gst`] ? described(`${l.key}.stated_gst`) : `${l.key}-stated-help`}
                  />
                  {err(`${l.key}.stated_gst`) ?? (
                    <span className="fy-help" id={`${l.key}-stated-help`}>
                      The input tax claimed is the {base} GST the tax invoice states.
                    </span>
                  )}
                </div>
              )}
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
                      aria-describedby={errors[`${l.key}.spread_months`] ? described(`${l.key}.spread_months`) : `${l.key}-spread-help`}
                    />
                    {err(`${l.key}.spread_months`) ?? (
                      <span className="fy-help" id={`${l.key}-spread-help`}>
                        {helpText("bill.spread").tooltip}
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
                      aria-describedby={errors[`${l.key}.spread_from`] ? described(`${l.key}.spread_from`) : `${l.key}-spread-from-help`}
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
