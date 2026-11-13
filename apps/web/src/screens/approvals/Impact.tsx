import { OctagonX } from "lucide-react";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { Icon } from "../../ui";
import type { Approval } from "./words";

type Impact = NonNullable<Approval["impact"]>;

// Impact is what approving a request would post now (FR-APR-02, PRD-001 rev 0.177): each entry's lines as debits and
// credits in the base currency, the balances of the accounts it moves before and after, or what would refuse it. The
// API works it out by carrying the change out and rolling it back; nothing is optimistic here (FR-UX-22).
export function Impact({ impact }: { impact: Impact }) {
  if (impact.refused)
    return (
      <div className="fy-banner fy-banner--critical" role="note">
        <Icon of={OctagonX} size={20} />
        <div className="fy-banner__text">
          <span className="fy-strong">Approving it now would be refused</span>
          <span className="fy-small">
            {impact.refused.message} {impact.refused.fix ? `${impact.refused.fix}.` : ""}
          </span>
        </div>
      </div>
    );
  const names = new Map(impact.balances.map((b) => [b.account_id, b]));
  const base = impact.balances[0]?.base_after.currency ?? "SGD";
  return (
    <div className="fy-stack fy-stack--3">
      {impact.entries.map((e) => (
        <div key={e.id} className="fy-tablewrap">
          <table className="fy-table fy-table--compact fy-ledger" aria-label={`The entry of ${date(e.posting_date)}`}>
            <caption className="fy-small fy-secondary text-left">
              {date(e.posting_date)}
              {e.memo ? ` · ${e.memo}` : ""}
            </caption>
            <thead>
              <tr>
                <th scope="col">Account</th>
                <th scope="col" className="fy-num">
                  Debit ({base})
                </th>
                <th scope="col" className="fy-num">
                  Credit ({base})
                </th>
              </tr>
            </thead>
            <tbody>
              {(e.lines ?? []).map((l) => {
                const a = names.get(l.account_id);
                const credit = l.base_amount.amount.startsWith("-");
                const amount = formatAmount(l.base_amount.amount.replace(/^-/, ""));
                return (
                  <tr key={l.line_no}>
                    <td>
                      <span className="fy-acct">{a?.code ?? "—"}</span>
                      {a?.name ?? "An account"}
                      {l.amount.currency !== base ? ` · ${l.amount.currency} ${formatAmount(l.amount.amount.replace(/^-/, ""))}` : ""}
                    </td>
                    <td className="fy-num">{credit ? "" : amount}</td>
                    <td className="fy-num">{credit ? amount : ""}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      ))}
      <div className="fy-tablewrap">
        <table className="fy-table fy-table--compact" aria-label="Balances it moves">
          <caption className="fy-small fy-secondary text-left">Balances through {impact.balances[0] ? date(impact.balances[0].through) : "the entries' date"}, debit positive</caption>
          <thead>
            <tr>
              <th scope="col">Account</th>
              <th scope="col" className="fy-num">
                Before ({base})
              </th>
              <th scope="col" className="fy-num">
                After ({base})
              </th>
            </tr>
          </thead>
          <tbody>
            {impact.balances.map((b) => (
              <tr key={`${b.account_id}-${b.before.currency}`}>
                <td>
                  <span className="fy-acct">{b.code}</span>
                  {b.name}
                  {b.before.currency !== base ? ` (${b.before.currency})` : ""}
                </td>
                <td className="fy-num">{formatAmount(b.base_before.amount)}</td>
                <td className="fy-num fy-strong">{formatAmount(b.base_after.amount)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
