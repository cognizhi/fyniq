import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ListPlus, Pencil, Plus } from "lucide-react";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { typed } from "../../format/decimal";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { money, useCategories, type Category } from "./claims";

// Who changes the expense policy's categories: A on "Expense claims" (Appendix D): the Approver and the Controller. The
// API decides; the page offers the buttons to them only.
export const policySetters = ["approver", "controller"];

// CategoryForm is a category as the person types it: amounts as typed, read when it is saved.
export interface CategoryForm {
  name: string;
  account_id: string;
  mileage: boolean;
  limit: string;
  receipt_above: string;
  attendees_above: string;
  active: boolean;
}

// checkCategory reads the form into the API's draft, or names each field that cannot be read, all at once.
export function checkCategory(f: CategoryForm, base: string) {
  const errors: Record<string, string> = {};
  if (!f.name.trim()) errors.name = "Name what its claims are for, such as Meals.";
  if (!f.account_id) errors.account_id = "Choose the account its claims cost.";
  const amount = (field: keyof CategoryForm, v: string) => {
    const t = typed(v);
    if (t === null || (t !== "" && t.startsWith("-"))) errors[field] = "Enter an amount, such as 50.00, or leave it empty.";
    return t ? { amount: t, currency: base } : undefined;
  };
  const draft = {
    name: f.name.trim(),
    account_id: f.account_id,
    mileage: f.mileage || undefined,
    limit: amount("limit", f.limit),
    receipt_above: amount("receipt_above", f.receipt_above),
    attendees_above: amount("attendees_above", f.attendees_above),
    active: f.active,
  };
  return Object.keys(errors).length > 0 ? { errors } : { errors, draft };
}

