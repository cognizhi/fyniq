import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { Info } from "lucide-react";
import { useId, useMemo, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Chart } from "../../components/chart/Chart";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { Icon, problemOf } from "../../ui";
import { listPeriods } from "../accounting/data";
import { presetNames, runOf, type Preset } from "./Statement";

type Report = components["schemas"]["ManagementReport"];
export type ManagementKind = "sales-by-customer" | "sales-by-item" | "spend-by-supplier" | "top-expenses";

// reports are the management reports (docs/design/screens ManagementReport; FR-RPT-03): each with its name, the column
// its rows are, and what it is, in plain words.
export const reports: Record<ManagementKind, { name: string; row: string; about: string }> = {
  "sales-by-customer": { name: "Sales by customer", row: "Customer", about: "What each customer was invoiced, less credit notes, net of GST." },
  "sales-by-item": { name: "Sales by item", row: "Item", about: "What each item sold, and how many, less credit notes, net of GST." },
  "spend-by-supplier": { name: "Spend by supplier", row: "Supplier", about: "What each supplier billed, less their credit notes, net of the GST you can claim." },
  "top-expenses": { name: "Top expenses", row: "Account", about: "The ten largest expense accounts in the P&L, and the rest as one line." },
};

const kinds = Object.keys(reports) as ManagementKind[];

