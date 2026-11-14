import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { ChevronLeft, CircleCheck, CircleOff, FlaskConical, Hourglass, KeyRound, Pencil, Plus, Power, RefreshCw, ShieldCheck, Tag, Trash2, TriangleAlert } from "lucide-react";
import { useId, useState, type FormEvent, type ReactNode } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { collect, initialValues, ManifestFields, propertiesOf, type ManifestValues } from "../../components/ManifestFields";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { sign, typed } from "../../format/decimal";
import { date, dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { aiAgents, members, personName } from "./AISettings";
import { processedIn, type CatalogueEntry } from "./connectorWords";
import { SettingsPage } from "./SettingsPage";

type Connection = components["schemas"]["AIConnection"];
type ModelList = components["schemas"]["AIModelList"];
type Model = ModelList["models"][number];
type Handling = components["schemas"]["AIDataHandling"];
type Entry = Handling["handling"][number];
type Definition = components["schemas"]["AIConnectionDefinition"];

async function connection(id: string): Promise<Connection> {
  const { data, error } = await api.GET("/v1/ai/connections/{connection_id}", { params: { path: { connection_id: id } } });
  if (error) throw error;
  return data;
}

async function models(id: string): Promise<ModelList> {
  const { data, error } = await api.GET("/v1/ai/connections/{connection_id}/models", { params: { path: { connection_id: id } } });
  if (error) throw error;
  return data;
}

async function entryOf(id: string): Promise<CatalogueEntry | undefined> {
  const { data, error } = await api.GET("/v1/connector-catalogue", { params: { query: { page_size: 200 } } });
  if (error) throw error;
  return (data.items ?? []).find((e) => e.id === id);
}

const stateWords: Record<Model["state"], string> = {
  new: "New",
  available: "Available",
  deprecated: "Retiring",
  no_longer_listed: "No longer listed",
  not_callable: "Refused by the provider",
};

const standingWords: Record<string, { label: string; tone: string; icon: typeof CircleCheck }> = {
  qualified: { label: "Qualified", tone: "fy-chip--good", icon: CircleCheck },
  out_of_date: { label: "To qualify again", tone: "fy-chip--warning", icon: TriangleAlert },
  unqualified: { label: "Not qualified", tone: "fy-chip--neutral", icon: CircleOff },
};

// definitionOf is a connection as changeAIConnection takes it.
function definitionOf(c: Connection): Definition {
  return {
    name: c.name,
    ...(c.preset ? { preset: c.preset } : {}),
    auth: c.auth,
    configuration: c.configuration,
    billing: c.billing,
    auto_qualify: c.auto_qualify,
    data_handling: c.data_handling,
  };
}

type Tab = "overview" | "models" | "data";

// AIConnectionPage is an AI connection's own page (docs/design/screens AiProviderConnection; AI-72, AI-75, AI-78, AI-79,
// AI-80, AI-82, AI-88): its settings and key, the models its provider lists with their probes, qualifications and
// prices, and its data handling. Changing it, replacing its key, turning it on and allowing a model each ask a second
// approver; turning it off and removing a model take effect at once.
export function AIConnectionPage() {
  const { connection: id } = useParams({ strict: false }) as { connection: string };
  const queries = useQueryClient();
  const c = useQuery({ queryKey: ["ai-connection", id], queryFn: () => connection(id) });
  const entry = useQuery({ queryKey: ["catalogue", "entry", c.data?.connector], queryFn: () => entryOf(c.data?.connector ?? ""), enabled: Boolean(c.data) });
  const [tab, setTab] = useState<Tab>("overview");
  const [asked, setAsked] = useState<string | null>(null);
  const refresh = () => Promise.all(["ai-connection", "ai-connections", "ai-models", "approvals"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
  const onAsked = async (approval: string | null) => {
    if (approval) setAsked(approval);
    await refresh();
  };
  return (
    <SettingsPage title={c.data?.name ?? helpText("settings.ai-connection").label} lead={helpText("settings.ai-connection").tooltip} screen="ai-provider-connection"
      actions={
        <Link className="fy-btn fy-btn--ghost fy-btn--sm" to="/settings/ai/providers">
          <Icon of={ChevronLeft} />
          {helpText("settings.ai-providers").label}
        </Link>
      }>
      {c.isError && <Refusal problem={problemOf(c.error)} title="The connection could not be shown" />}
      {!c.data && !c.isError && <p className="fy-small">Loading the connection…</p>}
      {asked && (
        <div className="fy-banner fy-banner--info" role="status">
          <Icon of={Hourglass} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">Waiting for a second approver</span>
            <span className="fy-small">
              Nothing changes until another member who approves it does.{" "}
              <Link to="/approvals/$approvalId" params={{ approvalId: asked }}>
                View the request
              </Link>
            </span>
          </div>
        </div>
      )}
      {c.data && (
        <>
          <Head c={c.data} onAsked={onAsked} />
          <div className="fy-tabs" role="tablist" aria-label="The connection">
            {(
              [
                ["overview", "Overview"],
                ["models", "Models"],
                ["data", "Data handling"],
              ] as const
            ).map(([t, label]) => (
              <button key={t} type="button" role="tab" aria-selected={tab === t} onClick={() => setTab(t)}>
                {label}
              </button>
            ))}
          </div>
          {tab === "overview" && <Overview c={c.data} entry={entry.data} onAsked={onAsked} />}
          {tab === "models" && <Models c={c.data} onAsked={onAsked} />}
          {tab === "data" && <DataHandling c={c.data} onAsked={onAsked} />}
        </>
      )}
    </SettingsPage>
  );
}

// useAction runs one write: its busy state, its refusal, and the approval it asked for when it answers 202.
function useAction() {
  const confirm = useConfirm();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const run = async <T,>(call: () => Promise<{ data?: T; error?: unknown; response: Response }>): Promise<{ ok: boolean; approval: string | null; data?: T }> => {
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, call);
    setBusy(false);
    if (out.error || !out.response.ok) {
      setProblem(problemOf(out.error));
      return { ok: false, approval: null };
    }
    return { ok: true, approval: out.response.status === 202 ? ((out.data as unknown as { id?: string })?.id ?? null) : null, data: out.data };
  };
  return { busy, problem, run };
}

function Head({ c, onAsked }: { c: Connection; onAsked: (approval: string | null) => Promise<void> }) {
  const a = useAction();
  const people = useQuery({ queryKey: ["members", "ai"], queryFn: members });
  const on = c.state === "enabled";
  const header = { "If-Match": `"${c.version}"`, "Idempotency-Key": newKey() };
  const enable = () =>
    a.run(() => api.POST("/v1/ai/connections/{connection_id}/enable", { params: { path: { connection_id: c.id }, header } })).then(async (r) => {
      if (r.ok) await onAsked(r.approval);
    });
  const disable = () =>
    a.run(() => api.POST("/v1/ai/connections/{connection_id}/disable", { params: { path: { connection_id: c.id }, header } })).then(async (r) => {
      if (r.ok) await onAsked(r.approval);
    });
  return (
    <section className="fy-card" aria-label="Status">
      <div className="fy-card__body fy-row fy-row--wrap gap-3">
        {on ? (
          <span className="fy-chip fy-chip--good">
            <Icon of={CircleCheck} />
            On
          </span>
        ) : (
          <span className="fy-chip fy-chip--neutral">
            <Icon of={CircleOff} />
            Off
          </span>
        )}
        <span className="fy-secondary fy-grow">
          {c.connector}
          {c.preset ? ` · ${c.preset}` : ""} · {c.host} · added by {personName(c.created_by, { agents: [], connections: [], people: people.data ?? [] })} on {date(c.created_at)}
        </span>
        {on ? (
          <Button variant="destructive" size="sm" onClick={() => void disable()} disabled={a.busy} title={helpText("ai.connection-disable").tooltip}>
            <Icon of={Power} />
            {helpText("ai.connection-disable").label}
          </Button>
        ) : (
          <Button size="sm" onClick={() => void enable()} disabled={a.busy} title={helpText("ai.connection-enable").tooltip}>
            <Icon of={Power} />
            {helpText("ai.connection-enable").label}
          </Button>
        )}
      </div>
      {a.problem && <Refusal problem={a.problem} title={on ? "The connection was not turned off" : "The request was not sent"} />}
    </section>
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

// Overview is the connection's sign-in and key, and its settings: billing, configuration and automatic qualification.
function Overview({ c, entry, onAsked }: { c: Connection; entry?: CatalogueEntry; onAsked: (approval: string | null) => Promise<void> }) {
  const [keying, setKeying] = useState(false);
  const [changing, setChanging] = useState(false);
  const config = propertiesOf(entry?.configuration);
  const agents = useQuery({ queryKey: ["ai-routes"], queryFn: aiAgents });
  const using = (agents.data ?? []).filter((x) => x.route?.steps.some((st) => st.connection_id === c.id));
  return (
    <>
      <Card title="Sign-in and key"
        aside={
          c.auth === "api-key" && (
            <Button size="sm" variant="secondary" onClick={() => setKeying(true)}>
              <Icon of={KeyRound} />
              {helpText("ai.connection-key").label}
            </Button>
          )
        }>
        <dl className="fy-dl">
          <div>
            <dt>Sign-in</dt>
            <dd>{c.auth === "api-key" ? "An API key" : c.auth === "workload-identity" ? "The service's own cloud account" : "None: a server on the organisation's own network"}</dd>
          </div>
          {c.auth === "api-key" && (
            <div>
              <dt>Key</dt>
              <dd>{c.credential.set ? `Set ${dateTime(c.credential.set_at)}, never shown again` : "Not set"}</dd>
            </div>
          )}
          <div>
            <dt>Calls go to</dt>
            <dd className="fy-mono">{c.host}</dd>
          </div>
        </dl>
      </Card>
      <Card title="Settings"
        aside={
          <Button size="sm" variant="secondary" onClick={() => setChanging(true)}>
            <Icon of={Pencil} />
            Change
          </Button>
        }>
        <dl className="fy-dl">
          <div>
            <dt>Billing</dt>
            <dd>
              {c.billing.mode === "usage"
                ? "Usage-based: charged per token"
                : `Subscription: ${formatAmount(String(c.billing.quota?.amount ?? 0))} ${c.billing.quota?.unit} a ${c.billing.quota?.window}, ${c.billing.quota?.currency} ${formatAmount(c.billing.quota?.fee ?? "0")}`}
            </dd>
          </div>
          {(c.billing.quota?.rates ?? []).length > 0 && (
            <div>
              <dt>Models that draw at another rate</dt>
              <dd>
                {(c.billing.quota?.rates ?? []).map((r) => `${r.model} ${r.rate}`).join(", ")}; others 1.{" "}
                {c.billing.quota?.terms && (
                  <>
                    From <a href={c.billing.quota.terms.url}>{c.billing.quota.terms.title}</a>, read {date(c.billing.quota.terms.checked_on)}.
                  </>
                )}
              </dd>
            </div>
          )}
          <div>
            <dt>Qualify new models automatically</dt>
            <dd>{c.auto_qualify ? "On" : "Off"}</dd>
          </div>
          {config.map(([k, p]) => (
            <div key={k}>
              <dt>{p.title ?? k}</dt>
              <dd className="fy-mono">{c.configuration[k] === undefined ? `${String(p.default ?? "Not set")} (the default)` : String(c.configuration[k])}</dd>
            </div>
          ))}
        </dl>
      </Card>
      <Card title="Agents that use it">
        {agents.data && using.length === 0 && <p className="fy-secondary">No agent's route names this connection yet.</p>}
        <ul className="fy-list">
          {using.map((x) => (
            <li key={x.agent} className="fy-list__item">
              <span className="fy-list__main">
                <span className="fy-list__title">{x.name}</span>
                <span className="fy-list__meta whitespace-normal">
                  {(x.route?.steps ?? [])
                    .map((st, i) => (st.connection_id === c.id ? `${i === 0 ? "primary" : `fallback ${i}`}: ${st.model}${st.serves ? "" : `, not serving${st.why ? ` (${st.why})` : ""}`}` : ""))
                    .filter(Boolean)
                    .join("; ")}
                </span>
              </span>
            </li>
          ))}
        </ul>
      </Card>
      {keying && <KeyForm c={c} onClose={() => setKeying(false)} onAsked={onAsked} />}
      {changing && entry && <ChangeForm c={c} entry={entry} onClose={() => setChanging(false)} onAsked={onAsked} />}
    </>
  );
}

// KeyForm replaces the connection's API key, write-only: the new key waits for a second approver (AI-79).
function KeyForm({ c, onClose, onAsked }: { c: Connection; onClose: () => void; onAsked: (approval: string | null) => Promise<void> }) {
  const a = useAction();
  const [value, setValue] = useState("");
  const [error, setError] = useState("");
  const [key] = useState(newKey);
  const h = helpText("ai.connection-key");
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!value.trim()) {
      setError("Paste the new API key.");
      return;
    }
    setError("");
    const r = await a.run(() =>
      api.PUT("/v1/ai/connections/{connection_id}/credential", {
        params: { path: { connection_id: c.id }, header: { "If-Match": `"${c.version}"`, "Idempotency-Key": key } },
        body: { value: value.trim() },
      }),
    );
    if (r.ok) {
      await onAsked(r.approval);
      onClose();
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !a.busy && onClose()} dismissible={!a.busy} eyebrow={c.name} title={h.label}>
      <form className="fy-stack fy-stack--4" onSubmit={(e) => void submit(e)} noValidate>
        {a.problem && <Refusal problem={a.problem} title="The key was not replaced" />}
        <div className="fy-field">
          <label className="fy-label" htmlFor="ai-key">
            New API key
          </label>
          <input id="ai-key" className="fy-input fy-mono" type="password" autoComplete="off" value={value} onChange={(e) => setValue(e.target.value)}
            aria-invalid={error !== ""} aria-describedby="ai-key-help" />
          <span className={error ? "fy-error" : "fy-help"} id="ai-key-help">
            {error || h.tooltip}
          </span>
        </div>
        <div className="fy-row gap-2">
          <Button type="submit" variant="primary" disabled={a.busy}>
            {a.busy ? "Asking…" : "Ask to replace it"}
          </Button>
          <Button type="button" variant="ghost" onClick={onClose} disabled={a.busy}>
            Cancel
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// ChangeForm asks to change the connection's name, billing, configuration and automatic qualification (AI-72).
function ChangeForm({ c, entry, onClose, onAsked }: { c: Connection; entry: CatalogueEntry; onClose: () => void; onAsked: (approval: string | null) => Promise<void> }) {
  const a = useAction();
  const [name, setName] = useState(c.name);
  const [values, setValues] = useState<ManifestValues>(() => initialValues(entry.configuration, c.configuration));
  const [autoQualify, setAutoQualify] = useState(c.auto_qualify);
  const [bad, setBad] = useState<Record<string, string>>({});
  const [key] = useState(newKey);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const { out, errs } = collect(entry.configuration, values);
    if (!name.trim()) errs.name = "Give the connection a name.";
    setBad(errs);
    if (Object.keys(errs).length > 0) return;
    const r = await a.run(() =>
      api.PUT("/v1/ai/connections/{connection_id}", {
        params: { path: { connection_id: c.id }, header: { "If-Match": `"${c.version}"`, "Idempotency-Key": key } },
        body: { ...definitionOf(c), name: name.trim(), configuration: out, auto_qualify: autoQualify },
      }),
    );
    if (r.ok) {
      await onAsked(r.approval);
      onClose();
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !a.busy && onClose()} dismissible={!a.busy} eyebrow={c.name} title="Change the connection">
      <form className="fy-stack fy-stack--4" onSubmit={(e) => void submit(e)} noValidate>
        {a.problem && <Refusal problem={a.problem} title="The change was not asked for" />}
        <div className="fy-field">
          <label className="fy-label" htmlFor="ai-change-name">
            Name
          </label>
          <input id="ai-change-name" className="fy-input" value={name} onChange={(e) => setName(e.target.value)} aria-invalid={Boolean(bad.name)} aria-describedby="ai-change-name-err" />
          {bad.name && (
            <span className="fy-error" id="ai-change-name-err">
              {bad.name}
            </span>
          )}
        </div>
        <ManifestFields schema={entry.configuration} values={values} onChange={setValues} bad={bad} />
        <div className="fy-row fy-row--top gap-3">
          <button className="fy-switch" type="button" role="switch" id="ai-change-auto" aria-checked={autoQualify} aria-labelledby="ai-change-auto-l" onClick={() => setAutoQualify(!autoQualify)} />
          <span className="fy-strong" id="ai-change-auto-l">
            Qualify new models automatically
          </span>
        </div>
        <p className="fy-small fy-secondary">The change waits for a second approver; until then the connection stays as it is.</p>
        <div className="fy-row gap-2">
          <Button type="submit" variant="primary" disabled={a.busy}>
            {a.busy ? "Asking…" : "Ask to change it"}
          </Button>
          <Button type="button" variant="ghost" onClick={onClose} disabled={a.busy}>
            Cancel
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

type Price = NonNullable<Model["price"]>;

const unitWords: Record<Price["rates"][number]["unit"], string> = { per_million_tokens: "a million tokens", per_image: "an image", per_request: "a request" };

const sourceWords: Record<Price["source"], string> = {
  provider_list: "the provider's list",
  google_billing: "Google's billing catalogue",
  litellm: "Fyniq's model price list",
  owner: "the Owner",
};

// priceText is a model's price in force, in words.
function priceText(m: Model): string {
  if (!m.price) return m.unpriced ? `No price: ${m.unpriced}` : "No price";
  const rates = m.price.rates.map((r) => `${r.kind.replace(/_/g, " ")} ${formatAmount(r.amount)} ${unitWords[r.unit]}`).join(", ");
  return `${m.price.currency} ${rates}, from ${sourceWords[m.price.source]}`;
}

// Models are the models the provider lists for this connection (AI-80, AI-81): the allowed ones with their probes,
// qualifications and prices, and the rest, to probe and allow.
function Models({ c, onAsked }: { c: Connection; onAsked: (approval: string | null) => Promise<void> }) {
  const { session } = useSignedIn();
  const owner = (session.roles ?? []).includes("owner");
  const queries = useQueryClient();
  const list = useQuery({ queryKey: ["ai-models", c.id], queryFn: () => models(c.id) });
  const a = useAction();
  const [qualifying, setQualifying] = useState<string | null>(null);
  const [pricing, setPricing] = useState<string | null>(null);
  const reload = () => queries.invalidateQueries({ queryKey: ["ai-models", c.id] });
  const act = async (call: () => Promise<{ data?: unknown; error?: unknown; response: Response }>) => {
    const r = await a.run(call);
    if (r.ok) {
      await reload();
      if (r.approval) await onAsked(r.approval);
    }
  };
  const path = { connection_id: c.id };
  if (list.isError) return <Refusal problem={problemOf(list.error)} title="The models could not be shown" />;
  if (!list.data) return <p className="fy-small">Loading the models…</p>;
  const allowed = list.data.models.filter((m) => m.allowed);
  const others = list.data.models.filter((m) => !m.allowed && m.state !== "no_longer_listed");
  const last = list.data.reads[0];
  const stale = !list.data.last_complete || Date.now() - Date.parse(list.data.last_complete) > 3 * 24 * 3600 * 1000;
  return (
    <>
      {stale && (
        <div className="fy-banner fy-banner--warning" role="status">
          <Icon of={TriangleAlert} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">{list.data.last_complete ? "The provider's list is more than 3 days old" : "The provider's list has not been read whole"}</span>
            <span className="fy-small">
              {list.data.last_complete
                ? "The AI routes check warns until it is read again: models the provider stopped listing may still be allowed here."
                : "The connection cannot be turned on until it is (AI_MODEL_LIST_UNAVAILABLE)."}{" "}
              Read it again; if the provider does not answer, check the key.
            </span>
          </div>
        </div>
      )}
      {a.problem && <Refusal problem={a.problem} title="That was not done" />}
      <Card title="The provider's list"
        aside={
          <Button size="sm" variant="secondary" disabled={a.busy}
            onClick={() => void act(() => api.POST("/v1/ai/connections/{connection_id}/models/refresh", { params: { path } }))}>
            <Icon of={RefreshCw} />
            Read it again
          </Button>
        }>
        <p className="fy-secondary">
          {last
            ? `Last read ${dateTime(last.read_at)}: ${last.outcome}, ${last.pages} page(s)${last.failure ? `, ${last.failure}` : ""}. Fyniq has no list of its own; a model the provider stops listing leaves this connection and every route by itself.`
            : "Not read yet: it is read when the key is approved, every morning, and when you ask."}
        </p>
      </Card>
      <Card title="Allowed on this connection">
        {allowed.length === 0 && <p className="fy-secondary">No model is allowed yet: probe one the provider lists below, then ask to allow it.</p>}
        <ul className="fy-list">
          {allowed.map((m) => (
            <li key={m.model} className="fy-list__item flex-wrap items-start">
              <span className="fy-list__main min-w-52 flex-1">
                <span className="fy-list__title fy-mono">{m.model}</span>
                <span className="fy-list__meta whitespace-normal">
                  {stateWords[m.state]}
                  {m.shutdown ? `, retires ${date(m.shutdown)}` : ""} · {m.probe ? `probe ${m.probe.ok ? "passed" : "failed"} ${dateTime(m.probe.at)}` : "not probed"} · {priceText(m)}
                </span>
                <span className="fy-row fy-row--wrap gap-1 mt-1">
                  {m.qualifications.map((q) => {
                    const w = standingWords[q.standing] ?? standingWords.unqualified!;
                    return (
                      <span key={q.agent} className={`fy-chip ${w.tone}`} title={q.out_of_date_why ?? `${q.passed} of ${q.cases} passed`}>
                        <Icon of={w.icon} />
                        {q.agent}: {w.label}
                      </span>
                    );
                  })}
                </span>
                {m.pending_price && owner && (
                  <span className="fy-row fy-row--wrap gap-2 mt-1">
                    <span className="fy-small">A new price waits for your review: {m.pending_price.review_detail ?? m.pending_price.review}.</span>
                    <Button size="sm" variant="secondary" disabled={a.busy}
                      onClick={() => void act(() => api.POST("/v1/ai/prices/{price_id}/review", { params: { path: { price_id: m.pending_price?.id ?? "" } }, body: { decision: "confirm" } }))}>
                      Confirm it
                    </Button>
                    <Button size="sm" variant="ghost" disabled={a.busy}
                      onClick={() => void act(() => api.POST("/v1/ai/prices/{price_id}/review", { params: { path: { price_id: m.pending_price?.id ?? "" } }, body: { decision: "decline" } }))}>
                      Decline it
                    </Button>
                  </span>
                )}
                {owner && (
                  <span className="fy-row fy-row--wrap gap-2 mt-1">
                    <Button size="sm" variant="ghost" disabled={a.busy} onClick={() => setPricing(m.model)} title={helpText("ai.model-price").tooltip} aria-label={`Enter a price for ${m.model}`}>
                      <Icon of={Tag} />
                      {helpText("ai.model-price").label}
                    </Button>
                    {m.price?.source === "owner" && (
                      <Button size="sm" variant="ghost" disabled={a.busy}
                        onClick={() => void act(() => api.POST("/v1/ai/prices/{price_id}/withdraw", { params: { path: { price_id: m.price?.id ?? "" } } }))}>
                        Withdraw your price
                      </Button>
                    )}
                  </span>
                )}
              </span>
              <Button size="sm" variant="ghost" disabled={a.busy} onClick={() => void act(() => api.POST("/v1/ai/connections/{connection_id}/models/probe", { params: { path }, body: { model: m.model } }))}
                aria-label={`Probe ${m.model}`}>
                <Icon of={FlaskConical} />
                Probe
              </Button>
              <Button size="sm" variant="ghost" disabled={a.busy} onClick={() => setQualifying(m.model)} aria-label={`Qualify ${m.model}`}>
                <Icon of={ShieldCheck} />
                Qualify
              </Button>
              <Button size="sm" variant="destructive" disabled={a.busy}
                onClick={() => void act(() => api.POST("/v1/ai/connections/{connection_id}/models/remove", { params: { path }, body: { model: m.model } }))}
                aria-label={`Remove ${m.model}`}>
                <Icon of={Trash2} />
                Remove
              </Button>
            </li>
          ))}
        </ul>
      </Card>
      <Card title="Other models the provider lists" aside={<span className="fy-badge fy-badge--quiet">{others.length}</span>}>
        <ul className="fy-list">
          {others.map((m) => (
            <li key={m.model} className="fy-list__item flex-wrap">
              <span className="fy-list__main min-w-52 flex-1">
                <span className="fy-list__title fy-mono">{m.model}</span>
                <span className="fy-list__meta whitespace-normal">
                  {stateWords[m.state]}
                  {m.alias_of ? `, an alias of ${m.alias_of}` : ""} · {m.probe ? `probe ${m.probe.ok ? "passed" : "failed"} ${dateTime(m.probe.at)}` : "not probed"}
                </span>
              </span>
              <Button size="sm" variant="ghost" disabled={a.busy} onClick={() => void act(() => api.POST("/v1/ai/connections/{connection_id}/models/probe", { params: { path }, body: { model: m.model } }))}
                aria-label={`Probe ${m.model}`}>
                <Icon of={FlaskConical} />
                Probe
              </Button>
              <Button size="sm" variant="secondary" disabled={a.busy || !m.probe?.ok}
                onClick={() => void act(() => api.POST("/v1/ai/connections/{connection_id}/models/allow", { params: { path, header: { "Idempotency-Key": newKey() } }, body: { model: m.model } }))}
                aria-label={`Ask to allow ${m.model}`} title={m.probe?.ok ? helpText("ai.model-allow").tooltip : "Probe it first"}>
                <Icon of={Plus} />
                {helpText("ai.model-allow").label}
              </Button>
            </li>
          ))}
        </ul>
        <p className="fy-small fy-secondary">
          Model names come only from the provider's list. Allowing one first needs a passing probe, then a second approver; it serves no agent until it passes that
          agent's evals.
        </p>
      </Card>
      {qualifying && <QualifyForm c={c} model={qualifying} onClose={() => setQualifying(null)} onDone={reload} />}
      {pricing && <PriceForm c={c} model={pricing} onClose={() => setPricing(null)} onAsked={onAsked} />}
    </>
  );
}

// PriceForm asks to put the Owner's own price on a model, per million tokens, when no source prices it or its source's
// price should not apply (AI-82); a second approver approves it.
function PriceForm({ c, model, onClose, onAsked }: { c: Connection; model: string; onClose: () => void; onAsked: (approval: string | null) => Promise<void> }) {
  const a = useAction();
  const [currency, setCurrency] = useState(c.billing.quota?.currency ?? "USD");
  const [input, setInput] = useState("");
  const [output, setOutput] = useState("");
  const [bad, setBad] = useState<Record<string, string>>({});
  const [key] = useState(newKey);
  const h = helpText("ai.model-price");
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const errs: Record<string, string> = {};
    const rate = (v: string) => {
      const d = typed(v);
      return d && sign(d) > 0 ? d : null;
    };
    if (!/^[A-Z]{3}$/.test(currency)) errs.currency = "Give the provider's currency as three letters, such as USD.";
    if (!rate(input)) errs.input = "Give the price of a million input tokens, above zero.";
    if (!rate(output)) errs.output = "Give the price of a million output tokens, above zero.";
    setBad(errs);
    if (Object.keys(errs).length > 0) return;
    const r = await a.run(() =>
      api.POST("/v1/ai/connections/{connection_id}/models/price", {
        params: { path: { connection_id: c.id }, header: { "Idempotency-Key": key } },
        body: {
          model,
          currency,
          rates: [
            { kind: "input", amount: rate(input) ?? "" },
            { kind: "output", amount: rate(output) ?? "" },
          ],
        },
      }),
    );
    if (r.ok) {
      await onAsked(r.approval);
      onClose();
    }
  };
  const field = (id: string, label: string, value: string, set: (v: string) => void, mono = false) => (
    <div className="fy-field">
      <label className="fy-label" htmlFor={`ai-price-${id}`}>
        {label}
      </label>
      <input id={`ai-price-${id}`} className={`fy-input ${mono ? "fy-mono" : "fy-num"}`} inputMode={mono ? undefined : "decimal"} value={value} onChange={(e) => set(e.target.value)}
        aria-invalid={Boolean(bad[id])} aria-describedby={bad[id] ? `ai-price-${id}-err` : undefined} />
      {bad[id] && (
        <span className="fy-error" id={`ai-price-${id}-err`}>
          {bad[id]}
        </span>
      )}
    </div>
  );
  return (
    <Dialog open onOpenChange={(o) => !o && !a.busy && onClose()} dismissible={!a.busy} eyebrow={model} title={h.label}>
      <form className="fy-stack fy-stack--4" onSubmit={(e) => void submit(e)} noValidate>
        {a.problem && <Refusal problem={a.problem} title="The price was not asked for" />}
        <p className="fy-small fy-secondary">{h.tooltip}</p>
        {field("currency", "Currency", currency, (v) => setCurrency(v.toUpperCase()), true)}
        {field("input", "A million input tokens", input, setInput)}
        {field("output", "A million output tokens", output, setOutput)}
        <div className="fy-row gap-2">
          <Button type="submit" variant="primary" disabled={a.busy}>
            {a.busy ? "Asking…" : "Ask to use this price"}
          </Button>
          <Button type="button" variant="ghost" onClick={onClose} disabled={a.busy}>
            Cancel
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// QualifyForm runs an agent's evals against a model on this connection (AI-78).
function QualifyForm({ c, model, onClose, onDone }: { c: Connection; model: string; onClose: () => void; onDone: () => Promise<void> }) {
  const agents = useQuery({ queryKey: ["ai-routes"], queryFn: aiAgents });
  const a = useAction();
  const [agent, setAgent] = useState("");
  const [result, setResult] = useState<string | null>(null);
  const h = helpText("ai.model-qualify");
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const chosen = agent || agents.data?.[0]?.agent || "";
    const r = await a.run(() => api.POST("/v1/ai/connections/{connection_id}/models/qualify", { params: { path: { connection_id: c.id } }, body: { model, agent: chosen } }));
    if (r.ok && r.data) {
      const q = r.data as { result: string; passed: number; cases: number; failed: string[] };
      setResult(`${q.result === "pass" ? "Passed" : "Failed"}: ${q.passed} of ${q.cases} cases${q.failed.length ? `; failed ${q.failed.join(", ")}` : ""}.`);
      await onDone();
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !a.busy && onClose()} dismissible={!a.busy} eyebrow={model} title={h.label}>
      <form className="fy-stack fy-stack--4" onSubmit={(e) => void submit(e)} noValidate>
        {a.problem && <Refusal problem={a.problem} title="The evals did not run" />}
        <div className="fy-field">
          <label className="fy-label" htmlFor="ai-qualify-agent">
            The agent whose evals it runs
          </label>
          <select id="ai-qualify-agent" className="fy-select" value={agent} onChange={(e) => setAgent(e.target.value)}>
            {(agents.data ?? []).map((x) => (
              <option key={x.agent} value={x.agent}>
                {x.name}
              </option>
            ))}
          </select>
          <span className="fy-help">{h.tooltip}</span>
        </div>
        {result && (
          <p className="fy-strong" role="status">
            {result}
          </p>
        )}
        <div className="fy-row gap-2">
          <Button type="submit" variant="primary" disabled={a.busy}>
            {a.busy ? "Running the evals…" : "Run the evals"}
          </Button>
          <Button type="button" variant="ghost" onClick={onClose} disabled={a.busy}>
            Close
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// DataHandling is where the connection's prompts are processed and kept, with evidence (AI-75, AI-88); changing it asks
// a second approver.
function DataHandling({ c, onAsked }: { c: Connection; onAsked: (approval: string | null) => Promise<void> }) {
  const [editing, setEditing] = useState(false);
  const d = c.data_handling;
  return (
    <Card title={helpText("ai.data-handling").label}
      aside={
        <Button size="sm" variant="secondary" onClick={() => setEditing(true)} title={helpText("ai.data-handling").tooltip}>
          <Icon of={Pencil} />
          Change
        </Button>
      }>
      {d.handling.length === 0 && <p className="fy-secondary">Not declared yet: switching the connection on needs where its prompts are processed, with evidence.</p>}
      {d.handling.map((e, i) => (
        <dl key={i} className="fy-dl">
          <div>
            <dt>For</dt>
            <dd>
              {e.model === "*" ? "Every model" : e.model} · {e.endpoint} · {e.feature === "*" ? "every feature" : e.feature}
            </dd>
          </div>
          <div>
            <dt>Processed</dt>
            <dd>{processedIn(e.processed_in)}{e.unclear ? ": the evidence is unclear, so it never counts as Singapore" : ""}</dd>
          </div>
          <div>
            <dt>Kept by the provider</dt>
            <dd>{e.retention_days === undefined || e.retention_days === null ? "Not known" : e.retention_days === 0 ? "Not kept: zero data retention" : `${e.retention_days} days`}</dd>
          </div>
          <div>
            <dt>Used for training</dt>
            <dd>{e.training === "no" ? "No" : e.training === "yes" ? "Yes" : "Not known"}</dd>
          </div>
          <div>
            <dt>Evidence</dt>
            <dd>
              {e.evidence.url ? <a href={e.evidence.url}>{e.evidence.title}</a> : e.evidence.title}, checked {date(e.evidence.checked_on)}
            </dd>
          </div>
        </dl>
      ))}
      <p className="fy-small fy-secondary">
        {d.inventory_id ? "Its data inventory row is recorded." : "Its data inventory row is not named yet."} {d.transfer_id ? "Its transfer assessment is recorded." : ""}
      </p>
      {editing && <HandlingForm c={c} onClose={() => setEditing(false)} onAsked={onAsked} />}
    </Card>
  );
}

const blank: Entry = { model: "*", endpoint: "", feature: "*", processed_in: [], training: "unknown", evidence: { kind: "source", url: "", title: "", checked_on: "" } };

// HandlingForm asks to change the data-handling declaration, one row per model, endpoint and feature (AI-88).
function HandlingForm({ c, onClose, onAsked }: { c: Connection; onClose: () => void; onAsked: (approval: string | null) => Promise<void> }) {
  const a = useAction();
  const [rows, setRows] = useState<Entry[]>(c.data_handling.handling.length ? c.data_handling.handling : [{ ...blank, endpoint: c.host }]);
  const [inventory, setInventory] = useState(c.data_handling.inventory_id ?? "");
  const [transfer, setTransfer] = useState(c.data_handling.transfer_id ?? "");
  const [error, setError] = useState("");
  const [key] = useState(newKey);
  const set = (i: number, patch: Partial<Entry>) => setRows(rows.map((r, j) => (j === i ? { ...r, ...patch } : r)));
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const bad = rows.findIndex((r) => (r.processed_in ?? []).length === 0 || !r.evidence.title || !/^\d{4}-\d{2}-\d{2}$/.test(r.evidence.checked_on));
    if (bad >= 0) {
      setError(`Row ${bad + 1}: give where prompts are processed, the evidence's title and the day it was checked (YYYY-MM-DD).`);
      return;
    }
    setError("");
    const handling = rows.map((r) => ({ ...r, evidence: { ...r.evidence, ...(r.evidence.url ? {} : { url: undefined }) } }));
    const r = await a.run(() =>
      api.PUT("/v1/ai/connections/{connection_id}", {
        params: { path: { connection_id: c.id }, header: { "If-Match": `"${c.version}"`, "Idempotency-Key": key } },
        body: { ...definitionOf(c), data_handling: { handling, ...(inventory ? { inventory_id: inventory } : {}), ...(transfer ? { transfer_id: transfer } : {}) } },
      }),
    );
    if (r.ok) {
      await onAsked(r.approval);
      onClose();
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !a.busy && onClose()} dismissible={!a.busy} eyebrow={c.name} title="Change where its prompts go">
      <form className="fy-stack fy-stack--4" onSubmit={(e) => void submit(e)} noValidate>
        {a.problem && <Refusal problem={a.problem} title="The change was not asked for" />}
        {rows.map((r, i) => (
          <fieldset key={i} className="fy-stack fy-stack--2">
            <legend className="fy-strong">Row {i + 1}</legend>
            <div className="fy-grid fy-grid--2 gap-3">
              <div className="fy-field">
                <label className="fy-label" htmlFor={`ai-h-model-${i}`}>
                  Model (* for every model)
                </label>
                <input id={`ai-h-model-${i}`} className="fy-input fy-mono" value={r.model} onChange={(e) => set(i, { model: e.target.value })} />
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`ai-h-in-${i}`}>
                  Processed in (country codes)
                </label>
                <input id={`ai-h-in-${i}`} className="fy-input fy-mono" value={(r.processed_in ?? []).join(", ")}
                  onChange={(e) => set(i, { processed_in: e.target.value.split(/[\s,]+/).filter(Boolean).map((x) => x.toUpperCase()) })} />
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`ai-h-train-${i}`}>
                  Used for training
                </label>
                <select id={`ai-h-train-${i}`} className="fy-select" value={r.training} onChange={(e) => set(i, { training: e.target.value as Entry["training"] })}>
                  <option value="no">No</option>
                  <option value="yes">Yes</option>
                  <option value="unknown">Not known</option>
                </select>
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`ai-h-days-${i}`}>
                  Kept by the provider (days; 0 for none)
                </label>
                <input id={`ai-h-days-${i}`} className="fy-input fy-num" inputMode="numeric" value={r.retention_days ?? ""}
                  onChange={(e) => set(i, { retention_days: e.target.value === "" ? undefined : Number.parseInt(e.target.value, 10) })} />
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`ai-h-url-${i}`}>
                  Evidence: the official page
                </label>
                <input id={`ai-h-url-${i}`} className="fy-input fy-mono" value={r.evidence.url ?? ""} onChange={(e) => set(i, { evidence: { ...r.evidence, url: e.target.value } })} />
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`ai-h-title-${i}`}>
                  Evidence: its title
                </label>
                <input id={`ai-h-title-${i}`} className="fy-input" value={r.evidence.title} onChange={(e) => set(i, { evidence: { ...r.evidence, title: e.target.value } })} />
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`ai-h-day-${i}`}>
                  Evidence: checked on
                </label>
                <input id={`ai-h-day-${i}`} className="fy-input" type="date" value={r.evidence.checked_on} onChange={(e) => set(i, { evidence: { ...r.evidence, checked_on: e.target.value } })} />
              </div>
            </div>
            {rows.length > 1 && (
              <Button type="button" size="sm" variant="ghost" onClick={() => setRows(rows.filter((_, j) => j !== i))}>
                <Icon of={Trash2} />
                Remove row {i + 1}
              </Button>
            )}
          </fieldset>
        ))}
        <Button type="button" size="sm" variant="secondary" onClick={() => setRows([...rows, { ...blank, endpoint: c.host }])}>
          <Icon of={Plus} />
          Add a row
        </Button>
        <div className="fy-grid fy-grid--2 gap-3">
          <div className="fy-field">
            <label className="fy-label" htmlFor="ai-h-inventory">
              Its data inventory row (ID)
            </label>
            <input id="ai-h-inventory" className="fy-input fy-mono" value={inventory} onChange={(e) => setInventory(e.target.value)} />
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor="ai-h-transfer">
              Its transfer assessment (ID), outside Singapore
            </label>
            <input id="ai-h-transfer" className="fy-input fy-mono" value={transfer} onChange={(e) => setTransfer(e.target.value)} />
          </div>
        </div>
        {error && (
          <p className="fy-error" role="alert">
            {error}
          </p>
        )}
        <div className="fy-row gap-2">
          <Button type="submit" variant="primary" disabled={a.busy}>
            {a.busy ? "Asking…" : "Ask to change it"}
          </Button>
          <Button type="button" variant="ghost" onClick={onClose} disabled={a.busy}>
            Cancel
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