// ExpensePolicy is Settings › Expense policy (docs/design/screens ExpensePolicy; FR-AP-15, FR-AP-16): the categories a
// claim line is for, the account each costs, its limit per line and when it needs a receipt or names who attended, and
// the mileage category. The claim deadline, the rates per km and the advances account are organisation settings under
// dual control.
export function ExpensePolicy() {
  const { session, organisation } = useSignedIn();
  const base = organisation?.base_currency ?? "SGD";
  const queries = useQueryClient();
  const confirm = useConfirm();
  const categories = useCategories();
  const [editing, setEditing] = useState<Category | "new" | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [said, setSaid] = useState("");
  const may = (session.roles ?? []).some((r) => policySetters.includes(r));
  const items = categories.data ?? [];
  const starter = helpText("expense-policy.load-starter");

  const loadStarter = async () => {
    setProblem(null);
    const key = newKey();
    const out = await guarded(confirm, () => api.POST("/v1/claim-categories:load-starter", { params: { header: { "Idempotency-Key": key } } }));
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["claim-categories"] });
    setSaid("The starter categories are loaded. Check each one's account and limits.");
  };

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/settings">Settings</Link>
          </nav>
          <h1 className="fy-h1">Expense policy</h1>
          <p className="fy-secondary">What members claim for, what each costs, and the limits and receipts the policy asks for.</p>
        </div>
        {may && items.length > 0 && (
          <div className="fy-pagehead__actions">
            <Button onClick={() => setEditing("new")} title={helpText("expense-policy.add").tooltip}>
              <Icon of={Plus} />
              {helpText("expense-policy.add").label}
            </Button>
          </div>
        )}
      </div>
      {problem && <Refusal problem={problem} title="The expense policy was not changed" />}
      {said && (
        <p className="fy-small" role="status">
          {said}
        </p>
      )}
      {categories.isError && <Refusal problem={problemOf(categories.error)} title="The categories could not be listed" />}
      {categories.data && items.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={ListPlus} size={24} />
          <h2 className="fy-h3">No categories yet</h2>
          <p className="fy-secondary">Members claim against the policy's categories. Start from Fyniq's: travel, meals, mileage and the rest, each on the standard chart's account.</p>
          {may ? (
            <Button className="self-start" onClick={() => void loadStarter()} title={starter.tooltip}>
              <Icon of={ListPlus} />
              {starter.label}
            </Button>
          ) : (
            <p className="fy-small fy-secondary">An Approver or a Controller sets the expense policy.</p>
          )}
        </div>
      )}
      {items.length > 0 && (
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--cards" aria-label="Categories">
            <thead>
              <tr>
                <th scope="col">Category</th>
                <th scope="col">Account</th>
                <th scope="col" className="fy-num">
                  Limit a line
                </th>
                <th scope="col" className="fy-num">
                  Receipt above
                </th>
                <th scope="col" className="fy-num">
                  Attendees above
                </th>
                <th scope="col">
                  <span className="sr-only">Action</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {items.map((c) => (
                <tr key={c.id}>
                  <td data-card="title">
                    {c.name}
                    <span className="fy-small fy-muted block">{[c.mileage ? "Mileage" : "", c.active ? "" : "Out of use"].filter(Boolean).join(" · ")}</span>
                  </td>
                  <td data-card="meta" className="fy-mono">
                    {c.account_code}
                  </td>
                  <td className="fy-num">{c.limit ? money(c.limit) : "None"}</td>
                  <td className="fy-num">{c.receipt_above ? money(c.receipt_above) : "Never"}</td>
                  <td className="fy-num">{c.attendees_above ? money(c.attendees_above) : "Never"}</td>
                  <td>
                    {may && (
                      <Button variant="ghost" size="sm" onClick={() => setEditing(c)} aria-label={`Change ${c.name}`}>
                        <Icon of={Pencil} />
                        Change
                      </Button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-label="The policy's settings">
        <h2 className="fy-h3">The claim deadline, rates per km and the advances account</h2>
        <p className="fy-small fy-secondary">They are organisation settings, changed with a second person's approval: the days a receipt can be claimed in, the rate per km for a car and for a motorcycle, and the account cash advances post to.</p>
        <Link className="fy-btn fy-btn--secondary fy-btn--sm self-start" to="/settings/organisation">
          Open Settings › Organisation
        </Link>
      </section>
      {editing && (
        <CategoryDialog
          category={editing === "new" ? null : editing}
          base={base}
          onClose={() => setEditing(null)}
          onDone={(message) => {
            setEditing(null);
            setSaid(message);
          }}
        />
      )}
    </div>
  );
}

function CategoryDialog({ category, base, onClose, onDone }: { category: Category | null; base: string; onClose: () => void; onDone: (message: string) => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [f, setF] = useState<CategoryForm>({
    name: category?.name ?? "",
    account_id: category?.account_id ?? "",
    mileage: category?.mileage ?? false,
    limit: category?.limit?.amount ?? "",
    receipt_above: category?.receipt_above?.amount ?? "",
    attendees_above: category?.attendees_above?.amount ?? "",
    active: category?.active ?? true,
  });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);
  const accounts = useQuery({
    queryKey: ["accounts", "claimable"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/accounts", { params: { query: { page_size: 500 } } });
      if (error) throw error;
      return (data.items ?? []).filter((a) => a.active && !a.group && !a.money_account && !a.control && (a.type === "expense" || a.type === "asset"));
    },
  });

  const save = async () => {
    const { draft, errors } = checkCategory(f, base);
    setErrors(errors);
    if (!draft) return;
    setBusy(true);
    setProblem(null);
    const out = category
      ? await guarded(confirm, () => api.PUT("/v1/claim-categories/{category_id}", { params: { path: { category_id: category.id }, header: { "If-Match": `"${category.version}"` } }, body: draft }))
      : await guarded(confirm, () => api.POST("/v1/claim-categories", { params: { header: { "Idempotency-Key": key } }, body: draft }));
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["claim-categories"] });
    onDone(`${out.data.name} is saved.`);
  };

  const field = (name: keyof CategoryForm, label: string, help?: string) => (
    <div className="fy-field">
      <label className="fy-label" htmlFor={`${id}-${name}`}>
        {label}
      </label>
      <input
        className="fy-input fy-num"
        id={`${id}-${name}`}
        inputMode="decimal"
        value={f[name] as string}
        onChange={(e) => setF({ ...f, [name]: e.target.value })}
        aria-invalid={errors[name] ? true : undefined}
        aria-describedby={[help && `${id}-${name}-help`, errors[name] && `${id}-${name}-error`].filter(Boolean).join(" ") || undefined}
      />
      {help && (
        <span className="fy-help" id={`${id}-${name}-help`}>
          {help}
        </span>
      )}
      {errors[name] && (
        <span className="fy-error" id={`${id}-${name}-error`}>
          {errors[name]}
        </span>
      )}
    </div>
  );

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={category ? `Change ${category.name}` : "Add a category"}
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
        {problem && <Refusal problem={problem} title="The category was not saved" />}
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-name`}>
            Name
          </label>
          <input className="fy-input" id={`${id}-name`} value={f.name} maxLength={80} onChange={(e) => setF({ ...f, name: e.target.value })} aria-invalid={errors.name ? true : undefined} aria-describedby={errors.name ? `${id}-name-error` : undefined} />
          {errors.name && (
            <span className="fy-error" id={`${id}-name-error`}>
              {errors.name}
            </span>
          )}
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-account`}>
            Account it costs
          </label>
          <select className="fy-select" id={`${id}-account`} value={f.account_id} onChange={(e) => setF({ ...f, account_id: e.target.value })} aria-invalid={errors.account_id ? true : undefined} aria-describedby={errors.account_id ? `${id}-account-error` : undefined}>
            <option value="">Choose the account</option>
            {(accounts.data ?? []).map((a) => (
              <option key={a.id} value={a.id}>
                {a.code} {a.name}
              </option>
            ))}
          </select>
          {errors.account_id && (
            <span className="fy-error" id={`${id}-account-error`}>
              {errors.account_id}
            </span>
          )}
        </div>
        {field("limit", `Limit a line (${base}, optional)`, "Above it, a line shows ⚠ and finance approves the claim too.")}
        {field("receipt_above", `Receipt needed above (${base}, optional)`)}
        {field("attendees_above", `Who attended asked above (${base}, optional)`)}
        {!category && (
          <label className="fy-row gap-2">
            <input type="checkbox" checked={f.mileage} onChange={(e) => setF({ ...f, mileage: e.target.checked })} />
            The mileage category: trips in a member's own vehicle (it never changes once made)
          </label>
        )}
        {category && (
          <label className="fy-row gap-2">
            <input type="checkbox" checked={f.active} onChange={(e) => setF({ ...f, active: e.target.checked })} />
            In use
          </label>
        )}
      </div>
    </Dialog>
  );
}
