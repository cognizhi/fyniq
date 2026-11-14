import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChartColumn, CircleCheck, CircleOff, Hourglass, Pencil, Plus, Power, TriangleAlert } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { percent, typed } from "../../format/decimal";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { SettingsPage } from "./SettingsPage";

type Settings = components["schemas"]["AISettings"];
export type Budget = components["schemas"]["AIBudget"];
type Member = components["schemas"]["Member"];
type Agent = components["schemas"]["AIAgentRoute"];
type Connection = components["schemas"]["AIConnection"];

async function aiSettings(): Promise<Settings> {
  const { data, error } = await api.GET("/v1/ai/settings");
  if (error) throw error;
  return data;
}

export async function aiAgents(): Promise<Agent[]> {
  const { data, error } = await api.GET("/v1/ai/routes");
  if (error) throw error;
  return data.agents;
}

export async function aiConnections(): Promise<Connection[]> {
  const { data, error } = await api.GET("/v1/ai/connections", { params: { query: { page_size: 500 } } });
  if (error) throw error;
  return data.items;
}

// members are the organisation's people, to name them; none when the person may not list them.
export async function members(): Promise<Member[]> {
  const { data, error } = await api.GET("/v1/members", { params: { query: { page_size: 200 } } });
  if (error) return [];
  return data.items ?? [];
}

// Names are how the AI pages name an agent, a connection and a person.
export interface Names {
  agents: Agent[];
  connections: Connection[];
  people: Member[];
}

// personName is user:<id> or system:<job> in words.
export function personName(who: string, n: Names): string {
  if (who.startsWith("system:")) return `Fyniq (${who.slice(7)})`;
  const id = who.replace(/^user:/, "");
  return n.people.find((m) => m.user_id === id)?.display_name ?? "A former member";
}

export function agentName(id: string, n: Names): string {
  return n.agents.find((a) => a.agent === id)?.name ?? id;
}

// budgetLabel is what a budget covers, in words.
export function budgetLabel(b: Pick<Budget, "scope" | "subject">, n: Names): string {
  switch (b.scope) {
    case "organisation":
      return "The organisation";
    case "tokens_organisation":
      return "The organisation, in tokens";
    case "agent":
      return agentName(b.subject ?? "", n);
    case "connection":
      return n.connections.find((c) => c.id === b.subject)?.name ?? "A connection";
    case "person":
      return personName(b.subject ?? "", n);
    case "tokens_person":
      return `${personName(b.subject ?? "", n)}, in tokens`;
  }
  return b.scope;
}

// budgetAmount is an amount in the budget's unit.
export function budgetAmount(b: Budget, value: string, currency: string): string {
  return b.unit === "tokens" ? `${formatAmount(value)} tokens` : `${currency} ${formatAmount(value)}`;
}

// AISettings is Settings › AI (docs/design/screens AiSettings; AI-13, AI-77, AI-86): the AI switch, the monthly budgets
// and the providers and routes at a glance. The assistant's own settings (proposals, task types, payroll access,
// exclusions) join it with p1-ai-assistant and p1-ai-proposals.
export function AISettings() {
  const { session } = useSignedIn();
  const owner = (session.roles ?? []).includes("owner");
  const settings = useQuery({ queryKey: ["ai-settings"], queryFn: aiSettings });
  const agents = useQuery({ queryKey: ["ai-routes"], queryFn: aiAgents });
  const connections = useQuery({ queryKey: ["ai-connections"], queryFn: aiConnections });
  const people = useQuery({ queryKey: ["members", "ai"], queryFn: members });
  const h = helpText("settings.ai");
  const names: Names = { agents: agents.data ?? [], connections: connections.data ?? [], people: people.data ?? [] };
  return (
    <SettingsPage title={h.label} lead={h.tooltip} screen="ai-settings"
      actions={
        <Link className="fy-btn fy-btn--secondary fy-btn--sm" to="/settings/ai/usage">
          <Icon of={ChartColumn} />
          {helpText("settings.ai-usage").label}
        </Link>
      }>
      {settings.isError && <Refusal problem={problemOf(settings.error)} title="The AI settings could not be shown" />}
      {!settings.data && !settings.isError && <p className="fy-small">Loading the AI settings…</p>}
      {settings.data && (
        <>
          <SwitchCard settings={settings.data} owner={owner} />
          <BudgetsCard list={settings.data.budgets} owner={owner} names={names} />
          <ProvidersCard agents={agents.data} connections={connections.data} />
        </>
      )}
    </SettingsPage>
  );
}

