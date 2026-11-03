import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, Gavel, Plus, Receipt, TriangleAlert } from "lucide-react";
import { useId, useState, type ReactElement } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { isZero, typed } from "../../format/decimal";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { EvidencePanel } from "../accounting/JournalEvidence";
import { listAccounts, postable, type Account } from "../accounting/data";
import { isDirectorAccount, useDirectors } from "./data";

// Accounting › Costs before incorporation (docs/design/screens PreIncorporationCosts; FR-EQ-03, design D7): the costs a
// director or shareholder paid before the books start, each with its receipt, waiting for the accountant's decision
// (⚠) or decided and posted to the person's account on 2500; and what they come to for each person.

export type PreCost = components["schemas"]["PreIncorporationCost"];
type Page = components["schemas"]["PreIncorporationCostPage"];
type Treatment = components["schemas"]["PreIncorporationCostDecisionDraft"]["treatment"];

// Who records a cost and attaches its receipt: A on "Director accounts" (Appendix D), an Owner or the Controller. Who
// decides it: P on the row, the accountant. The API decides; the page offers the buttons to them only.
export const recorders = ["owner", "controller"];
export const deciders = ["accountant"];

export const treatmentWords: Record<Treatment, string> = {
  deductible: "Deductible",
  capital: "Capital",
  not_deductible: "Not deductible",
};

type Filter = "all" | "needs_decision" | "decided";

async function costs(): Promise<Page> {
  let cursor: string | undefined;
  let page: Page | undefined;
  const items: PreCost[] = [];
  do {
    const r = await api.GET("/v1/pre-incorporation-costs", { params: { query: { page_size: 500, cursor } } });
    if (r.error) throw r.error;
    page = r.data;
    items.push(...(r.data.items ?? []));
    cursor = r.data.next_cursor;
  } while (cursor);
  return { ...page, items };
}

function Decided({ c }: { c: PreCost }) {
  if (!c.decision) {
    return (
      <span className="fy-chip fy-chip--warning">
        <Icon of={TriangleAlert} />
        Needs decision
      </span>
    );
  }
  const d = c.decision;
  return (
    <span className="fy-stack" style={{ gap: 0 }}>
      <span className="fy-chip fy-chip--good">
        <Icon of={CircleCheck} />
        {treatmentWords[d.treatment]}
        {d.claim_gst ? " · GST claimed" : ""}
      </span>
      <span className="fy-small fy-muted">
        {[d.account_code && `${d.account_code} ${d.account_name ?? ""}`.trim(), d.decided_by?.name, date(d.decided_on)].filter(Boolean).join(" · ")}
      </span>
    </span>
  );
}

