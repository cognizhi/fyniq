import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { ChevronRight, ListChecks, Pencil, Plus, TriangleAlert } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { billingNames, hours, keepers, statusNames, type ProjectTask } from "./data";
import { ProjectForm } from "./ProjectForm";
import { ProjectMilestones } from "./ProjectMilestones";
import { ProjectProfitability } from "./ProjectProfitability";
import { ProjectRates } from "./ProjectRates";
import { ProjectRevenue } from "./ProjectRevenue";
import { ProjectUnbilled } from "./ProjectUnbilled";
import { FeatureOff } from "./Projects";

// TaskForm adds a task to a project, or renames one, changes whether it is billable, or puts it out of use (FR-PRJ-05).
function TaskForm({ project, editing, onClose }: { project: string; editing: ProjectTask | "new" | null; onClose: () => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const id = useId();
  const current = editing && editing !== "new" ? editing : null;
  const [name, setName] = useState<string | null>(null);
  const [billable, setBillable] = useState<boolean | null>(null);
  const [active, setActive] = useState<boolean | null>(null);
  const [missing, setMissing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const n = name ?? current?.name ?? "";
  const bill = billable ?? current?.billable ?? true;
  const on = active ?? current?.active ?? true;
  const close = () => {
    setName(null);
    setBillable(null);
    setActive(null);
    setMissing(false);
    setProblem(null);
    onClose();
  };
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!n.trim()) return setMissing(true);
    setBusy(true);
    setProblem(null);
    const out = current
      ? await guarded(confirm, () =>
          api.PUT("/v1/project-tasks/{task_id}", {
            params: { path: { task_id: current.id }, header: { "If-Match": `"${current.version}"` } },
            body: { name: n.trim(), billable: bill, active: on },
          }),
        )
      : await (() => {
          const key = newKey();
          return guarded(confirm, () =>
            api.POST("/v1/project-tasks", { params: { header: { "Idempotency-Key": key } }, body: { project_id: project, name: n.trim(), billable: bill } }),
          );
        })();
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["project-tasks", project] });
    close();
  };
  return (
    <Dialog
      open={editing !== null}
      onOpenChange={(o) => !o && close()}
      dismissible={!busy}
      eyebrow="Tasks"
      title={current ? current.name : "Add a task"}
      description="Time is logged against a task of a project. A task is never deleted: put it out of use instead."
    >
      {problem && <Refusal problem={problem} title="The task was not saved" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-name`}>
            Name
          </label>
          <input
            className="fy-input"
            id={`${id}-name`}
            value={n}
            maxLength={100}
            onChange={(e) => {
              setName(e.target.value);
              setMissing(false);
            }}
            aria-invalid={missing || undefined}
            aria-describedby={missing ? `${id}-name-error` : undefined}
          />
          {missing && (
            <span className="fy-error" id={`${id}-name-error`}>
              Give the task a name, such as Data migration.
            </span>
          )}
        </div>
        <label className="fy-row gap-2">
          <input type="checkbox" checked={bill} onChange={(e) => setBillable(e.target.checked)} />
          Billable: time on it is billed unless an entry says otherwise
        </label>
        {current && (
          <label className="fy-row gap-2">
            <input type="checkbox" checked={on} onChange={(e) => setActive(e.target.checked)} />
            In use: takes new time
          </label>
        )}
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={close} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy}>
            {current ? "Save the task" : "Add the task"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// ProjectRecord is one project (FR-PRJ-01; docs/design/screens ProjectRecord): its customer, billing, dates and budgets,
// its people and which of its managers can approve time, and its tasks. Time, costs, billing and profitability join it
// with their slices.
export function ProjectRecord() {
  const { projectId } = useParams({ strict: false }) as { projectId: string };
  const { session } = useSignedIn();
  const id = useId();
  const [editing, setEditing] = useState(false);
  const [task, setTask] = useState<ProjectTask | "new" | null>(null);
  const may = (session.roles ?? []).some((r) => keepers.includes(r));
  const q = useQuery({
    queryKey: ["project", projectId],
    queryFn: async () => {
      const r = await api.GET("/v1/projects/{project_id}", { params: { path: { project_id: projectId } } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const tasks = useQuery({
    queryKey: ["project-tasks", projectId],
    enabled: q.data?.configured === true,
    queryFn: async () => {
      const r = await api.GET("/v1/projects/{project_id}/tasks", { params: { path: { project_id: projectId }, query: { page_size: 500 } } });
      if (r.error) throw r.error;
      return r.data.items ?? [];
    },
  });
  const p = q.data;
  const off = q.isError && problemOf(q.error).code === "PRJ-09";
  const addTask = helpText("project.task-add");
  const people = helpText("project.people");
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/projects">Projects</Link>
            <Icon of={ChevronRight} size={16} />
          </nav>
          <h1 className="fy-h1">{p?.name ?? "Project"}</h1>
          <p className="fy-secondary">
            {p ? [p.code, p.configured ? statusNames[p.status] : "Not set up", p.customer_name ?? "", p.billing_method ? billingNames[p.billing_method] : ""].filter(Boolean).join(" · ") : ""}
          </p>
        </div>
        {p && may && (
          <div className="fy-pagehead__actions">
            <Button variant="secondary" onClick={() => setEditing(true)}>
              <Icon of={Pencil} />
              {p.configured ? "Change the project" : "Set up the project"}
            </Button>
          </div>
        )}
      </div>
      {off && <FeatureOff />}
      {q.isError && !off && <Refusal problem={problemOf(q.error)} title="The project could not be read" />}
      {p && p.warnings.length > 0 && (
        <div className="fy-banner fy-banner--warning" role="status">
          <Icon of={TriangleAlert} size={20} />
          <div className="fy-banner__text">
            {p.warnings.map((w) => (
              <span key={w} className="fy-small">
                {w}.
              </span>
            ))}
          </div>
        </div>
      )}
      {p?.configured && (
        <>
          <div className="fy-grid fy-grid--4 gap-3" role="group" aria-label="The project at a glance">
            <div className="fy-tile">
              <span className="fy-tile__label">Customer</span>
              <span className="fy-tile__value">{p.customer_name ?? "None"}</span>
              <span className="fy-tile__meta">{p.billing_method ? billingNames[p.billing_method] : ""}</span>
            </div>
            <div className="fy-tile">
              <span className="fy-tile__label">Dates</span>
              <span className="fy-tile__value">{p.starts_on ? date(p.starts_on) : "Not set"}</span>
              <span className="fy-tile__meta">{p.ends_on ? `to ${date(p.ends_on)}` : "No end date"}</span>
            </div>
            <div className="fy-tile">
              <span className="fy-tile__label">Hours budget</span>
              <span className="fy-tile__value fy-num">{p.budget_minutes != null ? hours(p.budget_minutes) : "None"}</span>
              <span className="fy-tile__meta">Use arrives with time entry</span>
            </div>
            <div className="fy-tile">
              <span className="fy-tile__label">Money budget</span>
              <span className="fy-tile__value fy-num">{p.budget_amount ? `${p.currency} ${formatAmount(p.budget_amount)}` : "None"}</span>
              <span className="fy-tile__meta">In {p.currency}</span>
            </div>
          </div>
          <section className="fy-card" aria-labelledby={`${id}-people`}>
            <div className="fy-card__head">
              <h2 className="fy-h3" id={`${id}-people`} title={people.tooltip}>
                {people.label}
              </h2>
            </div>
            {p.people.length === 0 ? (
              <p className="fy-card__body fy-small fy-secondary">Nobody is on this project yet. {may ? "Change the project to name its people." : ""}</p>
            ) : (
              <div className="fy-tablewrap" style={{ border: 0, borderRadius: 0 }}>
                <table className="fy-table fy-table--compact" aria-label="People on the project">
                  <thead>
                    <tr>
                      <th scope="col">Person</th>
                      <th scope="col">On the project as</th>
                      <th scope="col">Approves time</th>
                    </tr>
                  </thead>
                  <tbody>
                    {p.people.map((x) => (
                      <tr key={x.user_id}>
                        <td>{x.display_name}</td>
                        <td>{x.manager ? "Manager" : "Member"}</td>
                        <td>{x.manager ? x.can_approve ? "Yes" : <span className="fy-warning">No: holds no role that approves time</span> : <span className="fy-muted">Not a manager</span>}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>
          <section className="fy-card" aria-labelledby={`${id}-tasks`}>
            <div className="fy-card__head fy-row--wrap gap-3">
              <h2 className="fy-h3" id={`${id}-tasks`}>
                Tasks
              </h2>
              {may && (
                <Button variant="secondary" size="sm" onClick={() => setTask("new")} title={addTask.tooltip}>
                  <Icon of={Plus} />
                  {addTask.label}
                </Button>
              )}
            </div>
            {tasks.isError && <Refusal problem={problemOf(tasks.error)} title="The tasks could not be listed" />}
            {tasks.data && tasks.data.length === 0 && (
              <div className="fy-empty fy-card--pad">
                <Icon of={ListChecks} size={24} />
                <h2 className="fy-h3">No tasks yet</h2>
                <p className="fy-secondary">Time is logged against a task, such as Data migration. {may ? "Add the first." : ""}</p>
                {may && (
                  <Button variant="secondary" size="sm" className="self-center" onClick={() => setTask("new")}>
                    <Icon of={Plus} />
                    {addTask.label}
                  </Button>
                )}
              </div>
            )}
            {tasks.data && tasks.data.length > 0 && (
              <div className="fy-tablewrap" style={{ border: 0, borderRadius: 0 }}>
                <table className="fy-table fy-table--compact" aria-label="Tasks">
                  <thead>
                    <tr>
                      <th scope="col">Task</th>
                      <th scope="col">Billable</th>
                      <th scope="col">Status</th>
                      {may && (
                        <th scope="col">
                          <span className="fy-sr">Actions</span>
                        </th>
                      )}
                    </tr>
                  </thead>
                  <tbody>
                    {tasks.data.map((t) => (
                      <tr key={t.id}>
                        <td>{t.name}</td>
                        <td>{t.billable ? "Yes" : "No"}</td>
                        <td>{t.active ? "In use" : <span className="fy-muted">Out of use</span>}</td>
                        {may && (
                          <td>
                            <Button variant="ghost" size="sm" onClick={() => setTask(t)} aria-label={`Change ${t.name}`}>
                              <Icon of={Pencil} />
                            </Button>
                          </td>
                        )}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>
          <ProjectProfitability project={projectId} startsOn={p.starts_on ?? undefined} />
          <ProjectRates project={projectId} billed={p.billing_method !== "internal"} may={may} />
          {(p.billing_method === "fixed_fee" || p.billing_method === "retainer") && <ProjectMilestones project={projectId} method={p.billing_method} />}
          {p.billing_method === "fixed_fee" && <ProjectRevenue project={projectId} />}
          <ProjectUnbilled project={projectId} />
        </>
      )}
      {p && <ProjectForm editing={editing ? p : null} onClose={() => setEditing(false)} onSaved={() => undefined} />}
      <TaskForm project={projectId} editing={task} onClose={() => setTask(null)} />
    </div>
  );
}
