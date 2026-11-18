import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { BadgeCheck, CircleSlash, EyeOff, Hourglass, ThumbsDown, ThumbsUp } from "lucide-react";
import { useId, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { AiBadge } from "../../components/ai/AiBadge";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { dateTime } from "../../format/date";
import { type ApiProblem, Icon, problemOf } from "../../ui";
import { classWords } from "./AIRoutes";

type Feedback = components["schemas"]["AIFeedback"];
type State = Feedback["state"];

const stateWords: Record<State, { label: string; tone: string; icon: typeof BadgeCheck }> = {
  waiting: { label: "Waiting for review", tone: "fy-chip--neutral", icon: Hourglass },
  golden: { label: "In the golden set", tone: "fy-chip--good", icon: BadgeCheck },
  dismissed: { label: "Dismissed", tone: "fy-chip--neutral", icon: CircleSlash },
};

// AIFeedbackReview is the AI activity page's feedback on the assistant's answers (QA-AI-05; p1-ai-assistant task 6.4),
// for the Owner and the Administrator: each thumbs up or down with its reason, the question and the answer, to add to
// the golden set or dismiss. Feedback on someone else's question that read payroll shows masked to a reviewer who may
// not see payroll (FR-PAY-13), and can only be dismissed.
export function AIFeedbackReview({ personName }: { personName: (person: string) => string }) {
  const id = useId();
  const queries = useQueryClient();
  const [state, setState] = useState<State | "">("waiting");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const list = useInfiniteQuery({
    queryKey: ["ai-feedback", state],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET("/v1/ai/feedback", { params: { query: { page_size: 20, cursor: pageParam, ...(state ? { state } : {}) } } });
      if (error) throw error;
      return data;
    },
    getNextPageParam: (last) => last.next_cursor,
  });
  const review = useMutation({
    mutationFn: async ({ feedback, decision }: { feedback: string; decision: "golden" | "dismissed" }) => {
      const { data, error } = await api.POST("/v1/ai/feedback/{feedback_id}/review", { params: { path: { feedback_id: feedback } }, body: { decision } });
      if (error) throw error;
      return data;
    },
    onSuccess: async () => {
      setProblem(null);
      await queries.invalidateQueries({ queryKey: ["ai-feedback"] });
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <section className="fy-card" aria-labelledby={`${id}-title`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-title`}>
          Feedback on answers
        </h2>
        <div className="fy-field fy-row gap-2">
          <label className="fy-label" htmlFor={`${id}-state`}>
            Show
          </label>
          <select id={`${id}-state`} className="fy-select" value={state} onChange={(e) => setState(e.target.value as State | "")}>
            <option value="waiting">Waiting for review</option>
            <option value="golden">In the golden set</option>
            <option value="dismissed">Dismissed</option>
            <option value="">All feedback</option>
          </select>
        </div>
      </div>
      <p className="fy-card__body fy-small fy-secondary">
        Members rate the assistant's answers with a thumbs up or down and a reason. What you add to the golden set feeds the assistant's evals; feedback nobody reviewed never does.
      </p>
      {list.isError && <Refusal problem={problemOf(list.error)} title="The feedback could not be shown" />}
      {problem && <Refusal problem={problem} title="The feedback was not reviewed" />}
      {list.data && items.length === 0 && <p className="fy-card__body fy-secondary">{state === "waiting" ? "No feedback is waiting for review." : "No feedback here yet."}</p>}
      {items.length > 0 && (
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--cards" aria-labelledby={`${id}-title`}>
            <thead>
              <tr>
                <th scope="col">When</th>
                <th scope="col">From</th>
                <th scope="col">Question and answer</th>
                <th scope="col">Rating and reason</th>
                <th scope="col">Review</th>
              </tr>
            </thead>
            <tbody>
              {items.map((f) => {
                const s = stateWords[f.state];
                return (
                  <tr key={f.id}>
                    <td data-card="title">{dateTime(f.given_at)}</td>
                    <td>{personName(f.person)}</td>
                    <td className="whitespace-normal">
                      {f.masked ? (
                        <span className="fy-small fy-secondary fy-row gap-1">
                          <Icon of={EyeOff} />
                          Masked: the question read payroll
                        </span>
                      ) : (
                        <span className="fy-stack gap-1">
                          <span className="fy-strong">{f.question}</span>
                          <span className="fy-small">
                            <AiBadge /> {f.answer}
                          </span>
                          {f.classes.length > 0 && <span className="fy-small fy-secondary">{f.classes.map((c) => classWords[c]).join(", ")}</span>}
                        </span>
                      )}
                    </td>
                    <td className="whitespace-normal">
                      <span className="fy-stack gap-1">
                        <span className="fy-row gap-1">
                          <Icon of={f.rating === "up" ? ThumbsUp : ThumbsDown} />
                          {f.rating === "up" ? "Helped" : "Did not help"}
                        </span>
                        {f.reason && <span className="fy-small">{f.reason}</span>}
                      </span>
                    </td>
                    <td className="whitespace-normal">
                      {f.state === "waiting" ? (
                        <span className="fy-stack gap-1">
                          {!f.masked && (
                            <Button size="sm" variant="secondary" className="self-start" disabled={review.isPending} onClick={() => review.mutate({ feedback: f.id, decision: "golden" })}
                              aria-label={`Add the feedback of ${dateTime(f.given_at)} to the golden set`}>
                              <Icon of={BadgeCheck} />
                              Add to the golden set
                            </Button>
                          )}
                          <Button size="sm" variant="ghost" className="self-start" disabled={review.isPending} onClick={() => review.mutate({ feedback: f.id, decision: "dismissed" })}
                            aria-label={`Dismiss the feedback of ${dateTime(f.given_at)}`}>
                            <Icon of={CircleSlash} />
                            Dismiss
                          </Button>
                          {f.masked && <span className="fy-small fy-secondary">Someone who sees payroll can add it to the golden set.</span>}
                        </span>
                      ) : (
                        <span className="fy-stack gap-1">
                          <span className={`fy-chip ${s.tone} self-start`}>
                            <Icon of={s.icon} />
                            {s.label}
                          </span>
                          {f.reviewed_by && f.reviewed_at && (
                            <span className="fy-small fy-secondary">
                              {personName(f.reviewed_by)}, {dateTime(f.reviewed_at)}
                            </span>
                          )}
                        </span>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      {list.hasNextPage && (
        <div className="fy-card__body">
          <Button variant="secondary" onClick={() => void list.fetchNextPage()} disabled={list.isFetchingNextPage}>
            {list.isFetchingNextPage ? "Loading…" : "Show more feedback"}
          </Button>
        </div>
      )}
    </section>
  );
}
