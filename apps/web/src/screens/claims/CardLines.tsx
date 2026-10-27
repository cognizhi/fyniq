import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CreditCard, Paperclip } from "lucide-react";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { EvidencePanel } from "../accounting/JournalEvidence";
import { money, useCategories, useClaimGSTCodes, type CardLine } from "./claims";

// CardLines are the lines of the company cards the member holds (FR-AP-18, design D9): each waiting line is completed
// with the expense policy's category it was for, or marked personal spend, which the member owes and refunds; then its
// receipt is attached to what completing it made.
export function CardLines() {
  const q = useQuery({
    queryKey: ["card-lines", "all"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/card-lines", { params: { query: { page_size: 100 } } });
      if (error) throw error;
      return data.items;
    },
  });
  const [completing, setCompleting] = useState<CardLine | null>(null);
  const [receipt, setReceipt] = useState<CardLine | null>(null);
  const [said, setSaid] = useState("");
  const items = q.data ?? [];
  const complete = helpText("card-line.complete");
  const attach = helpText("card-line.attach-evidence");

  return (
    <section className="fy-stack fy-stack--3" aria-label="Card lines">
      {said && (
        <p className="fy-small" role="status">
          {said}
        </p>
      )}
      {q.isError && <Refusal problem={problemOf(q.error)} title="Your card lines could not be listed" />}
      {q.data && items.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={CreditCard} size={24} />
          <h2 className="fy-h3">No card lines</h2>
          <p className="fy-secondary">When you hold a company card, what you pay with it shows here once finance imports the card's statement. You add each one's receipt and what it was for.</p>
        </div>
      )}
      {items.length > 0 && (
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--cards" aria-label="Your card lines">
            <thead>
              <tr>
                <th scope="col">Paid to</th>
                <th scope="col">Date</th>
                <th scope="col" className="fy-num">
                  Amount
                </th>
                <th scope="col">Status</th>
                <th scope="col">
                  <span className="sr-only">Action</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {items.map((l) => (
                <tr key={l.id}>
                  <td data-card="title">
                    {l.counterparty || l.description || "Card payment"}
                    <span className="fy-small fy-muted block">{l.card}</span>
                  </td>
                  <td data-card="meta">{date(l.booking_date)}</td>
                  <td data-card="amount" className="fy-num">
                    {money(l.amount)}
                  </td>
                  <td data-card="status">
                    <span className="fy-chip fy-chip--neutral">{l.completed === "personal" ? "Personal spend" : l.completed === "spend" ? "Completed" : waiting(l) ? "Needs a receipt" : "Done by finance"}</span>
                  </td>
                  <td>
                    {waiting(l) ? (
                      <Button size="sm" onClick={() => setCompleting(l)} title={complete.tooltip} aria-label={`${complete.label}: ${l.counterparty || l.description || "card payment"}`}>
                        {complete.label}
                      </Button>
                    ) : l.document_id ? (
                      <Button size="sm" variant="ghost" onClick={() => setReceipt(l)} title={attach.tooltip} aria-label={`Receipt for ${l.counterparty || l.description || "card payment"}`}>
                        <Icon of={Paperclip} />
                        Receipt
                      </Button>
                    ) : null}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {completing && (
        <CompleteDialog
          line={completing}
          onClose={() => setCompleting(null)}
          onDone={(line, message) => {
            setCompleting(null);
            setSaid(message);
            setReceipt(line);
          }}
        />
      )}
      {receipt && (
        <Dialog open onOpenChange={(o) => !o && setReceipt(null)} title="The receipt" eyebrow={receipt.counterparty || receipt.description || "Card payment"} wide>
          <EvidencePanel record={{ kind: "card-line", id: receipt.id, etag: `"${receipt.version}"` }} draft={false} mayAttach onChanged={setSaid} />
        </Dialog>
      )}
    </section>
  );
}

function waiting(l: CardLine): boolean {
  return l.status === "unreconciled" || l.status === "possible_duplicate";
}

// CompleteDialog completes one card line: spent for the company, with the category and GST code of its receipt; or
// personal spend.
function CompleteDialog({ line, onClose, onDone }: { line: CardLine; onClose: () => void; onDone: (line: CardLine, message: string) => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const categories = useCategories();
  const codes = useClaimGSTCodes(line.booking_date);
  const [personal, setPersonal] = useState(false);
  const [category, setCategory] = useState("");
  const [gst, setGST] = useState("");
  const [description, setDescription] = useState(line.counterparty ?? "");
  const [missing, setMissing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);
  const usable = (categories.data ?? []).filter((c) => c.active && !c.mileage);

  const send = async () => {
    if (!personal && !category) return setMissing(true);
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/card-lines/{line_id}/complete", {
        params: { path: { line_id: line.id }, header: { "Idempotency-Key": key, "If-Match": `"${line.version}"` } },
        body: personal ? { personal: true, description: description.trim() || undefined } : { category_id: category, gst_code: gst || undefined, description: description.trim() || undefined },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["card-lines"] });
    await queries.invalidateQueries({ queryKey: ["cash-advances"] });
    onDone(out.data, personal ? "Marked as personal spend: you owe it until you refund it. Attach its receipt now." : "Completed. Attach its receipt now.");
  };

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title="What was it for?"
      eyebrow={`${line.counterparty || line.description || "Card payment"} · ${money(line.amount)} · ${date(line.booking_date)}`}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={() => void send()} disabled={busy}>
            {busy ? "Completing…" : helpText("card-line.complete").label}
          </Button>
        </>
      }
    >
      <div className="fy-stack fy-stack--3">
        {problem && <Refusal problem={problem} title="The card line was not completed" />}
        <fieldset className="fy-stack fy-stack--2">
          <legend className="fy-label">Paid for</legend>
          <label className="fy-row">
            <input type="radio" name={`${id}-for`} checked={!personal} onChange={() => setPersonal(false)} />
            The company
          </label>
          <label className="fy-row">
            <input type="radio" name={`${id}-for`} checked={personal} onChange={() => setPersonal(true)} />
            Myself: personal spend I refund
          </label>
        </fieldset>
        {!personal && (
          <>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-category`}>
                Category
              </label>
              <select
                className="fy-select"
                id={`${id}-category`}
                value={category}
                onChange={(e) => {
                  setCategory(e.target.value);
                  setMissing(false);
                }}
                aria-invalid={missing || undefined}
                aria-describedby={missing ? `${id}-category-error` : undefined}
              >
                <option value="">Choose what it was for</option>
                {usable.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
              </select>
              {missing && (
                <span className="fy-error" id={`${id}-category-error`}>
                  Choose the category it was for, or mark it personal spend.
                </span>
              )}
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-gst`}>
                GST on the receipt
              </label>
              <select className="fy-select" id={`${id}-gst`} value={gst} onChange={(e) => setGST(e.target.value)}>
                <option value="">The category's usual code</option>
                {(codes.data?.codes ?? []).map((c) => (
                  <option key={c.code} value={c.code}>
                    {c.code}: {c.description}
                  </option>
                ))}
              </select>
            </div>
          </>
        )}
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-description`}>
            What it was for
          </label>
          <input className="fy-input" id={`${id}-description`} value={description} maxLength={140} onChange={(e) => setDescription(e.target.value)} />
        </div>
        {personal && <p className="fy-small fy-secondary">It becomes an amount you owe the company, shown under Cash advances, until you refund it. No claim settles it.</p>}
      </div>
    </Dialog>
  );
}
