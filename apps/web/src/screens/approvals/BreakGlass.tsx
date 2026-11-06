import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, CircleX, ClipboardCheck, ClockAlert, Info, OctagonAlert, Power, Siren } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";

export type BreakGlass = components["schemas"]["BreakGlass"];

// listBreakGlass is the organisation's break-glasses, newest first, with the one active now (FM-8).
export async function listBreakGlass() {
  const { data, error } = await api.GET("/v1/break-glass", { params: { query: { page_size: 100 } } });
  if (error) throw error;
  return data;
}

const money = (m: { amount: string; currency: string }) => `${m.currency} ${m.amount}`;

// StartForm starts a break-glass with a reason, stepped up (FM-8).
function StartForm({ cap, onStarted }: { cap: string; onStarted: () => void }) {
  const id = useId();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const start = helpText("break-glass.start");
  const [reason, setReason] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!reason.trim()) return setError("Say who is unreachable and what must be paid.");
    setError("");
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () => api.POST("/v1/break-glass", { params: { header: { "Idempotency-Key": newKey() } }, body: { reason: reason.trim() } }));
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    setReason("");
    await queries.invalidateQueries({ queryKey: ["break-glass"] });
    await queries.invalidateQueries({ queryKey: ["banners"] });
    onStarted();
  };
  return (
    <form className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-title`} onSubmit={submit} noValidate>
      <h2 className="fy-h3" id={`${id}-title`}>
        Start break-glass
      </h2>
      <p className="fy-small fy-secondary">{start.tooltip}</p>
      {problem && <Refusal problem={problem} title="Break-glass did not start" />}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-reason`}>
          Why
        </label>
        <textarea
          className="fy-textarea"
          id={`${id}-reason`}
          value={reason}
          maxLength={500}
          onChange={(e) => setReason(e.target.value)}
          aria-invalid={Boolean(error) || undefined}
          aria-describedby={`${id}-reason-help${error ? ` ${id}-reason-error` : ""}`}
        />
        <span className="fy-help" id={`${id}-reason-help`}>
          Who is unreachable, and what must be paid. Every Owner and the Accountant read it at once.
        </span>
        {error && (
          <span className="fy-error" id={`${id}-reason-error`}>
            {error}
          </span>
        )}
      </div>
      <ul className="fy-small fy-stack fy-stack--1" aria-label="What break-glass allows">
        <li>For 24 hours you release payment batches waiting for approval without a second person, up to {cap} in all.</li>
        <li>Never: repaying a director, paying yourself, or paying a bank account that changed in the last 30 days.</li>
        <li>Nothing else changes: every other high-risk change still waits for another person.</li>
      </ul>
      <div className="fy-row">
        <Button type="submit" variant="destructive" disabled={busy}>
          <Icon of={Siren} />
          {start.label}
        </Button>
      </div>
    </form>
  );
}