// Field is a labelled input with its error under it.
function Field({ id, label, error, help, children }: { id: string; label: string; error?: string; help?: string; children: ReactElement }) {
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

interface CostForm {
  date: string;
  supplier: string;
  description: string;
  contact_id: string;
  gross: string;
  gst: string;
}

const emptyCost: CostForm = { date: "", supplier: "", description: "", contact_id: "", gross: "", gst: "" };

// checkCost reads the form into the API's draft, or the field errors that stop it.
export function checkCost(f: CostForm, base: string, booksStart?: string) {
  const errors: Record<string, string> = {};
  if (!/^\d{4}-\d{2}-\d{2}$/.test(f.date)) errors.date = "Enter the date on the receipt.";
  else if (booksStart && f.date >= booksStart) errors.date = `The date is not before the books start, ${date(booksStart)}. Record a later cost as a claim or a bill.`;
  if (!f.supplier.trim()) errors.supplier = "Enter who it was paid to.";
  if (!f.description.trim()) errors.description = "Enter what it was for.";
  if (!f.contact_id) errors.contact_id = "Choose the director or shareholder who paid it.";
  const gross = typed(f.gross);
  if (!gross || gross.startsWith("-") || isZero(gross)) errors.gross = `Enter what they paid in ${base}, more than zero.`;
  const gst = typed(f.gst);
  if (gst === null || gst.startsWith("-")) errors.gst = "Enter the GST the receipt shows, or leave it empty.";
  if (Object.keys(errors).length > 0) return { errors };
  return {
    errors,
    draft: {
      date: f.date,
      supplier: f.supplier.trim(),
      description: f.description.trim(),
      contact_id: f.contact_id,
      gross: { amount: gross ?? "", currency: base },
      ...(gst && !isZero(gst) ? { gst: { amount: gst, currency: base } } : {}),
    },
  };
}

// CostDialog records a cost, or changes one that needs a decision.
function CostDialog({ cost, base, booksStart, onClose, onDone }: { cost: PreCost | null; base: string; booksStart?: string; onClose: () => void; onDone: (said: string, id: string) => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const directors = useDirectors();
  const [f, setF] = useState<CostForm>(
    cost
      ? { date: cost.date, supplier: cost.supplier, description: cost.description, contact_id: cost.contact_id, gross: cost.gross.amount, gst: cost.gst?.amount ?? "" }
      : emptyCost,
  );
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);
  const set = (k: keyof CostForm) => (e: { target: { value: string } }) => setF({ ...f, [k]: e.target.value });
  const save = async () => {
    const { draft, errors } = checkCost(f, base, booksStart);
    setErrors(errors);
    if (!draft) return;
    setBusy(true);
    setProblem(null);
    let out;
    if (cost) {
      const read = await api.GET("/v1/pre-incorporation-costs/{cost_id}", { params: { path: { cost_id: cost.id } } });
      out = read.error
        ? read
        : await guarded(confirm, () =>
            api.PUT("/v1/pre-incorporation-costs/{cost_id}", { params: { path: { cost_id: cost.id }, header: { "If-Match": etagOf(read.response), "Idempotency-Key": key } }, body: draft }),
          );
    } else {
      out = await guarded(confirm, () => api.POST("/v1/pre-incorporation-costs", { params: { header: { "Idempotency-Key": key } }, body: draft }));
    }
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["pre-incorporation-costs"] });
    onDone(cost ? `Saved ${out.data.number ?? "the cost"}.` : `Recorded ${out.data.number ?? "the cost"}. Attach its receipt for the accountant.`, out.data.id);
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title={cost ? `Change ${cost.number ?? "the cost"}` : "Add a cost"} description="A cost a director or shareholder paid themselves before the books start. It posts nothing until the accountant decides it.">
      <form
        className="fy-stack fy-stack--3"
        noValidate
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        {problem && <Refusal problem={problem} title="The cost was not saved" />}
        {directors.isError && <Refusal problem={problemOf(directors.error)} title="The directors could not be read" />}
        <div className="fy-form fy-form--2">
          <Field id={`${id}-date`} label="Date on the receipt" error={errors.date}>
            <input className="fy-input" type="date" value={f.date} onChange={set("date")} max={booksStart} {...described(`${id}-date`, errors.date)} />
          </Field>
          <Field id={`${id}-paid-by`} label="Paid by" error={errors.contact_id}>
            <select className="fy-select" value={f.contact_id} onChange={set("contact_id")} {...described(`${id}-paid-by`, errors.contact_id)}>
              <option value="">Choose a director or shareholder</option>
              {(directors.data ?? []).map((c) => (
                <option key={c.id} value={c.id}>
                  {c.legal_name}
                </option>
              ))}
            </select>
          </Field>
        </div>
        <Field id={`${id}-supplier`} label="Paid to" error={errors.supplier}>
          <input className="fy-input" value={f.supplier} onChange={set("supplier")} maxLength={200} {...described(`${id}-supplier`, errors.supplier)} />
        </Field>
        <Field id={`${id}-description`} label="What it was for" error={errors.description}>
          <input className="fy-input" value={f.description} onChange={set("description")} maxLength={500} {...described(`${id}-description`, errors.description)} />
        </Field>
        <div className="fy-form fy-form--2">
          <Field id={`${id}-gross`} label={`Amount paid (${base})`} error={errors.gross} help="GST included.">
            <input className="fy-input fy-num" inputMode="decimal" value={f.gross} onChange={set("gross")} {...described(`${id}-gross`, errors.gross, "GST included.")} />
          </Field>
          <Field id={`${id}-gst`} label={`GST shown (${base})`} error={errors.gst} help="Leave it empty when the receipt shows none.">
            <input className="fy-input fy-num" inputMode="decimal" value={f.gst} onChange={set("gst")} {...described(`${id}-gst`, errors.gst, "Leave it empty when the receipt shows none.")} />
          </Field>
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy}>
            <Icon of={Plus} />
            {busy ? "Saving…" : cost ? "Save" : "Add the cost"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// DecideDialog records the accountant's decision on a cost, which posts it.
function DecideDialog({ cost, onClose, onDone }: { cost: PreCost; onClose: () => void; onDone: (said: string) => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [treatment, setTreatment] = useState<Treatment | "">("");
  const [account, setAccount] = useState("");
  const [claimGST, setClaimGST] = useState(false);
  const [note, setNote] = useState("");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);
  const accounts = useQuery({ queryKey: ["accounts"], queryFn: listAccounts, staleTime: 60_000 });
  const choices = postable(accounts.data ?? []).filter((a: Account) => !isDirectorAccount(a) && a.role !== "gst-control");
  const hasGST = !!cost.gst && !isZero(cost.gst.amount);
  const decide = async () => {
    const errs: Record<string, string> = {};
    if (!treatment) errs.treatment = "Choose the income-tax treatment.";
    if (!account) errs.account = "Choose the account the cost posts to.";
    setErrors(errs);
    if (!treatment || !account) return;
    setBusy(true);
    setProblem(null);
    const read = await api.GET("/v1/pre-incorporation-costs/{cost_id}", { params: { path: { cost_id: cost.id } } });
    const out = read.error
      ? read
      : await guarded(confirm, () =>
          api.POST("/v1/pre-incorporation-costs/{cost_id}/decide", {
            params: { path: { cost_id: cost.id }, header: { "If-Match": etagOf(read.response), "Idempotency-Key": key } },
            body: { treatment, account_id: account, claim_gst: claimGST, ...(note.trim() ? { note: note.trim() } : {}) },
          }),
        );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["pre-incorporation-costs"] });
    await queries.invalidateQueries({ queryKey: ["director-accounts"] });
    onDone(`Decided ${cost.number ?? "the cost"}: posted on ${date(out.data.decision?.post_on ?? "")} to ${cost.paid_by}'s account.`);
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title={`Decide ${cost.number ?? "the cost"}`} description={`${cost.description}, paid by ${cost.paid_by}: ${formatAmount(cost.gross.amount)} ${cost.gross.currency}. The decision posts it at once.`}>
      <form
        className="fy-stack fy-stack--3"
        noValidate
        onSubmit={(e) => {
          e.preventDefault();
          void decide();
        }}
      >
        {problem && <Refusal problem={problem} title="The cost was not decided" />}
        {accounts.isError && <Refusal problem={problemOf(accounts.error)} title="The accounts could not be read" />}
        <Field id={`${id}-treatment`} label="Income-tax treatment" error={errors.treatment}>
          <select className="fy-select" value={treatment} onChange={(e) => setTreatment(e.target.value as Treatment)} {...described(`${id}-treatment`, errors.treatment)}>
            <option value="">Choose a treatment</option>
            <option value="deductible">Deductible: a revenue cost from up to 12 months before the business started</option>
            <option value="capital">Capital: such as incorporation costs or equipment</option>
            <option value="not_deductible">Not deductible</option>
          </select>
        </Field>
        <Field id={`${id}-account`} label="Account" error={errors.account}>
          <select className="fy-select" value={account} onChange={(e) => setAccount(e.target.value)} {...described(`${id}-account`, errors.account)}>
            <option value="">Choose an account</option>
            {choices.map((a) => (
              <option key={a.id} value={a.id}>
                {a.code} {a.name}
              </option>
            ))}
          </select>
        </Field>
        {hasGST && (
          <label className="fy-check">
            <input type="checkbox" checked={claimGST} onChange={(e) => setClaimGST(e.target.checked)} />
            <span>
              Claim the GST shown ({formatAmount(cost.gst?.amount ?? "0")}) as input tax
              <span className="fy-help block">Only while the company is GST-registered. Otherwise the GST stays in the cost.</span>
            </span>
          </label>
        )}
        <Field id={`${id}-note`} label="Note (optional)">
          <textarea className="fy-textarea" rows={2} maxLength={1000} value={note} onChange={(e) => setNote(e.target.value)} id={`${id}-note`} />
        </Field>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy}>
            <Icon of={Gavel} />
            {busy ? "Posting…" : "Decide and post"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// Detail is a cost's side panel: its facts, its statuses, its receipts, and the accountant's decision or the button to
// make it.
function Detail({ cost, mayRecord, mayDecide, onChange, onDecide, onSaid }: { cost: PreCost; mayRecord: boolean; mayDecide: boolean; onChange: () => void; onDecide: () => void; onSaid: (s: string) => void }) {
  const queries = useQueryClient();
  const q = useQuery({
    queryKey: ["pre-incorporation-costs", cost.id],
    queryFn: async () => {
      const r = await api.GET("/v1/pre-incorporation-costs/{cost_id}", { params: { path: { cost_id: cost.id } } });
      if (r.error) throw r.error;
      return { cost: r.data, etag: etagOf(r.response) };
    },
  });
  const c = q.data?.cost ?? cost;
  const undecided = c.status === "needs_decision";
  return (
    <aside className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`pic-${c.id}`}>
      <h2 className="fy-h3" id={`pic-${c.id}`}>
        {c.number} · {c.description}
      </h2>
      <dl className="fy-dl">
        <div>
          <dt>Date on the receipt</dt>
          <dd>{date(c.date)}</dd>
        </div>
        <div>
          <dt>Paid to</dt>
          <dd>{c.supplier}</dd>
        </div>
        <div>
          <dt>Paid by</dt>
          <dd>{c.paid_by}</dd>
        </div>
        <div>
          <dt>Amount ({c.gross.currency})</dt>
          <dd className="fy-num">{formatAmount(c.gross.amount)}</dd>
        </div>
        {c.gst && (
          <div>
            <dt>GST shown</dt>
            <dd className="fy-num">{formatAmount(c.gst.amount)}</dd>
          </div>
        )}
      </dl>
      {(c.checks ?? []).map((k) => (
        <p key={k.code + k.field} className="fy-row fy-row--wrap gap-2">
          <span className={`fy-chip ${k.severity === "error" ? "fy-chip--critical" : "fy-chip--warning"}`}>
            <Icon of={TriangleAlert} />
            {k.severity === "error" ? "Cannot be decided" : "Needs decision"}
          </span>
          <span className="fy-small fy-wrap">
            {k.message}. {k.fix}.
          </span>
        </p>
      ))}
      {c.decision && (
        <div className="fy-stack" style={{ gap: 4 }}>
          <Decided c={c} />
          {c.decision.note && <p className="fy-small">{c.decision.note}</p>}
          <p className="fy-small fy-muted">Posted on {date(c.decision.post_on)} to {c.paid_by}'s account on 2500, as a cost paid personally.</p>
        </div>
      )}
      {q.data && (
        <EvidencePanel
          record={{ kind: "pre-incorporation-cost", id: c.id, etag: q.data.etag }}
          draft={undecided}
          mayAttach={mayRecord && undecided}
          onChanged={async (s) => {
            await queries.invalidateQueries({ queryKey: ["pre-incorporation-costs"] });
            onSaid(s);
          }}
        />
      )}
      {undecided && (mayRecord || mayDecide) && (
        <div className="fy-row fy-row--wrap justify-end gap-2">
          {mayRecord && (
            <Button variant="ghost" size="sm" type="button" onClick={onChange}>
              {helpText("pre-incorporation.change").label}
            </Button>
          )}
          {mayDecide && (
            <Button variant="primary" size="sm" type="button" onClick={onDecide} disabled={c.receipts === 0} title={helpText("pre-incorporation.decide").tooltip}>
              <Icon of={Gavel} />
              {helpText("pre-incorporation.decide").label}
            </Button>
          )}
        </div>
      )}
    </aside>
  );
}

