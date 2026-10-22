import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, CircleCheck, CircleDashed, CircleMinus, ListChecks, Plus, RotateCcw, Trash2 } from "lucide-react";
import { useId, useState } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date, dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { who } from "./Journals";

type PeriodChecklist = components["schemas"]["PeriodChecklist"];
type Item = components["schemas"]["ChecklistItem"];
type TaskInput = components["schemas"]["ChecklistTaskInput"];
type OwnerRole = NonNullable<TaskInput["owner_role"]>;

const roleNames: [OwnerRole, string][] = [
  ["accountant", "Accountant"],
  ["controller", "Controller"],
  ["owner", "Owner"],
  ["bookkeeper", "Bookkeeper"],
  ["ar_specialist", "AR specialist"],
  ["ap_specialist", "AP specialist"],
  ["payroll_administrator", "Payroll administrator"],
  ["tax_agent", "Tax agent"],
];
const roleName = (r: string | undefined) => roleNames.find(([k]) => k === r)?.[1];

// Skip asks why a task does not apply this month: the attester reads it, and the month keeps it.
function Skip({ item, onClose, onSkip, busy }: { item: Item; onClose: () => void; onSkip: (reason: string) => void; busy: boolean }) {
  const id = useId();
  const [reason, setReason] = useState("");
  const [missing, setMissing] = useState(false);
  const h = helpText("close.skip-task");
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title={`Skip "${item.title}" this month?`}
      description="The task counts as dealt with for this month only. Say why it does not apply: the founder who attests the close reads it."
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => (reason.trim() ? onSkip(reason.trim()) : setMissing(true))} disabled={busy} title={h.tooltip}>
            <Icon of={CircleMinus} />
            {h.label}
          </Button>
        </>
      }
    >
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-reason`}>
          Why it does not apply this month
        </label>
        <textarea
          className="fy-textarea"
          id={`${id}-reason`}
          value={reason}
          maxLength={1000}
          onChange={(e) => {
            setReason(e.target.value);
            setMissing(false);
          }}
          aria-invalid={missing || undefined}
          aria-describedby={missing ? `${id}-reason-error` : undefined}
        />
        {missing && (
          <span className="fy-error" id={`${id}-reason-error`}>
            Say why the task does not apply this month.
          </span>
        )}
      </div>
    </Dialog>
  );
}

interface Row extends TaskInput {
  id: string; // the row's own key while it is edited
}

// Template edits the checklist template: every task in order, each with its owner and due day. A task keeps its key, so
// the months already marked keep their marks; a new one gets a key from its title.
function Template({ onClose, onSaved }: { onClose: () => void; onSaved: (version: number) => void }) {
  const confirm = useConfirm();
  const id = useId();
  const [rows, setRows] = useState<Row[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText("close.edit-template");
  const q = useQuery({
    queryKey: ["close-checklist-template"],
    queryFn: async () => {
      const r = await api.GET("/v1/close-checklist-template");
      if (r.error) throw r.error;
      return { template: r.data, etag: etagOf(r.response) };
    },
  });
  const list = rows ?? (q.data?.template.tasks ?? []).map((t) => ({ ...t, id: t.key }));
  const set = (i: number, patch: Partial<Row>) => setRows(list.map((r, j) => (j === i ? { ...r, ...patch } : r)));
  const move = (i: number, by: number) => {
    const next = [...list];
    const [r] = next.splice(i, 1);
    next.splice(i + by, 0, r!);
    setRows(next);
  };
  const save = async () => {
    if (!q.data) return;
    setBusy(true);
    setProblem(null);
    const etag = q.data.etag;
    const tasks = list.map(({ id: _id, ...t }) => ({ ...t, detail: t.detail?.trim() || undefined, owner_role: t.owner_role || undefined }));
    const out = await guarded(confirm, () => api.PUT("/v1/close-checklist-template", { params: { header: { "If-Match": etag } }, body: { tasks } }));
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onSaved(out.data.version);
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title="The close checklist template"
      description={
        q.data?.template.recommended
          ? "Every month's checklist has these tasks. This is the recommended list: change it to your own."
          : `Every month's checklist has these tasks: version ${q.data?.template.version ?? ""}${q.data?.template.written_by ? `, saved by ${who(q.data.template.written_by)}` : ""}.`
      }
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => void save()} disabled={busy || !q.data || list.length === 0} title={h.tooltip}>
            {h.label}
          </Button>
        </>
      }
    >
      {q.isError && <Refusal problem={problemOf(q.error)} title="The template could not be shown" />}
      {problem && <Refusal problem={problem} title="The template was not saved" />}
      <ol className="fy-stack fy-stack--4" aria-label="Tasks">
        {list.map((r, i) => (
          <li key={r.id} className="fy-card fy-card--pad fy-stack fy-stack--2">
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-${r.id}-title`}>
                Task {i + 1}
              </label>
              <input className="fy-input" id={`${id}-${r.id}-title`} value={r.title} maxLength={200} onChange={(e) => set(i, { title: e.target.value })} />
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-${r.id}-detail`}>
                What it means (optional)
              </label>
              <input className="fy-input" id={`${id}-${r.id}-detail`} value={r.detail ?? ""} maxLength={500} onChange={(e) => set(i, { detail: e.target.value })} />
            </div>
            <div className="fy-row fy-row--wrap gap-3">
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-${r.id}-owner`}>
                  Usually done by
                </label>
                <select className="fy-select" id={`${id}-${r.id}-owner`} value={r.owner_role ?? ""} onChange={(e) => set(i, { owner_role: (e.target.value || undefined) as OwnerRole | undefined })}>
                  <option value="">Anyone</option>
                  {roleNames.map(([k, label]) => (
                    <option key={k} value={k}>
                      {label}
                    </option>
                  ))}
                </select>
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-${r.id}-due`}>
                  Due, days after the month's end
                </label>
                <input
                  className="fy-input w-24"
                  id={`${id}-${r.id}-due`}
                  type="number"
                  inputMode="numeric"
                  min={0}
                  max={60}
                  value={r.due_days ?? 0}
                  onChange={(e) => set(i, { due_days: Math.max(0, Math.min(60, Math.trunc(Number(e.target.value) || 0))) })}
                />
              </div>
            </div>
            <div className="fy-row gap-2">
              <Button variant="ghost" size="sm" onClick={() => move(i, -1)} disabled={i === 0} aria-label={`Move task ${i + 1} up`}>
                <Icon of={ArrowUp} />
              </Button>
              <Button variant="ghost" size="sm" onClick={() => move(i, 1)} disabled={i === list.length - 1} aria-label={`Move task ${i + 1} down`}>
                <Icon of={ArrowDown} />
              </Button>
              <Button variant="ghost" size="sm" onClick={() => setRows(list.filter((_, j) => j !== i))} aria-label={`Remove task ${i + 1}`}>
                <Icon of={Trash2} />
              </Button>
            </div>
          </li>
        ))}
      </ol>
      <Button className="self-start" variant="secondary" size="sm" onClick={() => setRows([...list, { id: newKey(), title: "", due_days: 10 }])} disabled={list.length >= 40}>
        <Icon of={Plus} />
        Add a task
      </Button>
    </Dialog>
  );
}

