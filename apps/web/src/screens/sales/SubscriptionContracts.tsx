import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CalendarRange, Plus } from "lucide-react";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Contract = components["schemas"]["SubscriptionContract"];
type Money = components["schemas"]["Money"];

const money = (m: Money) => `${m.currency} ${formatAmount(m.amount)}`;
const statusWords: Record<string, string> = { active: "Active", ended: "Ended", not_started: "Not started yet" };

// SubscriptionContracts are the company's own subscription contracts with its customers (FR-AR-15): each with the monthly value in force today, and
// dated amendments (upgrade, downgrade, cancel, renew). They are records, not postings: invoices and credit notes post as they do, and the contracted
// MRR (FR-KPI-02) is derived from the contracts and their amendments.
export function SubscriptionContracts({ may }: { may: boolean }) {
  const id = useId();
  const queries = useQueryClient();
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const [adding, setAdding] = useState(false);
  const [amending, setAmending] = useState<Contract | null>(null);
  const [form, setForm] = useState({ contact: "", name: "", currency: "SGD", starts: "", ends: "", mrr: "" });
  const [change, setChange] = useState({ kind: "upgrade", on: "", mrr: "", reason: "" });
  const list = useQuery({
    queryKey: ["subscription-contracts"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/subscription-contracts", { params: { query: { page_size: 200 } } });
      if (error) throw error;
      return data.items;
    },
  });
  const mrr = useQuery({
    queryKey: ["subscription-mrr"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/subscription-mrr");
      if (error) throw error;
      return data;
    },
  });
  const customers = useQuery({
    queryKey: ["contacts", "customers"],
    enabled: adding,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/contacts", { params: { query: { role: "customer", page_size: 500 } } });
      if (error) throw error;
      return data.items;
    },
  });
  const nameOf = new Map((customers.data ?? []).map((c) => [c.id, c.legal_name]));
  const refresh = async () => {
    await queries.invalidateQueries({ queryKey: ["subscription-contracts"] });
    await queries.invalidateQueries({ queryKey: ["subscription-mrr"] });
  };
  const create = async () => {
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/subscription-contracts", {
      params: { header: { "Idempotency-Key": newKey() } },
      body: { contact_id: form.contact, name: form.name.trim(), currency: form.currency.trim().toUpperCase(), starts_on: form.starts, ends_on: form.ends || undefined, mrr: form.mrr.trim() },
    });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    setAdding(false);
    setForm({ contact: "", name: "", currency: "SGD", starts: "", ends: "", mrr: "" });
    await refresh();
  };
  const amend = async () => {
    if (!amending) return;
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/subscription-contracts/{contract_id}/amendments", {
      params: { path: { contract_id: amending.id }, header: { "Idempotency-Key": newKey(), "If-Match": `"${amending.version}"` } },
      body: { kind: change.kind as "upgrade", effective_on: change.on, mrr: change.kind === "cancel" ? undefined : change.mrr.trim(), reason: change.reason.trim() },
    });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    setAmending(null);
    setChange({ kind: "upgrade", on: "", mrr: "", reason: "" });
    await refresh();
  };
  const items = list.data ?? [];
  const add = helpText("subscription.contract");
  const amendHelp = helpText("subscription.amend");
  return (
    <section className="fy-card" aria-labelledby={`${id}-head`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-head`}>
          Your subscription contracts
        </h2>
        {may && (
          <Button type="button" size="sm" variant="secondary" onClick={() => setAdding((a) => !a)} title={add.tooltip}>
            <Icon of={Plus} />
            {add.label}
          </Button>
        )}
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        {mrr.data && mrr.data.totals.length > 0 && (
          <p className="fy-secondary" role="status">
            Contracted MRR on {date(mrr.data.as_at)}:{" "}
            {mrr.data.totals.map((t) => `${money(t.mrr)} from ${t.active} ${t.active === 1 ? "contract" : "contracts"}`).join("; ")}. This is the monthly value of the contracts in force,
            not the revenue booked.
          </p>
        )}
        {problem && <Refusal problem={problem} title="The change was not saved" />}
        {list.isError && <Refusal problem={problemOf(list.error)} title="The contracts could not be read" />}
        {adding && (
          <form
            className="fy-row fy-row--wrap gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              void create();
            }}
          >
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-cust`}>
                Customer
              </label>
              <select className="fy-input" id={`${id}-cust`} value={form.contact} onChange={(e) => setForm({ ...form, contact: e.target.value })}>
                <option value="">Choose a customer</option>
                {(customers.data ?? []).map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.legal_name}
                  </option>
                ))}
              </select>
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-name`}>
                Subscribes to
              </label>
              <input className="fy-input" id={`${id}-name`} value={form.name} maxLength={140} onChange={(e) => setForm({ ...form, name: e.target.value })} />
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-cur`}>
                Currency
              </label>
              <input className="fy-input" id={`${id}-cur`} value={form.currency} maxLength={3} onChange={(e) => setForm({ ...form, currency: e.target.value })} />
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-mrr`}>
                Monthly value
              </label>
              <input className="fy-input fy-num" id={`${id}-mrr`} inputMode="decimal" value={form.mrr} onChange={(e) => setForm({ ...form, mrr: e.target.value })} />
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-start`}>
                Starts
              </label>
              <input className="fy-input" id={`${id}-start`} type="date" value={form.starts} onChange={(e) => setForm({ ...form, starts: e.target.value })} />
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-end`}>
                Agreed end (if any)
              </label>
              <input className="fy-input" id={`${id}-end`} type="date" value={form.ends} onChange={(e) => setForm({ ...form, ends: e.target.value })} />
            </div>
            <div className="fy-field self-end">
              <Button type="submit" variant="primary" disabled={busy || !form.contact || !form.name.trim() || !form.starts || !form.mrr.trim()}>
                Save the contract
              </Button>
            </div>
          </form>
        )}
        {list.data && items.length === 0 && !adding && (
          <div className="fy-empty">
            <Icon of={CalendarRange} size={24} />
            <p className="fy-secondary">No subscription contracts yet. Record what each customer subscribes to, and an upgrade, downgrade, cancellation or renewal from its day.</p>
            {may && (
              <Button type="button" variant="primary" onClick={() => setAdding(true)}>
                {add.label}
              </Button>
            )}
          </div>
        )}
        {items.length > 0 && (
          <div className="fy-tablewrap">
            <table className="fy-table" aria-label="Subscription contracts">
              <thead>
                <tr>
                  <th scope="col">Subscribes to</th>
                  <th scope="col">From</th>
                  <th scope="col">Status today</th>
                  <th scope="col" className="fy-num">
                    Monthly value
                  </th>
                  <th scope="col">
                    <span className="sr-only">Action</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {items.map((c: Contract) => (
                  <tr key={c.id}>
                    <th scope="row">
                      {c.name}
                      <span className="fy-small fy-muted block">{nameOf.get(c.contact_id) ?? ""}</span>
                    </th>
                    <td>
                      {date(c.starts_on)}
                      {c.ends_on ? ` to ${date(c.ends_on)}` : ""}
                    </td>
                    <td>
                      <span className={`fy-chip fy-chip--${c.status === "active" ? "good" : "neutral"}`}>{statusWords[c.status] ?? c.status}</span>
                    </td>
                    <td className="fy-num">{money(c.mrr)}</td>
                    <td>
                      {may && (
                        <Button size="sm" variant="ghost" type="button" onClick={() => setAmending(c)} title={amendHelp.tooltip} aria-label={`${amendHelp.label}: ${c.name}`}>
                          {amendHelp.label}
                        </Button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {amending && (
          <form
            className="fy-row fy-row--wrap gap-2"
            aria-label={`Amend ${amending.name}`}
            onSubmit={(e) => {
              e.preventDefault();
              void amend();
            }}
          >
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-kind`}>
                Change
              </label>
              <select className="fy-input" id={`${id}-kind`} value={change.kind} onChange={(e) => setChange({ ...change, kind: e.target.value })}>
                <option value="upgrade">Upgrade</option>
                <option value="downgrade">Downgrade</option>
                <option value="cancel">Cancel</option>
                <option value="renew">Renew after it ended</option>
              </select>
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-on`}>
                From
              </label>
              <input className="fy-input" id={`${id}-on`} type="date" value={change.on} onChange={(e) => setChange({ ...change, on: e.target.value })} />
            </div>
            {change.kind !== "cancel" && (
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-new`}>
                  New monthly value
                </label>
                <input className="fy-input fy-num" id={`${id}-new`} inputMode="decimal" value={change.mrr} onChange={(e) => setChange({ ...change, mrr: e.target.value })} />
              </div>
            )}
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-why`}>
                Why
              </label>
              <input className="fy-input" id={`${id}-why`} value={change.reason} maxLength={500} onChange={(e) => setChange({ ...change, reason: e.target.value })} />
            </div>
            <div className="fy-field self-end fy-row gap-2">
              <Button type="submit" variant="primary" disabled={busy || !change.on || !change.reason.trim()}>
                Save the change
              </Button>
              <Button type="button" variant="ghost" onClick={() => setAmending(null)} disabled={busy}>
                Cancel
              </Button>
            </div>
          </form>
        )}
      </div>
    </section>
  );
}
