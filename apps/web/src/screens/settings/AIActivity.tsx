import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, CircleSlash, CircleX, EyeOff, Info, Repeat, X } from "lucide-react";
import { useId, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf } from "../../ui";
import { aiAgents, members, personName } from "./AISettings";
import { classWords } from "./AIRoutes";
import { SettingsPage } from "./SettingsPage";

type Entry = components["schemas"]["AIActivityEntry"];

const outcomeWords: Record<Entry["outcome"], { label: string; tone: string; icon: typeof CircleCheck }> = {
  ok: { label: "Served", tone: "fy-chip--good", icon: CircleCheck },
  skipped: { label: "Skipped", tone: "fy-chip--neutral", icon: CircleSlash },
  busy: { label: "Provider busy", tone: "fy-chip--warning", icon: CircleX },
  timeout: { label: "Timed out", tone: "fy-chip--warning", icon: CircleX },
  not_found: { label: "Model not found", tone: "fy-chip--critical", icon: CircleX },
  refused: { label: "Refused by the provider", tone: "fy-chip--critical", icon: CircleX },
  failed: { label: "Failed", tone: "fy-chip--critical", icon: CircleX },
  stopped: { label: "Stopped", tone: "fy-chip--neutral", icon: CircleSlash },
};

const count = (n: number) => n.toLocaleString("en-SG");

interface Filter {
  agent: string;
  person: string;
  run: string;
  from: string;
  to: string;
}

