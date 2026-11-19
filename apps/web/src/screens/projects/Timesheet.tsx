import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, CornerUpLeft } from "lucide-react";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { hours } from "./data";
import { mondayOf, TimeGrid, today } from "./TimeGrid";

type Submission = components["schemas"]["TimesheetSubmission"];

// Decide shows a submitted week with its entries and approves it, or returns it with what to fix (FR-PRJ-07).
function Decide({ submission, onClose }: { submission: Submission | null; onClose: () => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const id = useId();
  const [comment, setComment] = useState("");
  const [missing, setMissing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const detail = useQuery({
    queryKey: ["timesheet-submission", submission?.id],
    enabled: submission !== null,
    queryFn: async () => {
      const r = await api.GET("/v1/timesheet-submissions/{submission_id}", { params: { path: { submission_id: submission!.id } } });
      if (r.error) throw r.error;
      return { body: r.data, etag: r.response.headers.get("ETag") ?? "" };
    },
  });
  const close = () => {
    setComment("");
    setMissing(false);
    setProblem(null);
    onClose();
  };
  const act = async (approve: boolean) => {
    if (!submission || !detail.data) return;
    if (!approve && !comment.trim()) return setMissing(true);
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const params = { path: { submission_id: submission.id }, header: { "If-Match": detail.data.etag, "Idempotency-Key": key } };
    const out = await guarded(confirm, () =>
      approve ? api.POST("/v1/timesheet-submissions/{submission_id}/approve", { params, body: { comment: comment.trim() || undefined } }) : api.POST("/v1/timesheet-submissions/{submission_id}/return", { params, body: { comment: comment.trim() } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ predicate: (q) => String(q.queryKey[0]).startsWith("timesheet") });
    close();
  };
  const d = detail.data?.body;
  return (
    <Dialog
      open={submission !== null}
      onOpenChange={(o) => !o && close()}
      dismissible={!busy}
      eyebrow="Time to approve"
      title={submission ? `${submission.person_name}: ${submission.project_name}` : "Week"}
      description={submission ? `Week of ${submission.week_start}, ${hours(submission.minutes)}. Approving locks these entries; time posts nothing to the books.` : ""}
    >
      {problem && <Refusal problem={problem} title="The week was not decided" />}
      {detail.isError && <Refusal problem={problemOf(detail.error)} title="The week could not be read" />}
      {d && (
        <ul className="fy-list" aria-label="Entries in the week">
          {d.entries.map((e) => (
            <li key={e.id} className="fy-list__item">
              <span className="fy-list__main">
                <span className="fy-list__title">
                  {e.work_date} · {hours(e.minutes)}
                </span>
                <span className="fy-list__meta">
                  {e.billable ? "Billable" : "Not billable"}
                  {e.note ? ` · ${e.note}` : ""}
                  {e.reverses_id ? " · reverses an approved entry" : ""}
                </span>
              </span>
            </li>
          ))}
        </ul>
      )}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-comment`}>
          Comment (needed to return the week)
        </label>
        <textarea
          className="fy-textarea"
          id={`${id}-comment`}
          value={comment}
          maxLength={1000}
          onChange={(e) => {
            setComment(e.target.value);
            setMissing(false);
          }}
          aria-invalid={missing || undefined}
          aria-describedby={missing ? `${id}-comment-error` : undefined}
        />
        {missing && (
          <span className="fy-error" id={`${id}-comment-error`}>
            Say what the person should fix.
          </span>
        )}
      </div>
      <div className="fy-row justify-end gap-2">
        <Button variant="ghost" onClick={close} disabled={busy}>
          Cancel
        </Button>
        <Button variant="secondary" onClick={() => void act(false)} disabled={busy || !d}>
          <Icon of={CornerUpLeft} />
          Return with a comment
        </Button>
        <Button variant="primary" onClick={() => void act(true)} disabled={busy || !d}>
          <Icon of={Check} />
          Approve the week
        </Button>
      </div>
    </Dialog>
  );
}

// Timesheet is Projects › Timesheet (FR-PRJ-05, FR-PRJ-07; docs/design/screens Timesheet): the person's own week and the weeks
// sent to them to approve, for the projects they manage, or any project for the Controller. Nobody approves their own time.
export function Timesheet() {
  const [week, setWeek] = useState(mondayOf(today()));
  const [deciding, setDeciding] = useState<Submission | null>(null);
  const id = useId();
  const waiting = useQuery({
    queryKey: ["timesheet-submissions", "waiting"],
    queryFn: async () => {
      const r = await api.GET("/v1/timesheet-submissions", { params: { query: { whose: "waiting_for_me", page_size: 100 } } });
      if (r.error) throw r.error;
      return r.data.items;
    },
  });
  const h = helpText("time.approve");
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">Timesheet</h1>
          <p className="fy-secondary">Your week, and the weeks your team sent you to approve.</p>
        </div>
      </div>
      {waiting.data && waiting.data.length > 0 && (
        <section className="fy-card" aria-labelledby={`${id}-wait`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-wait`} title={h.tooltip}>
              {h.label}
            </h2>
          </div>
          <ul className="fy-list">
            {waiting.data.map((s) => (
              <li key={s.id} className="fy-list__item">
                <span className="fy-list__main">
                  <span className="fy-list__title">
                    {s.person_name} · {s.project_name}
                  </span>
                  <span className="fy-list__meta">
                    Week of {s.week_start} · {hours(s.minutes)}
                  </span>
                </span>
                <Button variant="secondary" size="sm" onClick={() => setDeciding(s)}>
                  Review the week
                </Button>
              </li>
            ))}
          </ul>
        </section>
      )}
      {waiting.isError && problemOf(waiting.error).code !== "PRJ-09" && <Refusal problem={problemOf(waiting.error)} title="The weeks to approve could not be listed" />}
      <TimeGrid week={week} onWeek={setWeek} />
      <Decide submission={deciding} onClose={() => setDeciding(null)} />
    </div>
  );
}
