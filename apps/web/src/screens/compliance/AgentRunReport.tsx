import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { ChevronLeft, Sparkles } from "lucide-react";
import { useId } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { date, dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf } from "../../ui";
import { FindingItem, RunChip, triggerWords } from "./CompanySecretary";

type Report = components["schemas"]["AgentRunReport"];
type Task = components["schemas"]["ComplianceTask"];

const taskStatus: Record<Task["status"], string> = { open: "Open", done: "Done", dismissed: "Dismissed", cleared: "Cleared by a later run" };

async function report(id: string): Promise<Report> {
  const { data, error } = await api.GET("/v1/compliance/secretary/runs/{run_id}", { params: { path: { run_id: id } } });
  if (error) throw error;
  return data;
}

// AgentRunReport is one run of the company secretary assistant (AI-64, AI-65, FR-CS-03; p1-scheduled-agents design D9): when
// and why it ran, what it read (the ledger revision, the packs, the knowledge base), every finding with its rule and
// source, Not assessed first, the tasks they have, the proposals it made that you may see, and the model's words apart,
// labelled AI. A run that failed or was skipped says why.
export function AgentRunReport() {
  const id = useId();
  const { runId } = useParams({ strict: false }) as { runId: string };
  const { mode } = useSignedIn();
  const h = helpText("compliance.secretary-run");
  const q = useQuery({ queryKey: ["agent-run", runId], queryFn: () => report(runId) });
  return (
    <div className="fy-page fy-page--narrow fy-enter">
      <Link className="fy-btn fy-btn--ghost fy-btn--sm -ms-2 self-start" to="/compliance/secretary">
        <Icon of={ChevronLeft} />
        Company secretary assistant
      </Link>
      <h1 className="fy-h1" title={h.tooltip}>
        {h.label}
      </h1>
      {q.isError ? (
        <Refusal problem={problemOf(q.error)} title="The run could not be shown" />
      ) : !q.data ? (
        <p className="fy-small">Loading the run…</p>
      ) : (
        <ReportBody report={q.data} id={id} expert={mode === "expert"} />
      )}
    </div>
  );
}

function ReportBody({ report: r, id, expert }: { report: Report; id: string; expert: boolean }) {
  return (
    <>
      <section className="fy-card" aria-labelledby={`${id}-run`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-run`}>
            {triggerWords(r.trigger)}, {dateTime(r.finished_at ?? r.started_at ?? r.slot_at)}
          </h2>
          <RunChip status={r.status} />
        </div>
        <div className="fy-card__body fy-stack fy-stack--2">
          {r.summary && <p>{r.summary}</p>}
          {r.reason && <p className="fy-small">{r.reason}</p>}
          {r.status === "done" && r.new && <p className="fy-small fy-secondary">What it found is new since the run done before it.</p>}
        </div>
      </section>

      <section className="fy-card" aria-labelledby={`${id}-findings`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-findings`} title={helpText("compliance.secretary-findings").tooltip}>
            Findings
          </h2>
          <span className="fy-small fy-muted">Not assessed and problems first</span>
        </div>
        {r.findings.length === 0 ? (
          <p className="fy-card__body fy-small fy-secondary">This run kept no finding.</p>
        ) : (
          <ul className="fy-list" aria-labelledby={`${id}-findings`}>
            {r.findings.map((f) => (
              <FindingItem key={f.key} finding={f} expert />
            ))}
          </ul>
        )}
      </section>

      {r.model ? (
        <section className="fy-ai-card" aria-labelledby={`${id}-model`}>
          <div className="fy-ai-card__head">
            <span className="fy-ai-badge">
              <Icon of={Sparkles} />
              AI
            </span>
            <h2 className="fy-h3 fy-grow" id={`${id}-model`}>
              Summary for the directors
            </h2>
          </div>
          <div className="fy-ai-card__body">
            <p className="fy-ai-text whitespace-pre-line">{r.model.text.text}</p>
            <p className="fy-small fy-muted">
              The AI wrote this, from the findings above, which are Fyniq's own checks. {expert && `${r.model.model}, ${r.model.tokens} tokens, ${r.model.tool_calls} reads.`}
            </p>
          </div>
        </section>
      ) : (
        r.status === "done" && <p className="fy-small fy-secondary">The model was not asked: nothing it reads had changed since the last run.</p>
      )}

      <section className="fy-card" aria-labelledby={`${id}-inputs`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-inputs`}>
            What this run read
          </h2>
        </div>
        <dl className="fy-card__body fy-dl">
          <div>
            <dt>Due</dt>
            <dd>{dateTime(r.slot_at)}</dd>
          </div>
          <div>
            <dt>The books' revision</dt>
            <dd className="fy-num">{r.ledger_revision}</dd>
          </div>
          <div>
            <dt>Statutory packs</dt>
            <dd>{r.packs.length === 0 ? "None" : r.packs.map((p) => p.version).join(", ")}</dd>
          </div>
          <div>
            <dt>Its skill</dt>
            <dd>Version {r.skill_version}</dd>
          </div>
          {expert && r.kb_snapshot && (
            <div>
              <dt>Knowledge base</dt>
              <dd>{r.kb_snapshot}</dd>
            </div>
          )}
        </dl>
      </section>

      {r.tasks.length > 0 && (
        <section className="fy-card" aria-labelledby={`${id}-tasks`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-tasks`}>
              Their tasks
            </h2>
          </div>
          <ul className="fy-list" aria-labelledby={`${id}-tasks`}>
            {r.tasks.map((t) => (
              <li key={t.id} className="fy-list__item">
                <span className="fy-list__main">
                  <span className="fy-list__title whitespace-normal">{t.title}</span>
                  <span className="fy-list__meta">
                    {taskStatus[t.status]}
                    {t.due_on && ` · due ${date(t.due_on)}`}
                    {t.owner_role && ` · ${t.owner_role.replaceAll("_", " ")}`}
                  </span>
                </span>
              </li>
            ))}
          </ul>
        </section>
      )}

      {r.proposals.length > 0 && (
        <section className="fy-card" aria-labelledby={`${id}-proposals`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-proposals`}>
              What it proposed
            </h2>
          </div>
          <ul className="fy-list" aria-labelledby={`${id}-proposals`}>
            {r.proposals.map((p) => (
              <li key={p.id}>
                <Link className="fy-list__item" to="/inbox/proposals/$proposalId" params={{ proposalId: p.id }}>
                  <span className="fy-list__main">
                    <span className="fy-list__title whitespace-normal">{p.summary ?? p.task_name}</span>
                    <span className="fy-list__meta">
                      {p.task_name} · {p.status}
                    </span>
                  </span>
                  <span className="fy-ai-badge">
                    <Icon of={Sparkles} />
                    AI
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        </section>
      )}
    </>
  );
}
