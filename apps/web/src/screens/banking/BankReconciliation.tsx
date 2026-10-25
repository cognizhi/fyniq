import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { ChevronRight, CircleCheck, FileCheck2, Lock, PenLine, RefreshCw, TriangleAlert, UserX } from "lucide-react";
import { useId, useState } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { formatAmount } from "../../format/amount";
import { date, dateTime, month } from "../../format/date";
import { isZero } from "../../format/decimal";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Period = components["schemas"]["Period"];

// preparers and signers are the roles of "Bank reconciliation sign-off" (Appendix D): C prepares, A signs off.
export const preparers = ["bookkeeper", "accountant"];
export const signers = ["controller"];

// lastEnded is the latest period that has ended by today, else the earliest open one: the month to reconcile.
export function lastEnded(periods: Period[], today: string): Period | undefined {
  const ended = periods.filter((p) => p.ends_on < today).sort((a, b) => b.ends_on.localeCompare(a.ends_on));
  return ended[0] ?? [...periods].sort((a, b) => a.starts_on.localeCompare(b.starts_on))[0];
}

export function usePeriods() {
  return useQuery({
    queryKey: ["periods"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/periods", { params: { query: { page_size: 200 } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
}

const money = (m: components["schemas"]["Money"]) => `${formatAmount(m.amount)} ${m.currency}`;

// BankReconciliation is Banking › reconcile an account for a month (docs/design/screens BankReconciliation; FR-BNK-10,
// FR-BNK-11, FM-5; p1-bank-import-reconcile task 10.5, Expert): the statement's balance at the month's end, the books'
// balance, the lines the books do not hold yet and the difference they leave; prepared by one person and signed off by
// another, stepped up, and then locked with the month.
export function BankReconciliation() {
  const { accountId, periodId } = useParams({ strict: false }) as { accountId: string; periodId: string };
  const { session, organisation } = useSignedIn();
  const roles = session.roles ?? [];
  const navigate = useNavigate();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const id = useId();
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const [typed, setTyped] = useState("");
  const founder = organisation?.team_mode === "founder";
  const account = useQuery({
    queryKey: ["money-account", accountId],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/money-accounts/{money_account_id}", { params: { path: { money_account_id: accountId } } });
      if (error) throw error;
      return data;
    },
  });
  const periods = usePeriods();
  const rec = useQuery({
    queryKey: ["bank-reconciliation", accountId, periodId],
    queryFn: async () => {
      const { data, error, response } = await api.GET("/v1/money-accounts/{money_account_id}/reconciliations/{period_id}", { params: { path: { money_account_id: accountId, period_id: periodId } } });
      if (response.status === 404) return null;
      if (error) throw error;
      return { ...data, etag: etagOf(response) };
    },
  });
  const suspense = useQuery({
    queryKey: ["bank-lines", accountId, "suspense"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/bank-lines", { params: { query: { money_account_id: accountId, status: "suspense", page_size: 100 } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
  const period = (periods.data ?? []).find((p) => p.id === periodId);
  const r = rec.data;
  const mine = r && r.prepared_by === `user:${session.user_id}`;
  const signed = Boolean(r?.signed_at);
  const prepare = async () => {
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/bank-reconciliations", {
      params: { header: { "Idempotency-Key": newKey() } },
      body: { money_account_id: accountId, period_id: periodId, statement_balance: !founder && typed.trim() ? { amount: typed.trim(), currency: account.data?.currency ?? "SGD" } : undefined },
    });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    void queries.invalidateQueries({ queryKey: ["bank-reconciliation", accountId, periodId] });
  };
  const sign = async () => {
    if (!r) return;
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/money-accounts/{money_account_id}/reconciliations/{period_id}/sign", {
        params: { path: { money_account_id: accountId, period_id: periodId }, header: { "Idempotency-Key": newKey(), "If-Match": r.etag } },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    void queries.invalidateQueries({ queryKey: ["bank-reconciliation", accountId, periodId] });
  };
  const h = helpText("banking.reconciliation");

  if (account.isError) return <Refusal problem={problemOf(account.error)} title="The money account could not be read" />;
  return (
    <div className="fy-page fy-enter">
      <nav className="fy-breadcrumb" aria-label="Breadcrumb">
        <Link to="/banking">Banking</Link>
        <Icon of={ChevronRight} />
        {account.data ? (
          <Link to="/banking/accounts/$accountId" params={{ accountId }}>
            {account.data.name}
          </Link>
        ) : (
          <span>An account</span>
        )}
        <Icon of={ChevronRight} />
        <span>Reconciliation</span>
      </nav>
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">
            Reconcile {account.data?.name ?? "the account"}
            {period && ` for ${month(period.starts_on.slice(0, 7))}`}
          </h1>
          <p className="fy-secondary">{h.tooltip}</p>
        </div>
        <div className="fy-pagehead__actions">
          <label className="fy-sr" htmlFor={`${id}-period`}>
            Month
          </label>
          <select
            className="fy-select"
            id={`${id}-period`}
            value={periodId}
            onChange={(e) => void navigate({ to: "/banking/reconciliations/$accountId/$periodId", params: { accountId, periodId: e.target.value } })}
          >
            {(periods.data ?? []).map((p) => (
              <option key={p.id} value={p.id}>
                {month(p.starts_on.slice(0, 7))}
              </option>
            ))}
          </select>
        </div>
      </div>
      <ReviewBanner screen="bank-reconciliation" />
      {problem && <Refusal problem={problem} title={signed ? "It was not signed off" : "The reconciliation was not done"} />}
      {rec.isError && <Refusal problem={problemOf(rec.error)} title="The reconciliation could not be read" />}

      {r ? (
        <>
          <div className="fy-row fy-row--wrap gap-2">
            {signed ? (
              <span className="fy-chip fy-chip--good">
                <Icon of={Lock} />
                Signed off: locked with the month
              </span>
            ) : isZero(r.difference.amount) ? (
              <span className="fy-chip fy-chip--good">
                <Icon of={CircleCheck} />
                Ready to sign off
              </span>
            ) : (
              <span className="fy-chip fy-chip--warning">
                <Icon of={TriangleAlert} />
                A difference to explain
              </span>
            )}
          </div>
          <section className="fy-card" aria-labelledby={`${id}-balances`}>
            <div className="fy-card__head">
              <h2 className="fy-h3" id={`${id}-balances`}>
                At {period ? date(period.ends_on) : "the month's end"}
              </h2>
            </div>
            <dl className="fy-dl fy-card__body">
              <div>
                <dt>The statement's balance</dt>
                <dd className="fy-num">{money(r.statement_balance)}</dd>
              </div>
              <div>
                <dt>The books' balance</dt>
                <dd className="fy-num">{money(r.book_balance)}</dd>
              </div>
              <div>
                <dt>Lines the books do not hold yet</dt>
                <dd className="fy-num">
                  {money(r.unreconciled)} ({r.unreconciled_count})
                </dd>
              </div>
              <div>
                <dt>Difference</dt>
                <dd className="fy-num fy-strong">{money(r.difference)}</dd>
              </div>
              <div className="fy-expert-only">
                <dt>Ledger revision</dt>
                <dd className="fy-mono">{r.ledger_revision}</dd>
              </div>
              <div>
                <dt>Prepared</dt>
                <dd>
                  {mine ? "By you" : "By another member"}, {dateTime(r.prepared_at)}
                </dd>
              </div>
              {signed && (
                <div>
                  <dt>Signed off</dt>
                  <dd>
                    {r.signed_by === `user:${session.user_id}` ? "By you" : "By another member"}, {dateTime(r.signed_at)}
                  </dd>
                </div>
              )}
            </dl>
          </section>
          {(r.unreconciled_lines ?? []).length > 0 && (
            <section className="fy-stack fy-stack--3" aria-labelledby={`${id}-open`}>
              <h2 className="fy-h2" id={`${id}-open`}>
                Lines the books do not hold yet
              </h2>
              <table className="fy-table" aria-label="Unreconciled lines">
                <thead>
                  <tr>
                    <th scope="col">Booked</th>
                    <th scope="col">Description</th>
                    <th scope="col" className="fy-num">
                      Amount
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {r.unreconciled_lines.map((l) => (
                    <tr key={l.id}>
                      <td>{date(l.booking_date)}</td>
                      <td className="fy-mono">{l.description ?? ""}</td>
                      <td className="fy-num">{formatAmount(l.amount.amount)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <p className="fy-small">
                <Link to="/banking/categorise" search={{ account: accountId }}>
                  Categorise them
                </Link>
                , then prepare it again.
              </p>
            </section>
          )}
        </>
      ) : (
        rec.data === null && (
          <div className="fy-empty fy-card fy-card--pad">
            <Icon of={FileCheck2} size={24} />
            <h2 className="fy-h3">Not prepared yet</h2>
            <p className="fy-secondary">Preparing it takes the balance of the latest statement imported up to the month's end, and the books' balance on that day.</p>
          </div>
        )
      )}
      {(suspense.data ?? []).length > 0 && (
        <p className="fy-small fy-row gap-2">
          <Icon of={TriangleAlert} />
          {suspense.data!.length === 1 ? "1 line of this account is" : `${suspense.data!.length} lines of this account are`} in suspense (2999): categorise them before the close.
        </p>
      )}

      {!signed && roles.some((x) => preparers.includes(x)) && (
        <section className="fy-card" aria-labelledby={`${id}-prepare`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-prepare`}>
              {r ? "Prepare it again" : "Prepare it"}
            </h2>
          </div>
          <div className="fy-card__body fy-stack fy-stack--3">
            {founder ? (
              <p className="fy-small fy-secondary">In Founder mode the statement's balance comes only from an imported statement or feed, never typed.</p>
            ) : (
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-typed`}>
                  The e-statement's balance (optional)
                </label>
                <input className="fy-input fy-num max-w-56" id={`${id}-typed`} inputMode="decimal" value={typed} onChange={(e) => setTyped(e.target.value)} />
                <span className="fy-small fy-muted">Left empty, the closing balance of the latest statement imported up to the month's end.</span>
              </div>
            )}
          </div>
          <div className="fy-card__foot">
            <button type="button" className="fy-btn fy-btn--secondary" onClick={() => void prepare()} disabled={busy}>
              <Icon of={RefreshCw} />
              {r ? "Prepare it again" : "Prepare it"}
            </button>
          </div>
        </section>
      )}
      {r && !signed && roles.some((x) => signers.includes(x)) && (
        <div className="fy-row fy-row--wrap gap-2">
          {mine ? (
            <p className="fy-small fy-row gap-2">
              <Icon of={UserX} />
              You prepared it: another person signs it off.
            </p>
          ) : (
            <button type="button" className="fy-btn fy-btn--primary" onClick={() => void sign()} disabled={busy} title={helpText("bank-reconciliation.sign").tooltip}>
              <Icon of={PenLine} />
              {helpText("bank-reconciliation.sign").label}
            </button>
          )}
        </div>
      )}
      <p className="fy-small fy-muted">The bank's statement for the month is the evidence: it is kept with its statement on the account.</p>
    </div>
  );
}
