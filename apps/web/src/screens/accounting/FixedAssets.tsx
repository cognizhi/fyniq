import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useSearch } from "@tanstack/react-router";
import { Archive, CircleCheck, Lock, PackagePlus, Pencil, TriangleAlert, Undo2 } from "lucide-react";
import { useEffect, useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date, month } from "../../format/date";
import { minus, sum, typed } from "../../format/decimal";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";

// The fixed asset register (FR-FA-01…03; p1-fixed-assets task 5.1; docs/design/screens FixedAssets): the assets with
// their cost, accumulated depreciation and net book value; an asset's side-peek with its depreciation schedule (posted,
// waiting, projected) and its source; registering one from a purchase not in the register yet, or bought before the
// books start; changing its estimates; and selling or writing it off, whole or in part.

export type FixedAsset = components["schemas"]["FixedAsset"];
export type Unregistered = components["schemas"]["UnregisteredLine"];
export type Disposal = components["schemas"]["AssetDisposalView"];
type Money = components["schemas"]["Money"];
type Account = components["schemas"]["Account"];

// Who keeps the register: C/P on Appendix D's "Fixed assets", the Accountant. The API decides; the page offers the
// buttons to them only.
export const assetKeepers = ["accountant"];

// The report groups of the accounts an asset sits on (its category).
export const registerGroups = ["Property, plant & equipment", "Intangible assets"];

const money = (m: Money | undefined | null) => (m ? formatAmount(m.amount) : "—");
const etagOf = (a: FixedAsset) => `"${a.version}-posted"`;

// lifeText is a life in months as it reads: "3 years", "18 months".
export function lifeText(months: number): string {
  if (months % 12 === 0) return months === 12 ? "1 year" : `${months / 12} years`;
  return months === 1 ? "1 month" : `${months} months`;
}

export function StatusChip({ asset }: { asset: FixedAsset }) {
  switch (asset.status) {
    case "disposed":
      return (
        <span className="fy-chip fy-chip--neutral">
          <Icon of={Archive} />
          Disposed
        </span>
      );
    case "fully_depreciated":
      return (
        <span className="fy-chip fy-chip--neutral">
          <Icon of={CircleCheck} />
          Fully depreciated
        </span>
      );
    default:
      return (
        <span className="fy-chip fy-chip--good">
          <Icon of={CircleCheck} />
          In use
        </span>
      );
  }
}

// RegisterForm is an asset as the person types it: amounts and the life as typed, read when it is saved.
export interface RegisterForm {
  name: string;
  life_months: string;
  residual: string;
  start: string;
  notes: string;
  // An asset bought before the books start.
  account_id: string;
  purchased_on: string;
  cost: string;
  accumulated: string;
}

export const emptyRegister: RegisterForm = { name: "", life_months: "", residual: "", start: "", notes: "", account_id: "", purchased_on: "", cost: "", accumulated: "" };

// checkRegister reads the form into the API's draft, from a source line or as bought before the books start, or names
// each field that cannot be read, all at once.
export function checkRegister(f: RegisterForm, base: string, source: { document_id: string; line: number } | null) {
  const errors: Record<string, string> = {};
  const life = Number(f.life_months);
  if (!/^[0-9]{1,3}$/.test(f.life_months.trim()) || life < 1 || life > 600) errors.life_months = "Enter the useful life in whole months, from 1 to 600, such as 36.";
  const amount = (field: keyof RegisterForm, v: string, required: boolean) => {
    const t = typed(v);
    if (t === null || t.startsWith("-") || (required && t === "")) {
      errors[field] = required ? "Enter an amount, such as 3600.00." : "Enter an amount, such as 0.00, or leave it empty.";
      return undefined;
    }
    return t ? { amount: t, currency: base } : undefined;
  };
  const residual = amount("residual", f.residual, false);
  if (f.start && !/^[0-9]{4}-(0[1-9]|1[0-2])$/.test(f.start)) errors.start = "Choose the month depreciation starts.";
  const draft: components["schemas"]["FixedAssetDraft"] = {
    name: f.name.trim() || undefined,
    notes: f.notes || undefined,
    life_months: life,
    residual,
    start: f.start || undefined,
    method: "straight_line",
  };
  if (source) {
    draft.source = source;
  } else {
    if (!f.account_id) errors.account_id = "Choose the asset account it sits on.";
    if (!/^[0-9]{4}-[0-9]{2}-[0-9]{2}$/.test(f.purchased_on)) errors.purchased_on = "Enter the day it was bought, before the books start.";
    if (!f.name.trim()) errors.name = "Name it as you would find it in the office.";
    const cost = amount("cost", f.cost, true);
    const accumulated = amount("accumulated", f.accumulated || "0", true);
    if (cost && accumulated) draft.opening = { account_id: f.account_id, purchased_on: f.purchased_on, cost, accumulated_depreciation: accumulated };
  }
  return Object.keys(errors).length > 0 ? { errors } : { errors, draft };
}

// DisposalForm is a disposal as the person types it.
export interface DisposalForm {
  date: string;
  part: boolean;
  cost: string;
  proceeds: "money" | "invoice" | "none";
  money_account_id: string;
  amount: string;
  gst_code: string;
  invoice_id: string;
  invoice_line: string;
  notes: string;
}