function StateIcon({ item }: { item: Item }) {
  if (item.state === "done")
    return (
      <span className="fy-list__icon fy-list__icon--good">
        <Icon of={CircleCheck} size={20} />
      </span>
    );
  if (item.state === "skipped")
    return (
      <span className="fy-list__icon">
        <Icon of={CircleMinus} size={20} />
      </span>
    );
  return (
    <span className="fy-list__icon">
      <Icon of={CircleDashed} size={20} />
    </span>
  );
}

function meta(it: Item): string {
  if (it.state === "done") return [`Done by ${who(it.by)}${it.at ? ` on ${dateTime(it.at)}` : ""}`, it.note].filter(Boolean).join(" · ");
  if (it.state === "skipped") return `Skipped by ${who(it.by)}: ${it.note ?? ""}`;
  return [roleName(it.owner_role), `due ${date(it.due_on)}`].filter(Boolean).join(" · ");
}

// Checklist is a month's close checklist (docs/design/screens PeriodClose "Checklist"; FR-GL-07, FM-4, Appendix C.2
// #58): the template's tasks with where each stands. The members who run the checks mark a task done, skip it for the
// month with a reason, or open it again, and change the template; the close checks refuse the close while a task is
// neither done nor skipped.
export function Checklist({ periodId, open, mayMark, onChanged }: { periodId: string; open: boolean; mayMark: boolean; onChanged: (message: string) => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const id = useId();
  const [skipping, setSkipping] = useState<Item | null>(null);
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const done = helpText("close.mark-task");
  const skip = helpText("close.skip-task");
  const edit = helpText("close.edit-template");
  const q = useQuery({
    queryKey: ["period-checklist", periodId],
    queryFn: async () => {
      const r = await api.GET("/v1/periods/{period_id}/checklist", { params: { path: { period_id: periodId } } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const c: PeriodChecklist | undefined = q.data;
  const finished = c ? c.done + c.skipped : 0;
  const total = c?.tasks.length ?? 0;

  const mark = async (it: Item, state: "done" | "skipped" | "open", note?: string) => {
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.POST("/v1/periods/{period_id}/checklist/{task_key}", {
        params: { path: { period_id: periodId, task_key: it.key }, header: { "Idempotency-Key": key } },
        body: { state, note },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    setSkipping(null);
    queries.setQueryData(["period-checklist", periodId], out.data);
    const words = state === "done" ? "Done" : state === "skipped" ? "Skipped for this month" : "Opened again";
    onChanged(`${words}: ${it.title}. Run the checks again so they see it.`);
  };

  return (
    <section className="fy-card" aria-labelledby={`${id}-checklist`}>
      <div className="fy-card__head" style={{ flexWrap: "wrap" }}>
        <h2 className="fy-h3" id={`${id}-checklist`}>
          Checklist
        </h2>
        {c && (
          <span className="fy-small fy-muted">
            {finished} of {total} tasks done or skipped
          </span>
        )}
      </div>
      {c && total > 0 && (
        <div className="fy-card__body" style={{ paddingTop: 0 }}>
          <div
            className={`fy-progress ${c.open === 0 ? "fy-progress--good" : "fy-progress--warning"}`}
            role="progressbar"
            aria-valuenow={finished}
            aria-valuemin={0}
            aria-valuemax={total}
            aria-label={`${finished} of ${total} tasks done or skipped`}
          >
            <span style={{ width: `${Math.round((finished / total) * 100)}%` }} />
          </div>
        </div>
      )}
      {q.isError && <Refusal problem={problemOf(q.error)} title="The checklist could not be shown" />}
      {problem && <Refusal problem={problem} title="The task was not marked" />}
      {c && (
        <ul className="fy-list" aria-labelledby={`${id}-checklist`}>
          {c.tasks.map((it) => (
            <li className="fy-list__item" key={it.key}>
              <StateIcon item={it} />
              <span className="fy-list__main">
                <span className="fy-list__title whitespace-normal">{it.title}</span>
                <span className="fy-list__meta whitespace-normal">{meta(it)}</span>
              </span>
              <span className="fy-sr">{it.state === "done" ? "Done" : it.state === "skipped" ? "Skipped this month" : "To do"}</span>
              {open && mayMark && (
                <span className="fy-row gap-1">
                  {it.state === "open" ? (
                    <>
                      <Button variant="secondary" size="sm" onClick={() => void mark(it, "done")} disabled={busy} title={done.tooltip} aria-label={`${done.label}: ${it.title}`}>
                        <Icon of={CircleCheck} />
                        {done.label}
                      </Button>
                      <Button variant="ghost" size="sm" onClick={() => setSkipping(it)} disabled={busy} title={skip.tooltip} aria-label={`${skip.label}: ${it.title}`}>
                        {skip.label}…
                      </Button>
                    </>
                  ) : (
                    <Button variant="ghost" size="sm" onClick={() => void mark(it, "open")} disabled={busy} aria-label={`Open again: ${it.title}`} title="Mark the task as not done yet">
                      <Icon of={RotateCcw} />
                    </Button>
                  )}
                </span>
              )}
            </li>
          ))}
        </ul>
      )}
      {c && (
        <div className="fy-card__foot fy-row fy-row--wrap gap-2">
          <span className="fy-small fy-muted">{c.template_version === 0 ? "The recommended checklist" : `Checklist template, version ${c.template_version}`}</span>
          {mayMark && (
            <Button variant="ghost" size="sm" onClick={() => setEditing(true)} title={edit.tooltip}>
              <Icon of={ListChecks} />
              Change the template…
            </Button>
          )}
        </div>
      )}
      {skipping && <Skip item={skipping} busy={busy} onClose={() => setSkipping(null)} onSkip={(reason) => void mark(skipping, "skipped", reason)} />}
      {editing && (
        <Template
          onClose={() => setEditing(false)}
          onSaved={async (version) => {
            setEditing(false);
            await Promise.all(["close-checklist-template", "period-checklist"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
            onChanged(`Saved version ${version} of the checklist template. Open months use it now.`);
          }}
        />
      )}
    </section>
  );
}
