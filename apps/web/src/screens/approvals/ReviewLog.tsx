import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChevronLeft, ClipboardCheck, ClockAlert, CircleCheck } from "lucide-react";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { objectLabel, type ReviewEntry } from "./words";

type Filter = "unreviewed" | "reviewed" | "all";

async function listReviewLog(status: Filter) {
  const { data, error } = await api.GET("/v1/review-log", { params: { query: { status, page_size: 200 } } });
  if (error) throw error;
  return data;
}

const kinds: Record<ReviewEntry["kind"], string> = {
  approved_change: "Approved change",
  founder_bootstrap: "Set up by the Owner alone, before a second member joined",
};

function MarkReviewed({ ids, open, onClose, onDone }: { ids: string[]; open: boolean; onClose: () => void; onDone: (n: number) => void }) {
  const queries = useQueryClient();
  const noteId = useId();
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText("review.mark");
  const run = async () => {
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/review-log/reviews", { params: { header: { "Idempotency-Key": newKey() } }, body: { entry_ids: ids, ...(note.trim() ? { note: note.trim() } : {}) } });
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    setNote("");
    await queries.invalidateQueries({ queryKey: ["review-log"] });
    onDone(out.data.reviewed?.length ?? ids.length);
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title={`${h.label}: ${ids.length === 1 ? "1 action" : `${ids.length} actions`}`}
      description="You confirm you checked each one. Each action is reviewed once, and the review is kept with it."
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={run} disabled={busy}>
            <Icon of={ClipboardCheck} />
            {h.label}
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="Nothing was marked as reviewed" />}
      <div className="fy-field">
        <label className="fy-label" htmlFor={noteId}>
          Note (optional)
        </label>
        <textarea className="fy-textarea" id={noteId} value={note} maxLength={500} onChange={(e) => setNote(e.target.value)} aria-describedby={`${noteId}-help`} />
        <span className="fy-help" id={`${noteId}-help`}>
          Recorded with each action, such as "Agreed at Monday's meeting".
        </span>
      </div>
    </Dialog>
  );
}

// ReviewLog is the weekly review of high-risk actions (FM-3, rev 0.46; docs/user-guide approvals.md
// "Review high-risk actions"): every change another person approved, and every role or invitation
// the Owner set up alone. Another founder marks them reviewed; nobody reviews their own, and an
// action unreviewed after 14 days is overdue.
export function ReviewLog() {
  const [filter, setFilter] = useState<Filter>("unreviewed");
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [marking, setMarking] = useState(false);
  const [status, setStatus] = useState("");
  const log = useQuery({ queryKey: ["review-log", filter], queryFn: () => listReviewLog(filter) });
  const items = log.data?.items ?? [];
  const mark = helpText("review.mark");
  const toggle = (id: string) =>
    setPicked((p) => {
      const n = new Set(p);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });
  const reviewable = items.filter((e) => e.you_may_review && !e.reviewed_at);
  const ids = [...picked].filter((id) => reviewable.some((e) => e.id === id));

  return (
    <div className="fy-page fy-page--narrow fy-enter">
      <Link className="fy-btn fy-btn--ghost fy-btn--sm -ms-2 self-start" to="/inbox">
        <Icon of={ChevronLeft} />
        Inbox
      </Link>
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Weekly review</span>
          <h1 className="fy-h1">Review log</h1>
          <p className="fy-secondary">
            {log.data ? `${log.data.unreviewed} not reviewed yet${log.data.overdue ? `, ${log.data.overdue} overdue` : ""}. ` : ""}
            Another founder reviews each high-risk action; nobody reviews their own.
          </p>
        </div>
        <div className="fy-pagehead__actions">
          <Button variant="primary" disabled={ids.length === 0} onClick={() => setMarking(true)} title={mark.tooltip}>
            <Icon of={ClipboardCheck} />
            {mark.label}
            {ids.length > 0 ? ` (${ids.length})` : ""}
          </Button>
        </div>
      </div>
      <div className="fy-segmented self-start" role="group" aria-label="Show">
        {(
          [
            ["unreviewed", "Not reviewed"],
            ["reviewed", "Reviewed"],
            ["all", "All"],
          ] as const
        ).map(([f, label]) => (
          <button
            key={f}
            type="button"
            aria-pressed={filter === f}
            onClick={() => {
              setFilter(f);
              setPicked(new Set());
            }}
          >
            {label}
          </button>
        ))}
      </div>
      <p className="fy-small" role="status">
        {status}
      </p>
      {log.isError && <Refusal problem={problemOf(log.error)} title="The review log could not be shown" />}
      {log.isPending && <p className="fy-secondary">Loading the review log…</p>}
      {log.data && items.length === 0 && (
        <div className="fy-empty">
          <Icon of={CircleCheck} size={24} />
          <p className="fy-secondary">{filter === "unreviewed" ? "Nothing waits for review." : "Nothing here yet."}</p>
        </div>
      )}
      {items.length > 0 && (
        <ul className="fy-list fy-stagger rounded-md border border-border bg-surface-1" aria-label="High-risk actions">
          {items.map((e) => {
            const can = e.you_may_review && !e.reviewed_at;
            const box = `pick-${e.id}`;
            return (
              <li key={e.id} className="fy-list__item items-start">
                {can ? (
                  <input type="checkbox" id={box} className="mt-1 size-4" checked={picked.has(e.id)} onChange={() => toggle(e.id)} aria-label={`Pick ${e.summary}`} />
                ) : (
                  <span className="size-4" aria-hidden="true" />
                )}
                <span className="fy-list__main">
                  <span className="fy-list__title whitespace-normal">{e.summary}</span>
                  <span className="fy-list__meta whitespace-normal">
                    {kinds[e.kind]} · {objectLabel(e.object_type)} · by {e.actor_name ?? "a client with no Fyniq user"}, {dateTime(e.logged_at)}
                  </span>
                  {e.reviewed_at && (
                    <span className="fy-list__meta whitespace-normal">
                      Reviewed by {e.reviewed_by_name}, {dateTime(e.reviewed_at)}
                      {e.note ? `: "${e.note}"` : ""}
                    </span>
                  )}
                  {!e.reviewed_at && !e.you_may_review && <span className="fy-list__meta whitespace-normal">You took this action, or cannot review it: another founder reviews it.</span>}
                  {e.approval_id && (
                    <Link className="fy-small" to="/approvals/$approvalId" params={{ approvalId: e.approval_id }}>
                      Open the approved request
                    </Link>
                  )}
                </span>
                {e.reviewed_at ? (
                  <span className="fy-chip fy-chip--good">
                    <Icon of={CircleCheck} />
                    Reviewed
                  </span>
                ) : e.overdue ? (
                  <span className="fy-chip fy-chip--serious">
                    <Icon of={ClockAlert} />
                    Overdue
                  </span>
                ) : null}
              </li>
            );
          })}
        </ul>
      )}
      <MarkReviewed
        ids={ids}
        open={marking}
        onClose={() => setMarking(false)}
        onDone={(n) => {
          setMarking(false);
          setPicked(new Set());
          setStatus(n === 1 ? "1 action marked as reviewed." : `${n} actions marked as reviewed.`);
        }}
      />
    </div>
  );
}
