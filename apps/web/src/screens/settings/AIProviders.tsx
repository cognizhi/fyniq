import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChartColumn, CircleCheck, CircleOff, Hourglass, KeyRound, Plug, Plus } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { collect, ManifestFields, type ManifestValues } from "../../components/ManifestFields";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import type { Approval } from "../approvals/words";
import { aiConnections } from "./AISettings";
import type { CatalogueEntry } from "./connectorWords";
import { SettingsPage } from "./SettingsPage";

type Connection = components["schemas"]["AIConnection"];
type Preset = components["schemas"]["CataloguePreset"];
type Auth = components["schemas"]["AIConnectionDefinition"]["auth"];

async function modelProviders(): Promise<CatalogueEntry[]> {
  const { data, error } = await api.GET("/v1/connector-catalogue", { params: { query: { page_size: 200 } } });
  if (error) throw error;
  return (data.items ?? []).filter((e) => e.model);
}

async function policy() {
  const { data, error } = await api.GET("/v1/ai/data-policy");
  if (error) throw error;
  return data;
}

async function usage() {
  const { data, error } = await api.GET("/v1/ai/usage");
  if (error) throw error;
  return data;
}

// waiting are the requests about AI connections that wait for a second approver; none when the person may not see them.
async function waiting(): Promise<Approval[]> {
  const r = await api.GET("/v1/approvals", { params: { query: { page_size: 200 } } });
  if (r.error) return [];
  return (r.data.items ?? []).filter((a) => a.object_type === "ai_connection" && a.status === "pending");
}

const authWords: Record<Auth, string> = {
  "api-key": "An API key",
  "workload-identity": "The service's own cloud account",
  none: "None: a server on the organisation's own network",
};

// connectable are the ways of signing in a connection may use, of those a provider or preset offers.
function connectable(offered: readonly string[]): Auth[] {
  return offered.filter((a): a is Auth => a in authWords);
}

// billingText is how a connection is paid for, in words.
function billingText(c: Connection): string {
  if (c.billing.mode === "usage") return "Usage-based";
  const q = c.billing.quota;
  return q ? `Subscription, ${formatAmount(String(q.amount))} ${q.unit} a ${q.window} for ${q.currency} ${formatAmount(q.fee)}` : "Subscription";
}

