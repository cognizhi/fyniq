import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { problemOf, type ApiProblem } from "../../ui";

// The asset reports (FR-FA-04; p2-reporting-planning task 5.2; docs/design/screens FixedAssets): the movement schedule of cost, accumulated depreciation and net book value for the
// financial statements, tied to the ledger; the tax capital allowance schedule of a year of assessment; and the dialog that elects an asset's capital allowance method.

type FixedAsset = components["schemas"]["FixedAsset"];
type Money = components["schemas"]["Money"];
type Movement = components["schemas"]["AssetMovementReport"];
type MovementAsset = components["schemas"]["MovementAsset"];

const money = (m: Money | undefined | null) => (m ? formatAmount(m.amount) : "—");

const monthOf = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;

export const reasonText: Record<string, string> = {
  no_election: "No method elected yet",
  before_pack: "Bought before YA 2023",
  disposed: "A disposal stands",
  over_cap: "Over the S$5,000 or S$30,000 limit",
  bad_life: "A working life that is not allowed",
  not_sgd: "Base currency is not SGD",
};

export const methodText: Record<string, string> = {
  s19: "Section 19, over its working life",
  s19a1: "Section 19A(1), 3 years",
  s19a2: "Section 19A(2), 1 year, no cap",
  s19a10a: "Section 19A(10A), 1 year",
};

function Cells({ r, bold }: { r: MovementAsset; bold?: boolean }) {
  const cls = bold ? "fy-num fy-strong" : "fy-num";
  return (
    <>
      <td className={cls}>{money(r.opening_cost)}</td>
      <td className={cls}>{money(r.additions)}</td>
      <td className={cls}>{money(r.disposals)}</td>
      <td className={cls}>{money(r.closing_cost)}</td>
      <td className={cls}>{money(r.opening_accumulated)}</td>
      <td className={cls}>{money(r.charge)}</td>
      <td className={cls}>{money(r.disposed_accumulated)}</td>
      <td className={cls}>{money(r.closing_accumulated)}</td>
      <td className={cls}>{money(r.closing_nbv)}</td>
    </>
  );
}

