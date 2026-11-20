import type { components } from "../../api/schema";
import { formatAmount } from "../../format/amount";
import { find, Tile, type KPIs } from "./kpi";

type Pack = components["schemas"]["BoardPack"];
type Statement = components["schemas"]["FinancialStatement"];

const headline = ["cash", "revenue", "net_burn", "runway", "gross_margin"];
const names: Record<string, string> = { pl: "Profit and loss", bs: "Balance sheet", cf: "Cash flow" };

// PackFigures shows a board pack's figures as the server gave them (FR-RPT-06; FR-UX-22): the month's headline KPIs as tiles, and each statement's
// sections with their totals beside the period before. Nothing is worked out here.
export function PackFigures({ pack }: { pack: Pack }) {
  const f = (pack.figures ?? {}) as { pl?: Statement; bs?: Statement; cf?: Statement; kpis?: KPIs };
  return (
    <div className="fy-stack">
      {f.kpis && (
        <div className="fy-tiles" aria-label="Headline KPIs">
          {headline.map((key) => {
            const k = find(f.kpis, key);
            return k ? <Tile key={key} k={k} kpis={f.kpis!} /> : null;
          })}
        </div>
      )}
      {(["pl", "bs", "cf"] as const).map((kind) => {
        const s = f[kind];
        if (!s) return null;
        return (
          <div className="fy-card" key={kind}>
            <div className="fy-tablewrap">
              <table className="fy-table fy-table--compact">
                <caption className="fy-sr">
                  {names[kind]}, {s.currency}, read at ledger revision {s.ledger_revision ?? pack.ledger_revision}
                </caption>
                <thead>
                  <tr>
                    <th scope="col">{names[kind]}</th>
                    {s.columns.map((c) => (
                      <th scope="col" className="fy-num" key={c.key}>
                        {c.label || c.key}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {s.blocks.map((b) => (
                    <tr key={b.key}>
                      <th scope="row">{b.title}</th>
                      {b.totals.map((t, i) => (
                        <td className="fy-num" key={i}>
                          {formatAmount(t.amount)}
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        );
      })}
      {pack.period_status && pack.period_status !== "closed" && (
        <p className="fy-secondary" role="note">
          The month was not closed when this pack was published, so its figures may change when it is.
        </p>
      )}
    </div>
  );
}
