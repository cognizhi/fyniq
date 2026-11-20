import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { LineChart } from "lucide-react";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Money = components["schemas"]["Money"];
const money = (m: Money) => `${m.currency} ${formatAmount(m.amount)}`;
const electors = ["accountant", "controller", "owner"];

// ProjectRevenue is a fixed-fee project's revenue over time (FR-PRJ-11): the accountant elects it with the estimated total cost; the month's figures (progress by cost-to-cost,
// the revenue earned, the contract asset or liability) are read from the posted ledger, and the month's journal is proposed as a draft for a person to approve.
export function ProjectRevenue({ project }: { project: string }) {
  const id = useId();
  const queries = useQueryClient();
  const { session } = useSignedIn();
  const may = (session.roles ?? []).some((r) => electors.includes(r));
  const [estimate, setEstimate] = useState("");
  const [month, setMonth] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const [said, setSaid] = useState("");
  const q = useQuery({
    queryKey: ["project-revenue", project, month],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/projects/{project_id}/revenue", { params: { path: { project_id: project }, query: month ? { month } : {} } });
      if (error) throw error;
      return data;
    },
  });
  const refresh = () => queries.invalidateQueries({ queryKey: ["project-revenue", project] });
  const elect = async (method: "over_time" | "milestone") => {
    setBusy(true);
    setProblem(null);
    const version = q.data?.election.version ?? 0;
    const out = await api.PUT("/v1/projects/{project_id}/revenue-election", {
      params: { path: { project_id: project }, header: { "Idempotency-Key": newKey(), ...(version > 0 ? { "If-Match": `"${version}"` } : {}) } },
      body: method === "over_time" ? { method, estimated_cost: estimate.trim() } : { method },
    });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    setEstimate("");
    await refresh();
  };
  const propose = async () => {
    const pos = q.data?.month;
    if (!pos) return;
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/projects/{project_id}/revenue-journals", {
      params: { path: { project_id: project }, header: { "Idempotency-Key": newKey() } },
      body: { month: pos },
    });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    setSaid(`The journal for ${date(pos)} is proposed as a draft, booking ${money(out.data.run.adjustment)}.`);
    await refresh();
  };
  const r = q.data;
  const pos = r?.position;
  const h = helpText("project.revenue-over-time");
  const over = r?.election.method === "over_time";
  return (
    <section className="fy-card" aria-labelledby={`${id}-head`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-head`} title={h.tooltip}>
          <Icon of={LineChart} /> {h.label}
        </h2>
        {over && <span className="fy-chip fy-chip--neutral">Recognised over time</span>}
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        {said && (
          <p className="fy-small" role="status">
            {said}
          </p>
        )}
        {problem && <Refusal problem={problem} title="The change was not saved" />}
        {q.isError && <Refusal problem={problemOf(q.error)} title="The project's revenue could not be read" />}
        {r && !over && (
          <p className="fy-secondary">
            Revenue is recognised as each milestone is invoiced. {may ? "Elect revenue over time to recognise it by the project's progress instead, with the estimated total cost." : "An accountant can elect revenue over time."}
          </p>
        )}
        {r && may && !over && (
          <form
            className="fy-row fy-row--wrap gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              void elect("over_time");
            }}
          >
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-est`}>
                Estimated total cost
              </label>
              <input className="fy-input fy-num" id={`${id}-est`} inputMode="decimal" value={estimate} onChange={(e) => setEstimate(e.target.value)} />
            </div>
            <div className="fy-field self-end">
              <Button type="submit" variant="secondary" disabled={busy || !estimate.trim()}>
                Elect revenue over time
              </Button>
            </div>
          </form>
        )}
        {over && r && (
          <>
            <div className="fy-row fy-row--wrap gap-2">
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-month`}>
                  Month
                </label>
                <input className="fy-input" id={`${id}-month`} type="date" value={month || r.month || ""} onChange={(e) => setMonth(e.target.value)} />
              </div>
              <p className="fy-secondary self-end">
                Estimated total cost {r.election.estimated_cost ? money(r.election.estimated_cost) : ""}.
                {may && (
                  <>
                    {" "}
                    <Button type="button" size="sm" variant="ghost" onClick={() => void elect("milestone")} disabled={busy}>
                      Back to milestones
                    </Button>
                  </>
                )}
              </p>
            </div>
            {r.problem && (
              <p className="fy-banner fy-banner--warning" role="alert">
                {r.problem}
              </p>
            )}
            {pos && (
              <dl className="fy-dl" aria-label={`Revenue over time, ${date(r.month ?? "")}`}>
                <dt>Fee (the milestones)</dt>
                <dd className="fy-num">{money(pos.fee)}</dd>
                <dt>Cost to date</dt>
                <dd className="fy-num">
                  {money(pos.cost_to_date)} of {money(pos.estimated_cost)}, {pos.progress}%{pos.overrun ? " (the estimate is reached: revisit it)" : ""}
                </dd>
                <dt>Revenue earned</dt>
                <dd className="fy-num">{money(pos.earned)}</dd>
                <dt>Invoiced to date</dt>
                <dd className="fy-num">{money(pos.invoiced)}</dd>
                <dt>Booked in the ledger</dt>
                <dd className="fy-num">{money(pos.held)}</dd>
                <dt>This month's journal books</dt>
                <dd className="fy-num">{money(pos.adjustment)}</dd>
                <dt>Contract asset (earned, not invoiced)</dt>
                <dd className="fy-num">{money(pos.contract_asset)}</dd>
                <dt>Contract liability (invoiced, not earned)</dt>
                <dd className="fy-num">{money(pos.contract_liability)}</dd>
              </dl>
            )}
            {pos && may && !pos.nothing_to_propose && (
              <div>
                <Button type="button" variant="primary" onClick={() => void propose()} disabled={busy}>
                  Propose the month's journal
                </Button>
                <span className="fy-small fy-muted"> It is a draft for a person to approve and post.</span>
              </div>
            )}
            {r.runs.length > 0 && (
              <ul className="fy-list" aria-label="Months proposed">
                {r.runs.map((x) => (
                  <li className="fy-list__item" key={x.month}>
                    <span className="fy-list__main">
                      <span className="fy-list__title">{date(x.month)}</span>
                      <span className="fy-list__meta">
                        Booked {money(x.adjustment)}, earned to date {money(x.earned)}.{" "}
                        <Link to="/accounting/journals/$journalId" params={{ journalId: x.journal_id }}>
                          Open the journal
                        </Link>
                      </span>
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </>
        )}
      </div>
    </section>
  );
}
