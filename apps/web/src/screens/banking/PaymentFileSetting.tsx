import { useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { helpText } from "../../i18n";
import { problemOf, type ApiProblem } from "../../ui";
import type { MoneyAccount } from "./BankAccounts";
import { paymentFiles, type PaymentFile } from "./paymentFiles";

// PaymentFileSetting is a money account's Payment file (docs/design/screens AddBankAccount, BankAccounts; FR-BNK-12,
// rev 0.157; p1-payment-files task 1.3): the file its payment batches give when released. The server says which files the
// account may be set to and why not the others (`payment_files`); nothing is worked out here. A batch released before a
// change keeps its file. Someone who cannot change the account sees the file it gives.
export function PaymentFileSetting({ account, may }: { account: MoneyAccount; may: boolean }) {
  const id = useId();
  const h = helpText("bank-account.payment-file");
  const queries = useQueryClient();
  const [chosen, setChosen] = useState<PaymentFile>(account.payment_file);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState("");
  const editable = may && !account.closed_on;
  const save = async () => {
    setBusy(true);
    setProblem(null);
    setSaved("");
    const out = await api.PUT("/v1/money-accounts/{money_account_id}", {
      params: { path: { money_account_id: account.id }, header: { "If-Match": `"${account.version}"`, "Idempotency-Key": newKey() } },
      // The change is a replacement: everything else is sent as it is.
      body: {
        kind: account.kind,
        name: account.name,
        institution: account.institution,
        opened_on: account.opened_on,
        closed_on: account.closed_on,
        cardholder_id: account.cardholder?.id,
        payment_file: chosen,
      },
    });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    void queries.invalidateQueries({ queryKey: ["money-accounts"] });
    void queries.invalidateQueries({ queryKey: ["money-account", account.id] });
    setChosen(out.data.payment_file);
    setSaved(`Saved: its payment batches give ${paymentFiles[out.data.payment_file].words.toLowerCase()} from now on.`);
  };
  return (
    <div className="fy-stack fy-stack--3">
      <p className="fy-small fy-secondary">
        {h.tooltip} {editable ? "" : "Someone who changes money accounts sets it."}
      </p>
      {problem && <Refusal problem={problem} title="The payment file was not changed" />}
      <div className="fy-stack fy-stack--3" role="radiogroup" aria-labelledby={`${id}-label`}>
        <span className="fy-label" id={`${id}-label`}>
          {h.label}
        </span>
        {account.payment_files.map((f) => {
          const words = paymentFiles[f.format];
          const off = !f.offered && f.format !== account.payment_file;
          return (
            <button
              key={f.format}
              className="fy-choice"
              type="button"
              role="radio"
              aria-checked={chosen === f.format}
              aria-disabled={off || !editable || undefined}
              disabled={off || !editable}
              onClick={() => setChosen(f.format)}
            >
              <span className="fy-radio" />
              <span className="fy-stack fy-grow gap-1">
                <span className="fy-strong">{words.words}</span>
                <span className="fy-small fy-secondary">{words.how}</span>
                {!f.offered && f.reason && <span className="fy-small fy-muted">{f.reason}</span>}
              </span>
            </button>
          );
        })}
      </div>
      {editable && (
        <div className="fy-row gap-2">
          <button type="button" className="fy-btn fy-btn--primary" onClick={() => void save()} disabled={busy || chosen === account.payment_file}>
            Save the payment file
          </button>
          {saved && (
            <span className="fy-small" role="status">
              {saved}
            </span>
          )}
        </div>
      )}
    </div>
  );
}
