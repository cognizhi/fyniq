import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChevronLeft, Package, Pencil, Plus, Trash2 } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { contactEditors } from "./Contacts";

type Item = components["schemas"]["Item"];
type Account = components["schemas"]["Account"];
interface Price {
  currency: string;
  unit_price: string;
}

async function listItems(): Promise<Item[]> {
  const out: Item[] = [];
  let cursor: string | undefined;
  do {
    const { data, error } = await api.GET("/v1/items", { params: { query: { page_size: 500, cursor } } });
    if (error) throw error;
    out.push(...(data.items ?? []));
    cursor = data.next_cursor;
  } while (cursor);
  return out;
}

async function revenueAccounts(): Promise<Account[]> {
  const { data, error } = await api.GET("/v1/accounts", { params: { query: { page_size: 500 } } });
  if (error) return []; // a role without V on the chart still keeps items
  return (data.items ?? []).filter((a) => a.type === "revenue" && a.active && !a.group);
}

// A unit price is an exact decimal, never a number in the browser (ENG-10, RND-DISPLAY).
const pricePattern = /^(0|[1-9][0-9]{0,13})(\.[0-9]{1,6})?$/;

interface Draft {
  code: string;
  name: string;
  type: "product" | "service";
  unit: string;
  description: string;
  gst_code: string;
  revenue_account_id: string;
  prices: Price[];
  active: boolean;
}

const blank: Draft = { code: "", name: "", type: "service", unit: "", description: "", gst_code: "", revenue_account_id: "", prices: [{ currency: "SGD", unit_price: "" }], active: true };

function draftOf(i: Item): Draft {
  return {
    code: i.code,
    name: i.name,
    type: i.type,
    unit: i.unit ?? "",
    description: i.description ?? "",
    gst_code: i.gst_code ?? "",
    revenue_account_id: i.revenue_account_id ?? "",
    prices: (i.prices ?? []).map((p) => ({ currency: p.currency, unit_price: p.unit_price })),
    active: i.active,
  };
}

