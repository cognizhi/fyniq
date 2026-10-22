import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleCheck, CircleX, Plus, RefreshCw, Scale } from "lucide-react";
import { useId, useState } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { negate, sign } from "../../format/decimal";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { listPeriods, type Period } from "./data";
import { who } from "./Journals";
import { monthOf } from "./Periods";

type Reval = components["schemas"]["FXRevaluation"];

// defaultPeriod is the month a revaluation is most likely due for: the latest open period that has ended by today,
// else the earliest open one, else the latest.
export function defaultPeriod(periods: readonly Period[], today: string): Period | undefined {
  const open = periods.filter((p) => p.status === "open");
  const ended = open.filter((p) => p.ends_on <= today);
  return ended[ended.length - 1] ?? open[0] ?? periods[periods.length - 1];
}

function State({ r }: { r: Reval }) {
  if (r.state === "nothing")
    return (
      <span className="fy-chip fy-chip--neutral">
        <Icon of={Scale} />
        Nothing to revalue
      </span>
    );
  if (r.state === "revalued")
    return (
      <span className="fy-chip fy-chip--good">
        <Icon of={CircleCheck} />
        Revalued
      </span>
    );
  return r.latest ? (
    <span className="fy-chip fy-chip--critical">
      <Icon of={CircleX} />A posting came after it: revalue again
    </span>
  ) : (
    <span className="fy-chip fy-chip--critical">
      <Icon of={CircleX} />
      Not revalued
    </span>
  );
}