// checkDisposal reads the form into the API's draft, or names each field that cannot be read, all at once.
export function checkDisposal(f: DisposalForm, base: string) {
  const errors: Record<string, string> = {};
  if (!/^[0-9]{4}-[0-9]{2}-[0-9]{2}$/.test(f.date)) errors.date = "Enter the day it was sold or written off.";
  const amount = (field: keyof DisposalForm, v: string) => {
    const t = typed(v);
    if (t === null || t === "" || t.startsWith("-") || Number(t) === 0) {
      errors[field] = "Enter an amount above zero, such as 1500.00.";
      return undefined;
    }
    return { amount: t, currency: base };
  };
  const draft: components["schemas"]["AssetDisposalDraft"] = { date: f.date, proceeds: f.proceeds, notes: f.notes || undefined };
  if (f.part) draft.cost = amount("cost", f.cost);
  if (f.proceeds === "money") {
    if (!f.money_account_id) errors.money_account_id = "Choose the account the money came into.";
    draft.money_account_id = f.money_account_id || undefined;
    draft.amount = amount("amount", f.amount);
    draft.gst_code = f.gst_code.trim() || undefined;
  }
  if (f.proceeds === "invoice") {
    if (!f.invoice_id) errors.invoice_id = "Choose the invoice to the buyer.";
    const line = Number(f.invoice_line);
    if (!/^[0-9]{1,3}$/.test(f.invoice_line) || line < 1) errors.invoice_line = "Enter the invoice's line for the asset, such as 1.";
    draft.invoice_id = f.invoice_id || undefined;
    draft.invoice_line = line;
  }
  return Object.keys(errors).length > 0 ? { errors } : { errors, draft };
}