function ItemForm({ editing, accounts, onClose, onSaved }: { editing: Item | "new" | null; accounts: Account[]; onClose: () => void; onSaved: (what: string) => void }) {
  const queries = useQueryClient();
  const id = useId();
  const isNew = editing === "new";
  const current = editing && editing !== "new" ? editing : null;
  const one = useQuery({
    queryKey: ["item", current?.id],
    enabled: current !== null,
    queryFn: async () => {
      const r = await api.GET("/v1/items/{item_id}", { params: { path: { item_id: current?.id ?? "" } } });
      if (r.error) throw r.error;
      return { item: r.data, etag: etagOf(r.response) || `"${r.data.version}"` };
    },
  });
  const [draft, setDraft] = useState<Draft | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const base = isNew ? blank : one.data ? draftOf(one.data.item) : null;
  const d = draft ?? base;
  const set = <K extends keyof Draft>(k: K, v: Draft[K]) => d && setDraft({ ...d, [k]: v });
  const setPrice = (i: number, p: Partial<Price>) => d && set("prices", d.prices.map((x, j) => (j === i ? { ...x, ...p } : x)));

  const close = () => {
    setDraft(null);
    setProblem(null);
    setErrors({});
    onClose();
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!d) return;
    const errs: Record<string, string> = {};
    if (isNew && !d.code.trim()) errs.code = "Give the item a code; it never changes.";
    if (!d.name.trim()) errs.name = "Give the item a name.";
    const prices = d.prices.filter((p) => p.currency.trim() || p.unit_price.trim()).map((p) => ({ currency: p.currency.trim().toUpperCase(), unit_price: p.unit_price.trim() }));
    prices.forEach((p, i) => {
      if (!/^[A-Z]{3}$/.test(p.currency)) errs[`price-${i}`] = "A currency is three letters, such as SGD.";
      else if (!pricePattern.test(p.unit_price)) errs[`price-${i}`] = "A price is a plain decimal with up to 6 decimals, such as 180.00, without commas or a currency sign.";
      else if (prices.findIndex((q) => q.currency === p.currency) !== i) errs[`price-${i}`] = `${p.currency} has a price already.`;
    });
    setErrors(errs);
    if (Object.keys(errs).length) return;
    setBusy(true);
    setProblem(null);
    const text = (v: string) => v.trim() || undefined;
    if (isNew) {
      const body = { code: d.code.trim(), name: d.name.trim(), type: d.type, unit: text(d.unit), description: text(d.description), gst_code: text(d.gst_code), revenue_account_id: text(d.revenue_account_id), prices };
      const out = await api.POST("/v1/items", { params: { header: { "Idempotency-Key": newKey() } }, body });
      setBusy(false);
      if (!out.data) return setProblem(problemOf(out.error));
      await queries.invalidateQueries({ queryKey: ["items"] });
      onSaved(`${d.code.trim()} ${d.name.trim()} added.`);
      return close();
    }
    if (!one.data || !base) return setBusy(false);
    const changes: Record<string, unknown> = {};
    for (const k of ["name", "type", "unit", "description", "gst_code", "revenue_account_id"] as const) if (d[k].trim() !== base[k]) changes[k] = d[k].trim();
    if (d.active !== base.active) changes.active = d.active;
    if (JSON.stringify(prices) !== JSON.stringify(base.prices)) changes.prices = prices; // prices sent replace every price
    if (Object.keys(changes).length === 0) {
      setBusy(false);
      return close();
    }
    const out = await api.PATCH("/v1/items/{item_id}", { params: { path: { item_id: one.data.item.id }, header: { "If-Match": one.data.etag } }, body: changes as never });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await Promise.all(["items", "item"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
    onSaved(`${d.code} ${d.name.trim()} saved.`);
    close();
  };

  const err = (k: string) =>
    errors[k] ? (
      <span className="fy-error" id={`${id}-${k}-error`}>
        {errors[k]}
      </span>
    ) : null;

  return (
    <Dialog
      open={editing !== null}
      onOpenChange={(o) => !o && close()}
      dismissible={!busy}
      wide
      eyebrow="Items and prices"
      title={isNew ? "New item" : current ? `${current.code} ${current.name}` : "Item"}
      description="A product or service you sell, with a unit price per currency. Its code never changes; an item is never deleted."
    >
      {one.isError && <Refusal problem={problemOf(one.error)} title="The item could not be read" />}
      {problem && <Refusal problem={problem} title={isNew ? "The item was not added" : "The item was not saved"} />}
      {d && (
        <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
          <div className="fy-form fy-form--2">
            {isNew && (
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-code`}>
                  Code
                </label>
                <input className="fy-input" id={`${id}-code`} value={d.code} onChange={(e) => set("code", e.target.value)} aria-invalid={errors.code ? true : undefined} aria-describedby={errors.code ? `${id}-code-error` : undefined} />
                {err("code")}
              </div>
            )}
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-name`}>
                Name
              </label>
              <input className="fy-input" id={`${id}-name`} value={d.name} onChange={(e) => set("name", e.target.value)} aria-invalid={errors.name ? true : undefined} aria-describedby={errors.name ? `${id}-name-error` : undefined} />
              {err("name")}
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-type`}>
                Type
              </label>
              <select className="fy-select" id={`${id}-type`} value={d.type} onChange={(e) => set("type", e.target.value as Draft["type"])}>
                <option value="service">Service</option>
                <option value="product">Product</option>
              </select>
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-unit`}>
                Unit
              </label>
              <input className="fy-input" id={`${id}-unit`} value={d.unit} placeholder="hour, month, seat…" onChange={(e) => set("unit", e.target.value)} />
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-account`}>
                Revenue account
              </label>
              <select className="fy-select" id={`${id}-account`} value={d.revenue_account_id} onChange={(e) => set("revenue_account_id", e.target.value)}>
                <option value="">None</option>
                {accounts.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.code} {a.name}
                  </option>
                ))}
              </select>
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-gst`}>
                GST code
              </label>
              <input className="fy-input" id={`${id}-gst`} value={d.gst_code} placeholder="SR" onChange={(e) => set("gst_code", e.target.value)} />
            </div>
            <div className="fy-field fy-field--full">
              <label className="fy-label" htmlFor={`${id}-desc`}>
                Printed description
              </label>
              <textarea className="fy-textarea" id={`${id}-desc`} value={d.description} onChange={(e) => set("description", e.target.value)} />
            </div>
          </div>
          <fieldset className="fy-fieldset">
            <legend className="fy-label">Unit prices</legend>
            {d.prices.map((p, i) => (
              <div key={i} className="fy-row fy-row--wrap fy-row--top gap-2">
                <label className="fy-sr" htmlFor={`${id}-cur-${i}`}>
                  Currency {i + 1}
                </label>
                <input className="fy-input w-24" id={`${id}-cur-${i}`} value={p.currency} maxLength={3} onChange={(e) => setPrice(i, { currency: e.target.value })} aria-describedby={errors[`price-${i}`] ? `${id}-price-${i}-error` : undefined} />
                <label className="fy-sr" htmlFor={`${id}-price-${i}`}>
                  Unit price {i + 1}
                </label>
                <input
                  className="fy-input fy-num max-w-48"
                  id={`${id}-price-${i}`}
                  inputMode="decimal"
                  value={p.unit_price}
                  placeholder="180.00"
                  onChange={(e) => setPrice(i, { unit_price: e.target.value })}
                  aria-invalid={errors[`price-${i}`] ? true : undefined}
                  aria-describedby={errors[`price-${i}`] ? `${id}-price-${i}-error` : undefined}
                />
                <Button variant="ghost" size="sm" type="button" onClick={() => set("prices", d.prices.filter((_, j) => j !== i))} aria-label={`Remove the ${p.currency || "new"} price`}>
                  <Icon of={Trash2} />
                </Button>
                {err(`price-${i}`)}
              </div>
            ))}
            <div>
              <Button size="sm" type="button" onClick={() => set("prices", [...d.prices, { currency: "", unit_price: "" }])}>
                <Icon of={Plus} />
                Add a price in another currency
              </Button>
            </div>
          </fieldset>
          {!isNew && (
            <label className="fy-row gap-2">
              <input type="checkbox" checked={d.active} onChange={(e) => set("active", e.target.checked)} />
              Active: offered for new documents
            </label>
          )}
          <div className="fy-row justify-end gap-2">
            <Button variant="ghost" type="button" onClick={close} disabled={busy}>
              Cancel
            </Button>
            <Button variant="primary" type="submit" disabled={busy}>
              {isNew ? "Add the item" : "Save the item"}
            </Button>
          </div>
        </form>
      )}
    </Dialog>
  );
}

