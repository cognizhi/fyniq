import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { CircleCheck, ClipboardCheck, ClockAlert, FileSearch, History, Hourglass, Link2, MessageSquarePlus, Reply, TriangleAlert } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { date, dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Review = components["schemas"]["AccountantReview"];
type Quarter = components["schemas"]["ReviewQuarter"];

const quarterState: Record<Quarter["status"], { label: string; tone: string; icon: typeof CircleCheck }> = {
  reviewed: { label: "Reviewed", tone: "fy-chip--good", icon: CircleCheck },
  in_review: { label: "In review", tone: "fy-chip--info", icon: Hourglass },
  due: { label: "Due", tone: "fy-chip--neutral", icon: ClockAlert },
  overdue: { label: "Overdue", tone: "fy-chip--warning", icon: TriangleAlert },
};

const reviewState: Record<Review["status"], { label: string; tone: string; icon: typeof CircleCheck }> = {
  in_review: { label: "In review", tone: "fy-chip--info", icon: Hourglass },
  signed_off: { label: "Signed off", tone: "fy-chip--good", icon: CircleCheck },
  superseded: { label: "Superseded", tone: "fy-chip--neutral", icon: History },
};

function Chip({ s }: { s: { label: string; tone: string; icon: typeof CircleCheck } }) {
  return (
    <span className={`fy-chip ${s.tone}`}>
      <Icon of={s.icon} />
      {s.label}
    </span>
  );
}

// AccountantReviews are the accountant's reviews (FM-7; PRD-001 rev 0.176; docs/user-guide accountant-review.md): each
// quarter with the review of its last month, the closed months the Accountant may open a review of, and every review.
export function AccountantReviews() {
  const navigate = useNavigate();
  const id = useId();
  const open = helpText("accountant-review.open");
  const [month, setMonth] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const q = useQuery({
    queryKey: ["accountant-reviews"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/accountant-reviews", { params: { query: { page_size: 100 } } });
      if (error) throw error;
      return data;
    },
  });
  const page = q.data;
  const items = page?.items ?? [];
  const start = async (e: FormEvent) => {
    e.preventDefault();
    const chosen = month || page?.to_review[0]?.id;
    if (!chosen) return;
    setBusy(true);
    setProblem(null);
    const close = await api.GET("/v1/periods/{period_id}/close", { params: { path: { period_id: chosen } } });
    const out = close.error
      ? close
      : await api.POST("/v1/periods/{period_id}/accountant-review", { params: { path: { period_id: chosen }, header: { "If-Match": etagOf(close.response), "Idempotency-Key": newKey() } } });
    setBusy(false);
    if (!out.data || !("queries" in out.data)) return setProblem(problemOf(out.error));
    await navigate({ to: "/accounting/reviews/$reviewId", params: { reviewId: out.data.id } });
  };
  return (
    <div className="fy-page fy-page--narrow fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/accounting">Accounting</Link>
          </nav>
          <h1 className="fy-h1">Accountant reviews</h1>
          <p className="fy-secondary">
            Each quarter the external accountant reviews a closed month, normally the quarter's last: its close snapshot, the queries management answers, any adjusting
            journals and the sign-off.
          </p>
        </div>
      </div>
      {q.isError && <Refusal problem={problemOf(q.error)} title="The reviews could not be shown" />}
      {q.isPending && <p className="fy-secondary">Loading the reviews…</p>}
      {page && (
        <>
          <section className="fy-card" aria-labelledby={`${id}-quarters`}>
            <div className="fy-card__head">
              <h2 className="fy-h3" id={`${id}-quarters`}>
                Quarters
              </h2>
            </div>
            {page.quarters.length === 0 ? (
              <p className="fy-card__body fy-small fy-secondary">No quarter has ended since the books started.</p>
            ) : (
              <ul className="fy-list" aria-labelledby={`${id}-quarters`}>
                {page.quarters.map((x) => (
                  <li key={x.key} className="fy-list__item">
                    <span className="fy-list__main">
                      <span className="fy-list__title">{x.label}</span>
                      <span className="fy-list__meta">
                        {x.status === "reviewed" ? `Reviewed through its last month` : `Due by ${date(x.due_by)}: the review of its last month, signed off`}
                      </span>
                    </span>
                    {x.review_id && (
                      <Link className="fy-small" to="/accounting/reviews/$reviewId" params={{ reviewId: x.review_id }}>
                        The review
                      </Link>
                    )}
                    <Chip s={quarterState[x.status]} />
                  </li>
                ))}
              </ul>
            )}
          </section>
          {page.you_may_open && (
            <form className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-open`} onSubmit={start} noValidate>
              <h2 className="fy-h3" id={`${id}-open`}>
                {open.label}
              </h2>
              {problem && <Refusal problem={problem} title="The review was not opened" />}
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-month`}>
                  Closed month
                </label>
                <select className="fy-select" id={`${id}-month`} value={month || page.to_review[0]?.id} onChange={(e) => setMonth(e.target.value)} aria-describedby={`${id}-month-help`}>
                  {page.to_review.map((m) => (
                    <option key={m.id} value={m.id}>
                      {m.name}
                    </option>
                  ))}
                </select>
                <span className="fy-help" id={`${id}-month-help`}>
                  {open.tooltip}
                </span>
              </div>
              <div className="fy-row">
                <Button type="submit" variant="primary" disabled={busy}>
                  <Icon of={FileSearch} />
                  {open.label}
                </Button>
              </div>
            </form>
          )}
          <section className="fy-card" aria-labelledby={`${id}-reviews`}>
            <div className="fy-card__head">
              <h2 className="fy-h3" id={`${id}-reviews`}>
                Reviews
              </h2>
              <span className="fy-badge fy-badge--quiet">{items.length}</span>
            </div>
            {items.length === 0 ? (
              <p className="fy-card__body fy-small fy-secondary">No review yet.</p>
            ) : (
              <ul className="fy-list" aria-labelledby={`${id}-reviews`}>
                {items.map((r) => (
                  <li key={r.id} className="fy-list__item">
                    <span className="fy-list__main">
                      <Link className="fy-list__title" to="/accounting/reviews/$reviewId" params={{ reviewId: r.id }}>
                        {r.period.name}
                      </Link>
                      <span className="fy-list__meta">
                        By {r.opened_by.name ?? "the Accountant"}, {date(r.opened_at)} · {r.queries.length} {r.queries.length === 1 ? "query" : "queries"}
                        {r.unanswered ? `, ${r.unanswered} waiting for an answer` : ""}
                      </span>
                    </span>
                    <Chip s={reviewState[r.status]} />
                  </li>
                ))}
              </ul>
            )}
          </section>
        </>
      )}
    </div>
  );
}

