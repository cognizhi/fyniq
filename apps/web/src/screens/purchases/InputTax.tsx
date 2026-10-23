import { CircleCheck, CircleMinus, TriangleAlert } from "lucide-react";
import type { components } from "../../api/schema";
import { helpText } from "../../i18n";
import { Icon } from "../../ui";

type Facts = components["schemas"]["BillInputTax"];
export type Decision = components["schemas"]["BillTaxDecision"];

// Tax is a bill's input-tax facts as the form keeps them (FR-AP-20; PRD-001 rev 0.89).
export interface Tax {
  registered: boolean; // the supplier is GST-registered: its GST number is on the document
  document: string; // tax_invoice, simplified, import_permit or none
  addressed_to: string; // company, or agent: an employee or director who bought it for the company
  repaid: string; // for an agent: paid, or director_loan
  tax_agent: string; // the tax agent's decision on a line the rules leave to them; blank while none
}

export const usualTax = (): Tax => ({ registered: true, document: "tax_invoice", addressed_to: "company", repaid: "paid", tax_agent: "" });

// taxOf is a saved bill's facts as the form shows them.
export function taxOf(f: Facts | undefined): Tax {
  return {
    registered: f?.supplier_registered ?? true,
    document: f?.document ?? "tax_invoice",
    addressed_to: f?.addressed_to ?? "company",
    repaid: f?.repaid ?? "paid",
    tax_agent: f?.tax_agent ?? "",
  };
}

// factsOf are the facts to send: only those that differ from the usual, none for the usual bill.
export function factsOf(t: Tax): Facts | undefined {
  const out: Facts = {};
  if (!t.registered) out.supplier_registered = false;
  if (t.document !== "tax_invoice") out.document = t.document as Facts["document"];
  if (t.addressed_to === "agent") {
    out.addressed_to = "agent";
    if (t.repaid !== "paid") out.repaid = t.repaid as Facts["repaid"];
  }
  if (t.tax_agent) out.tax_agent = t.tax_agent as Facts["tax_agent"];
  return Object.keys(out).length > 0 ? out : undefined;
}

const documents: [string, string][] = [
  ["tax_invoice", "Tax invoice"],
  ["simplified", "Simplified tax invoice"],
  ["import_permit", "Import permit"],
  ["none", "No valid tax invoice"],
];

// DecisionChip is how a line's input tax was decided: an icon and a label, never colour alone.
export function DecisionChip({ d }: { d: Decision }) {
  switch (d.condition) {
    case "claimable":
      return (
        <span className="fy-chip fy-chip--good">
          <Icon of={CircleCheck} />
          {d.business_share ? `Claimed for ${d.business_share}%` : "Claimable"}
        </span>
      );
    case "tax_agent":
      return (
        <span className="fy-chip fy-chip--warning">
          <Icon of={TriangleAlert} />
          For the tax agent
        </span>
      );
    default:
      return (
        <span className="fy-chip fy-chip--neutral">
          <Icon of={CircleMinus} />
          Not claimable
        </span>
      );
  }
}

