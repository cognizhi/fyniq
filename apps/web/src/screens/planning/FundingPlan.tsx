import { useQuery } from "@tanstack/react-query";
import { useId } from "react";
import { api } from "../../api/client";
import { formatAmount } from "../../format/amount";
import { month } from "../../format/date";
import { helpText } from "../../i18n";

// FundingPlan is a forecast's planned equity rounds taken through the equity register's round model over the cap table (FR-PLN-07): each round's price, the shares its
// money buys and each holder before and after. It is a sandbox; it is hidden from a person who may not read the whole cap table.
export function FundingPlan({ forecastId, version }: { forecastId: string; version: number }) {
  const id = useId();
  const q = useQuery({
    queryKey: ["forecast-funding-plan", forecastId, version],
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/forecasts/{forecast_id}/funding-plan", { params: { path: { forecast_id: forecastId } } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const p = q.data;
  if (!p || (p.rounds.length === 0 && p.other.length === 0)) return null;
  const sandbox = helpText("planning.funding-plan");
  return (
    <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-title`}>
      <h2 className="fy-h3" id={`${id}-title`} title={sandbox.tooltip}>
        {sandbox.label}
      </h2>
      <p className="fy-secondary">{sandbox.tooltip}</p>
      {p.rounds.map((r) => (
        <div key={r.row} className="fy-stack fy-stack--2">
          <h3 className="fy-strong">
            {month(r.month)}: {formatAmount(r.planned.amount)} {p.currency} of equity
          </h3>
          {!r.modelled ? (
            <p className="fy-small" role="status">
              Not modelled. {r.why}
            </p>
          ) : (
            <>
              <p className="fy-small">
                Priced at {r.price_per_share ? formatAmount(r.price_per_share.amount) : ""} a share on a pre-money valuation of {r.pre_money ? formatAmount(r.pre_money.amount) : ""}: {r.new_shares} new shares
                for {r.raised ? formatAmount(r.raised.amount) : ""}, a post-money valuation of {r.post_money ? formatAmount(r.post_money.amount) : ""}.
              </p>
              <div className="fy-tablewrap">
                <table className="fy-table fy-table--compact">
                  <caption className="fy-sr">Each holder before and after the round of {month(r.month)}</caption>
                  <thead>
                    <tr>
                      <th scope="col">Holder</th>
                      <th scope="col" className="fy-num">
                        Before
                      </th>
                      <th scope="col" className="fy-num">
                        After
                      </th>
                      <th scope="col" className="fy-num">
                        Points lost
                      </th>
                      <th scope="col" className="fy-num">
                        Shares after
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {r.holders.map((h) => (
                      <tr key={h.holder_id ?? h.holder_name}>
                        <th scope="row">{h.holder_name}</th>
                        <td className="fy-num">{h.before ? `${h.before}%` : "—"}</td>
                        <td className="fy-num">{h.after}%</td>
                        <td className="fy-num">{h.points_lost}</td>
                        <td className="fy-num">{formatAmount(h.shares_after).replace(/\.00$/, "")}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          )}
        </div>
      ))}
      {p.other.length > 0 && (
        <ul className="fy-list" aria-label="Funding that issues no shares in the plan">
          {p.other.map((o) => (
            <li key={o.row}>
              {month(o.month)}: {o.type} of {formatAmount(o.amount.amount)} {p.currency}. {o.why}.
            </li>
          ))}
        </ul>
      )}
      {p.notes.map((n) => (
        <p key={n} className="fy-small fy-muted">
          {n}
        </p>
      ))}
    </section>
  );
}
