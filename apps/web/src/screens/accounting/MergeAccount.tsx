import { useQueryClient } from "@tanstack/react-query";
import { GitMerge } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { timeZone } from "../../format/date";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Account = components["schemas"]["Account"];
export type Approval = components["schemas"]["Approval"];

// mergeable is an account a merge takes, as the one merged or the one kept (FR-COA-04, PRD-001 rev 0.174): active, not
// merged, and not a system, group, money or control account. The API decides; the page offers only these.
export function mergeable(a: Account): boolean {
  return a.active && !a.merged_into_id && !a.role && !a.group && !a.money_account && !a.control;
}

// targetsOf are the accounts from may be merged into: mergeable, of its type, held in its currency, and marked revalue
// when it is.
export function targetsOf(from: Account, accounts: Account[]): Account[] {
  return accounts.filter((a) => a.id !== from.id && mergeable(a) && a.type === from.type && (a.currency ?? "") === (from.currency ?? "") && (!from.revalue || a.revalue));
}

// waitingMerges are the merges waiting for approval that the person may see.
export async function waitingMerges(): Promise<Approval[]> {
  const { data, error } = await api.GET("/v1/approvals", { params: { query: { status: "pending", page_size: 200 } } });
  if (error) throw error;
  return (data.items ?? []).filter((a) => a.object_type === "account");
}

function today(): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone, year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date());
}

// MergeAccount asks for a merge of an account into another (FR-COA-04, Appendix B.8): the account it goes into and the
// day its balance moves. Another person approves it; until then nothing moves.
export function MergeAccount({ from, accounts, onClose, onAsked }: { from: Account | null; accounts: Account[]; onClose: () => void; onAsked: (a: Approval) => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const id = useId();
  const help = helpText("chart.merge");
  const [into, setInto] = useState("");
  const [date, setDate] = useState(today);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [error, setError] = useState("");
  const targets = from ? targetsOf(from, accounts) : [];
  const close = () => {
    setInto("");
    setProblem(null);
    setError("");
    onClose();
  };
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!from) return;
    if (!into) return setError("Choose the account to merge it into.");
    setError("");
    setBusy(true);
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.POST("/v1/accounts/{account_id}/merge-approval", {
        params: { path: { account_id: from.id }, header: { "Idempotency-Key": key, "If-Match": `"${from.version}"` } },
        body: { into_account_id: into, date },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["approvals"] });
    onAsked(out.data);
    close();
  };
  return (
    <Dialog
      open={from !== null}
      onOpenChange={(o) => !o && close()}
      dismissible={!busy}
      eyebrow="Chart of accounts"
      title={from ? `Merge ${from.code} ${from.name}` : "Merge an account"}
      description={help.tooltip}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={busy}>
            Cancel
          </Button>
          <Button type="submit" form={`${id}-form`} variant="primary" disabled={busy || targets.length === 0}>
            <Icon of={GitMerge} />
            Ask for approval
          </Button>
        </>
      }
    >
      {from && (
        <form className="fy-stack fy-stack--4" id={`${id}-form`} onSubmit={submit} noValidate>
          {problem && <Refusal problem={problem} title="The merge cannot be asked for" />}
          {targets.length === 0 ? (
            <p className="fy-secondary">No account can take it: one of the same type, held in the same currency, that is not a group, a system, money or control account.</p>
          ) : (
            <div className="fy-form">
              <div className="fy-field fy-field--full">
                <label className="fy-label" htmlFor={`${id}-into`}>
                  Merge it into
                </label>
                <select className="fy-select" id={`${id}-into`} value={into} onChange={(e) => setInto(e.target.value)} aria-invalid={Boolean(error) || undefined}
                  aria-describedby={error ? `${id}-into-error` : undefined}>
                  <option value="">Choose an account</option>
                  {targets.map((a) => (
                    <option key={a.id} value={a.id}>
                      {a.code} {a.name}
                    </option>
                  ))}
                </select>
                {error && (
                  <span className="fy-error" id={`${id}-into-error`}>
                    {error}
                  </span>
                )}
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-date`}>
                  On
                </label>
                <input className="fy-input" id={`${id}-date`} type="date" value={date} onChange={(e) => setDate(e.target.value)} aria-describedby={`${id}-date-help`} />
                <span className="fy-help" id={`${id}-date-help`}>
                  A day in an open period, on or after its last posting.
                </span>
              </div>
            </div>
          )}
          <p className="fy-small fy-secondary">
            Its balance moves into the other account by one entry, and it becomes inactive for good. Earlier postings stay where they are. Another person approves the
            merge; nothing moves until then.
          </p>
        </form>
      )}
    </Dialog>
  );
}