// InputTaxCard is what a bill's input tax rests on (FR-AP-20): the document, the supplier's registration, whom it is
// addressed to and how an agent was paid back, the tax agent's decision where the rules leave one to them, and how each
// line was decided as the server last checked it. The decisions are safety information: shown in both modes (§7.8).
export function InputTaxCard({ id, tax, onChange, editable, decisions, dirty }: {
  id: string;
  tax: Tax;
  onChange: (t: Tax) => void;
  editable: boolean;
  decisions: Decision[];
  dirty: boolean;
}) {
  const set = (patch: Partial<Tax>) => onChange({ ...tax, ...patch });
  const shown = decisions.filter((d) => d.reason !== "no_gst");
  const forAgent = decisions.some((d) => d.condition === "tax_agent" || d.reason === "tax_agent_decision") || tax.tax_agent !== "";
  const h = helpText("bill.input-tax");
  return (
    <section className="fy-card" aria-labelledby={`${id}-tax`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-tax`}>
          {h.label}
        </h2>
      </div>
      <div className="fy-card__body fy-stack fy-stack--4">
        <p className="fy-small fy-muted whitespace-normal">{h.tooltip}</p>
        <div className="fy-form fy-form--2">
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-document`}>
              What the claim rests on
            </label>
            <select className="fy-select" id={`${id}-document`} value={tax.document} disabled={!editable} onChange={(e) => set({ document: e.target.value })} aria-describedby={`${id}-document-help`}>
              {documents.map(([v, label]) => (
                <option key={v} value={v}>
                  {label}
                </option>
              ))}
            </select>
            <span className="fy-help" id={`${id}-document-help`}>
              A tax invoice or an import permit names the company. A simplified tax invoice needs no customer name, and is claimable while the bill&apos;s total, GST included, is within the limit in the GST pack.
            </span>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-addressed`}>
              Addressed to
            </label>
            <select
              className="fy-select"
              id={`${id}-addressed`}
              value={tax.addressed_to}
              disabled={!editable}
              onChange={(e) => set({ addressed_to: e.target.value })}
              aria-describedby={`${id}-addressed-help`}
            >
              <option value="company">The company</option>
              <option value="agent" disabled={tax.document === "import_permit"}>
                An employee or director
              </option>
            </select>
            <span className="fy-help" id={`${id}-addressed-help`}>
              &quot;Company c/o another&quot; counts as the company. An employee or director bought it for the company.
            </span>
          </div>
          <div className="fy-field fy-field--full">
            <label className="fy-row gap-2 fy-label">
              <input type="checkbox" checked={tax.registered} disabled={!editable} onChange={(e) => set({ registered: e.target.checked })} />
              The supplier is GST-registered: its GST number is on the document
            </label>
          </div>
          {tax.addressed_to === "agent" && (
            <div className="fy-field fy-field--full">
              <label className="fy-label" htmlFor={`${id}-repaid`}>
                How they were paid back
              </label>
              <select className="fy-select" id={`${id}-repaid`} value={tax.repaid} disabled={!editable} onChange={(e) => set({ repaid: e.target.value })} aria-describedby={`${id}-repaid-help`}>
                <option value="paid">Paid back</option>
                <option value="director_loan">Credited to their director&apos;s loan</option>
              </select>
              <span className="fy-help" id={`${id}-repaid-help`}>
                Bill what the company paid back, and the GST inside that amount; attach the invoice and the record of the repayment.
              </span>
            </div>
          )}
          {forAgent && (
            <div className="fy-field fy-field--full">
              <label className="fy-label" htmlFor={`${id}-agent`}>
                The tax agent&apos;s decision
              </label>
              <select className="fy-select" id={`${id}-agent`} value={tax.tax_agent} disabled={!editable} onChange={(e) => set({ tax_agent: e.target.value })} aria-describedby={`${id}-agent-help`}>
                <option value="">Not decided yet: not claimed</option>
                <option value="claimable">Claimable</option>
                <option value="not_claimable">Not claimable</option>
              </select>
              <span className="fy-help" id={`${id}-agent-help`}>
                {helpText("bill.tax-agent").tooltip}
              </span>
            </div>
          )}
        </div>
        {shown.length > 0 && (
          <div className="fy-stack fy-stack--2">
            <h3 className="fy-label">{dirty ? "As last saved" : "How each line's GST is treated"}</h3>
            <ul className="fy-list">
              {shown.map((d) => (
                <li key={d.line} className="fy-list__item">
                  <span className="fy-list__main fy-stack fy-stack--1">
                    <span className="fy-list__title whitespace-normal">
                      Line {d.line}: {d.text}
                    </span>
                    <span>
                      <DecisionChip d={d} />
                    </span>
                  </span>
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>
    </section>
  );
}
