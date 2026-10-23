import { CircleCheck, CirclePause, Gavel, TriangleAlert } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { who } from "../accounting/Journals";

type Bill = components["schemas"]["Bill"];
type Contact = components["schemas"]["Contact"];
type Decision = components["schemas"]["WithholdingDecision"];
type Draft = components["schemas"]["WithholdingDecisionDraft"];

// The natures of a payment to a non-resident supplier (FR-AP-19), in plain words.
export const natures: [string, string][] = [
  ["software_use", "Software for the company's own use, such as a subscription"],
  ["services_outside_singapore", "Services performed wholly outside Singapore"],
  ["services_in_singapore", "Services performed in Singapore"],
  ["software_rights", "Software rights or royalties"],
  ["goods", "Goods"],
  ["interest", "Interest"],
  ["movable_rent", "Rent of movable property"],
  ["other", "Other"],
];

export const natureWords = (n: string | undefined) => natures.find(([v]) => v === n)?.[1] ?? "";

// residenceOf is where a contact is resident for tax: its tax residence, else its country; "" when neither is recorded.
export const residenceOf = (c: Contact | undefined) => (c ? c.tax_residence || c.country || "" : "");

// nonResident reports whether a contact is recorded as resident for tax outside Singapore (FR-AP-19).
export const nonResident = (c: Contact | undefined) => {
  const r = residenceOf(c);
  return r !== "" && r !== "SG";
};

function decisionText(d: Decision): string {
  switch (d.outcome) {
    case "withhold":
      return `Withhold ${d.rate_percent}% of the payment before GST`;
    case "treaty":
      return `Withhold ${d.rate_percent}% under a tax treaty, with a certificate of residence`;
    default:
      return "Nothing is withheld";
  }
}

// RecordDecision records the tax agent's decision for the bill's supplier and nature (FR-AP-19), naming the tax agent;
// their advice is attached to the bill as evidence.
function RecordDecision({ open, bill, onClose, onDone }: { open: boolean; bill: Bill; onClose: () => void; onDone: (message: string) => void }) {
  const id = useId();
  const [outcome, setOutcome] = useState<Draft["outcome"]>("not_applicable");
  const [rate, setRate] = useState("");
  const [agent, setAgent] = useState("");
  const [note, setNote] = useState("");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const withholds = outcome !== "not_applicable";
  const run = async (e: FormEvent) => {
    e.preventDefault();
    const errs: Record<string, string> = {};
    const r = rate.trim().replace(/%$/, "").trim();
    if (withholds && !/^(100(\.0{1,2})?|\d{1,2}(\.\d{1,2})?)$/.test(r)) errs.rate = "Enter the rate the tax agent advised, above 0 and up to 100, such as 15.";
    if (withholds && /^0+(\.0+)?$/.test(r)) errs.rate = "Enter the rate the tax agent advised, above 0 and up to 100, such as 15.";
    if (!agent.trim()) errs.agent = "Name the tax agent who decided.";
    setErrors(errs);
    if (Object.keys(errs).length > 0) return;
    setBusy(true);
    setProblem(null);
    const body: Draft = { nature_of_payment: bill.nature_of_payment as Draft["nature_of_payment"], outcome, tax_agent: agent.trim(), note: note.trim() || undefined, bill_id: bill.id };
    if (withholds) body.rate_percent = r;
    // The supplier as read now: the decision is recorded for the tax residence it has (If-Match).
    const supplier = await api.GET("/v1/contacts/{contact_id}", { params: { path: { contact_id: bill.supplier?.id ?? "" } } });
    if (!supplier.data) {
      setBusy(false);
      return setProblem(problemOf(supplier.error));
    }
    const out = await api.POST("/v1/contacts/{contact_id}/withholding-decisions", {
      params: { path: { contact_id: supplier.data.id }, header: { "If-Match": supplier.response.headers.get("ETag") ?? "", "Idempotency-Key": newKey() } },
      body,
    });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone(
      withholds && bill.status === "posted"
        ? "Recorded. The bill posted before it: edit the bill so it withholds, then record its payment. Attach the tax agent's advice."
        : "Recorded. Attach the tax agent's advice to the bill.",
    );
  };
  const h = helpText("bill.record-decision");
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      eyebrow={bill.supplier?.name}
      title="Record the tax agent's decision"
      description={`For ${natureWords(bill.nature_of_payment).toLowerCase() || "this nature of payment"}. It applies to the supplier's bills for it from now on; bills posted before keep theirs.`}
    >
      {problem && <Refusal problem={problem} title="The decision was not recorded" />}
      <form className="fy-stack fy-stack--3" onSubmit={run} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-outcome`}>
            Decision
          </label>
          <select className="fy-select" id={`${id}-outcome`} value={outcome} onChange={(e) => setOutcome(e.target.value as Draft["outcome"])}>
            <option value="not_applicable">Not applicable: nothing is withheld</option>
            <option value="withhold">Withhold tax</option>
            <option value="treaty">Withhold at a reduced treaty rate</option>
          </select>
        </div>
        {withholds && (
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-rate`}>
              Rate (%)
            </label>
            <input
              className="fy-input fy-num"
              id={`${id}-rate`}
              inputMode="decimal"
              value={rate}
              onChange={(e) => setRate(e.target.value)}
              aria-invalid={Boolean(errors.rate) || undefined}
              aria-describedby={errors.rate ? `${id}-rate-error` : `${id}-rate-help`}
            />
            {errors.rate ? (
              <span className="fy-error" id={`${id}-rate-error`}>
                {errors.rate}
              </span>
            ) : (
              <span className="fy-help" id={`${id}-rate-help`}>
                As the tax agent advised, of the payment before GST.{outcome === "treaty" ? " Attach the certificate of residence." : ""}
              </span>
            )}
          </div>
        )}
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-agent`}>
            Tax agent
          </label>
          <input
            className="fy-input"
            id={`${id}-agent`}
            value={agent}
            maxLength={200}
            onChange={(e) => setAgent(e.target.value)}
            aria-invalid={Boolean(errors.agent) || undefined}
            aria-describedby={errors.agent ? `${id}-agent-error` : undefined}
          />
          {errors.agent && (
            <span className="fy-error" id={`${id}-agent-error`}>
              {errors.agent}
            </span>
          )}
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-note`}>
            Why (optional)
          </label>
          <textarea className="fy-textarea" id={`${id}-note`} value={note} maxLength={2000} onChange={(e) => setNote(e.target.value)} />
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy} title={h.tooltip}>
            <Icon of={Gavel} />
            {h.label}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// WithholdingCard is how withholding tax stands for a bill of a supplier resident outside Singapore (FR-AP-19): the