// SwitchCard is the AI switch (AI-13): off at once by the Owner, or by anyone who may change settings; on only with a
// second approver.
function SwitchCard({ settings, owner }: { settings: Settings; owner: boolean }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [asked, setAsked] = useState<string | null>(null);
  const [sure, setSure] = useState(false);
  const on = settings.switch.value === "true";
  const off = helpText("ai.switch-off");
  const ask = helpText("ai.switch-on");
  const refresh = () => Promise.all(["ai-settings", "settings", "approvals", "shell"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
  const switchOff = async () => {
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const out = await api.POST("/v1/ai/settings/switch-off", { params: { header: { "Idempotency-Key": key } } });
    setBusy(false);
    setSure(false);
    if (out.error) {
      setProblem(problemOf(out.error));
      return;
    }
    await refresh();
  };
  const askOn = async () => {
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.PATCH("/v1/organisation/settings/{key}", {
        params: { path: { key: settings.switch.key }, header: { "If-Match": `"${settings.switch.version}"` } },
        body: { value: "true" },
      }),
    );
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    if (out.response.status === 202) setAsked((out.data as unknown as { id: string }).id);
    await refresh();
  };
  return (
    <section className="fy-card" aria-labelledby={`${id}-switch`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-switch`}>
          The AI switch
        </h2>
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
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        <p className="fy-secondary">
          {on
            ? "The assistant and the agents run. Turning it off stops new requests at once, and a reply being written within seconds; accounting, checks and deadlines carry on."
            : "Nothing is sent to any AI provider. Turning it on needs a second approver; connections can still be set up and tested while it is off."}
        </p>
        {problem && <Refusal problem={problem} title={on ? "The AI was not turned off" : "The request was not sent"} />}
        {asked && (
          <p className="fy-small" role="status">
            <Icon of={Hourglass} /> Asked: it turns on once another member who approves settings approves it.{" "}
            <Link to="/approvals/$approvalId" params={{ approvalId: asked }}>
              Open the request
            </Link>
          </p>
        )}
        <div className="fy-row fy-row--wrap gap-2">
          {on && owner && !sure && (
            <Button variant="destructive" size="sm" onClick={() => setSure(true)} disabled={busy} title={off.tooltip}>
              <Icon of={Power} />
              {off.label}
            </Button>
          )}
          {on && owner && sure && (
            <>
              <Button variant="destructive" size="sm" onClick={() => void switchOff()} disabled={busy}>
                <Icon of={Power} />
                {busy ? "Turning it off…" : "Yes, turn it off now"}
              </Button>
              <Button variant="ghost" size="sm" onClick={() => setSure(false)} disabled={busy}>
                Keep it on
              </Button>
            </>
          )}
          {!on && !asked && (
            <Button size="sm" onClick={() => void askOn()} disabled={busy} title={ask.tooltip}>
              <Icon of={Power} />
              {busy ? "Asking…" : ask.label}
            </Button>
          )}
        </div>
      </div>
    </section>
  );
}

// BudgetsCard is the monthly budgets (AI-77, AI-86, AI-13): in the base currency and in tokens, each with what this month
// used of it; the Owner sets, changes and removes them.
function BudgetsCard({ list, owner, names }: { list: Settings["budgets"]; owner: boolean; names: Names }) {
  const id = useId();
  const [editing, setEditing] = useState<Budget | "new" | null>(null);
  const h = helpText("ai.budgets");
  return (
    <section className="fy-card" aria-labelledby={`${id}-budgets`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-budgets`} title={h.tooltip}>
          {h.label}
        </h2>
        {owner && (
          <Button size="sm" variant="secondary" onClick={() => setEditing("new")}>
            <Icon of={Plus} />
            Add a budget
          </Button>
        )}
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        <p className="fy-small fy-secondary">
          For {list.month}, in Singapore time. Before each call Fyniq sets aside the most it could cost, and the most tokens it could use, against every
          budget that applies: at 80% the Owners are alerted, and at 100% no new call starts until the month ends or the Owner raises the budget.
        </p>
        {list.budgets.length === 0 ? (
          <p className="fy-small">No budget is set: the AI may spend without a limit.</p>
        ) : (
          <ul className="fy-list" aria-labelledby={`${id}-budgets`}>
            {list.budgets.map((b) => {
              const share = b.amount ? percent(b.used, b.amount) : 0;
              return (
                <li key={b.key} className="fy-list__item">
                  <span className="fy-list__main">
                    <span className="fy-list__title whitespace-normal">{budgetLabel(b, names)}</span>
                    <span className="fy-list__meta whitespace-normal">
                      {b.amount
                        ? `${budgetAmount(b, b.used, list.currency)} of ${budgetAmount(b, b.amount, list.currency)} used · ${share}%`
                        : "Removed"}
                    </span>
                  </span>
                  {b.amount && share >= 100 ? (
                    <span className="fy-chip fy-chip--critical">
                      <Icon of={CircleOff} />
                      Spent
                    </span>
                  ) : b.amount && b.alerted ? (
                    <span className="fy-chip fy-chip--warning">
                      <Icon of={TriangleAlert} />
                      80% reached
                    </span>
                  ) : null}
                  {owner && (
                    <Button size="sm" variant="ghost" onClick={() => setEditing(b)} aria-label={`Change the budget of ${budgetLabel(b, names)}`}>
                      <Icon of={Pencil} />
                      Change
                    </Button>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </div>
      {editing && <BudgetForm budget={editing === "new" ? null : editing} names={names} currency={list.currency} onClose={() => setEditing(null)} />}
    </section>
  );
}

// choices are the budgets the Owner may add, by key: the organisation's, each agent's and connection's, and each
// person's, in the base currency or in tokens.
function choices(n: Names, currency: string): [string, string][] {
  return [
    ["organisation", `The organisation (${currency})`],
    ["tokens:organisation", "The organisation (tokens)"],
    ...n.agents.map((a): [string, string] => [`agent:${a.agent}`, `${a.name} (${currency})`]),
    ...n.connections.map((c): [string, string] => [`connection:${c.id}`, `${c.name} (${currency})`]),
    ...n.people.flatMap((m): [string, string][] => [
      [`person:${m.user_id}`, `${m.display_name} (${currency})`],
      [`tokens:person:${m.user_id}`, `${m.display_name} (tokens)`],
    ]),
  ];
}

// BudgetForm sets a budget's monthly amount, or removes it when left empty (AI-77).
function BudgetForm({ budget, names, currency, onClose }: { budget: Budget | null; names: Names; currency: string; onClose: () => void }) {
  const queries = useQueryClient();
  const options = choices(names, currency);
  const [key, setKey] = useState(budget?.key ?? options[0]?.[0] ?? "organisation");
  const [amount, setAmount] = useState(budget?.amount ?? "");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [error, setError] = useState("");
  const tokens = key.startsWith("tokens:");
  const h = helpText("ai.budget-set");
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const value = amount.trim() === "" ? "" : tokens ? (/^\d{1,15}$/.test(amount.trim().replace(/,/g, "")) ? amount.trim().replace(/,/g, "") : null) : typed(amount);
    if (value === null || (value !== "" && value.startsWith("-"))) {
      setError(tokens ? "Enter a whole number of tokens, such as 5000000." : `Enter an amount in ${currency}, such as 300.00.`);
      return;
    }
    if (value === "" && !budget) {
      setError("Enter the monthly amount.");
      return;
    }
    setError("");
    setBusy(true);
    setProblem(null);
    const out = await api.PUT("/v1/ai/budgets/{budget}", {
      params: { path: { budget: key }, header: { "If-Match": `"${budget?.version ?? 0}"`, "Idempotency-Key": newKey() } },
      body: { amount: value === "" ? null : value },
    });
    setBusy(false);
    if (out.error) {
      setProblem(problemOf(out.error));
      return;
    }
    await queries.invalidateQueries({ queryKey: ["ai-settings"] });
    await queries.invalidateQueries({ queryKey: ["ai-budgets"] });
    onClose();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && onClose()} dismissible={!busy} eyebrow="Budgets" title={budget ? `Change the budget of ${budgetLabel(budget, names)}` : "Add a budget"}>
      <form className="fy-stack fy-stack--4" onSubmit={(e) => void submit(e)} noValidate>
        {problem && <Refusal problem={problem} title="The budget was not saved" />}
        {!budget && (
          <div className="fy-field">
            <label className="fy-label" htmlFor="ai-budget-key">
              Budget for
            </label>
            <select id="ai-budget-key" className="fy-input" value={key} onChange={(e) => setKey(e.target.value)}>
              {options.map(([k, label]) => (
                <option key={k} value={k}>
                  {label}
                </option>
              ))}
            </select>
          </div>
        )}
        <div className="fy-field">
          <label className="fy-label" htmlFor="ai-budget-amount">
            {tokens ? "Tokens a month" : `Amount a month (${currency})`}
          </label>
          <input id="ai-budget-amount" className="fy-input fy-num" inputMode={tokens ? "numeric" : "decimal"} value={amount}
            onChange={(e) => setAmount(e.target.value)} aria-invalid={error !== ""} aria-describedby="ai-budget-help" />
          <span id="ai-budget-help" className={error ? "fy-error" : "fy-help"}>
            {error || (budget ? "Leave it empty to remove the budget." : h.tooltip)}
          </span>
        </div>
        <div className="fy-row gap-2">
          <Button type="submit" disabled={busy}>
            {busy ? "Saving…" : h.label}
          </Button>
          <Button type="button" variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// ProvidersCard is the providers and routes at a glance (AI-72, AI-74): connections on and waiting, and agents routed.
function ProvidersCard({ agents, connections }: { agents?: Agent[]; connections?: Connection[] }) {
  const id = useId();
  if (!agents || !connections) return null;
  const on = connections.filter((c) => c.state === "enabled");
  const waiting = connections.filter((c) => c.state !== "enabled");
  const routed = agents.filter((a) => a.route);
  return (
    <section className="fy-card" aria-labelledby={`${id}-providers`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-providers`}>
          Providers and model routes
        </h2>
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        <dl className="fy-dl">
          <div>
            <dt>Connections on</dt>
            <dd>{on.length ? on.map((c) => c.name).join(", ") : "None"}</dd>
          </div>
          <div>
            <dt>Waiting or off</dt>
            <dd>{waiting.length ? waiting.map((c) => c.name).join(", ") : "None"}</dd>
          </div>
          <div>
            <dt>Agents</dt>
            <dd>
              {routed.length} of {agents.length} have a route
              {routed.length ? `, last set ${date(routed.map((a) => a.route?.made_at ?? "").sort().at(-1))}` : ""}
            </dd>
          </div>
        </dl>
        <p className="fy-small fy-secondary">A connection or model serves an agent only after it passes that agent's evals and a second approver approves it.</p>
        <Link className="fy-btn fy-btn--secondary fy-btn--sm self-start" to="/settings/ai/providers">
          {helpText("settings.ai-providers").label}
        </Link>
      </div>
    </section>
  );
}
