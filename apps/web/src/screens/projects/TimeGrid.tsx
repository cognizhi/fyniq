import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, CirclePause, CircleSlash, Clock, Pencil, Play, Plus, RotateCcw, Send, Square, TriangleAlert } from "lucide-react";
import { useId, useState } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { hours, minutesOf } from "./data";

type Timesheet = components["schemas"]["Timesheet"];
type Entry = components["schemas"]["TimeEntry"];

const stateIcons = { draft: Pencil, submitted: CirclePause, approved: CircleCheck, returned: CircleSlash } as const;
const stateWords = { draft: "Draft", submitted: "Submitted", approved: "Approved", returned: "Returned" } as const;

// mondayOf is the Monday of the week a day (YYYY-MM-DD) falls in, and shift moves a Monday by whole weeks.
export function mondayOf(day: string): string {
  const d = new Date(`${day}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() - ((d.getUTCDay() + 6) % 7));
  return d.toISOString().slice(0, 10);
}
export function shift(day: string, weeks: number): string {
  const d = new Date(`${day}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + 7 * weeks);
  return d.toISOString().slice(0, 10);
}
export function today(): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Singapore" }).format(new Date());
}

interface Row {
  key: string;
  project_id: string;
  task_id: string;
  project: string;
  task: string;
  billable: boolean;
  entries: Entry[]; // every entry of the row, by day
}

// rowsOf groups a week's entries by project and task: the unit a grid row shows.
export function rowsOf(t: Timesheet, extra: { project_id: string; task_id: string }[]): Row[] {
  const names = new Map(t.projects.flatMap((p) => p.tasks.map((k) => [k.id, { project: `${p.code} · ${p.name}`, task: k.name, billable: k.billable && p.billing_method !== "internal" }] as const)));
  const rows = new Map<string, Row>();
  const add = (project_id: string, task_id: string, billable: boolean) => {
    const key = `${project_id}:${task_id}`;
    if (!rows.has(key)) {
      const n = names.get(task_id);
      const p = t.projects.find((x) => x.id === project_id);
      rows.set(key, { key, project_id, task_id, project: n?.project ?? (p ? `${p.code} · ${p.name}` : "A project you left"), task: n?.task ?? "A task", billable, entries: [] });
    }
    return rows.get(key)!;
  };
  for (const e of t.entries) add(e.project_id, e.task_id, e.billable).entries.push(e);
  for (const x of extra) add(x.project_id, x.task_id, names.get(x.task_id)?.billable ?? false);
  return [...rows.values()];
}