// AccountantReviewPage is one review (FM-7, G4-109): the month and close it covers, its queries with management's
// answers, its adjusting journals and its sign-off. Each change is over the version shown.
export function AccountantReviewPage() {
  const { reviewId } = useParams({ strict: false }) as { reviewId: string };
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const ask = helpText("accountant-review.ask");
  const reply = helpText("accountant-review.answer");
  const linkHelp = helpText("accountant-review.link");
  const sign = helpText("accountant-review.sign-off");
  const [question, setQuestion] = useState("");
  const [answers, setAnswers] = useState<Record<string, string>>({});
  const [journal, setJournal] = useState("");
  const [statement, setStatement] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<{ title: string; p: ApiProblem } | null>(null);
  const [status, setStatus] = useState("");
  const q = useQuery({
    queryKey: ["accountant-review", reviewId],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/accountant-reviews/{review_id}", { params: { path: { review_id: reviewId } } });
      if (error) throw error;
      return data;
    },
  });
  const r = q.data;
  const journals = useQuery({
    queryKey: ["journals", "for-review"],
    enabled: Boolean(r?.you_may_ask),
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/journals", { params: { query: { page_size: 50 } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
  const run = async (title: string, call: () => Promise<{ data?: unknown; error?: unknown }>, done: string, after?: () => void) => {
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, call);
    setBusy(false);
    if (!out.data) return setProblem({ title, p: problemOf(out.error) });
    after?.();
    setStatus(done);
    await queries.invalidateQueries({ queryKey: ["accountant-review", reviewId] });
    await queries.invalidateQueries({ queryKey: ["accountant-reviews"] });
  };
  if (q.isError) return <Refusal problem={problemOf(q.error)} title="The review could not be shown" />;
  if (!r) return <p className="fy-secondary">Loading the review…</p>;
  const header = () => ({ "If-Match": `"${r.version}"`, "Idempotency-Key": newKey() });
  const path = { review_id: r.id };
  return (
    <div className="fy-page fy-page--narrow fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/accounting">Accounting</Link>
            <span aria-hidden="true">›</span>
            <Link to="/accounting/reviews">Accountant reviews</Link>
          </nav>
          <h1 className="fy-h1">Review of {r.period.name}</h1>
          <p className="fy-secondary">
            Opened by {r.opened_by.name ?? "the Accountant"} on {dateTime(r.opened_at)}. It covers the close of {dateTime(r.close.attested_at)}.
          </p>
        </div>
        <div className="fy-pagehead__actions">
          <Chip s={reviewState[r.status]} />
        </div>
      </div>
      <p className="fy-small fy-secondary" role="status">
        {status}
      </p>
      {problem && <Refusal problem={problem.p} title={problem.title} />}
      {r.status === "superseded" && (
        <div className="fy-banner fy-banner--info" role="note">
          <Icon of={History} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">Superseded{r.superseded_at ? ` on ${dateTime(r.superseded_at)}` : ""}</span>
            <span className="fy-small">{r.period.name} was reopened. This review is kept as it was; the month closed again is reviewed afresh.</span>
          </div>
        </div>
      )}
      <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-labelledby={`${id}-close`}>
        <h2 className="fy-h3" id={`${id}-close`}>
          The close it covers
        </h2>
        <p className="fy-small">
          Snapshot SHA-256 <span className="fy-mono break-all">{r.close.snapshot_sha256}</span>
        </p>
        <Link className="fy-small" to="/accounting/close/$periodId" params={{ periodId: r.period.id }}>
          {r.period.name}'s close
        </Link>
      </section>
      <section className="fy-card" aria-labelledby={`${id}-queries`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-queries`}>
            Queries
          </h2>
          <span className="fy-badge fy-badge--quiet">{r.unanswered ? `${r.unanswered} waiting` : r.queries.length}</span>
        </div>
        {r.queries.length === 0 ? (
          <p className="fy-card__body fy-small fy-secondary">No query raised.</p>
        ) : (
          <ol className="fy-list" aria-labelledby={`${id}-queries`}>
            {r.queries.map((x) => (
              <li key={x.id} className="fy-list__item items-start">
                <span className="fy-list__main fy-stack fy-stack--2">
                  <span className="fy-list__title whitespace-normal">{x.question}</span>
                  <span className="fy-list__meta">
                    {x.asked_by.name ?? "The Accountant"}, {dateTime(x.asked_at)}
                  </span>
                  {x.answer ? (
                    <span className="fy-small whitespace-normal">
                      <span className="fy-strong">Answer: </span>
                      {x.answer} ({x.answered_by?.name ?? "management"}, {x.answered_at ? dateTime(x.answered_at) : ""})
                    </span>
                  ) : r.you_may_answer ? (
                    <span className="fy-field">
                      <label className="fy-label" htmlFor={`${id}-a-${x.id}`}>
                        Your answer
                      </label>
                      <textarea className="fy-textarea" id={`${id}-a-${x.id}`} value={answers[x.id] ?? ""} maxLength={2000} onChange={(e) => setAnswers({ ...answers, [x.id]: e.target.value })} />
                      <Button
                        size="sm"
                        variant="primary"
                        className="self-start"
                        disabled={busy || !(answers[x.id] ?? "").trim()}
                        title={reply.tooltip}
                        onClick={() =>
                          void run(
                            "The answer was not saved",
                            () =>
                              api.POST("/v1/accountant-reviews/{review_id}/queries/{query_id}/answer", {
                                params: { path: { ...path, query_id: x.id }, header: header() },
                                body: { answer: (answers[x.id] ?? "").trim() },
                              }),
                            "Answer saved.",
                          )
                        }
                      >
                        <Icon of={Reply} />
                        {reply.label}
                      </Button>
                    </span>
                  ) : (
                    <span className="fy-small fy-secondary">Waiting for an Owner or the Controller to answer.</span>
                  )}
                </span>
              </li>
            ))}
          </ol>
        )}
        {r.you_may_ask && (
          <div className="fy-card__body fy-field">
            <label className="fy-label" htmlFor={`${id}-question`}>
              New query
            </label>
            <textarea className="fy-textarea" id={`${id}-question`} value={question} maxLength={2000} onChange={(e) => setQuestion(e.target.value)} />
            <Button
              size="sm"
              variant="secondary"
              className="self-start"
              disabled={busy || !question.trim()}
              title={ask.tooltip}
              onClick={() =>
                void run(
                  "The query was not raised",
                  () => api.POST("/v1/accountant-reviews/{review_id}/queries", { params: { path, header: header() }, body: { question: question.trim() } }),
                  "Query raised: an Owner or the Controller answers it.",
                  () => setQuestion(""),
                )
              }
            >
              <Icon of={MessageSquarePlus} />
              {ask.label}
            </Button>
          </div>
        )}
      </section>
      <section className="fy-card" aria-labelledby={`${id}-journals`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-journals`}>
            Adjusting journals
          </h2>
        </div>
        <p className="fy-card__body fy-small fy-secondary">
          Each posts as any journal does, through approval above the self-approval threshold. One dated in a closed month needs the month reopened first, which supersedes
          this review.
        </p>
        {r.journals.length > 0 && (
          <ul className="fy-list" aria-labelledby={`${id}-journals`}>
            {r.journals.map((j) => (
              <li key={j.journal_id} className="fy-list__item">
                <span className="fy-list__main">
                  <Link className="fy-list__title" to="/accounting/journals/$journalId" params={{ journalId: j.journal_id }}>
                    {j.number ?? "Draft journal"}
                  </Link>
                  <span className="fy-list__meta">{j.memo}</span>
                </span>
                <span className="fy-chip fy-chip--neutral">{j.status === "posted" ? "Posted" : j.status === "draft" ? "Draft" : "Reversed"}</span>
              </li>
            ))}
          </ul>
        )}
        {r.you_may_ask && (
          <div className="fy-card__body fy-field">
            <label className="fy-label" htmlFor={`${id}-journal`}>
              Journal
            </label>
            <select className="fy-select" id={`${id}-journal`} value={journal} onChange={(e) => setJournal(e.target.value)}>
              <option value="">Choose a journal</option>
              {(journals.data ?? []).map((x) => (
                <option key={x.id} value={x.id}>
                  {x.number ?? "Draft"} · {x.memo}
                </option>
              ))}
            </select>
            <Button
              size="sm"
              variant="secondary"
              className="self-start"
              disabled={busy || !journal}
              title={linkHelp.tooltip}
              onClick={() =>
                void run(
                  "The journal was not linked",
                  () => api.POST("/v1/accountant-reviews/{review_id}/journals", { params: { path, header: header() }, body: { journal_id: journal } }),
                  "Journal linked.",
                  () => setJournal(""),
                )
              }
            >
              <Icon of={Link2} />
              {linkHelp.label}
            </Button>
          </div>
        )}
      </section>
      {r.signed_by ? (
        <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-labelledby={`${id}-signed`}>
          <h2 className="fy-h3" id={`${id}-signed`}>
            Signed off
          </h2>
          <p className="fy-small">
            By {r.signed_by.name ?? "the Accountant"} on {r.signed_at ? dateTime(r.signed_at) : ""}: "{r.statement}"
          </p>
        </section>
      ) : (
        r.status === "in_review" &&
        r.you_may_ask && (
          <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-sign`}>
            <h2 className="fy-h3" id={`${id}-sign`}>
              {sign.label}
            </h2>
            {!r.you_may_sign_off && <p className="fy-small fy-secondary">Each query needs its answer first.</p>}
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-statement`}>
                What you sign off
              </label>
              <textarea className="fy-textarea" id={`${id}-statement`} value={statement} maxLength={1000} onChange={(e) => setStatement(e.target.value)} />
            </div>
            <Button
              variant="primary"
              className="self-start"
              disabled={busy || !r.you_may_sign_off || !statement.trim()}
              title={sign.tooltip}
              onClick={() =>
                void run(
                  "The review was not signed off",
                  () => api.POST("/v1/accountant-reviews/{review_id}/sign-off", { params: { path, header: header() }, body: { statement: statement.trim() } }),
                  "Signed off.",
                )
              }
            >
              <Icon of={ClipboardCheck} />
              {sign.label}
            </Button>
          </section>
        )
      )}
    </div>
  );
}