// MovementSchedule is the run of a year: each asset's cost and accumulated depreciation as they moved, with the register tied to the ledger.
export function MovementSchedule() {
  const year = new Date().getFullYear();
  const [from, setFrom] = useState(`${year}-01`);
  const [to, setTo] = useState(`${year}-12`);
  const valid = /^[0-9]{4}-(0[1-9]|1[0-2])$/.test(from) && /^[0-9]{4}-(0[1-9]|1[0-2])$/.test(to) && from <= to;
  const q = useQuery({
    queryKey: ["fixed-assets", "movement", from, to],
    enabled: valid,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/reports/fixed-asset-movement", { params: { query: { from, to } } });
      if (error) throw error;
      return data as Movement;
    },
  });
  const id = useId();
  const r = q.data;
  return (
    <section className="fy-stack fy-stack--3" aria-label="Movement schedule">
      <p className="fy-small fy-secondary">{helpText("fixed-assets.movement").tooltip}</p>
      <div className="fy-row fy-row--wrap">
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-from`}>
            From (YYYY-MM)
          </label>
          <input id={`${id}-from`} className="fy-input" value={from} onChange={(e) => setFrom(e.target.value)} inputMode="numeric" maxLength={7} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-to`}>
            To (YYYY-MM)
          </label>
          <input id={`${id}-to`} className="fy-input" value={to} onChange={(e) => setTo(e.target.value)} inputMode="numeric" maxLength={7} />
        </div>
      </div>
      {!valid && <p className="fy-error">Enter the first and last month as YYYY-MM, the first not after the last.</p>}
      {q.isError && <Refusal problem={problemOf(q.error)} title="The movement schedule could not be run" />}
      {r && r.categories.length === 0 && (
        <p className="fy-secondary" role="status">
          No asset was in the register in these months.
        </p>
      )}
      {r && r.categories.length > 0 && (
        <>
          <div className="fy-tablewrap">
            <table className="fy-table fy-table--compact" aria-label="Asset movement">
              <thead>
                <tr>
                  <th scope="col">Asset</th>
                  <th scope="col" className="fy-num">Cost at start</th>
                  <th scope="col" className="fy-num">Additions</th>
                  <th scope="col" className="fy-num">Disposals</th>
                  <th scope="col" className="fy-num">Cost at end</th>
                  <th scope="col" className="fy-num">Depreciation at start</th>
                  <th scope="col" className="fy-num">Charge</th>
                  <th scope="col" className="fy-num">Disposals</th>
                  <th scope="col" className="fy-num">Depreciation at end</th>
                  <th scope="col" className="fy-num">Net book value</th>
                </tr>
              </thead>
              {r.categories.map((c) => (
                <tbody key={c.account_id}>
                  <tr>
                    <th scope="colgroup" colSpan={10} className="fy-strong">
                      {c.account_code} {c.account_name}
                    </th>
                  </tr>
                  {c.assets.map((a) => (
                    <tr key={a.asset_id}>
                      <th scope="row">
                        <span className="fy-mono">{a.number}</span> {a.name}
                      </th>
                      <Cells r={a} />
                    </tr>
                  ))}
                  <tr>
                    <th scope="row">Total {c.account_name}</th>
                    <Cells r={c.total} bold />
                  </tr>
                </tbody>
              ))}
              <tfoot>
                <tr>
                  <th scope="row">All assets, {r.currency}</th>
                  <Cells r={r.total} bold />
                </tr>
              </tfoot>
            </table>
          </div>
          <div className="fy-card fy-card--pad fy-stack fy-stack--2">
            <h2 className="fy-h3">{r.tied ? "The register ties to the ledger" : "The register does not tie to the ledger"}</h2>
            <ul className="fy-list" aria-label="Register against the ledger">
              {r.ties.map((t) => (
                <li key={t.account_code} className="fy-list__item">
                  <span className="fy-list__main">
                    <span className="fy-list__title">
                      {t.account_code} {t.account_name}
                    </span>
                    <span className="fy-list__meta whitespace-normal">
                      Ledger {money(t.ledger)} · register {money(t.register)} · unexplained {money(t.unexplained)}
                    </span>
                  </span>
                </li>
              ))}
            </ul>
          </div>
        </>
      )}
    </section>
  );
}

