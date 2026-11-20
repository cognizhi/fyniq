import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleCheck, ExternalLink, Plug, X } from "lucide-react";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Task = components["schemas"]["IntegrationTask"];

async function openTasks(): Promise<Task[]> {
  const { data, error } = await api.GET("/v1/tasks", { params: { query: { status: "open" } } });
  if (error) throw error;
  return data.items;
}

// closers are the people who finish a task a system made: Owner and Administrator, as the right on the row says. The API decides.
const closers = ["owner", "administrator"];

// IntegrationTasksToDo is the Inbox's "Tasks from other systems" (FR-INT-24; p2-api-third-parties 2.1): the open tasks a ticketing system or another connected
// client made, oldest due first, each with the system's reference and its link back. A task approves nothing and changes no record: it is a reminder. The system that
// made a task finishes it; an Owner or an Administrator may finish one here, for good, over the version they read. People who may not see them see no section.
export function IntegrationTasksToDo() {
  const id = useId();
  const { session } = useSignedIn();
  const tasks = useQuery({ queryKey: ["integration-tasks", "open"], queryFn: openTasks });
  if (tasks.isError || !tasks.data) return null;
  const closes = (session.roles ?? []).some((r) => closers.includes(r));
  const list = [...tasks.data].sort((a, b) => (a.due_on ?? "9999").localeCompare(b.due_on ?? "9999") || a.created_at.localeCompare(b.created_at));
  if (list.length === 0 && !closes) return null;
  return (
    <section className="fy-card" aria-labelledby={`${id}-integration-tasks`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-integration-tasks`} title={helpText("inbox.integration-tasks").tooltip}>
          {helpText("inbox.integration-tasks").label}
        </h2>
      </div>
      {list.length === 0 ? (
        <p className="fy-card__body fy-small fy-muted">No connected system is waiting for you.</p>
      ) : (
        <ul className="fy-list" aria-labelledby={`${id}-integration-tasks`}>
          {list.map((t) => (
            <Item key={t.id} task={t} closes={closes} />
          ))}
        </ul>
      )}
    </section>
  );
}

function Item({ task: t, closes }: { task: Task; closes: boolean }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const finish = useMutation({
    mutationFn: async (status: "done" | "cancelled") => {
      const out = await guarded(confirm, () =>
        api.POST("/v1/tasks/{task_id}/close", { params: { path: { task_id: t.id }, header: { "Idempotency-Key": newKey(), "If-Match": `"${t.version}"` } }, body: { status } }),
      );
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: () => queries.invalidateQueries({ queryKey: ["integration-tasks"] }),
    onError: (e) => setProblem(problemOf(e)),
  });
  return (
    <li className="fy-list__item">
      <span className="fy-list__icon">
        <Icon of={Plug} size={20} />
      </span>
      <span className="fy-list__main">
        <span className="fy-list__title whitespace-normal">{t.title}</span>
        {t.detail && <span className="fy-list__meta whitespace-normal">{t.detail}</span>}
        <span className="fy-list__meta">
          {t.due_on ? `Due ${date(t.due_on)}` : "No due date"} · from {t.client_id}, reference {t.external_ref}
        </span>
        {t.link_url && (
          <a className="fy-link fy-small" href={t.link_url} target="_blank" rel="noopener noreferrer">
            <Icon of={ExternalLink} size={16} /> Open in the other system
          </a>
        )}
        {problem && <Refusal problem={problem} title="The task did not change" />}
      </span>
      {closes && (
        <span className="fy-row gap-2">
          <Button size="sm" variant="secondary" disabled={finish.isPending} onClick={() => finish.mutate("done")} title={helpText("inbox.integration-task-done").tooltip} aria-label={`${helpText("inbox.integration-task-done").label}: ${t.title}`}>
            <Icon of={CircleCheck} />
            {helpText("inbox.integration-task-done").label}
          </Button>
          <Button size="sm" variant="ghost" disabled={finish.isPending} onClick={() => finish.mutate("cancelled")} title={helpText("inbox.integration-task-cancel").tooltip} aria-label={`${helpText("inbox.integration-task-cancel").label}: ${t.title}`}>
            <Icon of={X} />
            {helpText("inbox.integration-task-cancel").label}
          </Button>
        </span>
      )}
    </li>
  );
}