// nature of the payment, the tax agent's decision it posts with, the tax withheld, and why a payment waits. Safety
// information: shown in both modes (§7.8).
export function WithholdingCard({ bill, base, mayRecord, onDone }: { bill: Bill; base: string; mayRecord: boolean; onDone: (message: string) => void }) {
  const id = useId();
  const [recording, setRecording] = useState(false);
  const w = bill.withholding;
  if (!w) return null;
  const d = w.decision;
  const h = helpText("bill.withholding");
  return (
    <section className="fy-card" aria-labelledby={id}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={id}>
          {h.label}
        </h2>
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        <p className="fy-small fy-muted whitespace-normal">
          {bill.supplier?.name ?? "The supplier"} is resident for tax in {w.residence}. {h.tooltip}
        </p>
        <dl className="fy-dl">
          <dt>Nature of payment</dt>
          <dd>{natureWords(w.nature_of_payment) || "Not recorded yet"}</dd>
          <dt>The tax agent&apos;s decision</dt>
          <dd>
            {d ? (
              <span className="fy-stack fy-stack--1">
                <span>
                  <span className="fy-chip fy-chip--good">
                    <Icon of={CircleCheck} />
                    {decisionText(d)}
                  </span>
                </span>
                <span className="fy-small fy-muted">
                  By {d.tax_agent}, recorded {date(d.recorded_at.slice(0, 10))}
                  {d.recorded_by ? ` by ${who(d.recorded_by)}` : ""}
                  {d.note ? `: ${d.note}` : ""}
                </span>
              </span>
            ) : (
              <span className="fy-chip fy-chip--warning">
                <Icon of={TriangleAlert} />
                Not decided yet
              </span>
            )}
          </dd>
          {w.tax && (
            <>
              <dt>Tax withheld</dt>
              <dd className="fy-num">
                {base} {formatAmount(w.tax.amount)}
              </dd>
            </>
          )}
        </dl>
        {w.held && (
          <div className="fy-banner fy-banner--warning" role="note">
            <Icon of={CirclePause} size={20} />
            <div className="fy-banner__text">
              <span className="fy-strong">Its payment waits</span>
              <span className="fy-small">{w.held}: record the tax agent&apos;s decision, then edit the bill so it posts with it.</span>
            </div>
          </div>
        )}
        {mayRecord && bill.status !== "reversed" && (
          <div>
            <Button type="button" onClick={() => setRecording(true)} disabled={!bill.nature_of_payment} title={helpText("bill.record-decision").tooltip}>
              <Icon of={Gavel} />
              {d ? "Record a new decision…" : "Record the tax agent's decision…"}
            </Button>
            {!bill.nature_of_payment && <p className="fy-small fy-muted">Save the bill with its nature of payment first.</p>}
          </div>
        )}
      </div>
      {recording && (
        <RecordDecision
          open={recording}
          bill={bill}
          onClose={() => setRecording(false)}
          onDone={(message) => {
            setRecording(false);
            onDone(message);
          }}
        />
      )}
    </section>
  );
}
