import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChevronLeft, ChevronRight, Settings2 } from "lucide-react";
import { useId, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { date, month as monthText } from "../../format/date";
import { percent } from "../../format/decimal";
import { helpText } from "../../i18n";
import { Icon, problemOf } from "../../ui";
import { agentName, aiAgents, aiConnections, members, personName, type Budget, type Names } from "./AISettings";
import { SettingsPage } from "./SettingsPage";

type Usage = components["schemas"]["AIUsage"];
type Totals = components["schemas"]["AIUsageTotals"];

// usage is a month's use: this month's when m is null.
async function usage(m: string | null): Promise<Usage> {
  const { data, error } = await api.GET("/v1/ai/usage", { params: { query: m ? { month: m } : {} } });
  if (error) throw error;
  return data;
}

async function budgets() {
  const { data, error } = await api.GET("/v1/ai/budgets");
  if (error) throw error;
  return data;
}

// shift is the calendar month n months from m, as YYYY-MM.
export function shift(m: string, n: number): string {
  const [y = 0, mo = 1] = m.split("-").map(Number);
  const total = y * 12 + (mo - 1) + n;
  return `${Math.floor(total / 12)}-${String((total % 12) + 1).padStart(2, "0")}`;
}

// tokens is a totals' tokens, every kind.
function tokens(t: Totals): number {
  return t.input_tokens + t.output_tokens + t.cache_read_tokens + t.cache_write_tokens;
}

const count = (n: number) => n.toLocaleString("en-SG");

// AIUsage is Settings › AI › Usage and budgets (docs/design/screens AiUsage; AI-77, AI-73, AI-13): what the assistant
// and the agents used and cost in a month, by agent, connection and model, and person, against the budgets. Costs are
// exact, in the base currency at each day's rate.
export function AIUsage() {
  const id = useId();
  const h = helpText("settings.ai-usage");
  const [m, setM] = useState<string | null>(null);
  const shown = useQuery({ queryKey: ["ai-usage", m ?? "current"], queryFn: () => usage(m) });
  const list = useQuery({ queryKey: ["ai-budgets"], queryFn: budgets });
  const agents = useQuery({ queryKey: ["ai-routes"], queryFn: aiAgents });
  const connections = useQuery({ queryKey: ["ai-connections"], queryFn: aiConnections });
  const people = useQuery({ queryKey: ["members", "ai"], queryFn: members });
  const names: Names = { agents: agents.data ?? [], connections: connections.data ?? [], people: people.data ?? [] };
  const u = shown.data;
  const thisMonth = list.data?.month ?? u?.month ?? "";
  const budgetOf = (scope: string, subject = ""): Budget | undefined => (list.data?.budgets ?? []).find((b) => b.scope === scope && (b.subject ?? "") === subject && b.amount);
  const share = (b: Budget | undefined) => (b?.amount && u?.month === thisMonth ? `${percent(b.used, b.amount)}% of ${b.unit === "tokens" ? `${formatAmount(b.amount)} tokens` : `${u?.currency} ${formatAmount(b.amount)}`}` : "");
  return (
    <SettingsPage title={h.label} lead={h.tooltip} screen="ai-usage"
      actions={
        <Link className="fy-btn fy-btn--secondary fy-btn--sm" to="/settings/ai">
          <Icon of={Settings2} />
          {helpText("settings.ai").label}
        </Link>
      }>
      <div className="fy-row fy-row--wrap gap-2" role="group" aria-label="Month">
        <Button size="sm" variant="ghost" onClick={() => setM(shift(u?.month ?? thisMonth, -1))} disabled={!u} aria-label="The month before">
          <Icon of={ChevronLeft} />
        </Button>
        <span className="fy-h3" aria-live="polite">
          {u ? monthText(u.month) : ""}
        </span>
        <Button size="sm" variant="ghost" onClick={() => setM(shift(u?.month ?? thisMonth, 1))} disabled={!u || u.month >= thisMonth} aria-label="The month after">
          <Icon of={ChevronRight} />
        </Button>
      </div>
      {shown.isError && <Refusal problem={problemOf(shown.error)} title="The usage could not be shown" />}
      {!u && !shown.isError && <p className="fy-small">Loading the usage…</p>}
      {u && (
        <>
          <section className="fy-grid fy-grid--4 gap-3" aria-label="The month at a glance">
            <div className="fy-tile">
              <span className="fy-tile__label">Spent</span>
              <span className="fy-tile__value fy-num">
                {u.currency} {formatAmount(u.total.cost)}
              </span>
              <span className="fy-tile__meta">{share(budgetOf("organisation"))}</span>
            </div>
            <div className="fy-tile">
              <span className="fy-tile__label">Tokens</span>
              <span className="fy-tile__value fy-num">{count(tokens(u.total))}</span>
              <span className="fy-tile__meta">{share(budgetOf("tokens_organisation"))}</span>
            </div>
            <div className="fy-tile">
              <span className="fy-tile__label">Requests</span>
              <span className="fy-tile__value fy-num">{count(u.total.requests)}</span>
              <span className="fy-tile__meta">
                {count(u.total.served)} served · {count(u.total.errors)} failed · {count(u.total.skipped)} skipped
              </span>
            </div>
            <div className="fy-tile">
              <span className="fy-tile__label">Fallbacks</span>
              <span className="fy-tile__value fy-num">{count(u.total.fallbacks)}</span>
              <span className="fy-tile__meta">Served by a route's later step</span>
            </div>
          </section>
          {u.total.unpriced > 0 && (
            <p className="fy-small" role="status">
              {count(u.total.unpriced)} served call(s) had no cost in {u.currency}: a price or the day's exchange rate was missing.
            </p>
          )}

          <section className="fy-card" aria-labelledby={`${id}-agents`}>
            <div className="fy-card__head">
              <h2 className="fy-h3" id={`${id}-agents`}>
                By agent
              </h2>
              <span className="fy-small fy-muted">Alert at 80% · stops at 100%</span>
            </div>
            <div className="fy-tablewrap">
              <table className="fy-table fy-table--cards" aria-labelledby={`${id}-agents`}>
                <thead>
                  <tr>
                    <th scope="col">Agent</th>
                    <th scope="col" className="fy-num">
                      Tokens
                    </th>
                    <th scope="col" className="fy-num">
                      Cost ({u.currency})
                    </th>
                    <th scope="col">Budget</th>
                  </tr>
                </thead>
                <tbody>
                  {u.by_agent.map((a) => (
                    <tr key={a.agent}>
                      <td data-card="title">{agentName(a.agent, names)}</td>
                      <td className="fy-num">{count(tokens(a))}</td>
                      <td className="fy-num">{formatAmount(a.cost)}</td>
                      <td>{share(budgetOf("agent", a.agent)) || "No budget"}</td>
                    </tr>
                  ))}
                  {u.by_agent.length === 0 && (
                    <tr>
                      <td colSpan={4}>No agent used the AI this month.</td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </section>

          <section className="fy-card" aria-labelledby={`${id}-models`}>
            <div className="fy-card__head">
              <h2 className="fy-h3" id={`${id}-models`}>
                By connection and model
              </h2>
            </div>
            <div className="fy-tablewrap">
              <table className="fy-table fy-table--cards" aria-labelledby={`${id}-models`}>
                <thead>
                  <tr>
                    <th scope="col">Model</th>
                    <th scope="col" className="fy-num">
                      Input
                    </th>
                    <th scope="col" className="fy-num">
                      Of which cached
                    </th>
                    <th scope="col" className="fy-num">
                      Output
                    </th>
                    <th scope="col" className="fy-num">
                      Cost ({u.currency})
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {u.by_model.map((x) => (
                    <tr key={`${x.connection_id} ${x.model}`}>
                      <td data-card="title">
                        {x.model}
                        <span className="fy-small fy-secondary block">{names.connections.find((c) => c.id === x.connection_id)?.name ?? "A connection"}</span>
                      </td>
                      <td className="fy-num">{count(x.input_tokens + x.cache_read_tokens)}</td>
                      <td className="fy-num">{count(x.cache_read_tokens)}</td>
                      <td className="fy-num">{count(x.output_tokens)}</td>
                      <td className="fy-num">{formatAmount(x.cost)}</td>
                    </tr>
                  ))}
                  {u.by_model.length === 0 && (
                    <tr>
                      <td colSpan={5}>No model was used this month.</td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
            {u.by_connection
              .filter((c) => c.quota)
              .map((c) => (
                <p key={c.connection_id} className="fy-small fy-card__body">
                  {c.name}: {count(c.quota?.used ?? 0)} of {count(c.quota?.amount ?? 0)} {c.quota?.unit} this {c.quota?.window}, {percent(String(c.quota?.used ?? 0), String(c.quota?.amount ?? 0))}%; it
                  resets on {date(c.quota?.resets)}.
                </p>
              ))}
          </section>

          <section className="fy-card" aria-labelledby={`${id}-people`}>
            <div className="fy-card__head">
              <h2 className="fy-h3" id={`${id}-people`}>
                By person
              </h2>
            </div>
            <div className="fy-tablewrap">
              <table className="fy-table fy-table--cards" aria-labelledby={`${id}-people`}>
                <thead>
                  <tr>
                    <th scope="col">Person</th>
                    <th scope="col" className="fy-num">
                      Tokens
                    </th>
                    <th scope="col" className="fy-num">
                      Cost ({u.currency})
                    </th>
                    <th scope="col">Of their token budget</th>
                  </tr>
                </thead>
                <tbody>
                  {u.by_person.map((p) => (
                    <tr key={p.person}>
                      <td data-card="title">{personName(p.person, names)}</td>
                      <td className="fy-num">{count(tokens(p))}</td>
                      <td className="fy-num">{formatAmount(p.cost)}</td>
                      <td>{share(budgetOf("tokens_person", p.person)) || "No budget"}</td>
                    </tr>
                  ))}
                  {u.by_person.length === 0 && (
                    <tr>
                      <td colSpan={4}>Nobody used the AI this month.</td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </section>

          <section className="fy-card" aria-labelledby={`${id}-how`}>
            <div className="fy-card__head">
              <h2 className="fy-h3" id={`${id}-how`}>
                How budgets work
              </h2>
            </div>
            <p className="fy-card__body fy-secondary">
              Before each call, Fyniq sets aside the most it could cost, and the most tokens it could use, against every budget that applies; a call that
              does not fit is not started. At 80% the Owners are alerted; at 100% no new call starts until the month ends or the Owner raises the budget.
              Probes, evals and retries count too. When the AI stops, deadlines, postings and approvals carry on as usual. Provider invoices are entered as
              bills; compare them with the usage here.
            </p>
          </section>
        </>
      )}
    </SettingsPage>
  );
}