// ManagementReport is Reports › Management (docs/design/screens ManagementReport; FR-RPT-03, FR-RPT-08): sales by
// customer and by item, spend by supplier and the top expenses for a run of months, each row with its share of the
// total, read at one ledger revision. Who owes us and whom we owe live with sales and purchases; an expense opens its
// general ledger lines (FR-UX-04). The top expenses are also drawn as bars (p1-reports 5.3).
export function ManagementReport() {
  const id = useId();
  const { report } = useParams({ strict: false }) as { report: ManagementKind };
  const about = reports[report] ?? reports["sales-by-customer"];
  const [preset, setPreset] = useState<Preset>("month");
  const [custom, setCustom] = useState({ from: "", to: "" });
  const periods = useQuery({ queryKey: ["periods"], queryFn: listPeriods });
  const run = useMemo(() => runOf(preset, periods.data?.items ?? [], custom), [preset, periods.data, custom]);
  const data = useQuery({
    queryKey: ["management", report, run],
    enabled: run !== null,
    retry: false,
    queryFn: async () => {
      const query = { from: run!.from, to: run!.to };
      const r =
        report === "sales-by-item"
          ? await api.GET("/v1/reports/sales-by-item", { params: { query } })
          : report === "spend-by-supplier"
            ? await api.GET("/v1/reports/spend-by-supplier", { params: { query } })
            : report === "top-expenses"
              ? await api.GET("/v1/reports/top-expenses", { params: { query } })
              : await api.GET("/v1/reports/sales-by-customer", { params: { query } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const s = data.data;
  const h = helpText("reports.management");
  const items = report === "sales-by-item";
  const documents = report !== "top-expenses";

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/reports">Reports</Link>
          </nav>
          <h1 className="fy-h1">{about.name}</h1>
          <p className="fy-secondary">
            {about.about}
            {s ? ` ${date(s.from)} to ${date(s.to)} · ${s.currency}` : ""}
          </p>
        </div>
      </div>
      <nav className="fy-tabs" aria-label="Management reports">
        <Link to="/sales/statements">Who owes us</Link>
        <Link to="/purchases/bills">Whom we owe</Link>
        {kinds.map((k) => (
          <Link key={k} to="/reports/management/$report" params={{ report: k }} aria-current={k === report ? "page" : undefined}>
            {reports[k].name}
          </Link>
        ))}
      </nav>
      <ReviewBanner screen="management-report" />
      <div className="fy-card fy-card--pad fy-row fy-row--wrap gap-3">
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-period`}>
            Period
          </label>
          <select className="fy-select" id={`${id}-period`} value={preset} onChange={(e) => setPreset(e.target.value as Preset)}>
            {(Object.keys(presetNames) as Preset[]).map((p) => (
              <option key={p} value={p}>
                {presetNames[p]}
              </option>
            ))}
          </select>
        </div>
        {preset === "custom" && (
          <>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-from`}>
                From
              </label>
              <input className="fy-input" id={`${id}-from`} type="month" value={custom.from} onChange={(e) => setCustom({ ...custom, from: e.target.value })} />
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-to`}>
                To
              </label>
              <input className="fy-input" id={`${id}-to`} type="month" value={custom.to} onChange={(e) => setCustom({ ...custom, to: e.target.value })} />
            </div>
          </>
        )}
      </div>
      {data.error && <Refusal problem={problemOf(data.error)} title={`The ${about.name.toLowerCase()} could not be shown`} />}
      {data.isPending && run !== null && <div className="fy-skeleton h-40" aria-hidden="true" />}
      {s && s.rows.length === 0 && <p className="fy-secondary">Nothing in these months.</p>}
      {s && report === "top-expenses" && s.rows.length > 0 && (
        <Chart
          title="The largest expenses"
          summary={`${s.rows[0]!.name} is the largest expense, ${formatAmount(s.rows[0]!.amount.amount)} ${s.currency}${s.rows[0]!.share ? `, ${s.rows[0]!.share}% of the total` : ""}.`}
          form="bar-horizontal"
          label="Account"
          categories={s.rows.map((r) => r.name)}
          series={[{ key: "expenses", name: "Expenses", values: s.rows.map((r) => r.amount.amount) }]}
          unit={s.currency}
          file={`top-expenses-${s.from.slice(0, 7)}-to-${s.to.slice(0, 7)}`}
          height={Math.max(160, 36 * s.rows.length + 48)}
        />
      )}
      {s && s.rows.length > 0 && (
        <div className="fy-card">
          <div className="fy-tablewrap">
            <table className="fy-table fy-table--compact">
              <caption className="fy-sr">
                {about.name}, {date(s.from)} to {date(s.to)}, {s.currency}
              </caption>
              <thead>
                <tr>
                  <th scope="col">{about.row}</th>
                  {items && (
                    <th scope="col" className="fy-num">
                      Quantity
                    </th>
                  )}
                  {documents && (
                    <th scope="col" className="fy-num fy-expert-only">
                      Documents
                    </th>
                  )}
                  <th scope="col" className="fy-num">
                    Amount {s.currency}
                  </th>
                  <th scope="col" className="fy-num">
                    Share
                  </th>
                </tr>
              </thead>
              <tbody>
                {s.rows.map((r, i) => (
                  <Row key={r.id ?? `rest-${i}`} r={r} s={s} items={items} documents={documents} />
                ))}
              </tbody>
              <tfoot>
                <tr className="fy-strong">
                  <th scope="row">Total</th>
                  {items && <td />}
                  {documents && <td className="fy-expert-only" />}
                  <td className="fy-num">{formatAmount(s.total.amount)}</td>
                  <td className="fy-num">{/^-?0(\.0*)?$/.test(s.total.amount) ? "" : "100.0%"}</td>
                </tr>
              </tfoot>
            </table>
          </div>
        </div>
      )}
      {s && (
        <p className="fy-small fy-muted fy-row gap-1.5">
          <Icon of={Info} />
          {report === "top-expenses"
            ? `The P&L's figures, read at ledger revision ${s.ledger_revision}: they include what was paid at the bank and claimed.`
            : `Read at ledger revision ${s.ledger_revision}. Money received or paid at the bank without an invoice or bill, and expense claims, are in the P&L and the top expenses, not here.`}
        </p>
      )}
      <p className="fy-small fy-secondary">{h.tooltip}</p>
    </div>
  );
}

function Row({ r, s, items, documents }: { r: Report["rows"][number]; s: Report; items: boolean; documents: boolean }) {
  const name =
    s.report === "top_expenses" && r.id ? (
      <Link to="/reports/general-ledger" search={{ account_id: [r.id], from: s.from, to: s.to }}>
        {r.name}
      </Link>
    ) : (
      r.name
    );
  return (
    <tr>
      <td>
        {r.code && <span className="fy-mono fy-muted fy-expert-only">{r.code} </span>}
        {r.id ? name : <span className="fy-muted">{name}</span>}
      </td>
      {items && <td className="fy-num">{r.quantity ?? ""}</td>}
      {documents && <td className="fy-num fy-expert-only">{r.documents}</td>}
      <td className="fy-num">{formatAmount(r.amount.amount)}</td>
      <td className="fy-num fy-muted">{r.share ? `${r.share}%` : ""}</td>
    </tr>
  );
}
