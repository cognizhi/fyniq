import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { CircleCheck, CircleX, Lock, Plus, ReceiptText, Save, Trash2, UserCheck } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { listAccounts, postable } from "../accounting/data";
import { who } from "../accounting/Journals";
import { preparers } from "./Bills";
import { OrderStateChips, type Order } from "./PurchaseOrders";

type Draft = components["schemas"]["PurchaseOrderDraft"];
type Errors = Record<string, string>;

// The code the API answers when an order's total is above the self-approval threshold (FM-2): ask for approval.
const overThreshold = "PO-14";

interface Line {
  key: string;
  account_id: string;
  description: string;
  quantity: string;
  unit_price: string;
  gst_code: string;
}

export interface OrderForm {
  supplier_id: string;
  reference: string;
  date: string;
  expected_on: string;
  currency: string;
  notes: string;
  lines: Line[];
}

let keys = 0;
const newLine = (): Line => ({ key: `ol-${++keys}`, account_id: "", description: "", quantity: "1", unit_price: "", gst_code: "" });

function today(): string {
  return new Date().toLocaleDateString("en-CA");
}

// decimal is what was typed as an exact decimal with at most places decimals, separators removed; null when it is not
// one.
function decimal(s: string, places: number): string | null {
  const t = s.trim().replace(/,/g, "");
  return new RegExp(`^(0|[1-9]\\d{0,13})(\\.\\d{1,${places}})?$`).test(t) ? t.replace(/^(\d+)\.?$/, "$1") : null;
}

// orderFormOf is a saved order as the form shows it.
function orderFormOf(o: Order): OrderForm {
  return {
    supplier_id: o.supplier?.id ?? "",
    reference: o.reference ?? "",
    date: o.date ?? "",
    expected_on: o.expected_on ?? "",
    currency: o.currency,
    notes: o.notes ?? "",
    lines: o.lines.map((l) => ({ key: `ol-${++keys}`, account_id: l.account_id, description: l.description ?? "", quantity: l.quantity, unit_price: l.unit_price, gst_code: l.gst_code })),
  };
}

const blank = (l: Line) => !l.account_id && !l.description.trim() && !l.unit_price.trim();

// checkOrder turns what was typed into the order to send, or says what to fix under each field. The API checks the
// rest: the supplier, the codes, the accounts and the total.
export function checkOrder(f: OrderForm, base: string): { draft?: Draft; errors: Errors } {
  const errors: Errors = {};
  const currency = (f.currency.trim() || base).toUpperCase();
  if (!/^[A-Z]{3}$/.test(currency)) errors.currency = "Enter a currency code, such as SGD.";
  const lines: Draft["lines"] = [];
  for (const l of f.lines) {
    if (blank(l)) continue;
    const quantity = decimal(l.quantity, 6);
    const price = decimal(l.unit_price, 6);
    if (!l.account_id) errors[`${l.key}.account`] = "Choose what the line is for.";
    if (!l.gst_code) errors[`${l.key}.gst_code`] = "Choose the GST code the supplier will charge.";
    if (quantity === null || Number(quantity) === 0) errors[`${l.key}.quantity`] = "Enter how many, such as 3 or 2.5.";
    if (price === null) errors[`${l.key}.unit_price`] = "Enter the price of one before GST, such as 1,850.00.";
    lines.push({ account_id: l.account_id, description: l.description.trim() || undefined, quantity: quantity ?? "0", unit_price: price ?? "0", gst_code: l.gst_code });
  }
  if (lines.length === 0) errors.lines = "Add a line for what is ordered.";
  if (Object.keys(errors).length > 0) return { errors };
  return {
    errors,
    draft: {
      supplier_id: f.supplier_id || undefined,
      reference: f.reference.trim() || undefined,
      date: f.date || undefined,
      expected_on: f.expected_on || undefined,
      currency,
      notes: f.notes.trim() || undefined,
      lines,
    },
  };
}