// ReviewDialog records a review of a break-glass with an optional note.
function ReviewDialog({ glass, onClose }: { glass: BreakGlass | null; onClose: () => void }) {
  const id = useId();
  const queries = useQueryClient();
  const h = helpText("break-glass.review");
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const close = () => {
    setNote("");
    setProblem(null);
    onClose();
  };
  const run = async () => {
    if (!glass) return;
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/break-glass/{break_glass_id}/review", {
      params: { path: { break_glass_id: glass.id }, header: { "If-Match": `"${glass.version}"`, "Idempotency-Key": newKey() } },
      body: note.trim() ? { note: note.trim() } : {},
    });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["break-glass"] });
    await queries.invalidateQueries({ queryKey: ["review-log"] });
    close();
  };
  return (
    <Dialog
      open={glass !== null}
      onOpenChange={(o) => !o && close()}
      dismissible={!busy}
      title={h.label}
      description="You confirm you checked why it was used and each batch it released. It is reviewed once, and the review is kept with it."
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => void run()} disabled={busy}>
            <Icon of={ClipboardCheck} />
            {h.label}
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="The review was not recorded" />}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-note`}>
          Note (optional)
        </label>
        <textarea className="fy-textarea" id={`${id}-note`} value={note} maxLength={500} onChange={(e) => setNote(e.target.value)} />
      </div>
    </Dialog>
  );
}

// ReviewState is a break-glass's review in words: reviewed, due, or overdue (✖ on Checks).
function ReviewState({ g }: { g: BreakGlass }) {
  if (g.review)
    return (
      <span className="fy-chip fy-chip--good">
        <Icon of={CircleCheck} />
        Reviewed
      </span>
    );
  if (g.overdue)
    return (
      <span className="fy-chip fy-chip--critical">
        <Icon of={CircleX} />
        Review overdue
      </span>
    );
  if (g.status === "active")
    return (
      <span className="fy-chip fy-chip--critical">
        <Icon of={Siren} />
        Active
      </span>
    );
  return (
    <span className="fy-chip fy-chip--serious">
      <Icon of={ClockAlert} />
      Review due
    </span>
  );
}

// Releases lists the batches a break-glass released.
function Releases({ g }: { g: BreakGlass }) {
  if (g.releases.length === 0) return <span className="fy-list__meta">It released no batch.</span>;
  return (
    <ul className="fy-small fy-stack fy-stack--1" aria-label="Batches it released">
      {g.releases.map((r) => (
        <li key={r.batch_id}>
          <Link to="/purchases/payment-batches/$batchId" params={{ batchId: r.batch_id }}>
            {r.number}
          </Link>{" "}
          · <span className="tabular-nums">{money(r.total)}</span> · {dateTime(r.released_at)}
        </li>
      ))}
    </ul>
  );
}

// BreakGlass is break-glass for a single available founder (FM-8; PRD-001 rev 0.175; docs/user-guide team-mode.md
// "Break-glass"): starting one with a reason, the one active now with what it released and End now, and every
// break-glass with its review, due 7 days after it ends; another Owner reviews it, or the Accountant from its due date.
export function BreakGlassPage() {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const q = useQuery({ queryKey: ["break-glass"], queryFn: listBreakGlass });
  const [reviewing, setReviewing] = useState<BreakGlass | null>(null);
  const [status, setStatus] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const end = helpText("break-glass.end");
  const review = helpText("break-glass.review");
  const page = q.data;
  const active = page?.active;
  const items = page?.items ?? [];
  const stop = async (g: BreakGlass) => {
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/break-glass/{break_glass_id}/end", { params: { path: { break_glass_id: g.id }, header: { "If-Match": `"${g.version}"`, "Idempotency-Key": newKey() } } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    setStatus("Break-glass ended: it releases nothing more, and another Owner may review it now.");
    await queries.invalidateQueries({ queryKey: ["break-glass"] });
    await queries.invalidateQueries({ queryKey: ["banners"] });
  };
  return (
    <div className="fy-page fy-page--narrow fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Founder mode</span>
          <h1 className="fy-h1">Break-glass</h1>
          <p className="fy-secondary">
            When the other founder is unreachable, an Owner may release payment batches waiting for approval alone, for 24 hours and up to a cap. Another Owner reviews it
            afterwards; from 7 days after it ends, the Accountant may review it instead.
          </p>
        </div>
      </div>
      <p className="fy-small" role="status">
        {status}
      </p>
      {problem && <Refusal problem={problem} title="Break-glass did not end" />}
      {q.isError && <Refusal problem={problemOf(q.error)} title="Break-glass could not be shown" />}
      {q.isPending && <p className="fy-secondary">Loading break-glass…</p>}
      {page && (
        <>
          {active && (
            <section className="fy-banner fy-banner--critical" aria-labelledby="active-title">
              <Icon of={OctagonAlert} size={20} />
              <div className="fy-banner__text">
                <span className="fy-strong" id="active-title">
                  Break-glass is on until {dateTime(active.ended_at ?? active.expires_at)}
                </span>
                <span className="fy-small">
                  {active.started_by.name ?? "An Owner"} may release payment batches alone: "{active.reason}". Released {money(active.released)}; {money(active.left)} left of{" "}
                  {money(active.cap)}.
                </span>
                <Releases g={active} />
              </div>
              {active.you_may_end && (
                <Button size="sm" variant="secondary" onClick={() => void stop(active)} disabled={busy} title={end.tooltip}>
                  <Icon of={Power} />
                  {end.label}
                </Button>
              )}
            </section>
          )}
          {page.you_may_start && <StartForm cap={money(page.cap)} onStarted={() => setStatus("Break-glass started: every Owner and the Accountant have been told.")} />}
          {!active && page.blocked && (
            <div className="fy-banner fy-banner--info" role="note">
              <Icon of={Info} size={20} />
              <div className="fy-banner__text">
                <span className="fy-strong">{page.blocked.message}</span>
                <span className="fy-small">{page.blocked.fix}.</span>
              </div>
            </div>
          )}
          <section className="fy-card" aria-labelledby="history-title">
            <div className="fy-card__head">
              <h2 className="fy-h3" id="history-title">
                Every break-glass
              </h2>
              <span className="fy-badge fy-badge--quiet">{items.length}</span>
            </div>
            {items.length === 0 ? (
              <p className="fy-card__body fy-small fy-secondary">Break-glass was never used. The cap now is {money(page.cap)}.</p>
            ) : (
              <ul className="fy-list" aria-labelledby="history-title">
                {items.map((g) => (
                  <li key={g.id} className="fy-list__item items-start">
                    <span className="fy-list__main">
                      <span className="fy-list__title whitespace-normal">
                        {g.started_by.name ?? "An Owner"}: "{g.reason}"
                      </span>
                      <span className="fy-list__meta whitespace-normal">
                        {dateTime(g.started_at)} to {dateTime(g.ended_at ?? g.expires_at)}
                        {g.ended_by ? `, ended by ${g.ended_by.name ?? "an Owner"}` : ""} · released <span className="tabular-nums">{money(g.released)}</span> of{" "}
                        <span className="tabular-nums">{money(g.cap)}</span>
                      </span>
                      <Releases g={g} />
                      {g.review ? (
                        <span className="fy-list__meta whitespace-normal">
                          Reviewed by {g.review.reviewed_by.name ?? "a member"}, {dateTime(g.review.reviewed_at)}
                          {g.review.note ? `: "${g.review.note}"` : ""}
                        </span>
                      ) : (
                        <span className="fy-list__meta whitespace-normal">
                          {g.status === "active" ? "Reviewed once it ends; " : ""}its review is due by {dateTime(g.review_due_at)}
                          {g.overdue ? ": Checks shows it as cannot post until fixed, and the month's close waits for it." : "."}
                        </span>
                      )}
                    </span>
                    <span className="fy-stack fy-stack--2 items-end">
                      <ReviewState g={g} />
                      {g.you_may_review && (
                        <Button size="sm" variant="primary" onClick={() => setReviewing(g)} title={review.tooltip}>
                          <Icon of={ClipboardCheck} />
                          {review.label}
                        </Button>
                      )}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </section>
        </>
      )}
      <ReviewDialog glass={reviewing} onClose={() => setReviewing(null)} />
    </div>
  );
}
