import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, CircleOff, CircleX, ClockAlert, Pause, Play, RefreshCw, Sparkles, TriangleAlert, type LucideIcon } from "lucide-react";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { date, dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Workspace = components["schemas"]["CompanySecretary"];
type Finding = components["schemas"]["AgentRunFinding"];
type Run = components["schemas"]["AgentRunSummary"];
type Draft = components["schemas"]["AIProposalSummary"];
type Deadline = components["schemas"]["StatutoryItem"];

async function workspace(): Promise<Workspace> {
  const { data, error } = await api.GET("/v1/compliance/secretary");
  if (error) throw error;
  return data;
}

async function runs(): Promise<Run[]> {
  const { data, error } = await api.GET("/v1/compliance/secretary/runs");
  if (error) throw error;
  return data.items;
}

async function drafts(): Promise<Draft[]> {
  const { data, error } = await api.GET("/v1/ai/proposals", { params: { query: { status: "pending", agent: "company_secretary" } } });
  if (error) throw error;
  return data.items;
}

async function deadlines(): Promise<Deadline[]> {
  const { data, error } = await api.GET("/v1/compliance/calendar");
  if (error) throw error;
  return data.deadlines.filter((d) => d.status !== "filed" && d.status !== "not_required").slice(0, 5);
}

// findingLooks are the status chips of a finding (MASTER.md: icon and label, never colour alone).
const findingLooks: Record<Finding["status"], { label: string; icon: LucideIcon; tone: string }> = {
  not_assessed: { label: "Not assessed", icon: TriangleAlert, tone: "warning" },
  blocked: { label: "Cannot go on", icon: CircleX, tone: "critical" },
  due: { label: "Due", icon: ClockAlert, tone: "serious" },
  review: { label: "Please review", icon: TriangleAlert, tone: "warning" },
  ok: { label: "OK", icon: CircleCheck, tone: "good" },
};

export function FindingChip({ status }: { status: Finding["status"] }) {
  const l = findingLooks[status];
  return (
    <span className={`fy-chip fy-chip--${l.tone}`}>
      <Icon of={l.icon} />
      {l.label}
    </span>
  );
}

const runLooks: Record<Run["status"], { label: string; icon: LucideIcon; tone: string }> = {
  queued: { label: "Queued", icon: RefreshCw, tone: "neutral" },
  running: { label: "Running", icon: RefreshCw, tone: "neutral" },
  done: { label: "Done", icon: CircleCheck, tone: "good" },
  failed: { label: "Failed", icon: CircleX, tone: "warning" },
  skipped: { label: "Skipped", icon: CircleOff, tone: "warning" },
};

export function RunChip({ status }: { status: Run["status"] }) {
  const l = runLooks[status];
  return (
    <span className={`fy-chip fy-chip--${l.tone}`}>
      <Icon of={l.icon} />
      {l.label}
    </span>
  );
}

// triggerWords is what started a run, in plain words.
export function triggerWords(trigger: string): string {
  if (trigger === "run_now") return "Run now";
  if (trigger === "schedule:monitor") return "The daily monitor";
  if (trigger === "schedule:cross_check") return "The weekly cross-check";
  if (trigger.startsWith("event:period_closed")) return "A period closed";
  if (trigger.startsWith("event:shares_changed")) return "Shares changed";
  if (trigger.startsWith("event:filing_recorded")) return "A filing recorded";
  return trigger;
}

// countWords sums up a run's findings that need someone.
function countWords(r: Run): string {
  const c = r.counts;
  const parts = [
    c.blocked > 0 && `${c.blocked} cannot go on`,
    c.due > 0 && `${c.due} due`,
    c.review > 0 && `${c.review} to review`,
    c.not_assessed > 0 && `${c.not_assessed} not assessed`,
  ].filter(Boolean);
  return parts.length === 0 ? "nothing open" : parts.join(" · ");
}

// CompanySecretary is Compliance → Company secretary (FR-CS-04 in part, AI-62, AI-63, AI-65, AI-67; p1-scheduled-agents
// design D9): whether the assistant is on, who it acts for, what its latest run found (Basic: its summary; Expert: every
// finding with its rule and source), the open findings with Not assessed first, the obligations coming up, the drafts
// awaiting approval, its history with what is new, its schedule, Run now and Pause. The findings' statuses show in both
// modes, and it never says "all clear" while anything is open.
export function CompanySecretary() {
  const id = useId();
  const { mode } = useSignedIn();
  const expert = mode === "expert";
  const h = helpText("compliance.secretary");
  const ws = useQuery({ queryKey: ["company-secretary"], queryFn: workspace });
  const history = useQuery({ queryKey: ["agent-runs"], queryFn: runs });
  const waiting = useQuery({ queryKey: ["ai-proposals", "company_secretary", "pending"], queryFn: drafts });
  const coming = useQuery({ queryKey: ["statutory-calendar", "next"], queryFn: deadlines });
  if (ws.isError) return <Refusal problem={problemOf(ws.error)} title="The company secretary assistant could not be shown" />;
  if (!ws.data) return <p className="fy-small">Loading the company secretary assistant…</p>;
  const w = ws.data;
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/compliance">Compliance</Link>
          </nav>
          <h1 className="fy-h1" title={h.tooltip}>
            {h.label}
          </h1>
          <p className="fy-secondary">
            It checks the company's obligations on its schedule, explains what it finds, raises tasks and drafts reminders for a person to approve. It never files, signs, approves or marks an obligation as met.
          </p>
        </div>
        <Actions workspace={w} />
      </div>

      <StateCard workspace={w} />

      {w.ai_summary && w.last_done && (
        <section className="fy-ai-card" aria-labelledby={`${id}-ai`}>
          <div className="fy-ai-card__head">
            <span className="fy-ai-badge">
              <Icon of={Sparkles} />
              AI
            </span>
            <h2 className="fy-h3 fy-grow" id={`${id}-ai`}>
              What the AI makes of it
            </h2>
            <span className="fy-small fy-muted">{dateTime(w.last_done.finished_at ?? w.last_done.slot_at)}</span>
          </div>
          <div className="fy-ai-card__body">
            <p className="fy-ai-text whitespace-pre-line">{w.ai_summary.text}</p>
            <p className="fy-small fy-muted">
              The AI wrote this from the findings below, which are Fyniq's own checks.{" "}
              <Link to="/compliance/secretary/runs/$runId" params={{ runId: w.last_done.id }}>
                The run's report
              </Link>
            </p>
          </div>
        </section>
      )}

      <div className="fy-split">
        <div className="fy-stack fy-stack--6">
          <section className="fy-card" aria-labelledby={`${id}-findings`}>
            <div className="fy-card__head">
              <h2 className="fy-h3" id={`${id}-findings`} title={helpText("compliance.secretary-findings").tooltip}>
                What needs you
              </h2>
              {w.findings.length > 0 && <span className="fy-badge fy-badge--quiet">{w.findings.length}</span>}
            </div>
            {!w.last_done ? (
              <p className="fy-card__body fy-small fy-secondary">No run has finished yet.</p>
            ) : (
              <>
                {w.last_done.summary && <p className="fy-card__body">{w.last_done.summary}</p>}
                {w.findings.length === 0 ? (
                  <p className="fy-card__body fy-small fy-secondary">No open finding in the latest run.</p>
                ) : (
                  <ul className="fy-list fy-stagger" aria-labelledby={`${id}-findings`}>
                    {w.findings.map((f) => (
                      <FindingItem key={f.key} finding={f} expert={expert} />
                    ))}
                  </ul>
                )}
              </>
            )}
          </section>

          <section className="fy-card" aria-labelledby={`${id}-drafts`}>
            <div className="fy-card__head">
              <h2 className="fy-h3" id={`${id}-drafts`} title={helpText("compliance.secretary-drafts").tooltip}>
                Drafts awaiting approval
              </h2>
            </div>
            {waiting.isError ? (
              <p className="fy-card__body fy-small fy-secondary">Its drafts are shown to the people who may approve them.</p>
            ) : !waiting.data ? (
              <p className="fy-card__body fy-small">Loading…</p>
            ) : waiting.data.length === 0 ? (
              <p className="fy-card__body fy-small fy-secondary">No draft is waiting.</p>
            ) : (
              <ul className="fy-list" aria-labelledby={`${id}-drafts`}>
                {waiting.data.map((d) => (
                  <li key={d.id}>
                    {d.approval_id ? (
                      <Link className="fy-list__item" to="/approvals/$approvalId" params={{ approvalId: d.approval_id }}>
                        <DraftLine draft={d} />
                      </Link>
                    ) : (
                      <span className="fy-list__item">
                        <DraftLine draft={d} />
                      </span>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </section>

          <section className="fy-card" aria-labelledby={`${id}-coming`}>
            <div className="fy-card__head">
              <h2 className="fy-h3" id={`${id}-coming`}>
                Coming up
              </h2>
              <Link className="fy-small" to="/compliance/calendar">
                The statutory calendar
              </Link>
            </div>
            {coming.isError ? (
              <p className="fy-card__body fy-small fy-secondary">The statutory calendar is not yours to see.</p>
            ) : !coming.data ? (
              <p className="fy-card__body fy-small">Loading…</p>
            ) : coming.data.length === 0 ? (
              <p className="fy-card__body fy-small fy-secondary">Nothing due in the calendar's window.</p>
            ) : (
              <ul className="fy-list" aria-labelledby={`${id}-coming`}>
                {coming.data.map((d) => (
                  <li key={d.key} className="fy-list__item">
                    <span className="fy-list__main">
                      <span className="fy-list__title whitespace-normal">{d.title}</span>
                      <span className="fy-list__meta">{d.due_on ? `Due ${date(d.due_on)}` : d.reason}</span>
                    </span>
                    {(d.status === "overdue" || d.status === "due_soon") && (
                      <span className="fy-chip fy-chip--serious">
                        <Icon of={ClockAlert} />
                        {d.status === "overdue" ? "Overdue" : "Due soon"}
                      </span>
                    )}
                    {d.status === "not_assessed" && (
                      <span className="fy-chip fy-chip--warning">
                        <Icon of={TriangleAlert} />
                        Not assessed
                      </span>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </section>
        </div>

        <aside className="fy-stack fy-stack--6" aria-label="Its schedule and runs">
          <Schedule workspace={w} />
          <section className="fy-card" aria-labelledby={`${id}-history`}>
            <div className="fy-card__head">
              <h2 className="fy-h3" id={`${id}-history`} title={helpText("compliance.secretary-history").tooltip}>
                Its runs
              </h2>
            </div>
            {history.isError ? (
              <Refusal problem={problemOf(history.error)} title="Its runs could not be shown" />
            ) : !history.data ? (
              <p className="fy-card__body fy-small">Loading…</p>
            ) : history.data.length === 0 ? (
              <p className="fy-card__body fy-small fy-secondary">It has not run yet.</p>
            ) : (
              <ul className="fy-list" aria-labelledby={`${id}-history`}>
                {history.data.map((r) => (
                  <li key={r.id}>
                    <Link className="fy-list__item" to="/compliance/secretary/runs/$runId" params={{ runId: r.id }}>
                      <span className="fy-list__main">
                        <span className="fy-list__title whitespace-normal">
                          {triggerWords(r.trigger)}, {dateTime(r.finished_at ?? r.started_at ?? r.slot_at)}
                        </span>
                        <span className="fy-list__meta whitespace-normal">{r.status === "done" ? countWords(r) : r.reason}</span>
                      </span>
                      {r.status === "done" && r.new && <span className="fy-chip fy-chip--neutral">New since the last run</span>}
                      <RunChip status={r.status} />
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </section>
        </aside>
      </div>
    </div>
  );
}

function DraftLine({ draft }: { draft: Draft }) {
  return (
    <>
      <span className="fy-list__main">
        <span className="fy-list__title whitespace-normal">{draft.summary ?? draft.task_name}</span>
        <span className="fy-list__meta">
          {draft.task_name} · sent or made only once approved · expires {dateTime(draft.expires_at)}
        </span>
      </span>
      <span className="fy-ai-badge">
        <Icon of={Sparkles} />
        AI
      </span>
    </>
  );
}

export function FindingItem({ finding: f, expert }: { finding: Finding; expert: boolean }) {
  const l = findingLooks[f.status];
  return (
    <li className="fy-list__item">
      <span className={l.tone === "serious" ? "fy-list__icon" : `fy-list__icon fy-list__icon--${l.tone}`}>
        <Icon of={l.icon} size={20} />
      </span>
      <span className="fy-list__main">
        <span className="fy-list__title whitespace-normal">{f.title}</span>
        {f.detail && <span className="fy-list__meta whitespace-normal">{f.detail}</span>}
        {f.next && <span className="fy-list__meta whitespace-normal">Next: {f.next}</span>}
        {f.due_on && <span className="fy-list__meta">Due {date(f.due_on)}</span>}
        {expert && (f.rule || f.pack || f.source) && (
          <span className="fy-list__meta whitespace-normal">
            {[f.rule && `Rule ${f.rule}`, f.pack, f.source].filter(Boolean).join(" · ")}
          </span>
        )}
      </span>
      <FindingChip status={f.status} />
    </li>
  );
}

function StateCard({ workspace: w }: { workspace: Workspace }) {
  const failed = w.latest && (w.latest.status === "failed" || w.latest.status === "skipped");
  return (
    <section className="fy-card" aria-label="The assistant's state">
      <div className="fy-card__body fy-stack fy-stack--2">
        <p>
          {w.state === "off" && (
            <span className="fy-chip fy-chip--neutral">
              <Icon of={CircleOff} />
              Off
            </span>
          )}
          {w.state === "paused" && (
            <span className="fy-chip fy-chip--warning">
              <Icon of={Pause} />
              Paused
            </span>
          )}
          {w.state === "on" && (
            <span className="fy-chip fy-chip--good">
              <Icon of={CircleCheck} />
              On
            </span>
          )}{" "}
          {w.state === "off"
            ? "An Owner turns it on in Settings → Organisation, and a second approver confirms it."
            : `It acts for ${w.acts_for_name || "the Owner who turned it on"}, who turned it on${w.since ? ` on ${date(w.since)}` : ""}: it reads and proposes as them, never more.`}
        </p>
        {w.state === "paused" && <p className="fy-small fy-secondary">Paused, it runs nothing and keeps its schedule. The statutory calendar's reminders keep coming.</p>}
        {failed && w.latest && (
          <p className="fy-small" role="status">
            <span className="fy-chip fy-chip--warning">
              <Icon of={TriangleAlert} />
              Its last run {w.latest.status === "failed" ? "failed" : "was skipped"}
            </span>{" "}
            {w.latest.reason}
          </p>
        )}
        {w.next_runs.length > 0 && (
          <p className="fy-small fy-secondary">
            Next: {w.next_runs.map((n) => `${n.entry === "monitor" ? "the monitor" : "the cross-check"} ${dateTime(n.at)}`).join("; ")}.
          </p>
        )}
        <p className="fy-small fy-muted">
          <Icon of={Sparkles} /> Its runs are the AI's: what it writes is labelled AI, and each draft waits for a person.
        </p>
      </div>
    </section>
  );
}

// Actions are Run now and Pause or Resume: the server decides who may (C on the AI company secretary's row; pausing is
// a setting change, at once).
function Actions({ workspace: w }: { workspace: Workspace }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const refresh = () => Promise.all(["company-secretary", "agent-runs"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
  const runNow = useMutation({
    mutationFn: async () => {
      const out = await guarded(confirm, () => api.POST("/v1/compliance/secretary/runs", { params: { header: { "Idempotency-Key": newKey() } } }));
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      setProblem(null);
      await refresh();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  const pause = useMutation({
    mutationFn: async (value: "true" | "false") => {
      const path = "/v1/organisation/settings/{key}" as const;
      const params = { path: { key: "agents.company_secretary.paused" } };
      const current = await api.GET(path, { params });
      if (current.error) throw current.error;
      const etag = current.response.headers.get("ETag") ?? "";
      const out = await guarded(confirm, () => api.PATCH(path, { params: { ...params, header: { "If-Match": etag } }, body: { value } }));
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      setProblem(null);
      await refresh();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  if (w.state === "off") return null;
  return (
    <div className="fy-pagehead__actions fy-stack fy-stack--2">
      <div className="flex flex-wrap gap-2">
        <Button onClick={() => runNow.mutate()} disabled={w.state !== "on" || runNow.isPending}>
          <Icon of={Play} />
          Run now
        </Button>
        {w.state === "on" ? (
          <Button variant="secondary" onClick={() => pause.mutate("true")} disabled={pause.isPending}>
            <Icon of={Pause} />
            Pause
          </Button>
        ) : (
          <Button variant="secondary" onClick={() => pause.mutate("false")} disabled={pause.isPending}>
            <Icon of={Play} />
            Resume
          </Button>
        )}
      </div>
      {runNow.isSuccess && <p className="fy-small" role="status">A run is queued: it shows in its runs below.</p>}
      {problem && <Refusal problem={problem} title="That did not happen" />}
    </div>
  );
}

// cronWords says a schedule entry in words when it is a daily or weekly time, such as "Daily 07:00"; else its five fields.
const weekdays = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
export function cronWords(spec: string): string {
  if (spec === "off") return "Off";
  const m = /^(\d{1,2}) (\d{1,2}) \* \* (\*|[0-6])$/.exec(spec);
  if (!m) return `${spec} (Singapore time)`;
  const at = `${m[2]!.padStart(2, "0")}:${m[1]!.padStart(2, "0")}`;
  return m[3] === "*" ? `Daily ${at}` : `${weekdays[Number(m[3])]} ${at}`;
}

function Schedule({ workspace: w }: { workspace: Workspace }) {
  const id = useId();
  const s = w.schedule;
  const entries: [string, string, string][] = [
    ["monitor", cronWords(s.monitor), "The deadline and lodgement monitor: the model only if something changed"],
    ["cross_check", cronWords(s.cross_check), "The full cross-check, with a summary for the directors"],
  ];
  return (
    <section className="fy-card" aria-labelledby={`${id}-schedule`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-schedule`} title={helpText("compliance.secretary-schedule").tooltip}>
          Its schedule
        </h2>
        <span className="fy-small fy-secondary">Singapore time</span>
      </div>
      <ul className="fy-list" aria-labelledby={`${id}-schedule`}>
        {entries.map(([k, when, what]) => (
          <li key={k} className="fy-list__item">
            <span className="fy-list__main">
              <span className="fy-list__title">{when}</span>
              <span className="fy-list__meta whitespace-normal">{what}</span>
            </span>
          </li>
        ))}
        {s.after_close && (
          <li className="fy-list__item">
            <span className="fy-list__main">
              <span className="fy-list__title">After each month's close</span>
              <span className="fy-list__meta">The books against the registers</span>
            </span>
          </li>
        )}
        {s.fy_end_pack && (
          <li className="fy-list__item">
            <span className="fy-list__main">
              <span className="fy-list__title">One month after the financial year ends</span>
              <span className="fy-list__meta">The year-end route</span>
            </span>
          </li>
        )}
      </ul>
      <dl className="fy-card__body fy-dl">
        <div>
          <dt>Events that start a run</dt>
          <dd>{s.events.map((e) => e.replaceAll("_", " ")).join(", ") || "None"}</dd>
        </div>
        <div>
          <dt>Quiet hours</dt>
          <dd>{s.quiet_hours}</dd>
        </div>
        <div>
          <dt>Who it tells</dt>
          <dd>{s.recipients.map((r) => r.replaceAll("_", " ")).join(", ")}</dd>
        </div>
        <div>
          <dt>How it tells them</dt>
          <dd>{s.channels.join(", ")}</dd>
        </div>
        <div>
          <dt>Tells the Owner before a deadline</dt>
          <dd>{s.escalate_days} days</dd>
        </div>
        <div>
          <dt>Most one run may use</dt>
          <dd className="fy-num">
            {s.tool_calls} reads · {s.run_tokens} tokens
          </dd>
        </div>
      </dl>
      <p className="fy-card__foot fy-small fy-muted">
        {w.skill}. An Owner changes these in <Link to="/settings/organisation">Settings → Organisation</Link>; each change but pausing waits for a second approver.
      </p>
    </section>
  );
}