// MakeBill makes a draft bill from an approved order (FR-AP-01): how much of each line it takes, at most what is left;
// then the person enters the supplier's invoice on it.
function MakeBill({ open, order, etag, onClose }: { open: boolean; order: Order; etag: string; onClose: () => void }) {
  const confirm = useConfirm();
  const navigate = useNavigate();
  const id = useId();
  const h = helpText("order.bill");
  const [take, setTake] = useState<string[]>(order.lines.map((l) => l.left));
  const [errors, setErrors] = useState<Errors>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);

  const run = async (e: FormEvent) => {
    e.preventDefault();
    const errs: Errors = {};
    const lines: { line: number; quantity: string }[] = [];
    take.forEach((t, i) => {
      if (!t.trim()) return;
      const q = decimal(t, 6);
      if (q === null) errs[i] = "Enter a quantity, or leave it blank to take none of this line.";
      else if (Number(q) > 0) lines.push({ line: i + 1, quantity: q });
    });
    if (Object.keys(errs).length === 0 && lines.length === 0) errs.all = "Take some of at least one line.";
    setErrors(errs);
    if (Object.keys(errs).length > 0) return;
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/purchase-orders/{purchase_order_id}/bill", {
        params: { path: { purchase_order_id: order.id }, header: { "If-Match": etag, "Idempotency-Key": newKey() } },
        body: { lines },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    void navigate({ to: "/purchases/bills/$billId", params: { billId: out.data.id } });
  };

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()} dismissible={!busy} eyebrow={order.number} title={h.label} description="How much of each line the bill takes. The draft bill opens next: enter the supplier's invoice date and number, and correct the amounts to the invoice.">
      {problem && <Refusal problem={problem} title="The bill was not made" />}
      <form className="fy-stack fy-stack--3" onSubmit={(e) => void run(e)} noValidate>
        {order.lines.map((l, i) => (
          <div className="fy-field" key={i}>
            <label className="fy-label" htmlFor={`${id}-take-${i}`}>
              {l.description || `Line ${i + 1}`} ({l.left} left)
            </label>
            <input
              className="fy-input fy-num"
              id={`${id}-take-${i}`}
              inputMode="decimal"
              value={take[i] ?? ""}
              disabled={Number(l.left) === 0}
              onChange={(e) => setTake(take.map((t, k) => (k === i ? e.target.value : t)))}
              aria-invalid={Boolean(errors[i]) || undefined}
              aria-describedby={errors[i] ? `${id}-take-${i}-error` : undefined}
            />
            {errors[i] && (
              <span className="fy-error" id={`${id}-take-${i}-error`}>
                {errors[i]}
              </span>
            )}
          </div>
        ))}
        {errors.all && <span className="fy-error">{errors.all}</span>}
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy} title={h.tooltip}>
            <Icon of={ReceiptText} />
            {h.label}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// CloseOrder closes an approved order with a reason: no more bills are made from it.
