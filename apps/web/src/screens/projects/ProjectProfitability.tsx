import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { FileDown, TriangleAlert } from "lucide-react";
import { useId, useState } from "react";
import { api } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { Icon, problemOf } from "../../ui";
import { save } from "../reports/AccountantExports";
import { basisWords, hours, monthsBack } from "./data";

// ProjectProfitability is the project's own profitability, budget burn and approved time over its last twelve months or since it
// started (FR-PRJ-12; docs/design/screens ProjectRecord): the figures are the project report's row for this project, at the viewer's
// tier, so a viewer who may see hours and revenue only sees those, the basis of any time cost is named, and a small group is warned
// of (FR-PRJ-06). The approved entries behind the hours are listed on request: hours only, never a rate or a cost. A viewer with no
// right to project reports, or while Projects and time are off, sees nothing here.
export function ProjectProfitability({ project, startsOn }: { project: string; startsOn?: string }) {
  const id = useId();
  const [drill, setDrill] = useState(false);
  const [exporting, setExporting] = useState(false);
  const [exportProblem, setExportProblem] = useState<ReturnType<typeof problemOf> | null>(null);
  const now = new Date();
  const earliest = monthsBack(now, 11);
  const from = startsOn && startsOn.slice(0, 7) > earliest ? startsOn.slice(0, 7) : earliest;
  const to = monthsBack(now, 0);
  const last = new Date(Date.UTC(Number(to.slice(0, 4)), Number(to.slice(5, 7)), 0)).toISOString().slice(0, 10);
  const q = useQuery({
    queryKey: ["project-profitability", project, from, to],
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/reports/projects", { params: { query: { from: `${from}-01`, to: last } } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const entries = useQuery({
    queryKey: ["project-time-entries", project, from, to],
    enabled: drill,
    queryFn: async () => {
      const r = await api.GET("/v1/projects/{project_id}/time-entries", { params: { path: { project_id: project }, query: { from: `${from}-01`, to: last, page_size: 200 } } });
      if (r.error) throw r.error;
      return r.data.items ?? [];
    },
  });
  const h = helpText("project.profitability");
  if (q.isError) return null; // no right to project reports, or the module is off: the record shows nothing here
  const d = q.data;
  const row = d?.projects.find((p) => p.project_id === project);
  if (!d || !row) return null;
  const money = (v?: string) => (v ? `${d.currency} ${formatAmount(v)}` : <span className="fy-muted">n/a</span>);
  const small = d.costs_visible && (row.rate_basis === "standard" || row.rate_basis === "none");
  const exportCsv = async () => {
    setExporting(true);
    setExportProblem(null);
    const r = await api.GET("/v1/reports/projects/export", { params: { query: { from: `${from}-01`, to: last, format: "csv" } } });
    setExporting(false);
    if (r.error) return setExportProblem(problemOf(r.error));
    save(r.data.file_name, r.data.media_type, r.data.content_base64);
  };
  return (
    <section className="fy-card" aria-labelledby={`${id}-profit`}>
      <div className="fy-card__head fy-row--wrap gap-3">
        <h2 className="fy-h3" id={`${id}-profit`} title={h.tooltip}>
          {h.label}
        </h2>
        <span className="fy-small fy-muted">
          {date(`${from}-01`)} to {date(last)} · {d.currency}
        </span>
        <Button variant="ghost" size="sm" onClick={exportCsv} disabled={exporting}>
          <Icon of={FileDown} />
          Export CSV
        </Button>
      </div>
      {exportProblem && <Refusal problem={exportProblem} title="The export could not be made" />}
      <div className="fy-card__body">
        <div className="fy-grid fy-grid--4 gap-3" role="group" aria-label="The project's profitability">
          <div className="fy-tile">
            <span className="fy-tile__label">Revenue</span>
            <span className="fy-tile__value fy-num">{money(row.revenue)}</span>
            <span className="fy-tile__meta">Posted lines tagged to the project</span>
          </div>
          {d.costs_visible && (
            <div className="fy-tile">
              <span className="fy-tile__label">Direct costs</span>
              <span className="fy-tile__value fy-num">{money(row.direct_costs)}</span>
              <span className="fy-tile__meta">Without salary tagged to it</span>
            </div>
          )}
          {d.costs_visible && (
            <div className="fy-tile">
              <span className="fy-tile__label">Margin</span>
              <span className="fy-tile__value fy-num">{money(row.margin)}</span>
              <span className="fy-tile__meta">
                <span className="fy-chip">Time at {basisWords[row.rate_basis] ?? row.rate_basis}</span>
              </span>
            </div>
          )}
          <div className="fy-tile">
            <span className="fy-tile__label">Approved time</span>
            <span className="fy-tile__value fy-num">{hours(row.minutes)}</span>
            <span className="fy-tile__meta">
              {row.budget_used ? `${row.budget_used}% of the hours budget` : "No hours budget"}
              {row.realisation && d.costs_visible ? ` · realisation ${row.realisation}%` : ""}
            </span>
          </div>
        </div>
        {!d.costs_visible && <p className="fy-small fy-secondary">You may see revenue and hours only.</p>}
        {small && (
          <div className="fy-banner fy-banner--warning" role="status">
            <Icon of={TriangleAlert} size={20} />
            <div className="fy-banner__text">
              <span className="fy-small">
                {row.rate_basis === "none"
                  ? "No rate applies to this time, so no cost or margin is shown."
                  : "Fewer than three people with a rate hold each role, so time is costed at the standard rate, never at one person's own."}
              </span>
            </div>
          </div>
        )}
        <div className="fy-row gap-3">
          <Button variant="ghost" size="sm" onClick={() => setDrill((v) => !v)} aria-expanded={drill}>
            {drill ? "Hide the approved time" : "Show the approved time"}
          </Button>
          <Link to="/reports/projects" className="fy-small">
            All projects
          </Link>
        </div>
        {drill && entries.isError && <Refusal problem={problemOf(entries.error)} title="The time could not be read" />}
        {drill && entries.data && (
          <div className="fy-tablewrap" style={{ border: 0, borderRadius: 0 }}>
            <table className="fy-table fy-table--compact" aria-label="Approved time on the project">
              <thead>
                <tr>
                  <th scope="col">Day</th>
                  <th scope="col">What was done</th>
                  <th scope="col" className="fy-num">
                    Hours
                  </th>
                  <th scope="col">Billable</th>
                </tr>
              </thead>
              <tbody>
                {entries.data.length === 0 && (
                  <tr>
                    <td colSpan={4} className="fy-muted">
                      No approved time in these months.
                    </td>
                  </tr>
                )}
                {entries.data.map((e) => (
                  <tr key={e.id}>
                    <td>{date(e.work_date)}</td>
                    <td>{e.note ?? ""}</td>
                    <td className="fy-num">{hours(e.minutes)}</td>
                    <td>{e.billable ? "Yes" : "No"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </section>
  );
}