// TimeGrid is a person's own week (FR-PRJ-05; docs/design/screens Timesheet and PersonalTime): the timer, the grid of
// project and task by day in hours and minutes, the days nobody entered time on, saving the week and submitting it to the
// projects' managers, and correcting an approved entry with a reversing one. Time is whole minutes and posts nothing.
export function TimeGrid({ week, onWeek }: { week: string; onWeek: (w: string) => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const id = useId();
  const [edits, setEdits] = useState<Record<string, string>>({});
  const [notes, setNotes] = useState<Record<string, string>>({});
  const [extra, setExtra] = useState<{ project_id: string; task_id: string }[]>([]);
  const [pick, setPick] = useState({ project: "", task: "" });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const [timerPick, setTimerPick] = useState({ project: "", task: "" });

  const q = useQuery({
    queryKey: ["timesheet", week],
    queryFn: async () => {
      const r = await api.GET("/v1/timesheet/{week_start}", { params: { path: { week_start: week } } });
      if (r.error) throw r.error;
      return { sheet: r.data, etag: etagOf(r.response) };
    },
  });
  const sheet = q.data?.sheet;
  const etag = q.data?.etag ?? "";
  const reload = async () => {
    setEdits({});
    setNotes({});
    setExtra([]);
    setErrors({});
    await queries.invalidateQueries({ queryKey: ["timesheet"] });
  };
  const off = q.isError && problemOf(q.error).code === "PRJ-09";
  const g = helpText("time.grid");
  const timer = helpText("time.timer");
  const submit = helpText("time.submit");

  if (off) {
    return (
      <div className="fy-empty fy-card--pad">
        <Icon of={Clock} size={24} />
        <h2 className="fy-h3">Projects and time are switched off</h2>
        <p className="fy-secondary">An Owner or an Administrator turns them on under Settings, Organisation, Features.</p>
        <Link to="/settings/organisation" className="self-center">
          Open the organisation's settings
        </Link>
      </div>
    );
  }
  if (q.isError) return <Refusal problem={problemOf(q.error)} title="The week could not be read" />;
  if (!sheet) return <p className="fy-secondary">Reading the week…</p>;

  const rows = rowsOf(sheet, extra);
  const open = (e: Entry) => (e.status === "draft" || e.status === "returned") && !e.reverses_id;
  const cellKey = (row: Row, i: number) => `${row.key}:${i}`;
  const dayOf = (i: number) => sheet.days[i]!.date;
  const cellEntries = (row: Row, i: number) => row.entries.filter((e) => e.work_date === dayOf(i));
  const lockedMinutes = (row: Row, i: number) => cellEntries(row, i).filter((e) => !open(e)).reduce((n, e) => n + e.minutes, 0);
  const openEntries = (row: Row, i: number) => cellEntries(row, i).filter(open);
  const openMinutes = (row: Row, i: number) => openEntries(row, i).reduce((n, e) => n + e.minutes, 0);
  const shown = (row: Row, i: number) => edits[cellKey(row, i)] ?? (openMinutes(row, i) > 0 ? hoursText(openMinutes(row, i)) : "");
  const rowNote = (row: Row) => notes[row.key] ?? row.entries.find(open)?.note ?? "";
  const dirty = Object.keys(edits).length > 0 || Object.keys(notes).length > 0 || extra.length > 0;

  const save = async (): Promise<boolean> => {
    const errs: Record<string, string> = {};
    const entries: components["schemas"]["TimesheetEntryDraft"][] = [];
    for (const row of rows) {
      for (let i = 0; i < 7; i++) {
        const text = shown(row, i).trim();
        const existing = openEntries(row, i);
        if (text === "") continue;
        const m = minutesOf(text);
        if (m === null || m <= 0 || m > 1440) {
          errs[cellKey(row, i)] = "Write the time as hours, such as 2.25 or 2:15, up to 24 hours.";
          continue;
        }
        entries.push({ id: existing[0]?.id, project_id: row.project_id, task_id: row.task_id, work_date: dayOf(i), minutes: m, billable: row.billable, note: rowNote(row) || undefined });
      }
    }
    setErrors(errs);
    if (Object.keys(errs).length) return false;
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () => api.PUT("/v1/timesheet/{week_start}", { params: { path: { week_start: week }, header: { "If-Match": etag } }, body: { entries } }));
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return false;
    }
    await reload();
    return true;
  };

  const doSubmit = async () => {
    if (dirty && !(await save())) return;
    const fresh = await api.GET("/v1/timesheet/{week_start}", { params: { path: { week_start: week } } });
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.POST("/v1/timesheet/{week_start}/submit", { params: { path: { week_start: week }, header: { "If-Match": etagOf(fresh.response), "Idempotency-Key": key } }, body: {} }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await reload();
  };

  const timerAction = async (action: "start" | "stop" | "drop") => {
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const out = await guarded(confirm, () =>
      action === "start"
        ? api.POST("/v1/timer/start", { params: { header: { "Idempotency-Key": key } }, body: { project_id: timerPick.project, task_id: timerPick.task } })
        : action === "stop"
          ? api.POST("/v1/timer/stop", { params: { header: { "Idempotency-Key": key } } })
          : api.POST("/v1/timer/drop", { params: { header: { "Idempotency-Key": key } } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await reload();
  };

  const correct = async (e: Entry) => {
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const out = await guarded(confirm, () => api.POST("/v1/time-entries/{entry_id}/reverse", { params: { path: { entry_id: e.id }, header: { "Idempotency-Key": key } } }));
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await reload();
  };

  const project = sheet.projects.find((p) => p.id === pick.project);
  const timerProject = sheet.projects.find((p) => p.id === timerPick.project);
  const approvedEntries = sheet.entries.filter((e) => e.status === "approved" && !e.reverses_id && !sheet.entries.some((x) => x.reverses_id === e.id));
  const missing = sheet.days.filter((d) => d.missing);

  return (
    <div className="fy-stack fy-stack--4">
      {problem && <Refusal problem={problem} title="That was not done" />}
      <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-timer`}>
        <h2 className="fy-h3" id={`${id}-timer`} title={timer.tooltip}>
          {timer.label}
        </h2>
        {sheet.timer ? (
          <div className="fy-row fy-row--wrap gap-3">
            <span role="status">
              Running for {hours(sheet.timer.elapsed_minutes)} on {sheet.projects.find((p) => p.id === sheet.timer!.project_id)?.name ?? "a project"}
            </span>
            <Button variant="primary" onClick={() => void timerAction("stop")} disabled={busy}>
              <Icon of={Square} />
              Stop and add the time
            </Button>
            <Button variant="ghost" onClick={() => void timerAction("drop")} disabled={busy}>
              Drop it
            </Button>
          </div>
        ) : (
          <div className="fy-row fy-row--wrap items-end gap-3">
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-tproject`}>
                Project
              </label>
              <select className="fy-select" id={`${id}-tproject`} value={timerPick.project} onChange={(e) => setTimerPick({ project: e.target.value, task: "" })}>
                <option value="">Choose a project</option>
                {sheet.projects.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.code} · {p.name}
                  </option>
                ))}
              </select>
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-ttask`}>
                Task
              </label>
              <select className="fy-select" id={`${id}-ttask`} value={timerPick.task} onChange={(e) => setTimerPick({ ...timerPick, task: e.target.value })} disabled={!timerProject}>
                <option value="">Choose a task</option>
                {(timerProject?.tasks ?? []).map((k) => (
                  <option key={k.id} value={k.id}>
                    {k.name}
                  </option>
                ))}
              </select>
            </div>
            <Button variant="secondary" onClick={() => void timerAction("start")} disabled={busy || !timerPick.task}>
              <Icon of={Play} />
              Start the timer
            </Button>
          </div>
        )}
      </section>

      <section className="fy-card" aria-labelledby={`${id}-grid`}>
        <div className="fy-card__head fy-row--wrap gap-3">
          <h2 className="fy-h3" id={`${id}-grid`} title={g.tooltip}>
            Week of {sheet.week_start}
          </h2>
          <div className="fy-row gap-2">
            <Button variant="ghost" size="sm" onClick={() => onWeek(shift(week, -1))}>
              Earlier week
            </Button>
            <Button variant="ghost" size="sm" onClick={() => onWeek(mondayOf(today()))}>
              This week
            </Button>
            <Button variant="ghost" size="sm" onClick={() => onWeek(shift(week, 1))}>
              Later week
            </Button>
          </div>
        </div>
        {missing.length > 0 && (
          <div className="fy-banner fy-banner--warning" role="status">
            <Icon of={TriangleAlert} size={20} />
            <div className="fy-banner__text">
              <span className="fy-strong">{helpText("time.missing").label}</span>
              <span className="fy-small">No time yet on {missing.map((d) => `${d.weekday} ${d.date.slice(8)}`).join(", ")}.</span>
            </div>
          </div>
        )}
        {sheet.projects.length === 0 && rows.length === 0 ? (
          <div className="fy-empty fy-card--pad">
            <Icon of={Clock} size={24} />
            <h2 className="fy-h3">You are on no project yet</h2>
            <p className="fy-secondary">A manager of a project adds you to it, and then you log time here.</p>
            <Button variant="ghost" size="sm" className="self-center" onClick={() => onWeek(mondayOf(today()))}>
              Back to this week
            </Button>
          </div>
        ) : (
          <div className="fy-tablewrap" style={{ border: 0, borderRadius: 0 }}>
            <table className="fy-table fy-table--compact" aria-label="Your week">
              <thead>
                <tr>
                  <th scope="col">Project and task</th>
                  {sheet.days.map((d) => (
                    <th key={d.date} scope="col" className="fy-num">
                      {d.weekday} {d.date.slice(8)}
                      {d.holiday && <span className="fy-small fy-muted"> · {d.holiday}</span>}
                    </th>
                  ))}
                  <th scope="col">Billable</th>
                  <th scope="col">Note</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => (
                  <tr key={row.key}>
                    <th scope="row">
                      <span className="fy-small fy-muted">{row.project}</span>
                      <br />
                      {row.task}
                    </th>
                    {sheet.days.map((d, i) => {
                      const locked = lockedMinutes(row, i);
                      const k = cellKey(row, i);
                      return (
                        <td key={d.date} className="fy-num">
                          {locked !== 0 && <span className="fy-small">{hours(locked)} locked</span>}
                          <input
                            className="fy-input fy-num max-w-20"
                            aria-label={`${row.task} on ${d.weekday} ${d.date.slice(8)}`}
                            inputMode="decimal"
                            value={shown(row, i)}
                            onChange={(e) => setEdits({ ...edits, [k]: e.target.value })}
                            aria-invalid={errors[k] ? true : undefined}
                            aria-describedby={errors[k] ? `${id}-${k}-error` : undefined}
                          />
                          {errors[k] && (
                            <span className="fy-error" id={`${id}-${k}-error`}>
                              {errors[k]}
                            </span>
                          )}
                        </td>
                      );
                    })}
                    <td>{row.billable ? "Yes" : "No"}</td>
                    <td>
                      <input className="fy-input" aria-label={`Note for ${row.task}`} value={rowNote(row)} maxLength={500} onChange={(e) => setNotes({ ...notes, [row.key]: e.target.value })} />
                    </td>
                  </tr>
                ))}
              </tbody>
              <tfoot>
                <tr>
                  <th scope="row">Total</th>
                  {sheet.days.map((d) => (
                    <td key={d.date} className="fy-num">
                      {d.minutes !== 0 ? hours(d.minutes) : d.missing ? <span className="fy-warning">None</span> : ""}
                    </td>
                  ))}
                  <td colSpan={2} className="fy-num">
                    {hours(sheet.total_minutes)}
                  </td>
                </tr>
              </tfoot>
            </table>
          </div>
        )}
        {sheet.projects.length > 0 && (
          <div className="fy-card__body fy-row fy-row--wrap items-end gap-3">
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-project`}>
                Add a row: project
              </label>
              <select className="fy-select" id={`${id}-project`} value={pick.project} onChange={(e) => setPick({ project: e.target.value, task: "" })}>
                <option value="">Choose a project</option>
                {sheet.projects.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.code} · {p.name}
                  </option>
                ))}
              </select>
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-task`}>
                Task
              </label>
              <select className="fy-select" id={`${id}-task`} value={pick.task} onChange={(e) => setPick({ ...pick, task: e.target.value })} disabled={!project}>
                <option value="">Choose a task</option>
                {(project?.tasks ?? []).map((k) => (
                  <option key={k.id} value={k.id}>
                    {k.name}
                  </option>
                ))}
              </select>
            </div>
            <Button
              variant="secondary"
              disabled={!pick.task}
              onClick={() => {
                setExtra([...extra, { project_id: pick.project, task_id: pick.task }]);
                setPick({ project: pick.project, task: "" });
              }}
            >
              <Icon of={Plus} />
              Add the row
            </Button>
            <span className="fy-grow" />
            <Button variant="secondary" onClick={() => void save()} disabled={busy || !dirty}>
              Save the week
            </Button>
            <Button variant="primary" onClick={() => void doSubmit()} disabled={busy || (sheet.entries.every((e) => !open(e)) && !dirty)} title={submit.tooltip}>
              <Icon of={Send} />
              {submit.label}
            </Button>
          </div>
        )}
      </section>

      {sheet.submissions.length > 0 && (
        <section className="fy-card" aria-labelledby={`${id}-subs`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-subs`}>
              Sent for approval
            </h2>
          </div>
          <ul className="fy-list">
            {sheet.submissions.map((s) => {
              const Mark = stateIcons[s.status];
              return (
                <li key={s.id} className="fy-list__item">
                  <span className="fy-list__main">
                    <span className="fy-list__title">{s.project_name}</span>
                    <span className="fy-list__meta">
                      {hours(s.minutes)}
                      {s.decided_by_name ? ` · ${s.status} by ${s.decided_by_name}` : ""}
                      {s.comment ? ` · “${s.comment}”` : ""}
                    </span>
                  </span>
                  <span className={`fy-chip ${s.status === "approved" ? "fy-chip--good" : s.status === "returned" ? "fy-chip--warning" : "fy-chip--neutral"}`}>
                    <Icon of={Mark} size={16} />
                    {stateWords[s.status]}
                  </span>
                </li>
              );
            })}
          </ul>
        </section>
      )}

      {approvedEntries.length > 0 && (
        <section className="fy-card" aria-labelledby={`${id}-fix`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-fix`} title={helpText("time.correct").tooltip}>
              {helpText("time.correct").label}
            </h2>
          </div>
          <ul className="fy-list">
            {approvedEntries.map((e) => (
              <li key={e.id} className="fy-list__item">
                <span className="fy-list__main">
                  <span className="fy-list__title">
                    {e.work_date} · {hours(e.minutes)}
                  </span>
                  <span className="fy-list__meta">{e.note || "Approved"}</span>
                </span>
                <Button variant="ghost" size="sm" onClick={() => void correct(e)} disabled={busy} aria-label={`Correct the ${hours(e.minutes)} on ${e.work_date}`}>
                  <Icon of={RotateCcw} />
                  Correct it
                </Button>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}

// hoursText writes minutes as the hours a person types: 135 is "2:15".
export function hoursText(minutes: number): string {
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  return `${h}:${String(m).padStart(2, "0")}`;
}