function CloseOrder({ open, order, etag, onClose, onDone }: { open: boolean; order: Order; etag: string; onClose: () => void; onDone: (message: string) => void }) {
  const confirm = useConfirm();
  const id = useId();
  const h = helpText("order.close");
  const [reason, setReason] = useState("");
  const [missing, setMissing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const run = async (e: FormEvent) => {
    e.preventDefault();
    if (!reason.trim()) return setMissing(true);
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/purchase-orders/{purchase_order_id}/close", {
        params: { path: { purchase_order_id: order.id }, header: { "If-Match": etag, "Idempotency-Key": newKey() } },
        body: { reason: reason.trim() },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone(`Closed ${out.data.number ?? "the order"}: no more bills are made from it.`);
  };
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()} dismissible={!busy} eyebrow={order.number} title={h.label} description="No more bills are made from it. The bills made already stand.">
      {problem && <Refusal problem={problem} title="The order was not closed" />}
      <form className="fy-stack fy-stack--3" onSubmit={(e) => void run(e)} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-reason`}>
            Why it is closed
          </label>
          <textarea
            className="fy-textarea"
            id={`${id}-reason`}
            value={reason}
            maxLength={500}
            onChange={(e) => {
              setReason(e.target.value);
              setMissing(false);
            }}
            aria-invalid={missing || undefined}
            aria-describedby={missing ? `${id}-reason-error` : undefined}
          />
          {missing && (
            <span className="fy-error" id={`${id}-reason-error`}>
              Say why: the audit trail keeps the reason.
            </span>
          )}
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy} title={h.tooltip}>
            <Icon of={Lock} />
            {h.label}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// PurchaseOrderPage is a purchase order (docs/design/screens PurchaseOrders; FR-AP-01; PRD-001 rev 0.87): a draft is
// entered line by line with a quantity and a price, and approved as a bill is posted (alone at or below the
// self-approval threshold, otherwise by another person); an approved order shows what bills take of each line, makes
// bills, and is closed with a reason. It posts nothing.
export function PurchaseOrderPage() {
  const { orderId } = useParams({ strict: false }) as { orderId?: string };
  const isNew = !orderId;
  const { session, organisation } = useSignedIn();
  const base = organisation?.base_currency ?? "SGD";
  const queries = useQueryClient();
  const navigate = useNavigate();
  const confirm = useConfirm();
  const id = useId();
  const may = (session.roles ?? []).some((r) => preparers.includes(r));

  const accounts = useQuery({ queryKey: ["accounts"], queryFn: listAccounts });
  const vendors = useQuery({
    queryKey: ["contacts", "vendor"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/contacts", { params: { query: { role: "vendor", page_size: 500 } } });
      if (error) throw error;
      return (data.items ?? []).filter((c) => c.active);
    },
  });
  const one = useQuery({
    queryKey: ["purchase-order", orderId],
    enabled: !isNew,
    queryFn: async () => {
      const r = await api.GET("/v1/purchase-orders/{purchase_order_id}", { params: { path: { purchase_order_id: orderId ?? "" } } });
      if (r.error) throw r.error;
      return { order: r.data, etag: etagOf(r.response) };
    },
  });
  const order = one.data?.order;
  const [form, setForm] = useState<OrderForm | null>(null);
  const f: OrderForm | null = form ?? (isNew ? { supplier_id: "", reference: "", date: today(), expected_on: "", currency: base, notes: "", lines: [newLine()] } : order ? orderFormOf(order) : null);
  const codes = useQuery({
    queryKey: ["gst-codes", f?.date ?? ""],
    enabled: Boolean(f),
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/purchases/gst-codes", { params: { query: { on: f?.date || undefined } } });
      if (error) throw error;
      return data;
    },
  });
  const [errors, setErrors] = useState<Errors>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [failed, setFailed] = useState("");
  const [status, setStatus] = useState("");
  const [billing, setBilling] = useState(false);
  const [closing, setClosing] = useState(false);

  const editable = may && (isNew || order?.status === "draft");
  const choices = postable(accounts.data ?? []).filter((a) => a.type === "expense" || a.type === "asset");
  const set = (patch: Partial<OrderForm>) => f && setForm({ ...f, ...patch });
  const setLine = (key: string, patch: Partial<Line>) => f && setForm({ ...f, lines: f.lines.map((l) => (l.key === key ? { ...l, ...patch } : l)) });
  const h = (k: string) => helpText(k);
  const err = (k: string) =>
    errors[k] ? (
      <span className="fy-error" id={`${id}-${k}-error`}>
        {errors[k]}
      </span>
    ) : null;
  const described = (k: string) => (errors[k] ? `${id}-${k}-error` : undefined);

  const refresh = async () => {
    setForm(null);
    await queries.invalidateQueries({ queryKey: ["purchase-order", orderId] });
    await queries.invalidateQueries({ queryKey: ["purchase-orders"] });
  };

  const save = async (e?: FormEvent) => {
    e?.preventDefault();
    if (!f) return;
    const { draft, errors: errs } = checkOrder(f, base);
    setErrors(errs);
    if (!draft) return setStatus("Fix the fields marked, then save again.");
    setBusy(true);
    setProblem(null);
    setFailed("The order was not saved");
    if (isNew) {
      const out = await api.POST("/v1/purchase-orders", { params: { header: { "Idempotency-Key": newKey() } }, body: draft });
      setBusy(false);
      if (!out.data) return setProblem(problemOf(out.error));
      await queries.invalidateQueries({ queryKey: ["purchase-orders"] });
      void navigate({ to: "/purchases/orders/$orderId", params: { orderId: out.data.id } });
      return;
    }
    if (!one.data) return;
    const out = await api.PUT("/v1/purchase-orders/{purchase_order_id}", { params: { path: { purchase_order_id: one.data.order.id }, header: { "If-Match": one.data.etag } }, body: draft });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await refresh();
    setStatus("Saved.");
  };

  const approve = async (ask: boolean) => {
    if (!one.data) return;
    setBusy(true);
    setProblem(null);
    setFailed(ask ? "The request was not made" : "The order was not approved");
    const params = { path: { purchase_order_id: one.data.order.id }, header: { "If-Match": one.data.etag, "Idempotency-Key": newKey() } };
    const out = ask
      ? await guarded(confirm, () => api.POST("/v1/purchase-orders/{purchase_order_id}/approval", { params }))
      : await guarded(confirm, () => api.POST("/v1/purchase-orders/{purchase_order_id}/approve", { params }));
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await refresh();
    setStatus(ask ? "Asked for approval: the order is approved when another person approves it." : "Approved. Bills can be made from it now.");
  };

  if (!isNew && one.isPending) return <p className="fy-secondary">Loading the order…</p>;
  if (!isNew && one.isError) return <Refusal problem={problemOf(one.error)} title="The order could not be read" />;
  if (!f) return null;

  const needsApproval = problem?.code === overThreshold && order?.status === "draft";
  const title = isNew ? "New purchase order" : `${order?.number ?? "Draft order"}${order?.supplier?.name ? ` · ${order.supplier.name}` : ""}`;
  const left = order?.lines.some((l) => Number(l.left) > 0) ?? false;

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <span>Purchases</span>
            <span aria-hidden="true">›</span>
            <Link to="/purchases/orders">Purchase orders</Link>
          </nav>
          <div className="fy-row fy-row--wrap gap-2">
            <h1 className="fy-h1">{title}</h1>
            {order && <OrderStateChips order={order} />}
          </div>
          {order && (
            <p className="fy-secondary">
              {order.date ? `Dated ${date(order.date)}` : "No date"}
              {order.expected_on ? ` · expected ${date(order.expected_on)}` : ""}
              {order.prepared_by ? ` · prepared by ${who(order.prepared_by)}` : ""}
              {order.approved_by ? ` · approved by ${who(order.approved_by)}` : ""}
            </p>
          )}
        </div>
      </div>
      <ReviewBanner screen="purchase-orders" />
      {needsApproval && problem && (
        <div className="fy-banner fy-banner--info" role="note">
          <Icon of={UserCheck} size={20} />
          <div className="fy-banner__text">
            <strong>Another person approves this order</strong>
            <span className="fy-small">{problem.detail} The one who prepared it never approves it.</span>
          </div>
          <div className="fy-banner__action">
            <Button size="sm" variant="primary" type="button" onClick={() => void approve(true)} disabled={busy} title={h("order.ask-approval").tooltip}>
              <Icon of={UserCheck} />
              {h("order.ask-approval").label}
            </Button>
          </div>
        </div>
      )}
      {problem && !needsApproval && <Refusal problem={problem} title={failed} />}
      {status && (
        <p className="fy-small" role="status">
          {status}
        </p>
      )}
      {order?.approval_asked && (
        <div className="fy-banner fy-banner--info" role="status">
          <Icon of={UserCheck} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">Awaiting approval</span>
            <span className="fy-small">
              Another person approves it in the <Link to="/inbox">Inbox</Link>.
            </span>
          </div>
        </div>
      )}
      {order && order.status === "draft" && !form && order.checks.length > 0 && (
        <section className="fy-card" aria-labelledby={`${id}-checks`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-checks`}>
              {order.checks.length === 1 ? "1 thing to fix before it can be approved" : `${order.checks.length} things to fix before it can be approved`}
            </h2>
          </div>
          <ul className="fy-list">
            {order.checks.map((c, i) => (
              <li key={`${c.code}-${i}`} className="fy-list__item">
                <span className="fy-list__icon fy-list__icon--critical">
                  <Icon of={CircleX} size={20} />
                </span>
                <span className="fy-list__main">
                  <span className="fy-list__title whitespace-normal">
                    {c.message} <span className="fy-small fy-muted fy-mono">{c.code}</span>
                  </span>
                  {c.fix && <span className="fy-list__meta whitespace-normal">{c.fix}.</span>}
                </span>
                <span className="fy-sr">Stops its approval</span>
              </li>
            ))}
          </ul>
        </section>
      )}

      {order && order.status !== "draft" ? (
        <section className="fy-card" aria-labelledby={`${id}-billed`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-billed`}>
              What is ordered and billed
            </h2>
            <span className="fy-small fy-muted">{order.reference ?? ""}</span>
          </div>
          <table className="fy-table fy-table--compact fy-table--cards" aria-labelledby={`${id}-billed`}>
            <thead>
              <tr>
                <th scope="col">Line</th>
                <th scope="col" className="fy-num">
                  Ordered
                </th>
                <th scope="col" className="fy-num">
                  Billed
                </th>
                <th scope="col" className="fy-num">
                  Left
                </th>
                <th scope="col" className="fy-num">
                  Amount ({order.currency})
                </th>
              </tr>
            </thead>
            <tbody>
              {order.lines.map((l, i) => (
                <tr key={i}>
                  <td data-card="title">
                    {l.description || `Line ${i + 1}`}
                    <span className="fy-small fy-muted block">
                      {l.quantity} × {formatAmount(l.unit_price)} · {l.gst_code}
                    </span>
                  </td>
                  <td data-card="meta" className="fy-num">
                    {l.quantity}
                  </td>
                  <td data-card="meta" className="fy-num">
                    {l.billed}
                    {Number(l.on_drafts) > 0 ? ` (+${l.on_drafts} on drafts)` : ""}
                  </td>
                  <td data-card="meta" className="fy-num">
                    {l.left}
                  </td>
                  <td data-card="amount" className="fy-num">
                    {formatAmount(l.amount.amount)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <p className="fy-card__body fy-small">
            Net {formatAmount(order.net.amount)} · GST expected {formatAmount(order.gst.amount)} · total {formatAmount(order.total.amount)} {order.currency}
            {order.total_base && order.currency !== base ? ` (${base} ${formatAmount(order.total_base.amount)})` : ""}
          </p>
        </section>
      ) : (
        <form className="fy-stack fy-stack--6" onSubmit={(e) => void save(e)} noValidate>
          <section className="fy-card" aria-labelledby={`${id}-head`}>
            <div className="fy-card__head">
              <h2 className="fy-h3" id={`${id}-head`}>
                Supplier and dates
              </h2>
            </div>
            <div className="fy-card__body fy-form fy-form--2">
              <div className="fy-field fy-field--full">
                <label className="fy-label" htmlFor={`${id}-supplier`}>
                  Supplier
                </label>
                <select className="fy-select" id={`${id}-supplier`} value={f.supplier_id} disabled={!editable} onChange={(e) => set({ supplier_id: e.target.value })}>
                  <option value="">Choose the supplier</option>
                  {(vendors.data ?? []).map((c) => (
                    <option key={c.id} value={c.id}>
                      {c.legal_name}
                    </option>
                  ))}
                </select>
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-reference`}>
                  Reference (optional)
                </label>
                <input className="fy-input" id={`${id}-reference`} value={f.reference} maxLength={60} disabled={!editable} placeholder="Their quotation number" onChange={(e) => set({ reference: e.target.value })} />
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-date`}>
                  Order date
                </label>
                <input className="fy-input" id={`${id}-date`} type="date" value={f.date} disabled={!editable} onChange={(e) => set({ date: e.target.value })} />
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-expected`}>
                  Expected on (optional)
                </label>
                <input className="fy-input" id={`${id}-expected`} type="date" value={f.expected_on} disabled={!editable} onChange={(e) => set({ expected_on: e.target.value })} />
              </div>
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-currency`}>
                  Currency
                </label>
                <input
                  className="fy-input fy-mono"
                  id={`${id}-currency`}
                  value={f.currency}
                  maxLength={3}
                  disabled={!editable}
                  onChange={(e) => set({ currency: e.target.value.toUpperCase() })}
                  aria-invalid={Boolean(errors.currency) || undefined}
                  aria-describedby={described("currency")}
                />
                {err("currency")}
              </div>
            </div>
          </section>
          <section className="fy-card" aria-labelledby={`${id}-lines`}>
            <div className="fy-card__head">
              <h2 className="fy-h3" id={`${id}-lines`}>
                Lines
              </h2>
              <span className="fy-small fy-muted">What is ordered, how many and at what price</span>
            </div>
            <div className="fy-card__body fy-stack fy-stack--4">
              {f.lines.map((l, i) => (
                <fieldset key={l.key} className="fy-fieldset">
                  <legend className="fy-small fy-strong">Line {i + 1}</legend>
                  <div className="fy-form fy-form--2">
                    <div className="fy-field">
                      <label className="fy-label" htmlFor={`${l.key}-account`}>
                        What it is for
                      </label>
                      <select
                        className="fy-select"
                        id={`${l.key}-account`}
                        value={l.account_id}
                        disabled={!editable}
                        onChange={(e) => setLine(l.key, { account_id: e.target.value })}
                        aria-invalid={Boolean(errors[`${l.key}.account`]) || undefined}
                        aria-describedby={described(`${l.key}.account`)}
                      >
                        <option value="">Choose an account</option>
                        {choices.map((a) => (
                          <option key={a.id} value={a.id}>
                            {a.code} {a.name}
                          </option>
                        ))}
                      </select>
                      {err(`${l.key}.account`)}
                    </div>
                    <div className="fy-field">
                      <label className="fy-label" htmlFor={`${l.key}-description`}>
                        Description
                      </label>
                      <input className="fy-input" id={`${l.key}-description`} value={l.description} maxLength={500} disabled={!editable} onChange={(e) => setLine(l.key, { description: e.target.value })} />
                    </div>
                    <div className="fy-field">
                      <label className="fy-label" htmlFor={`${l.key}-quantity`}>
                        Quantity
                      </label>
                      <input
                        className="fy-input fy-num"
                        id={`${l.key}-quantity`}
                        inputMode="decimal"
                        value={l.quantity}
                        disabled={!editable}
                        onChange={(e) => setLine(l.key, { quantity: e.target.value })}
                        aria-invalid={Boolean(errors[`${l.key}.quantity`]) || undefined}
                        aria-describedby={described(`${l.key}.quantity`)}
                      />
                      {err(`${l.key}.quantity`)}
                    </div>
                    <div className="fy-field">
                      <label className="fy-label" htmlFor={`${l.key}-price`}>
                        Unit price before GST ({f.currency || base})
                      </label>
                      <input
                        className="fy-input fy-num"
                        id={`${l.key}-price`}
                        inputMode="decimal"
                        value={l.unit_price}
                        disabled={!editable}
                        onChange={(e) => setLine(l.key, { unit_price: e.target.value })}
                        aria-invalid={Boolean(errors[`${l.key}.unit_price`]) || undefined}
                        aria-describedby={described(`${l.key}.unit_price`)}
                      />
                      {err(`${l.key}.unit_price`)}
                    </div>
                    <div className="fy-field">
                      <label className="fy-label" htmlFor={`${l.key}-code`}>
                        GST code
                      </label>
                      <select
                        className="fy-select"
                        id={`${l.key}-code`}
                        value={l.gst_code}
                        disabled={!editable}
                        onChange={(e) => setLine(l.key, { gst_code: e.target.value })}
                        aria-invalid={Boolean(errors[`${l.key}.gst_code`]) || undefined}
                        aria-describedby={described(`${l.key}.gst_code`)}
                      >
                        <option value="">Choose a code</option>
                        {(codes.data?.codes ?? []).map((c) => (
                          <option key={c.code} value={c.code}>
                            {c.code}: {c.description}
                          </option>
                        ))}
                      </select>
                      {err(`${l.key}.gst_code`)}
                    </div>
                    {editable && f.lines.length > 1 && (
                      <div className="fy-field fy-field--full">
                        <Button variant="ghost" size="sm" type="button" aria-label={`Remove line ${i + 1}`} onClick={() => set({ lines: f.lines.filter((x) => x.key !== l.key) })}>
                          <Icon of={Trash2} />
                          Remove
                        </Button>
                      </div>
                    )}
                  </div>
                </fieldset>
              ))}
              {err("lines")}
              {editable && (
                <div>
                  <Button variant="ghost" type="button" onClick={() => set({ lines: [...f.lines, newLine()] })}>
                    <Icon of={Plus} />
                    Add a line
                  </Button>
                </div>
              )}
              {order && !form && (
                <p className="fy-small">
                  Net {formatAmount(order.net.amount)} · GST expected {formatAmount(order.gst.amount)} · total {formatAmount(order.total.amount)} {order.currency}
                </p>
              )}
            </div>
          </section>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-notes`}>
              Notes
            </label>
            <textarea className="fy-textarea" id={`${id}-notes`} value={f.notes} maxLength={2000} disabled={!editable} onChange={(e) => set({ notes: e.target.value })} />
          </div>
          {editable && (
            <div className="fy-row fy-row--wrap gap-2">
              <Button type="submit" disabled={busy} title={h("order.save").tooltip}>
                <Icon of={Save} />
                {h("order.save").label}
              </Button>
              {order && !form && !order.approval_asked && (
                <Button variant="primary" type="button" onClick={() => void approve(false)} disabled={busy || order.checks.length > 0} title={h("order.approve").tooltip}>
                  <Icon of={CircleCheck} />
                  {h("order.approve").label}
                </Button>
              )}
              {!isNew && form && <span className="fy-small fy-muted">Save the changes before approving it.</span>}
            </div>
          )}
        </form>
      )}

      {order && order.status === "approved" && may && (
        <div className="fy-row fy-row--wrap gap-2">
          {left && (
            <Button variant="primary" type="button" onClick={() => setBilling(true)} title={h("order.bill").tooltip}>
              <Icon of={ReceiptText} />
              {h("order.bill").label}…
            </Button>
          )}
          <Button type="button" onClick={() => setClosing(true)} title={h("order.close").tooltip}>
            <Icon of={Lock} />
            {h("order.close").label}…
          </Button>
        </div>
      )}
      {order && order.bills.length > 0 && (
        <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-labelledby={`${id}-bills`}>
          <h2 className="fy-h3" id={`${id}-bills`}>
            Bills made from it
          </h2>
          <ul className="fy-stack fy-stack--1">
            {order.bills.map((b) => (
              <li key={b.id} className="fy-small">
                <Link to="/purchases/bills/$billId" params={{ billId: b.id }}>
                  {b.number ?? "Draft bill"}
                </Link>
                {b.date ? ` · ${date(b.date)}` : ""} · {b.status === "posted" ? "posted" : "draft"}
              </li>
            ))}
          </ul>
        </section>
      )}
      {order && billing && one.data && <MakeBill open={billing} order={order} etag={one.data.etag} onClose={() => setBilling(false)} />}
      {order && closing && one.data && (
        <CloseOrder
          open={closing}
          order={order}
          etag={one.data.etag}
          onClose={() => setClosing(false)}
          onDone={(message) => {
            setClosing(false);
            void refresh().then(() => setStatus(message));
          }}
        />
      )}
    </div>
  );
}
