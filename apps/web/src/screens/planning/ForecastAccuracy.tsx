import { useQuery } from "@tanstack/react-query";
import { useId } from "react";
import { api } from "../../api/client";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";

// ForecastAccuracy is the kept weekly cash projections against the cash that came, 4 and 13 weeks on (FR-PLN-04): what each projection said the balance
// would be, what it was, and the difference split into receipts later than expected, timing and the unplanned rest, which add to it exactly.
export function ForecastAccuracy() {
  const id = useId();
  const q = useQuery({
    queryKey: ["forecast-accuracy"],
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/forecast-accuracy", {});
      if (r.error) throw r.error;
      return r.data;
    },
  });
  if (!q.data || q.data.items.length === 0) return null; // nothing is kept for long enough yet, or the person may not read it
  return (
    <section className="fy-card fy-card--pad" aria-labelledby={`${id}-title`}>
      <h2 className="fy-h3" id={`${id}-title`}>
        How the forecast did
      </h2>
      <p className="fy-secondary">{q.data.note}</p>
      <table className="fy-table">
        <caption className="fy-sr-only">Each kept projection against the cash that came</caption>
        <thead>
          <tr>
            <th scope="col">Kept on</th>
            <th scope="col">Weeks on</th>
            <th scope="col" className="fy-num">
              Projected
            </th>
            <th scope="col" className="fy-num">
              Actual
            </th>
            <th scope="col" className="fy-num">
              Difference
            </th>
            <th scope="col" className="fy-num">
              Receipts later
            </th>
            <th scope="col" className="fy-num">
              Timing
            </th>
            <th scope="col" className="fy-num">
              Unplanned
            </th>
          </tr>
        </thead>
        <tbody>
          {q.data.items.map((r) => (
            <tr key={`${r.snapshot_id}-${r.weeks}`}>
              <td>{date(r.taken_on)}</td>
              <td>{r.weeks}</td>
              {[r.projected, r.actual, r.difference, r.receipts_later, r.timing, r.unplanned].map((m, i) => (
                <td className="fy-num" key={i}>
                  {formatAmount(m.amount)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}
