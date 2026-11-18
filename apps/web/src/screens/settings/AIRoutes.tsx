import { useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { ArrowDown, ArrowUp, ChevronLeft, CircleCheck, CircleOff, Hourglass, Plus, ShieldCheck, Trash2, TriangleAlert } from "lucide-react";
import { useId, useState, type FormEvent, type ReactNode } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import type { Approval } from "../approvals/words";
import { statuses } from "../checks/data";
import { aiAgents, aiConnections, members, personName } from "./AISettings";
import { SettingsPage } from "./SettingsPage";

type Agent = components["schemas"]["AIAgentRoute"];
type Route = components["schemas"]["AIRoute"];
type Step = Route["steps"][number];
type RouteSet = components["schemas"]["AIRouteSet"];
type StepInput = RouteSet["steps"] extends (infer T)[] | null ? T : never;
type DataClass = Agent["handles"][number];
type Preset = components["schemas"]["AIPreset"];
type ModelList = components["schemas"]["AIModelList"];

// classWords names the data classes as the guide does (AI-74, AI-75).
export const classWords: Record<DataClass, string> = {
  "know-how": "Know-how",
  "business-figures": "Business figures",
  "personal-data": "Personal data",
  payroll: "Payroll",
};

const standingWords: Record<Step["qualification"], { label: string; tone: string; icon: typeof CircleCheck }> = {
  qualified: { label: "Qualified", tone: "fy-chip--good", icon: CircleCheck },
  out_of_date: { label: "To qualify again", tone: "fy-chip--warning", icon: TriangleAlert },
  unqualified: { label: "Not qualified", tone: "fy-chip--neutral", icon: CircleOff },
};

async function presets(): Promise<Preset[]> {
  const { data, error } = await api.GET("/v1/ai/presets");
  if (error) throw error;
  return data.presets;
}

async function usage() {
  const { data, error } = await api.GET("/v1/ai/usage");
  if (error) throw error;
  return data;
}

// attention is the AI routes check (C.2 #55); none when the person may not read it.
async function attention() {
  const { data, error } = await api.GET("/v1/checks/{key}", { params: { path: { key: "ai.model-routes" } } });
  if (error) return null;
  return data;
}

// waiting are the route changes that wait for the Owner or a second approver; none when the person may not see them.
async function waiting(): Promise<Approval[]> {
  const r = await api.GET("/v1/approvals", { params: { query: { page_size: 200 } } });
  if (r.error) return [];
  return (r.data.items ?? []).filter((a) => a.object_type === "ai_route" && a.status === "pending");
}

function StepChip({ s }: { s: Step }) {
  const w = standingWords[s.qualification];
  return (
    <span className={`fy-chip ${w.tone}`}>
      <Icon of={w.icon} />
      {w.label}
    </span>
  );
}

// StepLine is one step of a route in words: its connection and model, whether it serves now and why not.
function StepLine({ s, i }: { s: Step; i: number }) {
  return (
    <li className="fy-row fy-row--wrap gap-2">
      <span className="fy-small fy-muted">{i === 0 ? "First choice" : `Fallback ${i}`}</span>
      <span>
        {s.connection} · <span className="fy-mono">{s.model}</span>
      </span>
      <StepChip s={s} />
      {!s.serves && (
        <span className="fy-small fy-secondary">
          not serving{s.why ? `: ${s.why}` : ""}
        </span>
      )}
    </li>
  );
}

function Card({ title, aside, children }: { title: string; aside?: ReactNode; children: ReactNode }) {
  const id = useId();
  return (
    <section className="fy-card" aria-labelledby={id}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={id}>
          {title}
        </h2>
        {aside}
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">{children}</div>
    </section>
  );
}

// AIRoutes is Settings › AI › Model routes (docs/design/screens AiModelRoutes; AI-74, AI-76, AI-78, AI-26, AI-81): which
// connection and model each agent runs on, its fallbacks, the data it handles, whether each step is qualified, what it
// cost this month, and what needs attention.
export function AIRoutes() {
  const h = helpText("settings.ai-routes");
  const agents = useQuery({ queryKey: ["ai-routes"], queryFn: aiAgents });
  const month = useQuery({ queryKey: ["ai-usage", "current"], queryFn: usage });
  const check = useQuery({ queryKey: ["checks", "ai.model-routes"], queryFn: attention });
  const options = useQuery({ queryKey: ["ai-presets"], queryFn: presets });
  const asked = useQuery({ queryKey: ["approvals", "ai-routes"], queryFn: waiting });
  const cost = new Map((month.data?.by_agent ?? []).map((a) => [a.agent, a.cost]));
  const c = check.data;
  return (
    <SettingsPage title={h.label} lead={h.tooltip} screen="ai-model-routes"
      actions={
        <Link className="fy-btn fy-btn--ghost fy-btn--sm" to="/settings/ai">
          <Icon of={ChevronLeft} />
          {helpText("settings.ai").label}
        </Link>
      }>
      {(asked.data ?? []).length > 0 && (
        <div className="fy-banner fy-banner--info" role="status">
          <Icon of={Hourglass} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">{asked.data?.length === 1 ? "A route change waits for approval" : `${asked.data?.length} route changes wait for approval`}</span>
            <span className="fy-small">
              {(asked.data ?? []).map((a, i) => (
                <span key={a.id}>
                  {i > 0 && ", "}
                  <Link to="/approvals/$approvalId" params={{ approvalId: a.id }}>
                    {a.summary}
                  </Link>
                </span>
              ))}
            </span>
          </div>
        </div>
      )}
      {c && c.status !== "ok" && (
        <Card title="Needs attention"
          aside={
            <span className={`fy-chip ${statuses[c.status].tone}`}>
              <Icon of={statuses[c.status].icon} />
              {statuses[c.status].label}
            </span>
          }>
          {c.detail && <p className="fy-secondary">{c.detail}</p>}
          <ul className="fy-list">
            {c.items.map((it, i) => (
              <li key={`${it.kind}-${it.id ?? i}`} className="fy-list__item">
                <span className="fy-list__main">
                  <span className="fy-list__title">{it.label}</span>
                  {it.detail && <span className="fy-list__meta whitespace-normal">{it.detail}</span>}
                </span>
              </li>
            ))}
          </ul>
          {c.fix && <p className="fy-small">{c.fix}</p>}
        </Card>
      )}
      <Card title="Agents" aside={month.data && <span className="fy-small fy-muted">Cost in {month.data.month}, {month.data.currency}</span>}>
        {agents.isError && <Refusal problem={problemOf(agents.error)} title="The routes could not be shown" />}
        {!agents.data && !agents.isError && <p className="fy-small">Loading the routes…</p>}
        <ul className="fy-list">
          {(agents.data ?? []).map((a) => (
            <li key={a.agent} className="fy-list__item flex-wrap items-start">
              <span className="fy-list__main min-w-60 flex-1">
                <Link className="fy-list__title" to="/settings/ai/routes/$agent" params={{ agent: a.agent }}>
                  {a.name}
                </Link>
                <span className="fy-list__meta whitespace-normal">{a.description}</span>
                {a.route ? (
                  <>
                    <span className="fy-small">Data: {a.route.classes.map((x) => classWords[x]).join(", ")}</span>
                    <ol className="fy-stack fy-stack--1 mt-1">
                      {a.route.steps.map((s, i) => (
                        <StepLine key={`${s.connection_id}-${s.model}`} s={s} i={i} />
                      ))}
                    </ol>
                  </>
                ) : (
                  <span className="fy-small fy-secondary">No route: the agent does not run until one is set.</span>
                )}
              </span>
              <span className="fy-num fy-small">{cost.has(a.agent) ? formatAmount(cost.get(a.agent) ?? "0") : "—"}</span>
            </li>
          ))}
        </ul>
      </Card>
      <Card title="Presets">
        <p className="fy-secondary">A preset fills a route from the connections and models you have; you still review and save it in the agent's route.</p>
        <dl className="fy-dl">
          {(options.data ?? []).map((p) => (
            <div key={p.id}>
              <dt>
                {p.id}: {p.name}
              </dt>
              <dd>{p.description}</dd>
            </div>
          ))}
        </dl>
      </Card>
      <Card title="How failover works">
        <p className="fy-secondary">
          When a step cannot serve, the call moves to the next step only if that step may receive every class of data in the call and has the capabilities it needs;
          otherwise it stops with a plain message and an error code. A model serves an agent only while it is qualified for that agent.
        </p>
      </Card>
    </SettingsPage>
  );
}

// draftOf is a route as setAIRoute takes it.
function draftOf(r?: Route): RouteSet {
  return {
    classes: r ? [...r.classes] : [],
    steps: (r?.steps ?? []).map((s) => ({ connection_id: s.connection_id, model: s.model, cache: s.cache, ...(s.effort ? { effort: s.effort } : {}) })),
  };
}

const same = (a: RouteSet, b: RouteSet) => JSON.stringify(a) === JSON.stringify(b);

async function history(agent: string) {
  const { data, error, response } = await api.GET("/v1/ai/routes/{agent}", { params: { path: { agent } } });
  if (error) throw error;
  return { agent: data, etag: response.headers.get("ETag") ?? `"${data.route?.version ?? 0}"` };
}

async function modelsOf(connection: string): Promise<ModelList> {
  const { data, error } = await api.GET("/v1/ai/connections/{connection_id}/models", { params: { path: { connection_id: connection } } });
  if (error) throw error;
  return data;
}

// AIRouteEditor is an agent's route (docs/design/screens AiRouteEditor; AI-74, AI-75, AI-76, AI-77, AI-80): the data
// classes it handles and up to five steps, each a connection that is on and a model its allow-list holds, moved with
// buttons. Saving checks it first; a version that only narrows where data goes is saved at once, one that widens it waits
// for the Owner or a second approver.
export function AIRouteEditor() {
  const { agent } = useParams({ strict: false }) as { agent: string };
  const queries = useQueryClient();
  const confirm = useConfirm();
  const got = useQuery({ queryKey: ["ai-route", agent], queryFn: () => history(agent) });
  const connections = useQuery({ queryKey: ["ai-connections"], queryFn: aiConnections });
  const options = useQuery({ queryKey: ["ai-presets"], queryFn: presets });
  const people = useQuery({ queryKey: ["members", "ai"], queryFn: members });
  const on = (connections.data ?? []).filter((c) => c.state === "enabled");
  const lists = useQueries({ queries: on.map((c) => ({ queryKey: ["ai-models", c.id], queryFn: () => modelsOf(c.id) })) });
  const allowed = new Map(on.map((c, i) => [c.id, (lists[i]?.data?.models ?? []).filter((m) => m.allowed).map((m) => m.model)]));
  const [draft, setDraft] = useState<RouteSet | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [checked, setChecked] = useState<string | null>(null);
  const [asked, setAsked] = useState<string | null>(null);
  const [error, setError] = useState("");
  const [key, setKey] = useState(newKey);
  const h = helpText("settings.ai-route");
  const a = got.data?.agent;
  const base = draftOf(a?.route);
  const d = draft ?? base;
  const steps = d.steps ?? [];
  const classes = d.classes ?? [];
  const dirty = draft !== null && !same(draft, base);
  const edit = (next: RouteSet) => {
    setDraft(next);
    setChecked(null);
    setProblem(null);
  };
  const setStep = (i: number, patch: Partial<StepInput>) => edit({ ...d, steps: steps.map((s, j) => (j === i ? { ...s, ...patch } : s)) });
  const move = (i: number, by: number) => {
    const next = [...steps];
    const [s] = next.splice(i, 1);
    if (s) next.splice(i + by, 0, s);
    edit({ ...d, steps: next });
  };
  const send = async (preview: boolean) => {
    if (classes.length === 0 || steps.length === 0 || steps.some((s) => !s.connection_id || !s.model)) {
      setError("Choose at least one class of data, and a connection and a model for every step.");
      return;
    }
    setError("");
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.PUT("/v1/ai/routes/{agent}", {
        params: { path: { agent }, header: { "If-Match": got.data?.etag ?? '"0"', ...(preview ? { Prefer: "return=preview" } : { "Idempotency-Key": key }) } },
        body: d,
      }),
    );
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    if (preview) {
      setChecked(out.data.approval ? "It can be saved; it widens where the agent's data goes, so it will wait for the Owner or a second approver." : "It can be saved at once.");
      return;
    }
    setAsked(out.data.approval?.id ?? null);
    setDraft(null);
    setKey(newKey());
    await Promise.all(["ai-route", "ai-routes", "approvals"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
  };
  const fill = (p: Preset) => {
    const r = p.agents.find((x) => x.agent === agent)?.route;
    if (r) edit({ classes: r.classes ?? [], steps: r.steps ?? [], preset: p.id });
  };
  const names = new Map((connections.data ?? []).map((c) => [c.id, c.name]));
  return (
    <SettingsPage title={a ? `${h.label}: ${a.name}` : h.label} lead={h.tooltip} screen="ai-route-editor"
      actions={
        <Link className="fy-btn fy-btn--ghost fy-btn--sm" to="/settings/ai/routes">
          <Icon of={ChevronLeft} />
          {helpText("settings.ai-routes").label}
        </Link>
      }>
      {got.isError && <Refusal problem={problemOf(got.error)} title="The route could not be shown" />}
      {!got.data && !got.isError && <p className="fy-small">Loading the route…</p>}
      {asked && (
        <div className="fy-banner fy-banner--info" role="status">
          <Icon of={Hourglass} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">Waiting for the Owner or a second approver</span>
            <span className="fy-small">
              The route in force stays until it is approved.{" "}
              <Link to="/approvals/$approvalId" params={{ approvalId: asked }}>
                View the request
              </Link>
            </span>
          </div>
        </div>
      )}
      {a && (
        <form className="fy-stack fy-stack--4" onSubmit={(e: FormEvent) => (e.preventDefault(), void send(false))} noValidate>
          <Card title="What it handles">
            <p className="fy-secondary">{a.description}</p>
            <fieldset className="fy-stack fy-stack--2">
              <legend className="fy-label">Data it may send on this route</legend>
              {a.handles.map((x) => (
                <label key={x} className="fy-row gap-2">
                  <input type="checkbox" checked={classes.includes(x)}
                    onChange={(e) => edit({ ...d, classes: e.target.checked ? a.handles.filter((y) => y === x || classes.includes(y)) : classes.filter((y) => y !== x) })} />
                  {classWords[x]}
                </label>
              ))}
              <span className="fy-help">Personal data and payroll go only to steps whose processing in Singapore is evidenced.</span>
            </fieldset>
          </Card>
          <Card title="Steps"
            aside={
              (options.data ?? []).some((p) => p.agents.some((x) => x.agent === agent && x.route)) && (
                <span className="fy-row gap-2">
                  {(options.data ?? []).map((p) =>
                    p.agents.some((x) => x.agent === agent && x.route) ? (
                      <Button key={p.id} type="button" size="sm" variant="ghost" onClick={() => fill(p)} title={p.description}>
                        Fill from preset {p.id}
                      </Button>
                    ) : null,
                  )}
                </span>
              )
            }>
            {steps.length === 0 && <p className="fy-secondary">No step yet: add the first choice. Only connections that are on, and models their allow-lists hold, can be chosen.</p>}
            <ol className="fy-stack fy-stack--3">
              {steps.map((s, i) => {
                const models = allowed.get(s.connection_id) ?? [];
                return (
                  <li key={i} className="fy-stack fy-stack--2">
                    <span className="fy-strong">{i === 0 ? "First choice" : `Fallback ${i}`}</span>
                    <div className="fy-grid fy-grid--2 gap-3">
                      <div className="fy-field">
                        <label className="fy-label" htmlFor={`route-c-${i}`}>
                          Connection
                        </label>
                        <select id={`route-c-${i}`} className="fy-select" value={s.connection_id} onChange={(e) => setStep(i, { connection_id: e.target.value, model: "" })}>
                          <option value="">Choose a connection</option>
                          {on.map((c) => (
                            <option key={c.id} value={c.id}>
                              {c.name}
                            </option>
                          ))}
                          {s.connection_id && !on.some((c) => c.id === s.connection_id) && <option value={s.connection_id}>{names.get(s.connection_id) ?? "A connection that is off"}</option>}
                        </select>
                      </div>
                      <div className="fy-field">
                        <label className="fy-label" htmlFor={`route-m-${i}`}>
                          Model
                        </label>
                        <select id={`route-m-${i}`} className="fy-select fy-mono" value={s.model} onChange={(e) => setStep(i, { model: e.target.value })}>
                          <option value="">Choose a model</option>
                          {models.map((m) => (
                            <option key={m} value={m}>
                              {m}
                            </option>
                          ))}
                          {s.model && !models.includes(s.model) && <option value={s.model}>{s.model}</option>}
                        </select>
                      </div>
                      <div className="fy-field">
                        <label className="fy-label" htmlFor={`route-e-${i}`}>
                          Reasoning effort
                        </label>
                        <select id={`route-e-${i}`} className="fy-select" value={s.effort ?? ""}
                          onChange={(e) => setStep(i, { effort: (e.target.value || undefined) as StepInput["effort"] })}>
                          <option value="">The model's default</option>
                          <option value="low">Low</option>
                          <option value="medium">Medium</option>
                          <option value="high">High</option>
                        </select>
                      </div>
                      <label className="fy-row gap-2 self-end">
                        <input type="checkbox" checked={s.cache} onChange={(e) => setStep(i, { cache: e.target.checked })} />
                        Cache repeated instructions
                      </label>
                    </div>
                    <span className="fy-row gap-1">
                      <Button type="button" size="sm" variant="ghost" disabled={i === 0} onClick={() => move(i, -1)} aria-label={`Move step ${i + 1} up`}>
                        <Icon of={ArrowUp} />
                        Up
                      </Button>
                      <Button type="button" size="sm" variant="ghost" disabled={i === steps.length - 1} onClick={() => move(i, 1)} aria-label={`Move step ${i + 1} down`}>
                        <Icon of={ArrowDown} />
                        Down
                      </Button>
                      <Button type="button" size="sm" variant="ghost" onClick={() => edit({ ...d, steps: steps.filter((_, j) => j !== i) })} aria-label={`Remove step ${i + 1}`}>
                        <Icon of={Trash2} />
                        Remove
                      </Button>
                    </span>
                  </li>
                );
              })}
            </ol>
            {steps.length < 5 && (
              <Button type="button" size="sm" variant="secondary" className="self-start" onClick={() => edit({ ...d, steps: [...steps, { connection_id: "", model: "", cache: false }] })}>
                <Icon of={Plus} />
                {steps.length === 0 ? "Add the first choice" : "Add a fallback"}
              </Button>
            )}
          </Card>
          {problem && <Refusal problem={problem} title="The route cannot be saved" />}
          {error && (
            <p className="fy-error" role="alert">
              {error}
            </p>
          )}
          {checked && (
            <p className="fy-strong" role="status">
              {checked}
            </p>
          )}
          <div className="fy-row fy-row--wrap gap-2">
            <Button type="submit" variant="primary" disabled={busy || !dirty} title={helpText("ai.route-save").tooltip}>
              {busy ? "Saving…" : helpText("ai.route-save").label}
            </Button>
            <Button type="button" variant="secondary" disabled={busy || !dirty} onClick={() => void send(true)}>
              <Icon of={ShieldCheck} />
              Check it
            </Button>
            <Button type="button" variant="ghost" disabled={busy || !dirty} onClick={() => edit(base)}>
              Undo the changes
            </Button>
            {dirty && <span className="fy-small fy-muted">Unsaved changes</span>}
          </div>
          <p className="fy-small fy-secondary">
            Reordering, removing a step or a class, the effort and caching are saved at once. A new connection, model or class waits for the Owner or a second
            approver, and needs every step's model qualified for this agent. Its daily fallback threshold is in{" "}
            <Link to="/settings/organisation">Organisation settings</Link>; its budget in <Link to="/settings/ai">AI settings</Link>.
          </p>
        </form>
      )}
      {a && a.versions.length > 0 && (
        <Card title="Versions">
          <ul className="fy-list">
            {a.versions.map((v) => (
              <li key={v.version} className="fy-list__item">
                <span className="fy-list__main">
                  <span className="fy-list__title">
                    Version {v.version}
                    {v.preset ? `, from preset ${v.preset}` : ""}
                  </span>
                  <span className="fy-list__meta whitespace-normal">
                    {dateTime(v.made_at)} by {personName(v.made_by, { agents: [], connections: [], people: people.data ?? [] })} ·{" "}
                    {v.steps.map((s) => `${s.connection} ${s.model}`).join(" → ")}
                  </span>
                </span>
              </li>
            ))}
          </ul>
        </Card>
      )}
    </SettingsPage>
  );
}