// Revaluation is the month-end revaluation on Accounting › Currencies (docs/design/screens Currencies, "Revaluation";
// FR-GL-04; PRD-001 rev 0.73): a month's foreign-currency balances on accounts marked revalue, the period-end rate each
// takes and its gain or loss, what a run posts and when it reverses, what is not revalued, and the latest run. Every
// figure is the server's: the page moves signs to show debits and credits, and never works an amount out.
export function Revaluation({ today, mayRun, onAddRate }: { today: string; mayRun: boolean; onAddRate: (currency: string, on: string) => void }) {
  const id = useId();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const h = helpText("fx.revalue");
  const periods = useQuery({ queryKey: ["periods"], queryFn: listPeriods });
  const [chosen, setChosen] = useState("");
  const items = periods.data?.items ?? [];
  const period = items.find((p) => p.id === chosen) ?? defaultPeriod(items, today);
  const reval = useQuery({
    queryKey: ["fx-revaluation", period?.id],
    enabled: Boolean(period),
    queryFn: async () => {
      const r = await api.GET("/v1/periods/{period_id}/fx-revaluation", { params: { path: { period_id: period!.id } } });
      if (r.error) throw r.error;
      const b = r.data;
      return { body: { ...b, balances: b.balances ?? [], missing: b.missing ?? [], not_revalued: b.not_revalued ?? [], lines: b.lines ?? [] }, etag: etagOf(r.response) };
    },
  });
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [done, setDone] = useState("");
  const r = reval.data?.body;
  const run = async () => {
    if (!r || !reval.data) return;
    setBusy(true);
    setProblem(null);
    setDone("");
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.POST("/v1/periods/{period_id}/fx-revaluation", { params: { path: { period_id: r.period.id }, header: { "Idempotency-Key": key, "If-Match": reval.data.etag } } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    setDone(out.data.latest?.entry_id ? `${monthOf(r.period)} is revalued.` : `${monthOf(r.period)} is revalued: nothing changed.`);
    await queries.invalidateQueries({ predicate: (q) => ["fx-revaluation", "period-close"].includes(String(q.queryKey[0])) });
  };
  return (
    <div className="fy-stack fy-stack--4">
      <div className="fy-row fy-row--wrap items-end gap-3">
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-month`}>
            Month
          </label>
          <select className="fy-select" id={`${id}-month`} value={period?.id ?? ""} onChange={(e) => setChosen(e.target.value)}>
            {[...items].reverse().map((p) => (
              <option key={p.id} value={p.id}>
                {monthOf(p)}
                {p.status === "closed" ? " (closed)" : ""}
              </option>
            ))}
          </select>
        </div>
        {r && <State r={r} />}
      </div>
      {periods.data && items.length === 0 && <p className="fy-secondary">No periods yet: open the financial year in Accounting › Periods and close.</p>}
      {reval.isError && <Refusal problem={problemOf(reval.error)} title="The revaluation could not be shown" />}
      {r?.missing.map((m) => (
        <div key={m.currency} className="fy-banner fy-banner--critical" role="status">
          <Icon of={CircleX} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">
              No {m.currency} rate for {date(r.date)}
            </span>
            <span className="fy-small">Nothing posts until every currency has a rate for the month's last day: add one for the day or the month.</span>
          </div>
          {mayRun && (
            <div className="fy-banner__action">
              <Button variant="secondary" size="sm" onClick={() => onAddRate(m.currency, r.date)}>
                <Icon of={Plus} />
                Add a {m.currency} rate
              </Button>
            </div>
          )}
        </div>
      ))}
      {r && (
        <section className="fy-card" aria-labelledby={`${id}-items`}>
          <div className="fy-card__head fy-row--wrap gap-2">
            <h2 className="fy-h3" id={`${id}-items`}>
              Revaluation at {date(r.date)}
            </h2>
            <span className="fy-small fy-muted">Reversed on {date(r.reverse_on)}</span>
          </div>
          <p className="fy-card__body fy-small">
            Foreign-currency balances that will be settled in money are restated at the month-end rate. The difference is an unrealised gain or loss, reversed on the next month's first day.
          </p>
          {r.balances.length === 0 ? (
            <p className="fy-card__body fy-secondary">No account marked for revaluation has a foreign-currency balance at the month's end.</p>
          ) : (
            <div className="fy-tablewrap" style={{ border: 0, borderRadius: 0 }}>
              <table className="fy-table fy-table--cards">
                <thead>
                  <tr>
                    <th scope="col">Account</th>
                    <th scope="col" className="fy-num fy-from-medium">
                      Open
                    </th>
                    <th scope="col" className="fy-num fy-from-expanded">
                      Carrying ({r.balances[0]?.carrying.currency})
                    </th>
                    <th scope="col" className="fy-from-expanded">
                      Rate
                    </th>
                    <th scope="col" className="fy-num">
                      Gain or loss
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {r.balances.map((b) => (
                    <tr key={`${b.account_id} ${b.open.currency}`}>
                      <td data-card="title">
                        <span className="fy-acct">{b.code}</span>
                        {b.name}
                      </td>
                      <td data-card="meta" className="fy-num fy-from-medium">
                        {b.open.currency} {formatAmount(b.open.amount)}
                      </td>
                      <td data-card="hide" className="fy-num fy-from-expanded">
                        {formatAmount(b.carrying.amount)}
                      </td>
                      <td data-card="hide" className="fy-from-expanded">
                        {b.rate ? (
                          <>
                            <span className="fy-num">{b.rate}</span> <span className="fy-small fy-muted">{b.rate_source}</span>
                          </>
                        ) : (
                          <span className="fy-small fy-muted">No rate</span>
                        )}
                      </td>
                      <td data-card="amount" className="fy-num">
                        {b.change ? formatAmount(b.change.amount) : "—"}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      )}
      {r && (
        <div className="fy-grid fy-grid--2">
          <section className="fy-card" aria-labelledby={`${id}-post`}>
            <div className="fy-card__head">
              <h2 className="fy-h3" id={`${id}-post`}>
                Posting preview
              </h2>
              <span className="fy-small fy-muted">
                {date(r.date)}, reversed {date(r.reverse_on)}
              </span>
            </div>
            {r.lines.length === 0 ? (
              <p className="fy-card__body fy-secondary">{r.missing.length > 0 ? "Nothing can post until every currency has a rate." : "Nothing to post: every balance is at the month-end rate."}</p>
            ) : (
              <table className="fy-table fy-table--compact fy-ledger">
                <thead>
                  <tr>
                    <th scope="col">Account</th>
                    <th scope="col" className="fy-num">
                      Debit
                    </th>
                    <th scope="col" className="fy-num">
                      Credit
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {r.lines.map((l, i) => (
                    <tr key={i}>
                      <td>
                        <span className="fy-acct">{l.code}</span>
                        {l.name}
                        <span className="fy-small fy-muted block">{l.description}</span>
                      </td>
                      <td className="fy-num">{sign(l.base.amount) > 0 ? formatAmount(l.base.amount) : ""}</td>
                      <td className="fy-num">{sign(l.base.amount) < 0 ? formatAmount(negate(l.base.amount)) : ""}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
            {r.lines.length > 0 && (
              <p className="fy-card__foot fy-small fy-muted">
                A line on a revalued account changes only its {r.balances[0]?.carrying.currency} value: its amount in the currency stays as it is.
              </p>
            )}
          </section>
          <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-not`}>
            <h2 className="fy-h3" id={`${id}-not`}>
              Not revalued
            </h2>
            <p className="fy-small fy-muted">These stay at the rate on the day they were recorded, because they will not be settled in money, or their account is not marked for revaluation in the chart.</p>
            {r.not_revalued.length === 0 ? (
              <p className="fy-small">None.</p>
            ) : (
              <ul className="fy-list">
                {r.not_revalued.map((n) => (
                  <li key={`${n.account_id} ${n.amount.currency}`} className="fy-list__item">
                    <span className="fy-list__main">
                      <span className="fy-list__title">
                        <span className="fy-acct">{n.code}</span>
                        {n.name}
                      </span>
                      <span className="fy-list__meta">
                        {n.amount.currency} {formatAmount(n.amount.amount)}, carried at {n.carrying.currency} {formatAmount(n.carrying.amount)}
                      </span>
                    </span>
                  </li>
                ))}
              </ul>
            )}
            {r.latest && (
              <p className="fy-small fy-secondary">
                Last revalued by {who(r.latest.posted_by)}
                {r.latest.posted_at ? ` on ${date(r.latest.posted_at)}` : ""}
                {r.latest.entry_id ? (r.latest.reversal_id ? `, reversed on ${date(r.reverse_on)}.` : `; its reversal waits for ${date(r.reverse_on)}'s month to open.`) : ", with nothing to change."}
              </p>
            )}
          </section>
        </div>
      )}
      {problem && <Refusal problem={problem} title="The month was not revalued" />}
      {done && (
        <p className="fy-small" role="status">
          <Icon of={CircleCheck} /> {done}
        </p>
      )}
      {r && mayRun && r.period.status === "open" && r.state !== "nothing" && (
        <div className="fy-actionbar">
          <Button variant="secondary" onClick={() => void reval.refetch()} disabled={busy || reval.isFetching}>
            <Icon of={RefreshCw} />
            Preview again
          </Button>
          <Button variant="primary" onClick={() => void run()} disabled={busy || r.missing.length > 0} title={h.tooltip}>
            <Icon of={Scale} />
            {h.label} {monthOf(r.period)}
          </Button>
        </div>
      )}
    </div>
  );
}
