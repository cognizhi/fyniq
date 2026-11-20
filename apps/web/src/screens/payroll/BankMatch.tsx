import { useQuery } from "@tanstack/react-query";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { problemOf, type ApiProblem } from "../../ui";

type Batch = components["schemas"]["SalaryBatch"];

// minor reads a decimal string as integer minor units (two places: the batch is in SGD, ENG-10), so a sum is exact.
function minor(value: string): bigint {
  const m = /^(-?)(\d+)(?:\.(\d+))?$/.exec(value.trim());
  if (!m) return 0n;
  const cents = BigInt(`${m[2]}${(m[3] ?? "").padEnd(2, "0").slice(0, 2)}`);
  return m[1] === "-" ? -cents : cents;
}

function plain(v: bigint): string {
  const n = v < 0n ? -v : v;
  return `${v < 0n ? "-" : ""}${n / 100n}.${String(n % 100n).padStart(2, "0")}`;
}

// BankMatch pays released lines from the bank feed (FR-PPL-12): pick the debit of the batch's own account waiting in the Categorise queue, tick the lines it paid, and the amounts must agree to the cent.
export function BankMatch({ batch, etag, onClose, onDone }: { batch: Batch; etag: string; onClose: () => void; onDone: (what: string) => void }) {
  const id = useId();
  const h = helpText("salary.bank-match");
  const confirm = useConfirm();
  const [lineId, setLineId] = useState("");
  const [ticked, setTicked] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const released = (batch.lines ?? []).filter((l) => l.state === "released");
  const debits = useQuery({
    queryKey: ["salary-bank-debits", batch.id],
    queryFn: async () => {
      const r = await api.GET("/v1/bank-lines", { params: { query: { money_account_id: batch.money_account_id, status: "waiting", page_size: 200 } } });
      if (r.error) throw r.error;
      return r.data.items.filter((x) => x.amount.amount.startsWith("-") && x.amount.currency === "SGD");
    },
  });
  const chosen = (debits.data ?? []).find((x) => x.id === lineId);
  const sum = released.filter((l) => ticked.includes(l.line_id)).reduce((t, l) => t + minor(l.amount.amount), 0n);
  const agrees = !!chosen && ticked.length > 0 && -minor(chosen.amount.amount) === sum;
  const toggle = (line: string) => setTicked(ticked.includes(line) ? ticked.filter((x) => x !== line) : [...ticked, line]);
  const run = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/salary-batches/{batch_id}/bank-match", {
        params: { path: { batch_id: batch.id }, header: { "If-Match": etag, "Idempotency-Key": newKey() } },
        body: { bank_line_id: lineId, line_ids: ticked },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone(`Matched the bank debit and marked ${ticked.length} line(s) paid.`);
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} dismissible={!busy} eyebrow={batch.number} title={h.label} description={h.tooltip}>
      {problem && <Refusal problem={problem} title="Nothing was matched" />}
      <form className="fy-stack fy-stack--3" onSubmit={(e) => void run(e)} noValidate aria-label="Match a bank debit">
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-debit`}>
            The bank debit
          </label>
          <select className="fy-select" id={`${id}-debit`} value={lineId} onChange={(e) => setLineId(e.target.value)}>
            <option value="">{debits.isLoading ? "Loading the queue" : "Choose a debit waiting in the Categorise queue"}</option>
            {(debits.data ?? []).map((x) => (
              <option key={x.id} value={x.id}>
                {date(x.booking_date)}, {formatAmount(x.amount.amount)} {x.amount.currency}
                {x.description ? `, ${x.description}` : ""}
              </option>
            ))}
          </select>
          {debits.data && debits.data.length === 0 && <span className="fy-hint">No debit of this bank account waits in the queue. Import the statement first.</span>}
        </div>
        <fieldset className="fy-field">
          <legend className="fy-label">The lines it paid</legend>
          {released.length === 0 && <span className="fy-hint">No line is released and unpaid.</span>}
          {released.map((l) => (
            <label key={l.line_id} className="fy-row gap-2">
              <input type="checkbox" checked={ticked.includes(l.line_id)} onChange={() => toggle(l.line_id)} /> {l.person_name ?? l.person_id}, {formatAmount(l.amount.amount)} SGD
            </label>
          ))}
        </fieldset>
        <p className={agrees ? "fy-secondary" : "fy-hint"} role="status">
          Lines ticked: {formatAmount(plain(sum))} SGD
          {chosen ? (agrees ? ". It matches the debit to the cent." : `. The debit is ${formatAmount(chosen.amount.amount.replace("-", ""))} SGD: the lines must make it exactly.`) : "."}
        </p>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Not now
          </Button>
          <Button variant="primary" type="submit" disabled={busy || !agrees}>
            {h.label}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