// CapitalAllowances is a year of assessment's schedule: what each asset claims under its elected method, and the assets listed apart.
export function CapitalAllowances({ onElect }: { onElect?: (assetId: string) => void }) {
  const [ya, setYa] = useState(String(new Date().getFullYear() + 1));
  const valid = /^20[2-9][0-9]$/.test(ya);
  const q = useQuery({
    queryKey: ["fixed-assets", "capital-allowances", ya],
    enabled: valid,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/reports/capital-allowances", { params: { query: { ya: Number(ya) } } });
      if (error) throw error;
      return data;
    },
  });
  const id = useId();
  const r = q.data;
  return (
    <section className="fy-stack fy-stack--3" aria-label="Capital allowances">
      <p className="fy-small fy-secondary">{helpText("fixed-assets.capital-allowances").tooltip}</p>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-ya`}>
          Year of assessment
        </label>
        <input id={`${id}-ya`} className="fy-input fy-num" value={ya} onChange={(e) => setYa(e.target.value)} inputMode="numeric" maxLength={4} aria-describedby={`${id}-ya-help`} />
        <span className="fy-help" id={`${id}-ya-help`}>
          The year after the financial year ends: YA 2027 is the year that ends in 2026.
        </span>
      </div>
      {!valid && <p className="fy-error">Enter a year of assessment, such as 2027.</p>}
      {q.isError && <Refusal problem={problemOf(q.error)} title="The schedule could not be run" />}
      {r && (
        <>
          <p className="fy-secondary">
            YA {r.ya} · claim {r.currency} {money(r.totals.allowance)} · carried forward {money(r.totals.carried_forward)}
            {Number(r.totals.balancing_allowance.amount) !== 0 && <> · balancing allowance {money(r.totals.balancing_allowance)}</>}
            {Number(r.totals.balancing_charge.amount) !== 0 && <> · balancing charge {money(r.totals.balancing_charge)}</>} · rules from <span className="fy-mono">{r.pack}</span>
          </p>
          {r.rows.length === 0 && <p className="fy-secondary">No asset claims in this year.</p>}
          {r.rows.length > 0 && (
            <div className="fy-tablewrap">
              <table className="fy-table fy-table--compact" aria-label="Capital allowances">
                <thead>
                  <tr>
                    <th scope="col">Asset</th>
                    <th scope="col">Method</th>
                    <th scope="col" className="fy-num">Cost</th>
                    <th scope="col" className="fy-num">Brought forward</th>
                    <th scope="col" className="fy-num">Initial</th>
                    <th scope="col" className="fy-num">Annual</th>
                    <th scope="col" className="fy-num">Allowance</th>
                    <th scope="col" className="fy-num">Carried forward</th>
                  </tr>
                </thead>
                <tbody>
                  {r.rows.map((a) => (
                    <tr key={a.asset_id}>
                      <th scope="row">
                        <span className="fy-mono">{a.number}</span> {a.name}
                        <span className="fy-small fy-secondary block">bought {date(a.purchased_on)}</span>
                        {a.sold_on && (
                          <span className="fy-small fy-secondary block">
                            sold {date(a.sold_on)} for {money(a.proceeds)}:{" "}
                            {a.balancing_charge && Number(a.balancing_charge.amount) !== 0 ? `balancing charge ${money(a.balancing_charge)}` : `balancing allowance ${money(a.balancing_allowance)}`}
                          </span>
                        )}
                      </th>
                      <td>
                        {methodText[a.method] ?? a.method}
                        {a.working_life_years ? `, ${a.working_life_years} years` : ""}
                      </td>
                      <td className="fy-num">{money(a.cost)}</td>
                      <td className="fy-num">{money(a.brought_forward)}</td>
                      <td className="fy-num">{money(a.initial_allowance)}</td>
                      <td className="fy-num">{money(a.annual_allowance)}</td>
                      <td className="fy-num fy-strong">{money(a.allowance)}</td>
                      <td className="fy-num">{money(a.carried_forward)}</td>
                    </tr>
                  ))}
                </tbody>
                <tfoot>
                  <tr>
                    <th scope="row" colSpan={3}>
                      Total
                    </th>
                    <td className="fy-num fy-strong">{money(r.totals.brought_forward)}</td>
                    <td className="fy-num fy-strong">{money(r.totals.initial_allowance)}</td>
                    <td className="fy-num fy-strong">{money(r.totals.annual_allowance)}</td>
                    <td className="fy-num fy-strong">{money(r.totals.allowance)}</td>
                    <td className="fy-num fy-strong">{money(r.totals.carried_forward)}</td>
                  </tr>
                </tfoot>
              </table>
            </div>
          )}
          {r.apart.length > 0 && (
            <div className="fy-card fy-card--pad fy-stack fy-stack--2">
              <h2 className="fy-h3">Listed apart: Fyniq does not guess these</h2>
              <ul className="fy-list" aria-label="Assets listed apart">
                {r.apart.map((a) => (
                  <li key={a.asset_id} className="fy-list__item">
                    <span className="fy-list__main">
                      <span className="fy-list__title whitespace-normal">
                        <span className="fy-mono">{a.number}</span> {a.name} · cost {money(a.cost)}
                      </span>
                      <span className="fy-list__meta whitespace-normal">
                        {reasonText[a.reason] ?? a.reason}. {a.fix}
                      </span>
                    </span>
                    {onElect && (a.reason === "no_election" || a.reason === "over_cap" || a.reason === "bad_life") && (
                      <Button size="sm" variant="ghost" onClick={() => onElect(a.asset_id)} aria-label={`Elect a method for ${a.number}`}>
                        Elect a method
                      </Button>
                    )}
                  </li>
                ))}
              </ul>
            </div>
          )}
          <ul className="fy-small fy-secondary fy-list" aria-label="What the schedule does not work out">
            {r.notes.map((n) => (
              <li key={n}>{n}</li>
            ))}
          </ul>
        </>
      )}
    </section>
  );
}

// TaxElectionDialog elects the capital allowance method of an asset: it posts nothing; a section 19 election is irrevocable.
export function TaxElectionDialog({ asset, onClose, onDone }: { asset: FixedAsset; onClose: () => void; onDone: (s: string) => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const had = asset.tax_election;
  const [method, setMethod] = useState<string>(had?.method ?? "s19a1");
  const [prescribed, setPrescribed] = useState(String(had?.prescribed_working_life_years ?? 6));
  const [years, setYears] = useState(String(had?.working_life_years ?? 6));
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);
  const fixed = had?.method === "s19";
  const allowed = prescribed === "16" ? ["6", "12", "16"] : ["6", "12"];
  const save = async () => {
    setBusy(true);
    setProblem(null);
    const body: components["schemas"]["FixedAssetChange"] = {
      tax_election:
        method === "s19"
          ? { method: "s19", prescribed_working_life_years: Number(prescribed), working_life_years: Number(years) }
          : { method: method === "s19a10a" || method === "s19a2" ? method : "s19a1" },
    };
    const out = await guarded(confirm, () =>
      api.POST("/v1/fixed-assets/{asset_id}/change", {
        params: { path: { asset_id: asset.id }, header: { "If-Match": `"${asset.version}-posted"`, "Idempotency-Key": key } },
        body,
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["fixed-assets"] });
    onDone(`${out.data.number} claims its capital allowance under ${methodText[method]?.toLowerCase() ?? method}.`);
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`Capital allowance of ${asset.name}`}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={() => void save()} disabled={busy || fixed}>
            {busy ? "Saving…" : "Elect"}
          </Button>
        </>
      }
    >
      <div className="fy-stack fy-stack--3">
        {problem && <Refusal problem={problem} title="The method was not elected" />}
        <p className="fy-small fy-secondary">{helpText("fixed-assets.tax-election").tooltip}</p>
        {fixed && (
          <p className="fy-small" role="status">
            This asset was elected under section 19 over {had?.working_life_years} years. That election is irrevocable.
          </p>
        )}
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-method`}>
            Method
          </label>
          <select id={`${id}-method`} className="fy-input" value={method} disabled={fixed} onChange={(e) => setMethod(e.target.value)}>
            <option value="s19a1">Section 19A(1): a third a year over 3 years</option>
            <option value="s19a2">Section 19A(2): the whole cost in one year (a computer or prescribed automation equipment)</option>
            <option value="s19a10a">Section 19A(10A): the whole cost in one year (an asset of at most S$5,000)</option>
            <option value="s19">Section 19: 20% at once, the rest over its working life</option>
          </select>
        </div>
        {method === "s19" && (
          <>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-prescribed`}>
                Working life the Sixth Schedule gives it (years)
              </label>
              <select id={`${id}-prescribed`} className="fy-input" value={prescribed} disabled={fixed} onChange={(e) => (setPrescribed(e.target.value), setYears("6"))}>
                <option value="6">6 (such as a motor vehicle) or any life of 12 years or less</option>
                <option value="16">16</option>
              </select>
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-years`}>
                Years to claim over
              </label>
              <select id={`${id}-years`} className="fy-input" value={years} disabled={fixed} onChange={(e) => setYears(e.target.value)}>
                {allowed.map((y) => (
                  <option key={y} value={y}>
                    {y} years
                  </option>
                ))}
              </select>
            </div>
          </>
        )}
      </div>
    </Dialog>
  );
}

export { monthOf };
