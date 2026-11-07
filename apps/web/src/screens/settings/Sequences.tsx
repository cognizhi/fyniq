import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Pencil } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Sequence = components["schemas"]["Sequence"];

// documentNames are the numbered document types, as the app names them.
export const documentNames: Record<Sequence["document_type"], string> = {
  journal: "Journals",
  bill: "Bills",
  bill_payment: "Bill payments",
  vendor_credit: "Vendor credits",
  supplier_refund: "Refunds from suppliers",
  purchase_order: "Purchase orders",
  invoice: "Invoices",
  credit_note: "Credit notes",
  customer_receipt: "Receipts",
  write_off: "Write-offs",
  customer_refund: "Refunds to customers",
  customer_deposit: "Customer deposits",
  quote: "Quotes",
  sales_order: "Sales orders",
  statement: "Statements",
  expense_claim: "Expense claims",
  claim_reimbursement: "Claim reimbursements",
  cash_advance: "Cash advances",
  fixed_asset: "Fixed assets",
  asset_disposal: "Asset disposals",
  payment_batch: "Payment batches",
  pre_incorporation_cost: "Pre-incorporation costs",
  director_fee: "Director fees",
  director_settlement: "Director settlements",
  director_repayment: "Director repayments",
  equity_transaction: "Equity transactions",
};

async function listSequences() {
  const { data, error } = await api.GET("/v1/organisation/sequences", { params: { query: { page_size: 100 } } });
  if (error) throw error;
  return data.items ?? [];
}

// formatted is a number as the sequence prints it: the prefix, then the number padded to its digits.
function formatted(prefix: string, next: string, digits: string): string {
  const n = /^\d+$/.test(next) ? next : "";
  const d = Number(digits);
  return n ? prefix + (Number.isInteger(d) && d > n.length ? n.padStart(d, "0") : n) : "";
}

type Draft = { prefix: string; next_number: string; digits: string; gap_free: boolean };

const draftOf = (s: Sequence): Draft => ({ prefix: s.prefix, next_number: String(s.next_number), digits: String(s.digits), gap_free: s.gap_free });

// ChangeSequence changes a document type's numbering: only the fields changed are sent, over its version ("0" while
// it has its defaults). The next number only moves forward, so no number is given twice.
function ChangeSequence({ sequence, onClose, onDone }: { sequence: Sequence; onClose: () => void; onDone: (what: string) => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const id = useId();
  const [d, setDraft] = useState<Draft>(draftOf(sequence));
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const set = (k: keyof Draft, v: string | boolean) => setDraft({ ...d, [k]: v });
  const name = documentNames[sequence.document_type];
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const was = draftOf(sequence);
    const changes: components["schemas"]["SequenceChanges"] = {};
    if (d.prefix.trim() !== was.prefix) changes.prefix = d.prefix.trim();
    if (d.next_number.trim() !== was.next_number) changes.next_number = Number(d.next_number.trim());
    if (d.digits.trim() !== was.digits) changes.digits = Number(d.digits.trim());
    if (d.gap_free !== was.gap_free) changes.gap_free = d.gap_free;
    if (Object.keys(changes).length === 0) {
      onClose();
      return;
    }
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.PATCH("/v1/organisation/sequences/{document_type}", {
        params: { path: { document_type: sequence.document_type }, header: { "If-Match": `"${sequence.version}"` } },
        body: changes,
      }),
    );
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    await queries.invalidateQueries({ queryKey: ["sequences"] });
    onDone(`${name} are numbered from ${out.data.example}.`);
    onClose();
  };
  const example = formatted(d.prefix.trim(), d.next_number.trim(), d.digits.trim());
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && onClose()} dismissible={!busy} eyebrow="Document numbering" title={`Number the ${name.toLowerCase()}`}
      description="Numbers already given never change. The next number moves forward only, so no number is given twice.">
      {problem && <Refusal problem={problem} title="The numbering did not change" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <div className="fy-form fy-form--2">
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-prefix`}>
              Prefix
            </label>
            <input className="fy-input" id={`${id}-prefix`} value={d.prefix} maxLength={12} onChange={(e) => set("prefix", e.target.value.toUpperCase())}
              aria-describedby={`${id}-prefix-help`} autoComplete="off" />
            <span className="fy-help" id={`${id}-prefix-help`}>
              Up to 12 capital letters, digits, slashes or hyphens.
            </span>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-next`}>
              Next number
            </label>
            <input className="fy-input fy-num" id={`${id}-next`} inputMode="numeric" value={d.next_number} onChange={(e) => set("next_number", e.target.value)}
              aria-describedby={`${id}-next-help`} />
            <span className="fy-help" id={`${id}-next-help`}>
              {sequence.next_number} or more.
            </span>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-digits`}>
              Digits
            </label>
            <input className="fy-input fy-num" id={`${id}-digits`} inputMode="numeric" value={d.digits} onChange={(e) => set("digits", e.target.value)}
              aria-describedby={`${id}-digits-help`} />
            <span className="fy-help" id={`${id}-digits-help`}>
              Padded with zeros to this many, from 1 to 12.
            </span>
          </div>
          <fieldset className="fy-field">
            <legend className="fy-label">When a document takes its number</legend>
            <label className="fy-row gap-2">
              <input type="radio" name={`${id}-gap`} checked={d.gap_free} onChange={() => set("gap_free", true)} />
              When it posts: no gaps
            </label>
            <label className="fy-row gap-2">
              <input type="radio" name={`${id}-gap`} checked={!d.gap_free} onChange={() => set("gap_free", false)} />
              When it is saved: an abandoned draft leaves a gap
            </label>
          </fieldset>
        </div>
        <p className="fy-small fy-secondary" role="status">
          {example ? (
            <>
              The next one is <span className="fy-num fy-strong">{example}</span>.
            </>
          ) : (
            "Enter the next number as digits."
          )}
        </p>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy}>
            Save the numbering
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// Sequences is the document numbering section of Settings › Organisation (FR-ORG-04): each numbered document type with
// the number its next document takes and whether it is gap-free; those who may change the profile change them.
export function Sequences({ mayChange, onDone }: { mayChange: boolean; onDone: (what: string) => void }) {
  const list = useQuery({ queryKey: ["sequences"], queryFn: listSequences });
  const [editing, setEditing] = useState<Sequence | null>(null);
  const change = helpText("sequence.change");
  return (
    <section className="fy-card" aria-labelledby="numbering-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="numbering-title">
          Document numbering
        </h2>
      </div>
      {list.isError && <Refusal problem={problemOf(list.error)} title="The numbering could not be shown" />}
      {list.data && (
        <ul className="fy-list" aria-labelledby="numbering-title">
          {list.data.map((s) => (
            <li key={s.document_type} className="fy-list__item">
              <span className="fy-list__main">
                <span className="fy-list__title">{documentNames[s.document_type]}</span>
                <span className="fy-list__meta whitespace-normal">
                  Next: <span className="fy-num">{s.example}</span> · {s.gap_free ? "numbered when posted, no gaps" : "numbered when saved"}
                </span>
              </span>
              {mayChange && (
                <Button size="sm" onClick={() => setEditing(s)} aria-label={`${change.label}: ${documentNames[s.document_type]}`} title={change.tooltip}>
                  <Icon of={Pencil} />
                  <span className="fy-from-medium">{change.label}</span>
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      {editing && <ChangeSequence key={editing.document_type} sequence={editing} onClose={() => setEditing(null)} onDone={onDone} />}
    </section>
  );
}
