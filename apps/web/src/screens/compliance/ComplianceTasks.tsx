import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, ClipboardList, ClockAlert, X } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { today } from "../purchases/BillsBoard";

type Task = components["schemas"]["ComplianceTask"];

async function openTasks(): Promise<Task[]> {
  const { data, error } = await api.GET("/v1/compliance/tasks", { params: { query: { status: "open" } } });
  if (error) throw error;
  return data.items;
}

// closers are the roles that close a task: C on the AI company secretary's row, the Owner's A too. The API decides.
const closers = ["owner", "administrator"];

// TasksToDo is the Inbox's Tasks section (AI-65; p1-scheduled-agents tasks 7.2, design D7): the open compliance tasks owned
// by the person or one of their roles, raised by the company secretary assistant's findings or its approved proposals,
// due first. The Owner and the Administrator mark one done, or dismiss it with a reason; marking it done records
// nothing as filed. Those who may not see the tasks see no section.
export function TasksToDo() {
  const id = useId();
  const { session } = useSignedIn();
  const roles = session.roles ?? [];
  const tasks = useQuery({ queryKey: ["compliance-tasks", "open"], queryFn: openTasks });
  const [dismissing, setDismissing] = useState<Task | null>(null);
  if (tasks.isError || !tasks.data) return null;
  const mine = tasks.data
    .filter((t) => t.owner_user === session.user_id || (t.owner_role !== undefined && roles.includes(t.owner_role)))
    .sort((a, b) => (a.due_on ?? "9999").localeCompare(b.due_on ?? "9999"));
  const closes = roles.some((r) => closers.includes(r));
  return (
    <section className="fy-card" aria-labelledby={`${id}-tasks`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-tasks`} title={helpText("inbox.tasks").tooltip}>
          Tasks
        </h2>
        <Link className="fy-small" to="/compliance/secretary">
          The company secretary assistant
        </Link>
      </div>
      {mine.length === 0 ? (
        <p className="fy-card__body fy-small fy-muted">No compliance task waits for you.</p>
      ) : (
        <ul className="fy-list" aria-labelledby={`${id}-tasks`}>
          {mine.map((t) => (
            <TaskItem key={t.id} task={t} closes={closes} onDismiss={() => setDismissing(t)} />
          ))}
        </ul>
      )}
      {dismissing && <DismissTask task={dismissing} onClose={() => setDismissing(null)} />}
    </section>
  );
}

function TaskItem({ task: t, closes, onDismiss }: { task: Task; closes: boolean; onDismiss: () => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const done = useMutation({
    mutationFn: async () => {
      const out = await guarded(confirm, () =>
        api.POST("/v1/compliance/tasks/{task_id}/done", { params: { path: { task_id: t.id }, header: { "Idempotency-Key": newKey(), "If-Match": `"${t.version}"` } } }),
      );
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: () => queries.invalidateQueries({ queryKey: ["compliance-tasks"] }),
    onError: (e) => setProblem(problemOf(e)),
  });
  const overdue = t.due_on !== undefined && t.due_on < today();
  return (
    <li className="fy-list__item">
      <span className="fy-list__icon">
        <Icon of={ClipboardList} size={20} />
      </span>
      <span className="fy-list__main">
        <span className="fy-list__title whitespace-normal">{t.title}</span>
        {t.detail && <span className="fy-list__meta whitespace-normal">{t.detail}</span>}
        <span className="fy-list__meta">
          {t.due_on ? `Due ${date(t.due_on)}` : "No due date"}
          {t.source === "ai_proposal" ? " · suggested by the AI, approved by a person" : " · from the assistant's checks"}
        </span>
        {problem && <Refusal problem={problem} title="The task did not change" />}
      </span>
      {overdue && (
        <span className="fy-chip fy-chip--serious">
          <Icon of={ClockAlert} />
          Overdue
        </span>
      )}
      {closes && (
        <span className="flex gap-1">
          <Button size="sm" onClick={() => done.mutate()} disabled={done.isPending} aria-label={`Mark done: ${t.title}`}>
            <Icon of={CircleCheck} />
            Done
          </Button>
          <Button size="sm" variant="ghost" onClick={onDismiss} aria-label={`Dismiss: ${t.title}`}>
            <Icon of={X} />
            Dismiss
          </Button>
        </span>
      )}
    </li>
  );
}

// DismissTask dismisses a task with the reason it needs nothing done (design D7).
function DismissTask({ task, onClose }: { task: Task; onClose: () => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [reason, setReason] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const dismiss = useMutation({
    mutationFn: async () => {
      const out = await guarded(confirm, () =>
        api.POST("/v1/compliance/tasks/{task_id}/dismiss", {
          params: { path: { task_id: task.id }, header: { "Idempotency-Key": newKey(), "If-Match": `"${task.version}"` } },
          body: { reason: reason.trim() },
        }),
      );
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      await queries.invalidateQueries({ queryKey: ["compliance-tasks"] });
      onClose();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    dismiss.mutate();
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && !dismiss.isPending && onClose()}
      dismissible={!dismiss.isPending}
      eyebrow="Tasks"
      title="Dismiss the task"
      description={`Say why "${task.title}" needs nothing done. A finding that comes back raises a new task.`}
    >
      {problem && <Refusal problem={problem} title="The task was not dismissed" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-reason`}>
            Why it needs nothing done
          </label>
          <textarea id={`${id}-reason`} className="fy-input" rows={3} maxLength={500} value={reason} onChange={(e) => setReason(e.target.value)} />
        </div>
        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={onClose} disabled={dismiss.isPending}>
            Keep it
          </Button>
          <Button type="submit" variant="primary" disabled={reason.trim() === "" || dismiss.isPending}>
            Dismiss
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
