import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { BriefcaseBusiness, CalendarClock, CircleCheck, CirclePause, CircleSlash, Plus, TriangleAlert, ToggleLeft, type LucideIcon } from "lucide-react";
import { useId, useState } from "react";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf } from "../../ui";
import { billingNames, hours, keepers, listProjects, statusNames, type Project } from "./data";
import { ProjectForm } from "./ProjectForm";

const tones: Record<Project["status"], string> = { planned: "fy-chip--neutral", active: "fy-chip--good", on_hold: "fy-chip--warning", closed: "fy-chip--neutral" };
const marks: Record<Project["status"], LucideIcon> = { planned: CalendarClock, active: CircleCheck, on_hold: CirclePause, closed: CircleSlash };

// FeatureOff is what a page of Projects and time says when the organisation has them switched off (FR-ORG-06): the
// navigation hides them, and the API refuses them (PRJ-09), so a person who follows an old link is told why.
export function FeatureOff() {
  return (
    <div className="fy-empty fy-card--pad">
      <Icon of={ToggleLeft} size={24} />
      <h2 className="fy-h3">Projects and time are switched off</h2>
      <p className="fy-secondary">An Owner or an Administrator turns them on under Settings, Organisation, Features.</p>
      <Link to="/settings/organisation" className="self-center">
        Open the organisation's settings
      </Link>
    </div>
  );
}

// Projects is the list of every project the person may see (FR-PRJ-01; docs/design/screens Projects): its customer,
// how it is billed, its status, its people and its budget. Time, cost and profitability join it with their slices.
export function Projects() {
  const { session } = useSignedIn();
  const id = useId();
  const go = useNavigate();
  const [adding, setAdding] = useState(false);
  const [status, setStatus] = useState<"" | Project["status"]>("");
  const may = (session.roles ?? []).some((r) => keepers.includes(r));
  const q = useQuery({ queryKey: ["projects"], queryFn: listProjects });
  const off = q.isError && problemOf(q.error).code === "PRJ-09";
  const items = (q.data ?? []).filter((p) => status === "" || p.status === status);
  const add = helpText("project.new");
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">Projects</h1>
          <p className="fy-secondary">The work you track: its customer, how it is billed, who logs time on it and who approves that time.</p>
        </div>
        {may && !off && (
          <div className="fy-pagehead__actions">
            <Button variant="primary" onClick={() => setAdding(true)} title={add.tooltip}>
              <Icon of={Plus} />
              {add.label}
            </Button>
          </div>
        )}
      </div>
      {off && <FeatureOff />}
      {q.isError && !off && <Refusal problem={problemOf(q.error)} title="The projects could not be listed" />}
      {q.data && (
        <section className="fy-card" aria-labelledby={`${id}-list`}>
          <div className="fy-card__head fy-row--wrap gap-3">
            <h2 className="fy-h3" id={`${id}-list`}>
              {items.length === 1 ? "1 project" : `${items.length} projects`}
            </h2>
            <label className="fy-row gap-2 fy-small">
              Status
              <select className="fy-select" value={status} onChange={(e) => setStatus(e.target.value as typeof status)}>
                <option value="">Every status</option>
                {Object.entries(statusNames).map(([k, v]) => (
                  <option key={k} value={k}>
                    {v}
                  </option>
                ))}
              </select>
            </label>
          </div>
          {items.length === 0 ? (
            <div className="fy-empty fy-card--pad">
              <Icon of={BriefcaseBusiness} size={24} />
              <h2 className="fy-h3">{q.data.length === 0 ? "No projects yet" : "No project with that status"}</h2>
              <p className="fy-secondary">
                {q.data.length === 0
                  ? may
                    ? "Add a project to log time on it and to see what it earns and costs."
                    : "Projects you are on will show here."
                  : "Choose another status, or every status."}
              </p>
              {may && q.data.length === 0 && (
                <Button variant="secondary" size="sm" className="self-center" onClick={() => setAdding(true)}>
                  <Icon of={Plus} />
                  {add.label}
                </Button>
              )}
            </div>
          ) : (
            <div className="fy-tablewrap" style={{ border: 0, borderRadius: 0 }}>
              <table className="fy-table fy-table--cards" aria-label="Projects">
                <thead>
                  <tr>
                    <th scope="col">Project</th>
                    <th scope="col">Customer</th>
                    <th scope="col">Billing</th>
                    <th scope="col">Status</th>
                    <th scope="col" className="fy-num">
                      Hours budget
                    </th>
                    <th scope="col" className="fy-num fy-from-medium">
                      Money budget
                    </th>
                    <th scope="col" className="fy-from-medium">
                      Dates
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((p) => (
                    <tr key={p.id}>
                      <td data-card="title">
                        <Link to="/projects/$projectId" params={{ projectId: p.id }}>
                          <span className="fy-acct">{p.code}</span>
                          {p.name}
                        </Link>
                      </td>
                      <td data-card="meta">{p.customer_name ?? <span className="fy-muted">{p.billing_method === "internal" ? "Internal" : "None yet"}</span>}</td>
                      <td data-card="meta">{p.billing_method ? billingNames[p.billing_method] : <span className="fy-muted">None yet</span>}</td>
                      <td data-card="meta">
                        {p.configured ? (
                          <span className={`fy-chip ${tones[p.status]}`}>
                            <Icon of={marks[p.status]} size={16} />
                            {statusNames[p.status]}
                          </span>
                        ) : (
                          <span className="fy-chip fy-chip--warning">
                            <Icon of={TriangleAlert} size={16} />
                            Not set up
                          </span>
                        )}
                      </td>
                      <td data-card="amount" className="fy-num">
                        {p.budget_minutes != null ? hours(p.budget_minutes) : <span className="fy-muted">No budget</span>}
                      </td>
                      <td data-card="meta" className="fy-num fy-from-medium">
                        {p.budget_amount ? `${p.currency} ${formatAmount(p.budget_amount)}` : <span className="fy-muted">No budget</span>}
                      </td>
                      <td data-card="meta" className="fy-from-medium">
                        {p.starts_on || p.ends_on ? `${p.starts_on ? date(p.starts_on) : "…"} to ${p.ends_on ? date(p.ends_on) : "…"}` : <span className="fy-muted">No dates</span>}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      )}
      <ProjectForm
        editing={adding ? "new" : null}
        onClose={() => setAdding(false)}
        onSaved={(_, projectId) => void go({ to: "/projects/$projectId", params: { projectId } })}
      />
    </div>
  );
}
