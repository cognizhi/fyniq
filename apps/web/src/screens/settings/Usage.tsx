import { useQuery } from "@tanstack/react-query";
import { api } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { problemOf } from "../../ui";
import { SettingsPage } from "./SettingsPage";

// The size of a file set, in the units a person reads: 1,536 bytes is 1.5 KB.
export function sizeOf(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let v = bytes / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v >= 10 ? 0 : 1)} ${units[i]}`;
}

const month = new Intl.DateTimeFormat("en-SG", { month: "short", year: "numeric", timeZone: "UTC" });
const count = new Intl.NumberFormat("en-SG");

// Usage is Settings › Usage and cost (FR-ADM-05): what the organisation used of AI, storage and exports, a month at a time, and what is stored now. It reads and keeps nothing.
export function Usage() {
  const q = useQuery({
    queryKey: ["usage"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/usage", { params: { query: { months: 6 } } });
      if (error) throw error;
      return data;
    },
  });
  const months = [...(q.data?.months ?? [])].reverse(); // the latest first
  const stored = q.data?.stored;
  return (
    <SettingsPage title="Usage and cost" lead="What this organisation used of AI, storage and exports in the last months, and what it holds now." screen="usage">
      {q.isError && <Refusal problem={problemOf(q.error)} title="The usage could not be read" />}
      {q.isPending && <p className="fy-secondary">Loading the usage…</p>}
      {stored && (
        <section className="fy-card" aria-labelledby="usage-stored">
          <div className="fy-card__head">
            <h2 className="fy-h3" id="usage-stored">
              Held in storage now
            </h2>
            <span className="fy-small fy-muted">{sizeOf(stored.total_bytes)} in all</span>
          </div>
          <div className="fy-tablewrap">
            <table className="fy-table fy-table--compact" aria-labelledby="usage-stored">
              <thead>
                <tr>
                  <th scope="col">Kind</th>
                  <th scope="col" className="fy-num">
                    Files
                  </th>
                  <th scope="col" className="fy-num">
                    Size
                  </th>
                </tr>
              </thead>
              <tbody>
                {(
                  [
                    ["Evidence files", stored.evidence],
                    ["Captured documents", stored.captures],
                    ["Report exports", stored.exports],
                  ] as const
                ).map(([name, f]) => (
                  <tr key={name}>
                    <th scope="row">{name}</th>
                    <td className="fy-num">{count.format(f.files)}</td>
                    <td className="fy-num">{sizeOf(f.bytes)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}
      {q.data && (
        <section className="fy-card" aria-labelledby="usage-months">
          <div className="fy-card__head">
            <h2 className="fy-h3" id="usage-months">
              By month
            </h2>
            <span className="fy-small fy-muted">Calendar months in UTC</span>
          </div>
          <div className="fy-tablewrap">
            <table className="fy-table fy-table--compact" aria-labelledby="usage-months">
              <thead>
                <tr>
                  <th scope="col">Month</th>
                  <th scope="col" className="fy-num">
                    AI calls
                  </th>
                  <th scope="col" className="fy-num fy-from-medium">
                    Tokens in
                  </th>
                  <th scope="col" className="fy-num fy-from-medium">
                    Tokens out
                  </th>
                  <th scope="col" className="fy-num">
                    AI cost
                  </th>
                  <th scope="col" className="fy-num fy-from-medium">
                    Exports
                  </th>
                  <th scope="col" className="fy-num fy-from-medium">
                    Evidence added
                  </th>
                </tr>
              </thead>
              <tbody>
                {months.map((m) => (
                  <tr key={m.month}>
                    <th scope="row">{month.format(new Date(`${m.month}-01T00:00:00Z`))}</th>
                    <td className="fy-num">{count.format(m.ai_calls)}</td>
                    <td className="fy-num fy-from-medium">{count.format(m.input_tokens)}</td>
                    <td className="fy-num fy-from-medium">{count.format(m.output_tokens)}</td>
                    <td className="fy-num">{m.ai_cost.length ? m.ai_cost.map((c) => `${c.currency} ${Number(c.amount).toFixed(2)}`).join(", ") : "None"}</td>
                    <td className="fy-num fy-from-medium">
                      {count.format(m.exports.files)} ({sizeOf(m.exports.bytes)})
                    </td>
                    <td className="fy-num fy-from-medium">
                      {count.format(m.evidence.files)} ({sizeOf(m.evidence.bytes)})
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}
      <p className="fy-small fy-secondary">The cost is what the AI's prices say, in the currency of each price: it is not an invoice. Storage counts files as uploaded.</p>
    </SettingsPage>
  );
}