// Items (docs/design/screens Items; FR-ITM-01): the products and services the company sells, each with
// its unit, revenue account, GST code and a unit price per currency, as exact decimals. The year's
// quantity and sales join with the sales documents.
export function Items() {
  const { session } = useSignedIn();
  const q = useQuery({ queryKey: ["items"], queryFn: listItems });
  const accounts = useQuery({ queryKey: ["accounts", "revenue"], queryFn: revenueAccounts });
  const [editing, setEditing] = useState<Item | "new" | null>(null);
  const [inactive, setInactive] = useState(false);
  const [status, setStatus] = useState("");
  const may = (session.roles ?? []).some((r) => contactEditors.includes(r));
  const add = helpText("item.add");
  const accountName = (id?: string) => {
    const a = (accounts.data ?? []).find((x) => x.id === id);
    return a ? `${a.code} ${a.name}` : "";
  };
  const shown = (q.data ?? []).filter((i) => inactive || i.active).sort((a, b) => a.code.localeCompare(b.code, "en", { numeric: true }));

  return (
    <div className="fy-page fy-enter">
      <Link className="fy-btn fy-btn--ghost fy-btn--sm -ms-2 self-start" to="/contacts">
        <Icon of={ChevronLeft} />
        Contacts
      </Link>
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">Items and prices</h1>
          <p className="fy-secondary">The products and services the company sells.</p>
        </div>
        {may && (
          <div className="fy-pagehead__actions">
            <Button variant="primary" onClick={() => setEditing("new")} title={add.tooltip}>
              <Icon of={Plus} />
              {add.label}
            </Button>
          </div>
        )}
      </div>
      <p className="fy-small" role="status">
        {status}
      </p>
      {q.isError && <Refusal problem={problemOf(q.error)} title="The items could not be shown" />}
      {q.isPending && <p className="fy-secondary">Loading the items…</p>}
      {q.data && q.data.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={Package} size={24} />
          <h2 className="fy-h3">No items yet</h2>
          <p className="fy-secondary">Add what you sell, so invoices start with its name, price, account and GST code.</p>
          {!may && <p className="fy-small fy-secondary">A Bookkeeper, an AR or AP specialist, an Accountant or a Controller adds them.</p>}
        </div>
      )}
      {(q.data ?? []).length > 0 && (
        <>
          <label className="fy-row gap-2 fy-small">
            <input type="checkbox" checked={inactive} onChange={(e) => setInactive(e.target.checked)} />
            Show inactive items
          </label>
          <section className="fy-card" aria-labelledby="items-title">
            <h2 className="fy-sr" id="items-title">
              Items
            </h2>
            <div className="fy-tablewrap">
              <table className="fy-table" aria-labelledby="items-title">
                <thead>
                  <tr>
                    <th scope="col">Code</th>
                    <th scope="col">Name</th>
                    <th scope="col" className="fy-from-medium">
                      Unit
                    </th>
                    <th scope="col" className="fy-num">
                      Prices
                    </th>
                    <th scope="col" className="fy-from-expanded">
                      Revenue account
                    </th>
                    <th scope="col" className="fy-from-expanded">
                      GST
                    </th>
                    <th scope="col">
                      <span className="fy-sr">Actions</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {shown.map((i) => (
                    <tr key={i.id}>
                      <td className="fy-mono">{i.code}</td>
                      <td>
                        {i.name}
                        {!i.active && <span className="fy-chip fy-chip--neutral ms-2">Inactive</span>}
                        <span className="fy-small fy-muted block">{i.type === "product" ? "Product" : "Service"}</span>
                      </td>
                      <td className="fy-from-medium">{i.unit}</td>
                      <td className="fy-num">
                        {(i.prices ?? []).map((p) => (
                          <span key={p.currency} className="block">
                            {p.currency} {formatAmount(p.unit_price)}
                          </span>
                        ))}
                      </td>
                      <td className="fy-from-expanded fy-secondary">{accountName(i.revenue_account_id)}</td>
                      <td className="fy-from-expanded fy-mono">{i.gst_code}</td>
                      <td>
                        {may && (
                          <Button variant="ghost" size="sm" onClick={() => setEditing(i)} aria-label={`Change ${i.code} ${i.name}`}>
                            <Icon of={Pencil} />
                          </Button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>
        </>
      )}
      <ItemForm editing={editing} accounts={accounts.data ?? []} onClose={() => setEditing(null)} onSaved={setStatus} />
    </div>
  );
}
