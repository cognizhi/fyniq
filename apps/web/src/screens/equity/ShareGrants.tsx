import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChevronRight, FilePlus, Plus, Save, Trash2 } from "lucide-react";
import { useId, useMemo, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { helpText } from "../../i18n";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { listAccounts, postable } from "../accounting/data";

type Grant = components["schemas"]["ShareGrant"];
type Input = components["schemas"]["ShareGrantInput"];
type Schedule = components["schemas"]["ShareGrantSchedule"];

const empty: Input = { recipient: "", fair_value: "", valuation: "", service_start: "", tranches: [{ shares: 0, vest_on: "" }], expense_account: "", reserve_account: "" };

// ShareGrants is Equity › Shares for services (docs/design/screens ShareGrants; FR-EQ-13, Expert): a grant of shares for services is an equity-settled share-based payment. Its fair value is fixed at the grant
// date; each tranche's expense is spread over its whole service months, and each month is proposed as a draft journal (Dr expense, Cr the share-based payment reserve) that an approver posts. Nothing here
// posts, and the reserve moves to share capital when the shares are issued.
export function ShareGrants() {
  const id = useId();
  const client = useQueryClient();
  const h = helpText("equity.share-grants");
  const grants = useQuery({
    queryKey: ["share-grants"],
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/share-grants", { params: { query: { page_size: 200 } } });
      if (r.error) throw r.error;
      return r.data.items ?? [];
    },
  });
  const accounts = useQuery({ queryKey: ["accounts", "all"], queryFn: listAccounts });
  const expenses = useMemo(() => postable(accounts.data ?? []).filter((a) => a.type === "expense"), [accounts.data]);
  const reserves = useMemo(() => postable(accounts.data ?? []).filter((a) => a.type === "equity" && a.role !== "share-capital"), [accounts.data]);
  const [openId, setOpenId] = useState("");
  const [etag, setEtag] = useState("");
  const [form, setForm] = useState<Input>(empty);
  const [schedule, setSchedule] = useState<Schedule | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);

  const loadSchedule = async (grant: string) => {
    const { data, error } = await api.GET("/v1/share-grants/{grant_id}/schedule", { params: { path: { grant_id: grant } } });
    if (error || !data) return setProblem(problemOf(error)), setSchedule(null);
    setSchedule(data);
  };
  const choose = async (next: string) => {
    setOpenId(next);
    setSchedule(null);
    setProblem(null);
    if (!next) {
      setForm(empty);
      setEtag("");
      return;
    }
    const { data, response, error } = await api.GET("/v1/share-grants/{grant_id}", { params: { path: { grant_id: next } } });
    if (error || !data) return setProblem(problemOf(error));
    setForm({ recipient: data.recipient, fair_value: data.fair_value, valuation: data.valuation, service_start: data.service_start, tranches: data.tranches, expense_account: data.expense_account, reserve_account: data.reserve_account, archived: data.archived });
    setEtag(response.headers.get("ETag") ?? "");
    await loadSchedule(next);
  };
  const setTranche = (i: number, patch: Partial<Input["tranches"][number]>) => setForm({ ...form, tranches: form.tranches.map((t, k) => (k === i ? { ...t, ...patch } : t)) });

  const save = async () => {
    setBusy(true);
    const r = openId
      ? await api.PUT("/v1/share-grants/{grant_id}", { params: { path: { grant_id: openId }, header: { "If-Match": etag } }, body: form })
      : await api.POST("/v1/share-grants", { body: form });
    setBusy(false);
    if (r.error || !r.data) return setProblem(problemOf(r.error));
    setProblem(null);
    setOpenId(r.data.id);
    setEtag(r.response.headers.get("ETag") ?? "");
    await client.invalidateQueries({ queryKey: ["share-grants"] });
    await loadSchedule(r.data.id);
  };
  const propose = async (month: string) => {
    setBusy(true);
    const { error } = await api.POST("/v1/share-grant-months", { body: { grant_id: openId, month } });
    setBusy(false);
    if (error) return setProblem(problemOf(error));
    setProblem(null);
    await loadSchedule(openId);
  };

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/equity">Equity</Link>
            <Icon of={ChevronRight} />
            <span>Shares for services</span>
          </nav>
          <h1 className="fy-h1">Shares for services</h1>
          <p className="fy-secondary">{h.tooltip}</p>
        </div>
      </div>
      {grants.error && <Refusal problem={problemOf(grants.error)} title="The grants could not be shown" />}
      <div className="fy-card fy-card--pad fy-row fy-row--wrap gap-3">
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-open`}>
            Open a grant
          </label>
          <select className="fy-select" id={`${id}-open`} value={openId} onChange={(e) => void choose(e.target.value)}>
            <option value="">A new grant</option>
            {(grants.data ?? []).map((g: Grant) => (
              <option key={g.id} value={g.id}>
                {g.recipient}, from {g.service_start}
                {g.archived ? " (archived)" : ""}
              </option>
            ))}
          </select>
        </div>
      </div>
      {problem && <Refusal problem={problem} title="The grant could not be saved" />}
      <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-grant`}>
        <h2 className="fy-h3" id={`${id}-grant`}>
          The grant
        </h2>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-who`}>
            Recipient
          </label>
          <input className="fy-input" id={`${id}-who`} value={form.recipient} onChange={(e) => setForm({ ...form, recipient: e.target.value })} />
        </div>
        <div className="fy-row fy-row--wrap gap-3">
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-fv`}>
              Fair value of a share at the grant date
            </label>
            <input className="fy-input" id={`${id}-fv`} inputMode="decimal" value={form.fair_value} onChange={(e) => setForm({ ...form, fair_value: e.target.value })} />
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-start`}>
              Service starts (the first of a month)
            </label>
            <input className="fy-input" id={`${id}-start`} type="date" value={form.service_start} onChange={(e) => setForm({ ...form, service_start: e.target.value })} />
          </div>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-val`}>
            Where the fair value comes from (the valuation)
          </label>
          <input className="fy-input" id={`${id}-val`} value={form.valuation ?? ""} onChange={(e) => setForm({ ...form, valuation: e.target.value })} />
        </div>
        <div className="fy-stack fy-stack--2">
          <h3 className="fy-h3">Tranches</h3>
          {form.tranches.map((t, i) => (
            <div className="fy-row fy-row--wrap gap-3" key={i}>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-t${i}-shares`}>
                  Shares in tranche {i + 1}
                </label>
                <input className="fy-input" id={`${id}-t${i}-shares`} inputMode="numeric" value={t.shares || ""} onChange={(e) => setTranche(i, { shares: Number(e.target.value) || 0 })} />
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-t${i}-vest`}>
                  They vest on
                </label>
                <input className="fy-input" id={`${id}-t${i}-vest`} type="date" value={t.vest_on} onChange={(e) => setTranche(i, { vest_on: e.target.value })} />
              </div>
              <Button variant="ghost" onClick={() => setForm({ ...form, tranches: form.tranches.filter((_, k) => k !== i) })} disabled={form.tranches.length === 1}>
                <Icon of={Trash2} /> Remove tranche {i + 1}
              </Button>
            </div>
          ))}
          <div>
            <Button variant="secondary" onClick={() => setForm({ ...form, tranches: [...form.tranches, { shares: 0, vest_on: "" }] })}>
              <Icon of={Plus} /> Add a tranche
            </Button>
          </div>
        </div>
        <div className="fy-row fy-row--wrap gap-3">
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-exp`}>
              Expense account
            </label>
            <select className="fy-select" id={`${id}-exp`} value={form.expense_account} onChange={(e) => setForm({ ...form, expense_account: e.target.value })}>
              <option value="">Choose an account</option>
              {expenses.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.code} {a.name}
                </option>
              ))}
            </select>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-res`}>
              Share-based payment reserve (equity)
            </label>
            <select className="fy-select" id={`${id}-res`} value={form.reserve_account} onChange={(e) => setForm({ ...form, reserve_account: e.target.value })}>
              <option value="">Choose an account</option>
              {reserves.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.code} {a.name}
                </option>
              ))}
            </select>
          </div>
        </div>
        <div className="fy-row gap-3">
          <Button onClick={() => void save()} disabled={busy}>
            <Icon of={Save} /> {openId ? "Save the next version" : "Record the grant"}
          </Button>
        </div>
      </section>
      {schedule && (
        <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-sched`}>
          <h2 className="fy-h3" id={`${id}-sched`}>
            Expense over the service months: {schedule.total}
          </h2>
          <table className="fy-table">
            <caption className="fy-sr">The grant's expense by month and the draft journal proposed for it</caption>
            <thead>
              <tr>
                <th scope="col">Month</th>
                <th scope="col" className="fy-num">
                  Expense
                </th>
                <th scope="col">Journal</th>
              </tr>
            </thead>
            <tbody>
              {schedule.months.map((m) => (
                <tr key={m.month}>
                  <th scope="row">{m.month}</th>
                  <td className="fy-num">{m.expense}</td>
                  <td>
                    {m.journal_id ? (
                      <Link to="/accounting/journals/$journalId" params={{ journalId: m.journal_id }}>
                        Open the draft
                      </Link>
                    ) : (
                      <Button variant="secondary" onClick={() => void propose(m.month)} disabled={busy}>
                        <Icon of={FilePlus} /> Propose {m.month}
                      </Button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <p className="fy-small fy-muted">{schedule.note}</p>
        </section>
      )}
    </div>
  );
}