// AIProviders is Settings › AI › Providers (docs/design/screens AiProviders; AI-71, AI-72, AI-73, AI-75): the
// organisation's connections to AI model providers, which data may go where, and adding one from a provider or one of
// its presets. Every connection starts off; adding, changing or turning one on needs a second approver.
export function AIProviders() {
  const id = useId();
  const h = helpText("settings.ai-providers");
  const connections = useQuery({ queryKey: ["ai-connections"], queryFn: aiConnections });
  const catalogue = useQuery({ queryKey: ["catalogue", "models"], queryFn: modelProviders });
  const rules = useQuery({ queryKey: ["ai-data-policy"], queryFn: policy });
  const month = useQuery({ queryKey: ["ai-usage", "current"], queryFn: usage });
  const asked = useQuery({ queryKey: ["approvals", "ai-connections"], queryFn: waiting });
  const [adding, setAdding] = useState<{ entry: CatalogueEntry; preset?: Preset } | null>(null);
  const [requested, setRequested] = useState<string | null>(null);
  const names = new Map((connections.data ?? []).map((c) => [c.id, c.name]));
  const costOf = (c: Connection) => month.data?.by_connection.find((x) => x.connection_id === c.id)?.cost;
  return (
    <SettingsPage title={h.label} lead={h.tooltip} screen="ai-providers"
      actions={
        <Link className="fy-btn fy-btn--secondary fy-btn--sm" to="/settings/ai/usage">
          <Icon of={ChartColumn} />
          {helpText("settings.ai-usage").label}
        </Link>
      }>
      {requested && !(asked.data ?? []).some((a) => a.id === requested) && (
        <div className="fy-banner fy-banner--info" role="status">
          <Icon of={Hourglass} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">Waiting for a second approver</span>
            <span className="fy-small">
              The connection is added once another member who approves it does; it starts off.{" "}
              <Link to="/approvals/$approvalId" params={{ approvalId: requested }}>
                View the request
              </Link>
            </span>
          </div>
        </div>
      )}
      {(asked.data ?? []).map((a) => (
        <div key={a.id} className="fy-banner fy-banner--info" role="status">
          <Icon of={Hourglass} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">Waiting for a second approver</span>
            <span className="fy-small">
              {a.summary}. Nothing changes until another member who approves it does.{" "}
              <Link to="/approvals/$approvalId" params={{ approvalId: a.id }}>
                View the request
              </Link>
            </span>
          </div>
        </div>
      ))}

      <section className="fy-card" aria-labelledby={`${id}-connections`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-connections`}>
            Connections
          </h2>
          {connections.data && (
            <span className="fy-small fy-muted">
              {connections.data.filter((c) => c.state === "enabled").length} on · {connections.data.filter((c) => c.state !== "enabled").length} off
            </span>
          )}
        </div>
        {connections.isError && <Refusal problem={problemOf(connections.error)} title="The connections could not be shown" />}
        {connections.data && connections.data.length === 0 && <p className="fy-card__body fy-secondary">No connection yet: add one below. Nothing reaches an AI provider until one is on.</p>}
        {connections.data && connections.data.length > 0 && (
          <div className="fy-tablewrap">
            <table className="fy-table fy-table--cards" aria-labelledby={`${id}-connections`}>
              <thead>
                <tr>
                  <th scope="col">Connection</th>
                  <th scope="col">Status</th>
                  <th scope="col">Billing</th>
                  <th scope="col">Key</th>
                  <th scope="col" className="fy-num">
                    This month ({month.data?.currency ?? ""})
                  </th>
                </tr>
              </thead>
              <tbody>
                {connections.data.map((c) => (
                  <tr key={c.id}>
                    <td data-card="title" className="whitespace-normal">
                      <span className="fy-stack gap-0">
                        <Link to="/settings/ai/providers/$connection" params={{ connection: c.id }}>
                          {c.name}
                        </Link>
                        <span className="fy-small fy-secondary font-normal">
                          {c.connector}
                          {c.preset ? ` · ${c.preset}` : ""} · {c.host}
                        </span>
                      </span>
                    </td>
                    <td>
                      {c.state === "enabled" ? (
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
                    </td>
                    <td>{billingText(c)}</td>
                    <td>{c.auth !== "api-key" ? authWords[c.auth] : c.credential.set ? "Set, never shown" : "Not set"}</td>
                    <td className="fy-num">{formatAmount(costOf(c) ?? "0")}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <section className="fy-card" aria-labelledby={`${id}-policy`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-policy`} title={helpText("ai.data-policy").tooltip}>
            {helpText("ai.data-policy").label}
          </h2>
          {rules.data && <span className="fy-small fy-muted">{rules.data.version === 0 ? "The defaults" : `Version ${rules.data.version}`}</span>}
        </div>
        <div className="fy-card__body fy-stack fy-stack--2">
          <p className="fy-secondary">
            Know-how and business figures may go to any connection that is on. Personal data and payroll go only where processing in Singapore is
            evidenced. Restricted data goes nowhere. NRIC and FIN numbers, full bank account numbers and secrets never leave Fyniq.
          </p>
          {rules.data &&
            [...Object.entries(rules.data.allow).map(([k, v]) => ["also to", k, v] as const), ...Object.entries(rules.data.exclude).map(([k, v]) => ["never to", k, v] as const)]
              .filter(([, , v]) => (v ?? []).length > 0)
              .map(([how, cls, v]) => (
                <p key={`${how}-${cls}`} className="fy-small">
                  {cls}, {how}: {(v ?? []).map((x) => names.get(x) ?? "a connection").join(", ")}
                </p>
              ))}
        </div>
      </section>

      <section className="fy-card" aria-labelledby={`${id}-add`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-add`}>
            Add a connection
          </h2>
          <span className="fy-small fy-muted">Off until a second approver turns it on</span>
        </div>
        {catalogue.isError && <Refusal problem={problemOf(catalogue.error)} title="The providers could not be shown" />}
        <ul className="fy-list" aria-labelledby={`${id}-add`}>
          {(catalogue.data ?? []).flatMap((e) => [
            <li key={e.id} className="fy-list__item">
              <span className="fy-list__icon">
                <Icon of={Plug} size={20} />
              </span>
              <span className="fy-list__main">
                <span className="fy-list__title">{e.name}</span>
                <span className="fy-list__meta whitespace-normal">{e.summary}</span>
              </span>
              <Button size="sm" onClick={() => setAdding({ entry: e })} aria-label={`Set up ${e.name}`}>
                <Icon of={Plus} />
                Set up
              </Button>
            </li>,
            ...(e.model?.presets ?? []).map((p) => (
              <li key={`${e.id}-${p.id}`} className="fy-list__item">
                <span className="fy-list__icon">
                  <Icon of={Plug} size={20} />
                </span>
                <span className="fy-list__main">
                  <span className="fy-list__title">{p.name}</span>
                  <span className="fy-list__meta whitespace-normal">
                    {p.self_hosted ? "A server on the organisation's own network, reached privately" : `Through ${e.name}${p.base_url ? `, at ${p.base_url}` : ""}`}
                  </span>
                </span>
                <Button size="sm" onClick={() => setAdding({ entry: e, preset: p })} aria-label={`Set up ${p.name}`}>
                  <Icon of={Plus} />
                  Set up
                </Button>
              </li>
            )),
          ])}
        </ul>
      </section>

      <section className="fy-card" aria-labelledby={`${id}-paid`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-paid`}>
            How a connection is paid for
          </h2>
        </div>
        <dl className="fy-dl fy-card__body">
          <div>
            <dt>Usage-based</dt>
            <dd>Charged per token. Fyniq works out each request's cost from the provider's own usage figures and its price.</dd>
          </div>
          <div>
            <dt>Subscription</dt>
            <dd>A fixed fee with a quota. When the quota runs out, the agent moves to its next route step or stops; it never switches to paying per token on its own.</dd>
          </div>
          <div>
            <dt>A person's own plan</dt>
            <dd>Not offered yet: it arrives in phase 2, for that person's own chats only.</dd>
          </div>
          <div>
            <dt>Not supported</dt>
            <dd>Signing in with a consumer chat plan meant for the provider's own apps, or reusing a chat session or browser cookie.</dd>
          </div>
        </dl>
      </section>
      {adding && (
        <AddConnection entry={adding.entry} preset={adding.preset} onClose={() => setAdding(null)} onAsked={(approval) => setRequested(approval)} />
      )}
    </SettingsPage>
  );
}