function useAccounts() {
  return useQuery({
    queryKey: ["accounts", "all"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/accounts", { params: { query: { page_size: 500 } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
}

// FixedAssets is Accounting › Fixed assets (Expert).
export function FixedAssets() {
  const { session, organisation } = useSignedIn();
  const base = organisation?.base_currency ?? "SGD";
  const search = useSearch({ strict: false }) as { register?: string; line?: string };
  const may = (session.roles ?? []).some((r) => assetKeepers.includes(r));
  const [tab, setTab] = useState<"register" | "unregistered">("register");
  const [selected, setSelected] = useState<string | null>(null);
  const [registering, setRegistering] = useState<Unregistered | "opening" | null>(null);
  const [said, setSaid] = useState("");
  const assets = useQuery({
    queryKey: ["fixed-assets"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/fixed-assets", { params: { query: { page_size: 500 } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
  const unregistered = useQuery({
    queryKey: ["fixed-assets", "unregistered"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/fixed-assets/unregistered", { params: { query: { page_size: 500 } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
  const accounts = useAccounts();
  const codes = new Map((accounts.data ?? []).map((a) => [a.id, a]));
  const items = assets.data ?? [];
  const inUse = items.filter((a) => a.status !== "disposed");
  const waiting = unregistered.data ?? [];

  // A purchase named in the address, from its bill's prompt: open its registration once the list has it.
  useEffect(() => {
    if (!search.register || !may) return;
    const l = waiting.find((x) => x.document_id === search.register && String(x.line) === (search.line ?? "1"));
    if (l) setRegistering(l);
  }, [search.register, search.line, may, waiting.length]); // eslint-disable-line react-hooks/exhaustive-deps

  const add = helpText("fixed-assets.register");
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/accounting">Accounting</Link>
          </nav>
          <h1 className="fy-h1">Fixed asset register</h1>
          {items.length > 0 && (
            <p className="fy-secondary">
              {inUse.length === 1 ? "1 asset" : `${inUse.length} assets`} · cost {base} {formatAmount(sum(inUse.map((a) => a.cost.amount)))} · net book value {base}{" "}
              {formatAmount(sum(inUse.map((a) => a.net_book_value.amount)))}
            </p>
          )}
        </div>
        {may && (
          <div className="fy-pagehead__actions">
            <Button variant="secondary" onClick={() => setRegistering("opening")} title={helpText("fixed-assets.register-opening").tooltip}>
              <Icon of={PackagePlus} />
              {helpText("fixed-assets.register-opening").label}
            </Button>
          </div>
        )}
      </div>
      <ReviewBanner screen="fixed-assets" />
      {said && (
        <p className="fy-small" role="status">
          {said}
        </p>
      )}
      {assets.isError && <Refusal problem={problemOf(assets.error)} title="The register could not be listed" />}
      <div className="fy-tabs" role="tablist" aria-label="Register views">
        <button type="button" role="tab" aria-selected={tab === "register"} onClick={() => setTab("register")}>
          Register
        </button>
        <button type="button" role="tab" aria-selected={tab === "unregistered"} onClick={() => setTab("unregistered")} title={helpText("fixed-assets.unregistered").tooltip}>
          Not in the register yet{waiting.length > 0 ? ` (${waiting.length})` : ""}
        </button>
      </div>
      {tab === "register" && assets.data && items.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={PackagePlus} size={24} />
          <h2 className="fy-h3">No assets yet</h2>
          <p className="fy-secondary">An asset is registered from the bill or bank line that bought it, under Not in the register yet, or, bought before the books start, with its opening figures.</p>
        </div>
      )}
      {tab === "register" && items.length > 0 && (
        <div className="fy-split">
          <div className="fy-tablewrap">
            <table className="fy-table fy-table--cards fy-table--compact" aria-label="Assets">
              <thead>
                <tr>
                  <th scope="col">Asset</th>
                  <th scope="col">Category</th>
                  <th scope="col">Bought</th>
                  <th scope="col" className="fy-num">
                    Cost
                  </th>
                  <th scope="col">Life</th>
                  <th scope="col" className="fy-num">
                    Accumulated
                  </th>
                  <th scope="col" className="fy-num">
                    Net book value
                  </th>
                  <th scope="col">Status</th>
                </tr>
              </thead>
              <tbody>
                {items.map((a) => (
                  <tr key={a.id} aria-selected={selected === a.id}>
                    <td data-card="title">
                      <button type="button" className="cursor-pointer border-0 bg-transparent p-0 text-start font-[inherit] text-[var(--link)] underline-offset-2 hover:underline" onClick={() => setSelected(a.id)}>
                        {a.name}
                      </button>
                      <span className="fy-small fy-muted block fy-mono">{a.number}</span>
                    </td>
                    <td data-card="meta" className="fy-mono">
                      {codes.get(a.account_id)?.code ?? ""}
                    </td>
                    <td data-card="meta">{date(a.purchased_on)}</td>
                    <td className="fy-num">{money(a.cost)}</td>
                    <td>{lifeText(a.life_months)}</td>
                    <td className="fy-num">{money(a.accumulated_depreciation)}</td>
                    <td data-card="amount" className="fy-num">
                      {money(a.net_book_value)}
                    </td>
                    <td data-card="status">
                      <StatusChip asset={a} />
                      {a.checks.length > 0 && (
                        <span className="fy-chip fy-chip--warning ml-1">
                          <Icon of={TriangleAlert} />
                          Review
                        </span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
              <tfoot>
                <tr>
                  <td>Total, in use</td>
                  <td />
                  <td />
                  <td className="fy-num">{formatAmount(sum(inUse.map((a) => a.cost.amount)))}</td>
                  <td />
                  <td className="fy-num">{formatAmount(sum(inUse.map((a) => a.accumulated_depreciation.amount)))}</td>
                  <td className="fy-num">{formatAmount(sum(inUse.map((a) => a.net_book_value.amount)))}</td>
                  <td />
                </tr>
              </tfoot>
            </table>
          </div>
          {selected && <AssetPeek id={selected} may={may} base={base} accounts={codes} onSaid={setSaid} />}
        </div>
      )}
      {tab === "unregistered" && (
        <section className="fy-stack fy-stack--2" aria-label="Not in the register yet">
          <p className="fy-small fy-secondary">
            Lines of posted bills and bank lines that put a cost on an asset account, and that no asset was registered from. One at or above the capitalisation threshold should be in the
            register, so it depreciates.
          </p>
          {waiting.length === 0 ? (
            <p className="fy-secondary">Every purchase on an asset account is in the register.</p>
          ) : (
            <div className="fy-tablewrap">
              <table className="fy-table fy-table--cards fy-table--compact" aria-label="Purchases not in the register">
                <thead>
                  <tr>
                    <th scope="col">Purchase</th>
                    <th scope="col">Date</th>
                    <th scope="col">Account</th>
                    <th scope="col" className="fy-num">
                      Cost
                    </th>
                    <th scope="col">Threshold</th>
                    <th scope="col">
                      <span className="sr-only">Action</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {waiting.map((l) => (
                    <tr key={`${l.document_id}-${l.line}`}>
                      <td data-card="title">
                        {l.description || "Line " + l.line}
                        <span className="fy-small fy-muted block">
                          {l.type === "bill" ? (
                            <Link to="/purchases/bills/$billId" params={{ billId: l.document_id }}>
                              {l.number || "Bill"}
                            </Link>
                          ) : (
                            "Bank line"
                          )}{" "}
                          · line {l.line}
                        </span>
                      </td>
                      <td data-card="meta">{date(l.date)}</td>
                      <td className="fy-mono">{codes.get(l.account_id)?.code ?? ""}</td>
                      <td data-card="amount" className="fy-num">
                        {money(l.cost)}
                      </td>
                      <td>
                        {l.at_threshold ? (
                          <span className="fy-chip fy-chip--warning">
                            <Icon of={TriangleAlert} />
                            At or above
                          </span>
                        ) : (
                          <span className="fy-small fy-muted">Below</span>
                        )}
                      </td>
                      <td>
                        {may && (
                          <Button size="sm" variant="secondary" onClick={() => setRegistering(l)} title={add.tooltip}>
                            {add.label}
                          </Button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      )}
      {registering && (
        <RegisterDialog
          line={registering === "opening" ? null : registering}
          base={base}
          accounts={(accounts.data ?? []).filter((a) => a.active && !a.group && registerGroups.includes(a.report_group ?? ""))}
          onClose={() => setRegistering(null)}
          onDone={(a) => {
            setRegistering(null);
            setTab("register");
            setSelected(a.id);
            setSaid(`${a.number} ${a.name} is in the register.`);
          }}
        />
      )}
    </div>
  );
}

// MonthStatus is how a month of depreciation stands, with its icon and words.
function MonthStatus({ status }: { status: string }) {
  switch (status) {
    case "posted":
      return (
        <span className="fy-small">
          <Icon of={Lock} size={16} /> Posted
        </span>
      );
    case "reversed":
      return <span className="fy-small fy-muted">Reversed</span>;
    case "waiting":
      return (
        <span className="fy-chip fy-chip--warning">
          <Icon of={TriangleAlert} />
          Waiting
        </span>
      );
    default:
      return <span className="fy-small fy-muted">Projected</span>;
  }
}

// AssetPeek is an asset's side-peek: its figures, source, checks, depreciation schedule and disposals.
function AssetPeek({ id, may, base, accounts, onSaid }: { id: string; may: boolean; base: string; accounts: Map<string, Account>; onSaid: (s: string) => void }) {
  const heading = useId();
  const [dialog, setDialog] = useState<"change" | "dispose" | Disposal | null>(null);
  const asset = useQuery({
    queryKey: ["fixed-assets", id],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/fixed-assets/{asset_id}", { params: { path: { asset_id: id } } });
      if (error) throw error;
      return data;
    },
  });
  const disposals = useQuery({
    queryKey: ["asset-disposals", id],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/asset-disposals", { params: { query: { asset_id: id } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
  if (asset.isError) return <Refusal problem={problemOf(asset.error)} title="The asset could not be read" />;
  const a = asset.data;
  if (!a) return <aside className="fy-card fy-card--pad" aria-busy="true" />;
  const account = accounts.get(a.account_id);
  return (
    <aside className="fy-card" aria-labelledby={heading}>
      <div className="fy-card__head">
        <div className="fy-stack fy-stack--2">
          <span className="fy-eyebrow">
            {a.number} · {account ? `${account.code} ${account.name}` : "Asset"}
          </span>
          <h2 className="fy-h3" id={heading}>
            {a.name}
          </h2>
        </div>
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        <dl className="fy-dl">
          <div>
            <dt>Cost</dt>
            <dd className="fy-num">{money(a.cost)}</dd>
          </div>
          <div>
            <dt>Residual value</dt>
            <dd className="fy-num">{money(a.residual)}</dd>
          </div>
          <div>
            <dt>Life</dt>
            <dd>
              {lifeText(a.life_months)}, {month(a.start)} to {month(a.end)}, straight-line
            </dd>
          </div>
          <div>
            <dt>Accumulated depreciation</dt>
            <dd className="fy-num">{money(a.accumulated_depreciation)}</dd>
          </div>
          <div>
            <dt>Net book value</dt>
            <dd className="fy-num">{money(a.net_book_value)}</dd>
          </div>
          <div>
            <dt>Bought</dt>
            <dd>
              {date(a.purchased_on)}
              {a.source ? (
                <>
                  {" · "}
                  {a.source.type === "bill" ? (
                    <Link className="fy-mono" to="/purchases/bills/$billId" params={{ billId: a.source.document_id }}>
                      {a.source.number || "Bill"}
                    </Link>
                  ) : (
                    "a bank line"
                  )}{" "}
                  line {a.source.line}
                </>
              ) : (
                <> · before the books start, {money(a.opening_accumulated)} depreciated then</>
              )}
            </dd>
          </div>
        </dl>
        {a.checks.length > 0 && (
          <ul className="fy-list" aria-label="To review">
            {a.checks.map((c) => (
              <li key={c.code} className="fy-list__item">
                <span className="fy-list__icon fy-list__icon--warning">
                  <Icon of={TriangleAlert} size={20} />
                </span>
                <span className="fy-list__main">
                  <span className="fy-list__title whitespace-normal">{c.message}</span>
                  {c.fix && <span className="fy-list__meta whitespace-normal">{c.fix}.</span>}
                </span>
              </li>
            ))}
          </ul>
        )}
        <h3 className="fy-strong">Depreciation schedule</h3>
        <div className="fy-tablewrap max-h-72 overflow-y-auto">
          <table className="fy-table fy-table--compact" aria-label="Depreciation schedule">
            <thead>
              <tr>
                <th scope="col">Month</th>
                <th scope="col" className="fy-num">
                  Charge
                </th>
                <th scope="col">State</th>
              </tr>
            </thead>
            <tbody>
              {a.depreciation.map((m, i) => (
                <tr key={`${m.month}-${m.version}-${i}`}>
                  <td>{month(m.month)}</td>
                  <td className={`fy-num ${m.status === "projected" || m.status === "reversed" ? "fy-muted" : ""}`}>{money(m.amount)}</td>
                  <td>
                    <MonthStatus status={m.status} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <p className="fy-small fy-muted">
          Each month posts on its last day: Dr Depreciation, Cr Accumulated depreciation (6610 and 1590 in the standard chart). The cents are spread evenly over the months (ALLOC-EVEN).
        </p>
        {(disposals.data ?? []).length > 0 && (
          <>
            <h3 className="fy-strong">Disposals</h3>
            <ul className="fy-list" aria-label="Disposals">
              {(disposals.data ?? []).map((d) => (
                <li key={d.id} className="fy-list__item">
                  <span className="fy-list__main">
                    <span className="fy-list__title whitespace-normal">
                      <span className="fy-mono">{d.number}</span> · {date(d.date)} · {d.proceeds === "none" ? "written off" : d.whole ? "sold" : "part sold"}
                      {d.status === "reversed" ? " · voided" : ""}
                    </span>
                    <span className="fy-list__meta whitespace-normal">
                      Cost {money(d.cost)}, net book value {money(d.net_book_value)}, {d.gain_or_loss.amount.startsWith("-") ? "loss" : "gain"} {base}{" "}
                      {formatAmount(d.gain_or_loss.amount.replace(/^-/, ""))}
                      {d.approval_asked ? " · a void is asked for" : ""}
                    </span>
                  </span>
                  {may && d.status === "posted" && !d.approval_asked && (
                    <Button size="sm" variant="ghost" onClick={() => setDialog(d)} aria-label={`Void ${d.number}`}>
                      <Icon of={Undo2} />
                      Void
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          </>
        )}
      </div>
      {may && a.status !== "disposed" && (
        <div className="fy-card__foot fy-row fy-row--wrap">
          <Button size="sm" variant="secondary" onClick={() => setDialog("dispose")} title={helpText("fixed-assets.dispose").tooltip}>
            <Icon of={Archive} />
            {helpText("fixed-assets.dispose").label}
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setDialog("change")} title={helpText("fixed-assets.change").tooltip}>
            <Icon of={Pencil} />
            {helpText("fixed-assets.change").label}
          </Button>
        </div>
      )}
      {dialog === "change" && <ChangeDialog asset={a} base={base} onClose={() => setDialog(null)} onDone={(s) => (setDialog(null), onSaid(s))} />}
      {dialog === "dispose" && <DisposeDialog asset={a} base={base} onClose={() => setDialog(null)} onDone={(s) => (setDialog(null), onSaid(s))} />}
      {dialog && typeof dialog === "object" && <VoidDialog disposal={dialog} onClose={() => setDialog(null)} onDone={(s) => (setDialog(null), onSaid(s))} />}
    </aside>
  );
}

// Field is a labelled input with its error under it.
function Field({ id, label, error, help, children }: { id: string; label: string; error?: string; help?: string; children: React.ReactElement<Record<string, unknown>> }) {
  return (
    <div className="fy-field">
      <label className="fy-label" htmlFor={id}>
        {label}
      </label>
      {children}
      {help && (
        <span className="fy-help" id={`${id}-help`}>
          {help}
        </span>
      )}
      {error && (
        <span className="fy-error" id={`${id}-error`}>
          {error}
        </span>
      )}
    </div>
  );
}

const described = (id: string, error?: string, help?: string) => ({
  id,
  "aria-invalid": error ? true : undefined,
  "aria-describedby": [help && `${id}-help`, error && `${id}-error`].filter(Boolean).join(" ") || undefined,
});

// RegisterDialog registers an asset from a purchase not in the register yet, or one bought before the books start.
function RegisterDialog({ line, base, accounts, onClose, onDone }: { line: Unregistered | null; base: string; accounts: Account[]; onClose: () => void; onDone: (a: FixedAsset) => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [f, setF] = useState<RegisterForm>({ ...emptyRegister, name: line?.description ?? "", account_id: accounts.length === 1 ? (accounts[0]?.id ?? "") : "" });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);
  const set = (k: keyof RegisterForm) => (e: { target: { value: string } }) => setF({ ...f, [k]: e.target.value });

  const save = async () => {
    const { draft, errors } = checkRegister(f, base, line ? { document_id: line.document_id, line: line.line } : null);
    setErrors(errors);
    if (!draft) return;
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () => api.POST("/v1/fixed-assets", { params: { header: { "Idempotency-Key": key } }, body: draft }));
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["fixed-assets"] });
    onDone(out.data);
  };

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={line ? `Register ${line.description || "the purchase"}` : "Register an asset bought before the books start"}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={() => void save()} disabled={busy}>
            {busy ? "Registering…" : "Register"}
          </Button>
        </>
      }
    >
      <div className="fy-stack fy-stack--3">
        {problem && <Refusal problem={problem} title="The asset was not registered" />}
        {line && (
          <p className="fy-small fy-secondary">
            {line.number || "The bank line"}, line {line.line}, {date(line.date)}: it costs {base} {formatAmount(line.cost.amount)}, what the line put on the asset account.
          </p>
        )}
        <Field id={`${id}-name`} label="Name" error={errors.name}>
          <input className="fy-input" {...described(`${id}-name`, errors.name)} value={f.name} maxLength={200} onChange={set("name")} />
        </Field>
        {!line && (
          <>
            <Field id={`${id}-account`} label="Asset account (its category)" error={errors.account_id}>
              <select className="fy-select" {...described(`${id}-account`, errors.account_id)} value={f.account_id} onChange={set("account_id")}>
                <option value="">Choose the account</option>
                {accounts.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.code} {a.name}
                  </option>
                ))}
              </select>
            </Field>
            <Field id={`${id}-bought`} label="Bought on" error={errors.purchased_on}>
              <input className="fy-input" type="date" {...described(`${id}-bought`, errors.purchased_on)} value={f.purchased_on} onChange={set("purchased_on")} />
            </Field>
            <Field id={`${id}-cost`} label={`Cost (${base})`} error={errors.cost} help="As the opening balances carry it on the asset account.">
              <input className="fy-input fy-num" inputMode="decimal" {...described(`${id}-cost`, errors.cost, "help")} value={f.cost} onChange={set("cost")} />
            </Field>
            <Field id={`${id}-acc`} label={`Accumulated depreciation at the books start (${base})`} error={errors.accumulated} help="As the opening balances carry it on 1590.">
              <input className="fy-input fy-num" inputMode="decimal" {...described(`${id}-acc`, errors.accumulated, "help")} value={f.accumulated} onChange={set("accumulated")} />
            </Field>
          </>
        )}
        <Field id={`${id}-life`} label="Useful life (months)" error={errors.life_months} help="Such as 36 for a laptop; the accountant confirms the lives the company uses.">
          <input className="fy-input fy-num" inputMode="numeric" {...described(`${id}-life`, errors.life_months, "help")} value={f.life_months} onChange={set("life_months")} />
        </Field>
        <Field id={`${id}-residual`} label={`Residual value (${base}, optional)`} error={errors.residual}>
          <input className="fy-input fy-num" inputMode="decimal" {...described(`${id}-residual`, errors.residual)} value={f.residual} onChange={set("residual")} />
        </Field>
        <Field id={`${id}-start`} label="Depreciation starts (optional)" error={errors.start} help="The month it went into use; left empty, the month it was bought.">
          <input className="fy-input" type="month" {...described(`${id}-start`, errors.start, "help")} value={f.start} onChange={set("start")} />
        </Field>
        <Field id={`${id}-notes`} label="Notes (optional)">
          <textarea className="fy-input" id={`${id}-notes`} value={f.notes} maxLength={2000} onChange={set("notes")} />
        </Field>
      </div>
    </Dialog>
  );
}

// ChangeDialog saves an asset's next version: its name, notes and estimates, for the months not posted yet.
function ChangeDialog({ asset, base, onClose, onDone }: { asset: FixedAsset; base: string; onClose: () => void; onDone: (s: string) => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [f, setF] = useState({ name: asset.name, notes: asset.notes ?? "", life: String(asset.life_months), residual: asset.residual.amount });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);

  const save = async () => {
    const e: Record<string, string> = {};
    const life = Number(f.life);
    if (!/^[0-9]{1,3}$/.test(f.life) || life < 1 || life > 600) e.life = "Enter the useful life in whole months, from 1 to 600.";
    const residual = typed(f.residual);
    if (residual === null || residual === "" || residual.startsWith("-")) e.residual = "Enter an amount, such as 0.00.";
    if (!f.name.trim()) e.name = "Name it.";
    setErrors(e);
    if (Object.keys(e).length > 0) return;
    setBusy(true);
    setProblem(null);
    const body: components["schemas"]["FixedAssetChange"] = { name: f.name.trim(), notes: f.notes, life_months: life, residual: { amount: residual ?? "0", currency: base } };
    const out = await guarded(confirm, () =>
      api.POST("/v1/fixed-assets/{asset_id}/change", { params: { path: { asset_id: asset.id }, header: { "If-Match": etagOf(asset), "Idempotency-Key": key } }, body }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["fixed-assets"] });
    onDone(`${out.data.number} is changed: its next months depreciate ${formatAmount(minus(out.data.net_book_value.amount, out.data.residual.amount))} more by ${month(out.data.end)}.`);
  };

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`Change ${asset.name}`}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={() => void save()} disabled={busy}>
            {busy ? "Saving…" : "Save"}
          </Button>
        </>
      }
    >
      <div className="fy-stack fy-stack--3">
        {problem && <Refusal problem={problem} title="The asset was not changed" />}
        <p className="fy-small fy-secondary">A new life or residual value applies to the months not posted yet; what has posted stays.</p>
        <Field id={`${id}-name`} label="Name" error={errors.name}>
          <input className="fy-input" {...described(`${id}-name`, errors.name)} value={f.name} maxLength={200} onChange={(e) => setF({ ...f, name: e.target.value })} />
        </Field>
        <Field id={`${id}-life`} label="Useful life from its start (months)" error={errors.life}>
          <input className="fy-input fy-num" inputMode="numeric" {...described(`${id}-life`, errors.life)} value={f.life} onChange={(e) => setF({ ...f, life: e.target.value })} />
        </Field>
        <Field id={`${id}-residual`} label={`Residual value (${base})`} error={errors.residual}>
          <input className="fy-input fy-num" inputMode="decimal" {...described(`${id}-residual`, errors.residual)} value={f.residual} onChange={(e) => setF({ ...f, residual: e.target.value })} />
        </Field>
        <Field id={`${id}-notes`} label="Notes">
          <textarea className="fy-input" id={`${id}-notes`} value={f.notes} maxLength={2000} onChange={(e) => setF({ ...f, notes: e.target.value })} />
        </Field>
      </div>
    </Dialog>
  );
}

// DisposeDialog sells or writes off an asset, whole or in part: it shows what it would post before recording it.
function DisposeDialog({ asset, base, onClose, onDone }: { asset: FixedAsset; base: string; onClose: () => void; onDone: (s: string) => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [f, setF] = useState<DisposalForm>({
    date: new Date().toLocaleDateString("en-CA"),
    part: false,
    cost: "",
    proceeds: "money",
    money_account_id: "",
    amount: "",
    gst_code: "",
    invoice_id: "",
    invoice_line: "1",
    notes: "",
  });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [preview, setPreview] = useState<Disposal | null>(null);
  const [key] = useState(newKey);
  const accounts = useAccounts();
  const codes = new Map((accounts.data ?? []).map((a) => [a.id, a]));
  const invoices = useQuery({
    queryKey: ["invoices", "issued"],
    enabled: f.proceeds === "invoice",
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/invoices", { params: { query: { status: "posted", page_size: 100 } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
  const set = (k: keyof DisposalForm) => (e: { target: { value: string } }) => (setPreview(null), setF({ ...f, [k]: e.target.value }));
  const send = async (prefer?: string) => {
    const { draft, errors } = checkDisposal(f, base);
    setErrors(errors);
    if (!draft) return null;
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/fixed-assets/{asset_id}/disposals", {
        params: { path: { asset_id: asset.id }, header: { "If-Match": etagOf(asset), "Idempotency-Key": prefer ? newKey() : key, ...(prefer ? { Prefer: prefer } : {}) } },
        body: draft,
      }),
    );
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return null;
    }
    return out.data;
  };
  const check = async () => setPreview(await send("return=preview"));
  const record = async () => {
    const d = await send();
    if (!d) return;
    await queries.invalidateQueries({ queryKey: ["fixed-assets"] });
    await queries.invalidateQueries({ queryKey: ["asset-disposals"] });
    onDone(`${d.number} records it: a ${d.gain_or_loss.amount.startsWith("-") ? "loss" : "gain"} of ${base} ${formatAmount(d.gain_or_loss.amount.replace(/^-/, ""))}.`);
  };

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`Sell or write off ${asset.name}`}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          {preview ? (
            <Button onClick={() => void record()} disabled={busy}>
              {busy ? "Recording…" : "Record"}
            </Button>
          ) : (
            <Button onClick={() => void check()} disabled={busy}>
              {busy ? "Checking…" : "Check"}
            </Button>
          )}
        </>
      }
    >
      <div className="fy-stack fy-stack--3">
        {problem && <Refusal problem={problem} title="The disposal was not recorded" />}
        <Field id={`${id}-date`} label="Sold or written off on" error={errors.date}>
          <input className="fy-input" type="date" {...described(`${id}-date`, errors.date)} value={f.date} onChange={set("date")} />
        </Field>
        <fieldset className="fy-stack fy-stack--2">
          <legend className="fy-label">What went</legend>
          <label className="fy-row gap-2">
            <input type="radio" name={`${id}-part`} checked={!f.part} onChange={() => (setPreview(null), setF({ ...f, part: false }))} />
            The whole asset
          </label>
          <label className="fy-row gap-2">
            <input type="radio" name={`${id}-part`} checked={f.part} onChange={() => (setPreview(null), setF({ ...f, part: true }))} />
            Part of it
          </label>
        </fieldset>
        {f.part && (
          <Field id={`${id}-cost`} label={`The part of its cost (${base})`} error={errors.cost} help={`Of ${base} ${formatAmount(asset.cost.amount)}; its depreciation goes with it pro rata.`}>
            <input className="fy-input fy-num" inputMode="decimal" {...described(`${id}-cost`, errors.cost, "help")} value={f.cost} onChange={set("cost")} />
          </Field>
        )}
        <fieldset className="fy-stack fy-stack--2">
          <legend className="fy-label">Proceeds</legend>
          {(
            [
              ["money", "Money received into a bank, cash or wallet account"],
              ["invoice", "On a sales invoice to the buyer, through the disposal clearing account"],
              ["none", "None: a write-off"],
            ] as const
          ).map(([v, label]) => (
            <label key={v} className="fy-row gap-2">
              <input type="radio" name={`${id}-proceeds`} checked={f.proceeds === v} onChange={() => (setPreview(null), setF({ ...f, proceeds: v }))} />
              {label}
            </label>
          ))}
        </fieldset>
        {f.proceeds === "money" && (
          <>
            <Field id={`${id}-account`} label="Money account" error={errors.money_account_id}>
              <select className="fy-select" {...described(`${id}-account`, errors.money_account_id)} value={f.money_account_id} onChange={set("money_account_id")}>
                <option value="">Choose the account</option>
                {(accounts.data ?? [])
                  .filter((a) => a.money_account && a.active && !a.group && (!a.currency || a.currency === base))
                  .map((a) => (
                    <option key={a.id} value={a.id}>
                      {a.code} {a.name}
                    </option>
                  ))}
              </select>
            </Field>
            <Field id={`${id}-amount`} label={`Amount received (${base}, GST included)`} error={errors.amount}>
              <input className="fy-input fy-num" inputMode="decimal" {...described(`${id}-amount`, errors.amount)} value={f.amount} onChange={set("amount")} />
            </Field>
            <Field id={`${id}-gst`} label="GST code (when GST-registered)" help="Usually SR: the sale of a business asset is a taxable supply.">
              <input className="fy-input" {...described(`${id}-gst`, undefined, "help")} value={f.gst_code} maxLength={10} onChange={set("gst_code")} />
            </Field>
          </>
        )}
        {f.proceeds === "invoice" && (
          <>
            <Field id={`${id}-invoice`} label="Invoice to the buyer" error={errors.invoice_id} help="Its line for the asset is on the disposal clearing account.">
              <select className="fy-select" {...described(`${id}-invoice`, errors.invoice_id, "help")} value={f.invoice_id} onChange={set("invoice_id")}>
                <option value="">Choose the invoice</option>
                {(invoices.data ?? []).map((i) => (
                  <option key={i.id} value={i.id}>
                    {i.number} · {date(i.date)}
                  </option>
                ))}
              </select>
            </Field>
            <Field id={`${id}-line`} label="Its line" error={errors.invoice_line}>
              <input className="fy-input fy-num" inputMode="numeric" {...described(`${id}-line`, errors.invoice_line)} value={f.invoice_line} onChange={set("invoice_line")} />
            </Field>
          </>
        )}
        <Field id={`${id}-notes`} label="Notes (optional)">
          <textarea className="fy-input" id={`${id}-notes`} value={f.notes} maxLength={2000} onChange={set("notes")} />
        </Field>
        {preview && (
          <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-label="What it posts" role="status">
            <p className="fy-strong">
              {preview.gain_or_loss.amount.startsWith("-") ? "A loss" : "A gain"} of {base} {formatAmount(preview.gain_or_loss.amount.replace(/^-/, ""))}
            </p>
            <p className="fy-small fy-secondary">
              Net book value {base} {formatAmount(preview.net_book_value.amount)} (cost {formatAmount(preview.cost.amount)} less {formatAmount(preview.accumulated_depreciation.amount)}{" "}
              depreciated); proceeds net of GST {base} {formatAmount(preview.net_proceeds.amount)}.
            </p>
            <ul className="fy-small fy-mono">
              {preview.ledger_impact.map((l, i) => (
                <li key={i}>
                  {codes.get(l.account_id)?.code ?? ""} {formatAmount(l.base_amount.amount)}
                </li>
              ))}
            </ul>
          </section>
        )}
      </div>
    </Dialog>
  );
}

// VoidDialog voids a disposal, or in Founder mode asks another person to approve its void (FM-1).
function VoidDialog({ disposal, onClose, onDone }: { disposal: Disposal; onClose: () => void; onDone: (s: string) => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [day, setDay] = useState(new Date().toLocaleDateString("en-CA"));
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);

  const save = async () => {
    if (!reason.trim()) return setProblem({ code: "API-INPUT-01", title: "Say why it is voided", detail: "The audit trail keeps the reason." } as ApiProblem);
    setBusy(true);
    setProblem(null);
    const params = { path: { disposal_id: disposal.id }, header: { "If-Match": `"${disposal.version}-${disposal.status}"`, "Idempotency-Key": key } };
    const body = { date: day, reason: reason.trim() };
    const out = await guarded(confirm, () => api.POST("/v1/asset-disposals/{disposal_id}/void", { params, body }));
    if (!out.data && problemOf(out.error).code === "ASSET-19") {
      const asked = await guarded(confirm, () => api.POST("/v1/asset-disposals/{disposal_id}/void-approval", { params: { ...params, header: { ...params.header, "Idempotency-Key": newKey() } }, body }));
      setBusy(false);
      if (!asked.data) return setProblem(problemOf(asked.error));
      await queries.invalidateQueries({ queryKey: ["asset-disposals"] });
      return onDone(`Voiding ${disposal.number} waits for another person's approval.`);
    }
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["fixed-assets"] });
    await queries.invalidateQueries({ queryKey: ["asset-disposals"] });
    onDone(`${disposal.number} is voided, and the asset is as it was.`);
  };

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`Void ${disposal.number}`}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={() => void save()} disabled={busy}>
            {busy ? "Voiding…" : "Void"}
          </Button>
        </>
      }
    >
      <div className="fy-stack fy-stack--3">
        {problem && <Refusal problem={problem} title="The disposal was not voided" />}
        <p className="fy-small fy-secondary">The void reverses its entry and restores the asset; its months depreciate again from the disposal's month.</p>
        <Field id={`${id}-date`} label="Voided on">
          <input className="fy-input" type="date" id={`${id}-date`} value={day} onChange={(e) => setDay(e.target.value)} />
        </Field>
        <Field id={`${id}-reason`} label="Why">
          <input className="fy-input" id={`${id}-reason`} value={reason} maxLength={500} onChange={(e) => setReason(e.target.value)} />
        </Field>
      </div>
    </Dialog>
  );
}
