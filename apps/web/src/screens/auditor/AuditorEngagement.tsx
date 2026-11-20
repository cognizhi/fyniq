import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CheckCheck, Dices, MessageSquarePlus, Plus, Send, XCircle } from "lucide-react";
import { useId, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Engagement = components["schemas"]["AuditEngagement"];
type Request = components["schemas"]["AuditRequestBody"];
type Sample = components["schemas"]["AuditSampleBody"];
type Member = components["schemas"]["Member"];

// AuditorEngagement is the auditor's engagement (FR-UX-42; docs/design/screens AuditorWorkspace): an Owner opens a time-boxed engagement for the auditor and a
// second Owner confirms it; while it is active the auditor asks for items and draws samples from the ledger, an Owner, Controller or Accountant answers, and the
// auditor accepts the answer. The auditor writes nothing else, and every figure here comes from the ledger as the sample drew it.
export function AuditorEngagement() {
  const id = useId();
  const client = useQueryClient();
  const { session } = useSignedIn();
  const roles = session.roles ?? [];
  const isOwner = roles.includes("owner");
  const isAuditor = roles.includes("auditor");
  const mayAnswer = isOwner || roles.includes("controller") || roles.includes("accountant");
  const [openId, setOpenId] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [form, setForm] = useState({ name: "", firm: "", auditor: "", from: "", to: "", opens: "", closes: "" });
  const [ask, setAsk] = useState({ title: "", detail: "", due: "", assignee: "" });
  const [rule, setRule] = useState({ from: "", to: "", account: "", min: "", count: "25" });
  const [answers, setAnswers] = useState<Record<string, string>>({});
  const h = helpText("audit.engagement");

  const list = useQuery({
    queryKey: ["audit-engagements"],
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/audit-engagements", { params: { query: { page_size: 100 } } });
      if (r.error) throw r.error;
      return r.data.items ?? [];
    },
  });
  const e: Engagement | undefined = (list.data ?? []).find((x) => x.id === openId) ?? (list.data ?? [])[0];
  const members = useQuery({
    queryKey: ["members", "engagement"],
    queryFn: async () => {
      const r = await api.GET("/v1/members", { params: { query: { page_size: 200 } } });
      return r.error ? ([] as Member[]) : (r.data.items ?? []);
    },
  });
  const requests = useQuery({
    queryKey: ["audit-requests", e?.id],
    enabled: !!e,
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/audit-engagements/{id}/requests", { params: { path: { id: e!.id }, query: { page_size: 200 } } });
      if (r.error) throw r.error;
      return r.data.items ?? [];
    },
  });
  const samples = useQuery({
    queryKey: ["audit-samples", e?.id],
    enabled: !!e,
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/audit-engagements/{id}/samples", { params: { path: { id: e!.id }, query: { page_size: 50 } } });
      if (r.error) throw r.error;
      return r.data.items ?? [];
    },
  });
  const withRole = (role: string) => (members.data ?? []).filter((m) => m.roles?.some((r) => r.role === role));
  const answerers = (members.data ?? []).filter((m) => m.roles?.some((r) => ["owner", "controller", "accountant"].includes(r.role)));
  const refresh = async () => {
    await client.invalidateQueries({ queryKey: ["audit-engagements"] });
    await client.invalidateQueries({ queryKey: ["audit-requests"] });
    await client.invalidateQueries({ queryKey: ["audit-samples"] });
  };
  const done = async (error: unknown) => {
    if (error) return setProblem(problemOf(error));
    setProblem(null);
    await refresh();
  };

  const open = async () => {
    const { data, error } = await api.POST("/v1/audit-engagements", {
      body: { name: form.name, firm: form.firm || undefined, auditor_user_id: form.auditor, period_from: form.from, period_to: form.to, opens_on: form.opens, closes_on: form.closes },
    });
    if (data) setOpenId(data.id);
    await done(error);
  };
  const act = (what: "confirm" | "close") => async () => {
    if (!e) return;
    const { error } = await api.POST(what === "confirm" ? "/v1/audit-engagements/{id}/confirm" : "/v1/audit-engagements/{id}/close", { params: { path: { id: e.id } } });
    await done(error);
  };
  const askFor = async () => {
    if (!e) return;
    const { error } = await api.POST("/v1/audit-engagements/{id}/requests", {
      params: { path: { id: e.id } },
      body: { title: ask.title, detail: ask.detail || undefined, due_on: ask.due || undefined, assignee_user_id: ask.assignee },
    });
    if (!error) setAsk({ title: "", detail: "", due: "", assignee: ask.assignee });
    await done(error);
  };
  const answer = async (r: Request) => {
    const { error } = await api.POST("/v1/audit-requests/{id}/answer", { params: { path: { id: r.id } }, body: { answer: answers[r.id] ?? "" } });
    await done(error);
  };
  const accept = async (r: Request) => {
    const { error } = await api.POST("/v1/audit-requests/{id}/close", { params: { path: { id: r.id } } });
    await done(error);
  };
  const draw = async () => {
    if (!e) return;
    const { error } = await api.POST("/v1/audit-engagements/{id}/samples", {
      params: { path: { id: e.id } },
      body: { rule: { from: rule.from || e.period_from, to: rule.to || e.period_to, account: rule.account || undefined, min_amount: rule.min || undefined, count: Number(rule.count) || 25 } },
    });
    await done(error);
  };
  const active = e?.status === "active";

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/reports">Reports</Link>
          </nav>
          <h1 className="fy-h1">Auditor engagement</h1>
          <p className="fy-secondary">{h.tooltip}</p>
        </div>
      </div>
      {list.error && <Refusal problem={problemOf(list.error)} title="The engagements could not be shown" />}
      {problem && <Refusal problem={problem} title="That could not be saved" />}
      {isOwner && (
        <section className="fy-card fy-card--pad" aria-labelledby={`${id}-new`}>
          <h2 className="fy-h3" id={`${id}-new`}>
            Open an engagement
          </h2>
          <div className="fy-row fy-row--wrap gap-3">
            {(
              [
                ["name", "Name", "text"],
                ["firm", "Firm", "text"],
                ["from", "Period from", "date"],
                ["to", "Period to", "date"],
                ["opens", "Opens on", "date"],
                ["closes", "Closes on", "date"],
              ] as const
            ).map(([k, label, type]) => (
              <div className="fy-field" key={k}>
                <label className="fy-label" htmlFor={`${id}-n-${k}`}>
                  {label}
                </label>
                <input className="fy-input" id={`${id}-n-${k}`} type={type} value={form[k]} onChange={(ev) => setForm({ ...form, [k]: ev.target.value })} />
              </div>
            ))}
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-n-auditor`}>
                Auditor
              </label>
              <select className="fy-select" id={`${id}-n-auditor`} value={form.auditor} onChange={(ev) => setForm({ ...form, auditor: ev.target.value })}>
                <option value="">Choose a member</option>
                {withRole("auditor").map((m) => (
                  <option key={m.user_id} value={m.user_id}>
                    {m.display_name}
                  </option>
                ))}
              </select>
            </div>
            <Button variant="secondary" onClick={open} disabled={!form.name || !form.auditor || !form.from || !form.to || !form.opens || !form.closes}>
              <Icon of={Plus} /> Open it
            </Button>
          </div>
          <p className="fy-small fy-muted">It stays pending until a second Owner confirms it, and it runs for at most 366 days between the two dates.</p>
        </section>
      )}
      <section className="fy-card fy-card--pad" aria-labelledby={`${id}-list`}>
        <h2 className="fy-h3" id={`${id}-list`}>
          Engagements
        </h2>
        {(list.data ?? []).length === 0 && <p className="fy-secondary">No engagement yet.</p>}
        <ul className="fy-list">
          {(list.data ?? []).map((x) => (
            <li key={x.id}>
              <button type="button" className="fy-link" aria-current={x.id === e?.id ? "true" : undefined} onClick={() => setOpenId(x.id)}>
                {x.name}
              </button>{" "}
              <span className="fy-muted fy-small">
                {x.status}, {date(x.opens_on)} to {date(x.closes_on)}
              </span>
            </li>
          ))}
        </ul>
      </section>
      {e && (
        <section className="fy-card fy-card--pad" aria-labelledby={`${id}-eng`}>
          <h2 className="fy-h3" id={`${id}-eng`}>
            {e.name}
          </h2>
          <p className="fy-secondary">
            Period under audit {date(e.period_from)} to {date(e.period_to)}. Requests and samples are added {date(e.opens_on)} to {date(e.closes_on)} while the engagement is active; it is {e.status}.
          </p>
          {isOwner && (
            <div className="fy-row gap-3">
              {e.status === "pending" && (
                <Button onClick={act("confirm")}>
                  <Icon of={CheckCheck} /> Confirm it
                </Button>
              )}
              {e.status !== "closed" && (
                <Button variant="secondary" onClick={act("close")}>
                  <Icon of={XCircle} /> Close it
                </Button>
              )}
            </div>
          )}
          <h3 className="fy-h4">Requests</h3>
          {(requests.data ?? []).length === 0 && <p className="fy-secondary">No request yet.</p>}
          <ul className="fy-list">
            {(requests.data ?? []).map((r) => (
              <li key={r.id}>
                <strong>
                  {r.number}. {r.title}
                </strong>{" "}
                <span className="fy-muted fy-small">
                  {r.status}
                  {r.due_on ? `, due ${date(r.due_on)}` : ""}
                </span>
                {r.detail && <p className="fy-small">{r.detail}</p>}
                {r.answer && <p className="fy-small">Answer: {r.answer}</p>}
                {mayAnswer && active && r.status !== "closed" && (
                  <div className="fy-row gap-3">
                    <label className="fy-label" htmlFor={`${id}-a-${r.id}`}>
                      Answer to request {r.number}
                    </label>
                    <input className="fy-input" id={`${id}-a-${r.id}`} value={answers[r.id] ?? ""} maxLength={4000} onChange={(ev) => setAnswers({ ...answers, [r.id]: ev.target.value })} />
                    <Button variant="secondary" onClick={() => answer(r)} disabled={(answers[r.id] ?? "").trim() === ""}>
                      <Icon of={Send} /> Answer
                    </Button>
                  </div>
                )}
                {isAuditor && active && r.status === "answered" && (
                  <Button variant="secondary" onClick={() => accept(r)}>
                    <Icon of={CheckCheck} /> Accept and close request {r.number}
                  </Button>
                )}
              </li>
            ))}
          </ul>
          {isAuditor && active && (
            <div className="fy-row fy-row--wrap gap-3">
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-q-title`}>
                  Ask for
                </label>
                <input className="fy-input" id={`${id}-q-title`} value={ask.title} maxLength={200} onChange={(ev) => setAsk({ ...ask, title: ev.target.value })} />
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-q-who`}>
                  Answered by
                </label>
                <select className="fy-select" id={`${id}-q-who`} value={ask.assignee} onChange={(ev) => setAsk({ ...ask, assignee: ev.target.value })}>
                  <option value="">Choose a member</option>
                  {answerers.map((m) => (
                    <option key={m.user_id} value={m.user_id}>
                      {m.display_name}
                    </option>
                  ))}
                </select>
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-q-due`}>
                  Wanted by
                </label>
                <input className="fy-input" id={`${id}-q-due`} type="date" value={ask.due} onChange={(ev) => setAsk({ ...ask, due: ev.target.value })} />
              </div>
              <Button variant="secondary" onClick={askFor} disabled={ask.title.trim().length < 2 || !ask.assignee}>
                <Icon of={MessageSquarePlus} /> Ask
              </Button>
            </div>
          )}
          <h3 className="fy-h4">Samples</h3>
          {isAuditor && active && (
            <div className="fy-row fy-row--wrap gap-3">
              {(
                [
                  ["from", "Sample from", "date"],
                  ["to", "Sample to", "date"],
                  ["account", "Account code", "text"],
                  ["min", "Smallest line", "text"],
                  ["count", "How many", "number"],
                ] as const
              ).map(([k, label, type]) => (
                <div className="fy-field" key={k}>
                  <label className="fy-label" htmlFor={`${id}-s-${k}`}>
                    {label}
                  </label>
                  <input className="fy-input" id={`${id}-s-${k}`} type={type} value={rule[k]} onChange={(ev) => setRule({ ...rule, [k]: ev.target.value })} />
                </div>
              ))}
              <Button variant="secondary" onClick={draw}>
                <Icon of={Dices} /> Draw a sample
              </Button>
            </div>
          )}
          {(samples.data ?? []).length === 0 && <p className="fy-secondary">No sample drawn yet.</p>}
          {(samples.data ?? []).map((s: Sample) => (
            <details key={s.id}>
              <summary>
                {s.items.length} of {s.population} lines, {date(s.rule.from)} to {date(s.rule.to)}
                {s.rule.account ? `, account ${s.rule.account}` : ""}, seed {s.seed}
              </summary>
              <table className="fy-table">
                <caption className="fy-sr-only">The sampled lines</caption>
                <thead>
                  <tr>
                    <th scope="col">Date</th>
                    <th scope="col">Account</th>
                    <th scope="col" className="fy-num">
                      Amount
                    </th>
                    <th scope="col">From</th>
                  </tr>
                </thead>
                <tbody>
                  {s.items.map((l) => (
                    <tr key={`${l.entry_id}-${l.line_no}`}>
                      <td>{date(l.posting_date)}</td>
                      <td>
                        {l.account} {l.account_name}
                      </td>
                      <td className="fy-num">{l.base_amount}</td>
                      <td>{l.source_type}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </details>
          ))}
        </section>
      )}
    </div>
  );
}