// AddConnection asks for a new connection (AI-72): its name, how it signs in and its key, how it is paid for and its
// configuration. It is added once a second approver approves the request, and starts off.
function AddConnection({ entry, preset, onClose, onAsked }: { entry: CatalogueEntry; preset?: Preset; onClose: () => void; onAsked: (approval: string) => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const auths = connectable(preset?.auth ?? entry.model?.auth ?? []);
  const [name, setName] = useState(preset?.name ?? entry.name);
  const [auth, setAuth] = useState<Auth>(auths[0] ?? "api-key");
  const [credential, setCredential] = useState("");
  const [mode, setMode] = useState<"usage" | "subscription">("usage");
  const [quota, setQuota] = useState({ amount: "", unit: "tokens", window: "month", fee: "", currency: "USD" });
  const [values, setValues] = useState<ManifestValues>({});
  const [autoQualify, setAutoQualify] = useState(false);
  const [bad, setBad] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);
  const h = helpText("ai.connection-add");
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const { out, errs } = collect(entry.configuration, values);
    if (!name.trim()) errs.name = "Give the connection a name.";
    if (auth === "api-key" && !credential.trim()) errs.credential = "Paste the provider's API key.";
    if (mode === "subscription" && (!/^\d+$/.test(quota.amount) || !/^\d+(\.\d{1,4})?$/.test(quota.fee) || !/^[A-Z]{3}$/.test(quota.currency))) {
      errs.quota = "Give the quota as a whole number, the fee as an amount such as 30.00, and the fee's currency, such as USD.";
    }
    setBad(errs);
    if (Object.keys(errs).length > 0) return;
    setBusy(true);
    setProblem(null);
    const body = {
      connector: entry.id,
      name: name.trim(),
      ...(preset ? { preset: preset.id } : {}),
      auth,
      configuration: out,
      billing:
        mode === "usage"
          ? { mode }
          : { mode, quota: { amount: Number.parseInt(quota.amount, 10), unit: quota.unit as "tokens" | "requests", window: quota.window as "day" | "month", fee: quota.fee, currency: quota.currency } },
      ...(auth === "api-key" ? { credential: credential.trim() } : {}),
      auto_qualify: autoQualify,
    };
    const res = await guarded(confirm, () => api.POST("/v1/ai/connections", { params: { header: { "Idempotency-Key": key } }, body }));
    setBusy(false);
    if (!res.data) {
      setProblem(problemOf(res.error));
      return;
    }
    await Promise.all(["ai-connections", "approvals"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
    onAsked((res.data as unknown as { id: string }).id);
    onClose();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && onClose()} dismissible={!busy} eyebrow="Add a connection" title={preset?.name ?? entry.name}>
      <form className="fy-stack fy-stack--4" onSubmit={(e) => void submit(e)} noValidate>
        {problem && <Refusal problem={problem} title="The connection was not asked for" />}
        <div className="fy-field">
          <label className="fy-label" htmlFor="ai-conn-name">
            Name
          </label>
          <input id="ai-conn-name" className="fy-input" value={name} onChange={(e) => setName(e.target.value)} aria-invalid={Boolean(bad.name)} aria-describedby="ai-conn-name-err" />
          {bad.name && (
            <span className="fy-error" id="ai-conn-name-err">
              {bad.name}
            </span>
          )}
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor="ai-conn-auth">
            Sign-in
          </label>
          <select id="ai-conn-auth" className="fy-select" value={auth} onChange={(e) => setAuth(e.target.value as Auth)}>
            {auths.map((a) => (
              <option key={a} value={a}>
                {authWords[a]}
              </option>
            ))}
          </select>
        </div>
        {auth === "api-key" && (
          <div className="fy-field">
            <label className="fy-label" htmlFor="ai-conn-key">
              <Icon of={KeyRound} /> API key
            </label>
            <input id="ai-conn-key" className="fy-input fy-mono" type="password" autoComplete="off" value={credential} onChange={(e) => setCredential(e.target.value)}
              aria-invalid={Boolean(bad.credential)} aria-describedby="ai-conn-key-help" />
            <span className={bad.credential ? "fy-error" : "fy-help"} id="ai-conn-key-help">
              {bad.credential || "Kept in the secret store and never shown again, logged, or sent to a model, a browser or a connected app."}
            </span>
          </div>
        )}
        <fieldset className="fy-field">
          <legend className="fy-label">Billing</legend>
          <label className="fy-row gap-2">
            <input type="radio" name="ai-conn-billing" checked={mode === "usage"} onChange={() => setMode("usage")} />
            Usage-based: charged per token
          </label>
          <label className="fy-row gap-2">
            <input type="radio" name="ai-conn-billing" checked={mode === "subscription"} onChange={() => setMode("subscription")} />
            Subscription: a fixed fee with a quota
          </label>
        </fieldset>
        {mode === "subscription" && (
          <div className="fy-grid fy-grid--2 gap-3">
            <div className="fy-field">
              <label className="fy-label" htmlFor="ai-conn-quota">
                Quota
              </label>
              <input id="ai-conn-quota" className="fy-input fy-num" inputMode="numeric" value={quota.amount} onChange={(e) => setQuota({ ...quota, amount: e.target.value })}
                aria-describedby="ai-conn-quota-err" />
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor="ai-conn-unit">
                Counted in
              </label>
              <select id="ai-conn-unit" className="fy-select" value={quota.unit} onChange={(e) => setQuota({ ...quota, unit: e.target.value })}>
                <option value="tokens">Tokens</option>
                <option value="requests">Requests</option>
              </select>
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor="ai-conn-window">
                Resets each
              </label>
              <select id="ai-conn-window" className="fy-select" value={quota.window} onChange={(e) => setQuota({ ...quota, window: e.target.value })}>
                <option value="month">Month</option>
                <option value="day">Day</option>
              </select>
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor="ai-conn-fee">
                Fee each time
              </label>
              <input id="ai-conn-fee" className="fy-input fy-num" inputMode="decimal" value={quota.fee} onChange={(e) => setQuota({ ...quota, fee: e.target.value })} />
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor="ai-conn-currency">
                Fee's currency
              </label>
              <input id="ai-conn-currency" className="fy-input fy-mono" value={quota.currency} maxLength={3} onChange={(e) => setQuota({ ...quota, currency: e.target.value.toUpperCase() })} />
            </div>
            {bad.quota && (
              <span className="fy-error" id="ai-conn-quota-err">
                {bad.quota}
              </span>
            )}
          </div>
        )}
        <ManifestFields schema={entry.configuration} values={values} onChange={setValues} bad={bad} />
        <div className="fy-row fy-row--top gap-3">
          <button className="fy-switch" type="button" role="switch" id="ai-conn-auto" aria-checked={autoQualify} aria-labelledby="ai-conn-auto-l" aria-describedby="ai-conn-auto-help"
            onClick={() => setAutoQualify(!autoQualify)} />
          <span className="fy-stack gap-0.5">
            <span className="fy-strong" id="ai-conn-auto-l">
              Qualify new models automatically
            </span>
            <span className="fy-help" id="ai-conn-auto-help">
              Each model the provider lists for the first time runs the evals of every agent whose route uses this connection. Adding it to the allow-list or
              a route stays a person's decision.
            </span>
          </span>
        </div>
        <p className="fy-small fy-secondary">{h.tooltip}</p>
        <div className="fy-row gap-2">
          <Button type="submit" variant="primary" disabled={busy}>
            {busy ? "Asking…" : h.label}
          </Button>
          <Button type="button" variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
