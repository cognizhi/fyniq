import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChartColumn } from "lucide-react";
import { useId, useState } from "react";
import { api } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { helpText } from "../../i18n";
import { Icon, problemOf } from "../../ui";
import { hours } from "./data";

const basisWords: Record<string, string> = {
  person: "each person's own rate",
  role_average: "the average rate of their role",
  standard: "the standard rate",
  mixed: "more than one rate",
  none: "hours only",
};

function thisMonth(): string {
  return new Date().toISOString().slice(0, 7);
}

// ProjectReports is Reports › Projects (FR-PRJ-12; docs/design/screens ProjectReports): each project's revenue, direct costs, time at cost
// and margin, utilisation and realisation, from the posted ledger and approved time. The cost is the server's, at the viewer's tier, and
// the page says which rate it used. Time posts nothing: it is a management view.
export function ProjectReports() {
  const id = useId();
  const [from, setFrom] = useState(thisMonth());
  const [to, setTo] = useState(thisMonth());
  const h = helpText("reports.projects");
  const q = useQuery({
    queryKey: ["project-report", from, to],
    retry: false,
    queryFn: async () => {
      const last = new Date(Date.UTC(Number(to.slice(0, 4)), Number(to.slice(5, 7)), 0)).toISOString().slice(0, 10);
      const r = await api.GET("/v1/reports/projects", { params: { query: { from: `${from}-01`, to: last } } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const d = q.data;
  const off = q.isError && problemOf(q.error).code === "PRJ-09";
  const money = (v?: string) => (v ? formatAmount(v) : <span className="fy-muted">n/a</span>);
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1" title={h.tooltip}>
            {h.label}
          </h1>
          <p className="fy-secondary">Revenue, direct costs and time at cost per project, from posted ledger lines and approved time.</p>
        </div>
        <div className="fy-pagehead__actions fy-row gap-3">
          <label className="fy-row gap-2 fy-small">
            From
            <input className="fy-input" type="month" value={from} onChange={(e) => setFrom(e.target.value)} />
          </label>
          <label className="fy-row gap-2 fy-small">
            To
            <input className="fy-input" type="month" value={to} onChange={(e) => setTo(e.target.value)} />
          </label>
        </div>
      </div>
      {off && (
        <div className="fy-empty fy-card--pad">
          <Icon of={ChartColumn} size={24} />
          <h2 className="fy-h3">Projects and time are switched off</h2>
          <p className="fy-secondary">An Owner or an Administrator turns them on under Settings, Organisation, Features.</p>
          <Link to="/settings/organisation" className="self-center">
            Open the organisation's settings
          </Link>
        </div>
      )}
      {q.isError && !off && <Refusal problem={problemOf(q.error)} title="The report could not be read" />}
      {d && (
        <>
          {!d.costs_visible && <p className="fy-banner fy-banner--warning" role="status">You may see revenue and hours only.</p>}
          <section className="fy-card" aria-labelledby={`${id}-p`}>
            <div className="fy-card__head">
              <h2 className="fy-h3" id={`${id}-p`}>
                Projects
              </h2>
              <span className="fy-small fy-muted">{d.currency}; margin is revenue less time at cost less direct costs</span>
            </div>
            {d.projects.length === 0 ? (
              <div className="fy-empty fy-card--pad">
                <Icon of={ChartColumn} size={24} />
                <h2 className="fy-h3">No projects to report</h2>
                <p className="fy-secondary">Projects you may see appear here once they have time or tagged lines.</p>
                <Link to="/projects" className="self-center">
                  Open Projects
                </Link>
              </div>
            ) : (
              <div className="fy-tablewrap" style={{ border: 0, borderRadius: 0 }}>
                <table className="fy-table fy-table--compact" aria-label="Project profitability">
                  <thead>
                    <tr>
                      <th scope="col">Project</th>
                      <th scope="col" className="fy-num">Revenue</th>
                      {d.costs_visible && <th scope="col" className="fy-num">Direct costs</th>}
                      {d.costs_visible && <th scope="col" className="fy-num">Time at cost</th>}
                      {d.costs_visible && <th scope="col" className="fy-num">Margin</th>}
                      <th scope="col" className="fy-num">Hours</th>
                      {d.costs_visible && <th scope="col" className="fy-num">Realisation</th>}
                      <th scope="col" className="fy-num">Budget used</th>
                    </tr>
                  </thead>
                  <tbody>
                    {d.projects.map((p) => (
                      <tr key={p.project_id}>
                        <th scope="row">
                          <Link to="/projects/$projectId" params={{ projectId: p.project_id }}>
                            {p.code} · {p.name}
                          </Link>
                          {d.costs_visible && <span className="fy-small fy-muted"> at {basisWords[p.rate_basis] ?? p.rate_basis}</span>}
                          {d.costs_visible && p.payroll_tagged && p.payroll_tagged !== "0.00" && <span className="fy-small fy-muted"> · {formatAmount(p.payroll_tagged)} of salary tagged, listed apart</span>}
                        </th>
                        <td className="fy-num">{money(p.revenue)}</td>
                        {d.costs_visible && <td className="fy-num">{money(p.direct_costs)}</td>}
                        {d.costs_visible && <td className="fy-num">{money(p.time_cost)}</td>}
                        {d.costs_visible && <td className="fy-num">{money(p.margin)}</td>}
                        <td className="fy-num">{hours(p.minutes)}</td>
                        {d.costs_visible && <td className="fy-num">{p.realisation ? `${p.realisation}%` : <span className="fy-muted">n/a</span>}</td>}
                        <td className="fy-num">{p.budget_used ? `${p.budget_used}%` : <span className="fy-muted">n/a</span>}</td>
                      </tr>
                    ))}
                    {d.untagged && (
                      <tr>
                        <th scope="row">{d.untagged.name}</th>
                        <td className="fy-num">{money(d.untagged.revenue)}</td>
                        {d.costs_visible && <td className="fy-num">{money(d.untagged.direct_costs)}</td>}
                        {d.costs_visible && <td />}
                        {d.costs_visible && <td />}
                        <td />
                        {d.costs_visible && <td />}
                        <td />
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            )}
          </section>
          {d.people.length > 0 && (
            <section className="fy-card" aria-labelledby={`${id}-u`}>
              <div className="fy-card__head">
                <h2 className="fy-h3" id={`${id}-u`}>
                  Utilisation
                </h2>
                <span className="fy-small fy-muted">Billable over available hours: weekdays less public holidays, at 8 hours</span>
              </div>
              <table className="fy-table fy-table--compact" aria-label="Utilisation by person">
                <thead>
                  <tr>
                    <th scope="col">Person</th>
                    <th scope="col" className="fy-num">Hours</th>
                    <th scope="col" className="fy-num">Billable</th>
                    <th scope="col" className="fy-num">Utilisation</th>
                    {d.people.some((x) => x.cost) && <th scope="col" className="fy-num">Cost</th>}
                  </tr>
                </thead>
                <tbody>
                  {d.people.map((x) => (
                    <tr key={x.user_id}>
                      <th scope="row">{x.person_name}</th>
                      <td className="fy-num">{hours(x.minutes)}</td>
                      <td className="fy-num">{hours(x.billable_minutes)}</td>
                      <td className="fy-num">{x.utilisation ? `${x.utilisation}%` : "n/a"}</td>
                      {d.people.some((y) => y.cost) && <td className="fy-num">{x.cost ? formatAmount(x.cost) : ""}</td>}
                    </tr>
                  ))}
                </tbody>
              </table>
            </section>
          )}
          <Button variant="ghost" size="sm" className="self-start" onClick={() => void q.refetch()}>
            Read again
          </Button>
        </>
      )}
    </div>
  );
}