// AIActivity is Settings › AI › AI activity (docs/design/screens AiActivityLog; AI-12, AI-74, AI-76): every attempt of a
// model call, newest first: each member reads their own, the Owner and the DPO everyone's. Someone else's entry that
// carried payroll shows masked to a reader who may not see payroll unmasked (FR-PAY-13).
export function AIActivity() {
  const id = useId();
  const h = helpText("settings.ai-activity");
  const { organisation } = useSignedIn();
  const base = organisation?.base_currency ?? "SGD";
  const [filter, setFilter] = useState<Filter>({ agent: "", person: "", run: "", from: "", to: "" });
  const agents = useQuery({ queryKey: ["ai-routes"], queryFn: aiAgents, retry: false });
  const people = useQuery({ queryKey: ["members", "ai"], queryFn: members });
  const list = useInfiniteQuery({
    queryKey: ["ai-activity", filter],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const query = { page_size: 50, cursor: pageParam, ...Object.fromEntries(Object.entries(filter).filter(([, v]) => v !== "")) };
      const { data, error } = await api.GET("/v1/ai/activity", { params: { query } });
      if (error) throw error;
      return data;
    },
    getNextPageParam: (last) => last.next_cursor,
  });
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  const everyone = list.data?.pages[0]?.scope === "all";
  const names = { agents: agents.data ?? [], connections: [], people: people.data ?? [] };
  const agentName = (x: string) => names.agents.find((a) => a.agent === x)?.name ?? x;
  const agentIds = [...new Set([...(agents.data ?? []).map((a) => a.agent), ...items.map((e) => e.agent)])];
  const set = (patch: Partial<Filter>) => setFilter({ ...filter, ...patch });
  const served = items.filter((e) => e.outcome === "ok").length;
  const fellBack = items.filter((e) => e.fell_back_from).length;
  const failed = items.filter((e) => !["ok", "skipped"].includes(e.outcome)).length;
  const masked = items.filter((e) => e.masked).length;
  return (
    <SettingsPage title={h.label} lead={h.tooltip} screen="ai-activity-log">
      <section className="fy-card" aria-labelledby={`${id}-filter`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-filter`}>
            Filter
          </h2>
          {list.data && (
            <span className="fy-chip fy-chip--neutral">
              <Icon of={Info} />
              {everyone ? "Every member's entries" : "Your own entries"}
            </span>
          )}
        </div>
        <div className="fy-card__body fy-grid fy-grid--4 gap-3">
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-agent`}>
              Agent
            </label>
            <select id={`${id}-agent`} className="fy-select" value={filter.agent} onChange={(e) => set({ agent: e.target.value })}>
              <option value="">Every agent</option>
              {agentIds.map((a) => (
                <option key={a} value={a}>
                  {agentName(a)}
                </option>
              ))}
            </select>
          </div>
          {everyone && (
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-person`}>
                Member
              </label>
              <select id={`${id}-person`} className="fy-select" value={filter.person} onChange={(e) => set({ person: e.target.value })}>
                <option value="">Every member</option>
                {(people.data ?? []).map((m) => (
                  <option key={m.user_id} value={m.user_id}>
                    {m.display_name}
                  </option>
                ))}
              </select>
            </div>
          )}
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-from`}>
              From
            </label>
            <input id={`${id}-from`} className="fy-input" type="date" value={filter.from} onChange={(e) => set({ from: e.target.value })} />
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-to`}>
              To
            </label>
            <input id={`${id}-to`} className="fy-input" type="date" value={filter.to} onChange={(e) => set({ to: e.target.value })} />
          </div>
        </div>
        {filter.run && (
          <div className="fy-card__body fy-row gap-2">
            <span className="fy-small">Showing one call and its fallbacks.</span>
            <Button size="sm" variant="ghost" onClick={() => set({ run: "" })}>
              <Icon of={X} />
              Show every call
            </Button>
          </div>
        )}
      </section>
      {items.length > 0 && (
        <div className="fy-grid fy-grid--4" role="group" aria-label="In the entries shown">
          {(
            [
              ["Served", served],
              ["Fell back", fellBack],
              ["Failed or stopped", failed],
              ["Masked", masked],
            ] as const
          ).map(([label, n]) => (
            <div key={label} className="fy-tile">
              <span className="fy-tile__label">{label}</span>
              <span className="fy-tile__value fy-num">{count(n)}</span>
              <span className="fy-tile__meta">in the {count(items.length)} entries shown</span>
            </div>
          ))}
        </div>
      )}
      <section className="fy-card" aria-labelledby={`${id}-entries`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-entries`}>
            Model calls
          </h2>
        </div>
        {list.isError && <Refusal problem={problemOf(list.error)} title="The AI activity could not be shown" />}
        {list.isPending && <p className="fy-card__body fy-small">Loading the AI activity…</p>}
        {list.data && items.length === 0 && (
          <p className="fy-card__body fy-secondary">No AI activity {filter.agent || filter.from || filter.to || filter.person ? "matches the filter" : "yet"}: every model call made for you, or for the organisation, is listed here.</p>
        )}
        {items.length > 0 && (
          <div className="fy-tablewrap">
            <table className="fy-table fy-table--cards" aria-labelledby={`${id}-entries`}>
              <thead>
                <tr>
                  <th scope="col">When</th>
                  {everyone && <th scope="col">For</th>}
                  <th scope="col">Agent and step</th>
                  <th scope="col">Connection and model</th>
                  <th scope="col">Outcome</th>
                  <th scope="col" className="fy-num">
                    Tokens in / out
                  </th>
                  <th scope="col" className="fy-num">
                    Cost ({base})
                  </th>
                </tr>
              </thead>
              <tbody>
                {items.map((e) => {
                  const o = outcomeWords[e.outcome];
                  return (
                    <tr key={e.id}>
                      <td data-card="title">{dateTime(e.started_at)}</td>
                      {everyone && <td>{personName(e.person, names)}</td>}
                      <td className="whitespace-normal">
                        <span className="fy-stack gap-0">
                          <span>{agentName(e.agent)}</span>
                          <span className="fy-small fy-secondary">
                            {e.route_version === 0 ? "outside any route" : `route v${e.route_version}, step ${e.step}`}
                            {e.fell_back_from ? `, fell back from step ${e.fell_back_from}` : ""}
                          </span>
                          {e.classes.length > 0 && <span className="fy-small fy-secondary">{e.classes.map((c) => classWords[c]).join(", ")}</span>}
                        </span>
                      </td>
                      <td className="whitespace-normal">
                        <span className="fy-stack gap-0">
                          <span>{e.connection}</span>
                          <span className="fy-small fy-mono">
                            {e.model_requested}
                            {e.model_reported && e.model_reported !== e.model_requested ? ` → ${e.model_reported}` : ""}
                            {e.version_reported ? ` (${e.version_reported})` : ""}
                          </span>
                        </span>
                      </td>
                      <td className="whitespace-normal">
                        <span className="fy-stack gap-1">
                          <span className={`fy-chip ${o.tone} self-start`}>
                            <Icon of={o.icon} />
                            {o.label}
                          </span>
                          {e.masked ? (
                            <span className="fy-small fy-secondary fy-row gap-1">
                              <Icon of={EyeOff} />
                              Masked: it carried payroll
                            </span>
                          ) : (
                            e.detail && <span className="fy-small fy-secondary">{e.detail}</span>
                          )}
                          {!filter.run && (
                            <Button size="sm" variant="ghost" className="self-start" onClick={() => set({ run: e.run })} aria-label={`Show the call of ${dateTime(e.started_at)} and its fallbacks`}>
                              <Icon of={Repeat} />
                              Its fallbacks
                            </Button>
                          )}
                        </span>
                      </td>
                      <td className="fy-num">
                        {count(e.tokens.input + e.tokens.cache_read + e.tokens.cache_write)} / {count(e.tokens.output)}
                      </td>
                      <td className="fy-num">
                        {e.cost_base ? formatAmount(e.cost_base) : "—"}
                        {e.cost && e.currency && e.currency !== base && (
                          <span className="fy-small fy-secondary block">
                            {e.currency} {formatAmount(e.cost)}
                          </span>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
        {list.hasNextPage && (
          <div className="fy-card__body">
            <Button variant="secondary" onClick={() => void list.fetchNextPage()} disabled={list.isFetchingNextPage}>
              {list.isFetchingNextPage ? "Loading…" : "Show more"}
            </Button>
          </div>
        )}
      </section>
      <p className="fy-small fy-secondary">
        Every attempt is kept, served or not. Costs are exact, in the base currency at the day's rate; a call with no price shows none.{" "}
        <Link to="/settings/profile/ai-plan">{helpText("settings.ai-plan").label}</Link>
      </p>
    </SettingsPage>
  );
}