// PreIncorporationCosts is the page: the costs, filtered by status, with what they come to for each person.
export function PreIncorporationCosts() {
  const { session } = useSignedIn();
  const roles = session.roles ?? [];
  const mayRecord = roles.some((r) => recorders.includes(r));
  const mayDecide = roles.some((r) => deciders.includes(r));
  const [filter, setFilter] = useState<Filter>("all");
  const [selected, setSelected] = useState<string | null>(null);
  const [dialog, setDialog] = useState<"add" | "change" | "decide" | null>(null);
  const [said, setSaid] = useState("");
  const q = useQuery({ queryKey: ["pre-incorporation-costs"], queryFn: costs });
  const all = q.data?.items ?? [];
  const items = filter === "all" ? all : all.filter((c) => c.status === filter);
  const waiting = all.filter((c) => c.status === "needs_decision").length;
  const chosen = all.find((c) => c.id === selected) ?? null;
  const base = q.data?.total.currency ?? "SGD";
  const add = helpText("pre-incorporation.add");
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/accounting">Accounting</Link>
            <span aria-hidden="true">›</span>
            <span>Costs before incorporation</span>
          </nav>
          <h1 className="fy-h1">Costs before incorporation</h1>
          <p className="fy-secondary">
            Costs the founders paid themselves before the books start{q.data?.books_start ? ` on ${date(q.data.books_start)}` : ""}. The accountant decides how each one is
            treated; nothing posts until then.
          </p>
        </div>
        {mayRecord && (
          <Button variant="primary" type="button" onClick={() => setDialog("add")} title={add.tooltip}>
            <Icon of={Plus} />
            {add.label}
          </Button>
        )}
      </div>
      {said && (
        <p className="fy-small" role="status">
          {said}
        </p>
      )}
      {q.isError && <Refusal problem={problemOf(q.error)} title="The costs could not be listed" />}
      {q.isPending && <p className="fy-secondary">Loading the costs…</p>}
      {q.data && !q.data.books_start && (
        <div className="fy-banner fy-banner--warning" role="alert">
          <Icon of={TriangleAlert} size={20} />
          <div className="fy-banner__text">
            <span className="fy-small">
              The books start is not set, so no cost is before it yet. Set it in <Link to="/settings/organisation">Settings › Organisation</Link>.
            </span>
          </div>
        </div>
      )}
      <div className="fy-tabs" role="tablist" aria-label="Costs by status">
        <button type="button" role="tab" aria-selected={filter === "all"} onClick={() => setFilter("all")}>
          All {all.length}
        </button>
        <button type="button" role="tab" aria-selected={filter === "needs_decision"} onClick={() => setFilter("needs_decision")}>
          Needs decision {waiting}
        </button>
        <button type="button" role="tab" aria-selected={filter === "decided"} onClick={() => setFilter("decided")}>
          Decided {all.length - waiting}
        </button>
      </div>
      {q.data && all.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={Receipt} size={24} />
          <h2 className="fy-h3">No costs before the books start</h2>
          <p className="fy-secondary">Add each cost a founder paid before the company's books start, with its receipt, for the accountant to decide.</p>
        </div>
      )}
      {items.length > 0 && (
        <div className="fy-split">
          <div className="fy-tablewrap">
            <table className="fy-table fy-table--cards fy-table--compact" aria-label="Costs before incorporation">
              <thead>
                <tr>
                  <th scope="col">Date</th>
                  <th scope="col">Cost</th>
                  <th scope="col">Paid by</th>
                  <th scope="col" className="fy-num">
                    Amount ({base})
                  </th>
                  <th scope="col">Accountant's decision</th>
                </tr>
              </thead>
              <tbody>
                {items.map((c) => (
                  <tr key={c.id} aria-selected={selected === c.id}>
                    <td data-card="meta">{date(c.date)}</td>
                    <td data-card="title">
                      <button type="button" className="cursor-pointer border-0 bg-transparent p-0 text-start font-[inherit] text-[var(--link)] underline-offset-2 hover:underline" onClick={() => setSelected(c.id)}>
                        {c.description}
                      </button>
                      <span className="fy-small fy-muted block">
                        {[c.number, c.supplier, c.gst ? `GST ${formatAmount(c.gst.amount)}` : ""].filter(Boolean).join(" · ")}
                      </span>
                    </td>
                    <td data-card="meta">{c.paid_by}</td>
                    <td data-card="amount" className="fy-num">
                      {formatAmount(c.gross.amount)}
                    </td>
                    <td data-card="status">
                      <Decided c={c} />
                    </td>
                  </tr>
                ))}
              </tbody>
              <tfoot>
                <tr>
                  <td colSpan={3}>Total paid by the founders</td>
                  <td className="fy-num">{formatAmount(q.data?.total.amount ?? "0")}</td>
                  <td />
                </tr>
              </tfoot>
            </table>
          </div>
          {chosen && (
            <Detail key={chosen.id} cost={chosen} mayRecord={mayRecord} mayDecide={mayDecide} onChange={() => setDialog("change")} onDecide={() => setDialog("decide")} onSaid={setSaid} />
          )}
        </div>
      )}
      {(q.data?.owed ?? []).length > 0 && (
        <section className="fy-card" aria-labelledby="pic-owed">
          <div className="fy-card__head">
            <h2 className="fy-h3" id="pic-owed">
              Owed to the founders
            </h2>
          </div>
          <ul className="fy-list">
            {(q.data?.owed ?? []).map((o) => (
              <li key={o.contact_id} className="fy-list__item">
                <span className="fy-list__main">
                  <span className="fy-list__title">{o.legal_name}</span>
                  <span className="fy-list__meta whitespace-normal">
                    {formatAmount(o.decided.amount)} decided · {formatAmount(o.needs_decision.amount)} after the accountant decides
                  </span>
                </span>
                <span className="fy-num">{formatAmount(o.total.amount)}</span>
              </li>
            ))}
          </ul>
          <p className="fy-card__body fy-small fy-muted">
            Amounts in {base}. Each decided cost is credited to the person's account on 2500 Loan from director / shareholder, as a cost paid personally.
          </p>
        </section>
      )}
      {dialog === "add" && (
        <CostDialog
          cost={null}
          base={base}
          booksStart={q.data?.books_start}
          onClose={() => setDialog(null)}
          onDone={(s, id) => {
            setDialog(null);
            setSaid(s);
            setSelected(id);
          }}
        />
      )}
      {dialog === "change" && chosen && (
        <CostDialog
          cost={chosen}
          base={base}
          booksStart={q.data?.books_start}
          onClose={() => setDialog(null)}
          onDone={(s) => {
            setDialog(null);
            setSaid(s);
          }}
        />
      )}
      {dialog === "decide" && chosen && (
        <DecideDialog
          cost={chosen}
          onClose={() => setDialog(null)}
          onDone={(s) => {
            setDialog(null);
            setSaid(s);
          }}
        />
      )}
    </div>
  );
}
