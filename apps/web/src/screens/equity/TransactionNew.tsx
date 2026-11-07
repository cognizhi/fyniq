import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { problemOf, type ApiProblem } from "../../ui";
import { listAccounts } from "../accounting/data";
import { EquityTabs } from "./Holders";
import { blankDraft, bodyOf, previewOf, TransactionFields, type TransactionDraft } from "./TransactionForm";

// TransactionNew is Equity › Transactions › New (docs/design/screens EquityTransaction; FR-EQ-17, FR-DOC-06): the type,
// the holders, the class, the shares and the price or the reason, with what it will post. Saved as a draft, it takes
// its evidence and asks for approval on its own page. ?reverses=<id> starts the reversal of an effective transaction.
export function TransactionNew() {
  const search = useSearch({ strict: false }) as { reverses?: string };
  const navigate = useNavigate();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const [form, setForm] = useState<TransactionDraft>(search.reverses ? { kind: "reversal", reverses_id: search.reverses } : blankDraft);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);
  const chart = useQuery({ queryKey: ["accounts", "all"], queryFn: listAccounts });
  const codeOf = (id?: string) => (chart.data ?? []).find((a) => a.id === id)?.code ?? "";
  const draftH = helpText("equity-transaction.draft");
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () => api.POST("/v1/equity-transactions", { params: { header: { "Idempotency-Key": key } }, body: bodyOf(form) }));
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["equity-transactions"] });
    await navigate({ to: "/equity/transactions/$transactionId", params: { transactionId: out.data.id } });
  };
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/equity/transactions">Transactions</Link>
            <span aria-hidden="true">›</span>
            <span>New</span>
          </nav>
          <h1 className="fy-h1" title={draftH.tooltip}>
            New equity transaction
          </h1>
          <p className="fy-secondary">Draft it, attach what it rests on, then ask another person to approve it. Nothing moves until ACRA's register shows it.</p>
        </div>
      </div>
      <EquityTabs current="transactions" />
      <form className="fy-card fy-card--pad fy-stack fy-stack--4" onSubmit={submit} aria-label="New equity transaction">
        {problem && <Refusal problem={problem} title="The transaction was not drafted" />}
        <TransactionFields form={form} set={setForm} kindFixed={false} />
        <p className="fy-small fy-secondary" role="note">
          {previewOf(form, codeOf)}
        </p>
        <div className="fy-row justify-end gap-2">
          <Link className="fy-btn fy-btn--ghost" to="/equity/transactions">
            Cancel
          </Link>
          <Button variant="primary" type="submit" disabled={busy} title={draftH.tooltip}>
            Draft the transaction
          </Button>
        </div>
      </form>
    </div>
  );
}
