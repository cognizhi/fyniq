import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { problemOf, type ApiProblem } from "../../ui";
import { billingNames, hours, listCustomers, listProjectPeople, minutesOf, statusNames, type Project, type ProjectDraft } from "./data";

type Method = NonNullable<Project["billing_method"]>;

// ProjectForm adds a project, changes one, or sets up a value of the Project dimension that has no project record yet
// (FR-PRJ-01). Every check is the API's; the form only keeps what is obviously wrong from being sent.
export function ProjectForm({ editing, onClose, onSaved }: { editing: Project | "new" | null; onClose: () => void; onSaved: (what: string, id: string) => void }) {
  const { organisation } = useSignedIn();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const id = useId();
  const isNew = editing === "new";
  const current = editing && editing !== "new" ? editing : null;
  const setUp = current !== null && !current.configured;
  const customers = useQuery({ queryKey: ["project-customers"], queryFn: listCustomers, enabled: editing !== null });
  const people = useQuery({ queryKey: ["project-people"], queryFn: listProjectPeople, enabled: editing !== null });
  const h = helpText("project.people");

  const [code, setCode] = useState("");
  const [name, setName] = useState<string | null>(null);
  const [customer, setCustomer] = useState<string | null>(null);
  const [currency, setCurrency] = useState<string | null>(null);
  const [status, setStatus] = useState<string | null>(null);
  const [method, setMethod] = useState<Method | null>(null);
  const [startsOn, setStartsOn] = useState<string | null>(null);
  const [endsOn, setEndsOn] = useState<string | null>(null);
  const [budgetHours, setBudgetHours] = useState<string | null>(null);
  const [budgetAmount, setBudgetAmount] = useState<string | null>(null);
  const [chosen, setChosen] = useState<Record<string, boolean | "manager"> | null>(null);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);

  const n = name ?? current?.name ?? "";
  const cust = customer ?? current?.customer_id ?? "";
  const cur = currency ?? current?.currency ?? organisation?.base_currency ?? "SGD";
  const st = status ?? (current?.configured ? current.status : "active");
  const bm: Method = method ?? (current?.configured ? (current.billing_method ?? "internal") : "time_and_materials");
  const starts = startsOn ?? current?.starts_on ?? "";
  const ends = endsOn ?? current?.ends_on ?? "";
  const bh = budgetHours ?? (current?.budget_minutes != null ? String(current.budget_minutes / 60) : "");
  const ba = budgetAmount ?? current?.budget_amount ?? "";
  const picked: Record<string, boolean | "manager"> = chosen ?? Object.fromEntries((current?.people ?? []).map((p) => [p.user_id, p.manager ? ("manager" as const) : true]));

  const close = () => {
    setCode("");
    setName(null);
    setCustomer(null);
    setCurrency(null);
    setStatus(null);
    setMethod(null);
    setStartsOn(null);
    setEndsOn(null);
    setBudgetHours(null);
    setBudgetAmount(null);
    setChosen(null);
    setErrors({});
    setProblem(null);
    onClose();
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const errs: Record<string, string> = {};
    if (isNew && !/^[0-9A-Za-z][0-9A-Za-z._/-]{0,29}$/.test(code.trim())) errs.code = "Give a code of a letter or digit, then up to 29 letters, digits, dots, slashes, underscores or hyphens, such as ACME-01.";
    if (!n.trim()) errs.name = "Give the project a name, such as Acme rollout.";
    if (!/^[A-Za-z]{3}$/.test(cur.trim())) errs.currency = "Give the currency as three letters, such as SGD.";
    let budgetMinutes: number | undefined;
    if (bh.trim() !== "") {
      const m = minutesOf(bh);
      if (m === null) errs.budgetHours = "Write the hours as 400, 7.5 or 7:30.";
      else budgetMinutes = m;
    }
    if (ba.trim() !== "" && !/^\d+(\.\d+)?$/.test(ba.trim())) errs.budgetAmount = "Write the amount as a plain number, such as 60000.00.";
    setErrors(errs);
    if (Object.keys(errs).length) return;
    const crew = Object.entries(picked)
      .filter(([, v]) => v)
      .map(([user_id, v]) => ({ user_id, manager: v === "manager" }));
    const common = {
      customer_id: bm === "internal" || cust === "" ? undefined : cust,
      billing_method: bm,
      starts_on: starts || undefined,
      ends_on: ends || undefined,
      budget_minutes: budgetMinutes,
      budget_amount: ba.trim() || undefined,
      people: crew,
    };
    setBusy(true);
    setProblem(null);
    const out = current
      ? await guarded(confirm, () =>
          api.PUT("/v1/projects/{project_id}", {
            params: { path: { project_id: current.id }, header: { "If-Match": `"${current.version}"` } },
            body: { ...common, name: n.trim(), status: st as NonNullable<ProjectDraft["status"]>, currency: setUp ? cur.trim().toUpperCase() : undefined },
          }),
        )
      : await (() => {
          const key = newKey();
          return guarded(confirm, () =>
            api.POST("/v1/projects", {
              params: { header: { "Idempotency-Key": key } },
              body: { ...common, code: code.trim(), name: n.trim(), currency: cur.trim().toUpperCase(), status: st as NonNullable<ProjectDraft["status"]> },
            }),
          );
        })();
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ predicate: (q) => ["projects", "project"].includes(String(q.queryKey[0])) });
    onSaved(current ? `${out.data.name} saved.` : `${out.data.name} added.`, out.data.id);
    close();
  };

  const field = (key: string) => (errors[key] ? { "aria-invalid": true as const, "aria-describedby": `${id}-${key}-error` } : {});
  const error = (key: string) =>
    errors[key] ? (
      <span className="fy-error" id={`${id}-${key}-error`}>
        {errors[key]}
      </span>
    ) : null;

  return (
    <Dialog
      open={editing !== null}
      onOpenChange={(o) => !o && close()}
      dismissible={!busy}
      eyebrow="Projects"
      title={isNew ? "Add a project" : setUp ? `Set up ${current?.name ?? "the project"}` : (current?.name ?? "Project")}
      description={isNew ? "A project is a value of your Project dimension: lines you tag with it count towards it." : "Its code and its currency never change."}
    >
      {problem && <Refusal problem={problem} title={isNew ? "The project was not added" : "The project was not saved"} />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <div className="fy-form fy-form--2">
          {isNew && (
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-code`}>
                Code
              </label>
              <input className="fy-input fy-mono" id={`${id}-code`} value={code} maxLength={30} onChange={(e) => setCode(e.target.value)} {...field("code")} />
              {error("code")}
            </div>
          )}
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-name`}>
              Name
            </label>
            <input className="fy-input" id={`${id}-name`} value={n} maxLength={200} onChange={(e) => setName(e.target.value)} {...field("name")} />
            {error("name")}
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-method`}>
              How it is billed
            </label>
            <select className="fy-select" id={`${id}-method`} value={bm} onChange={(e) => setMethod(e.target.value as Method)}>
              {(Object.keys(billingNames) as Method[]).map((m) => (
                <option key={m} value={m}>
                  {billingNames[m]}
                </option>
              ))}
            </select>
          </div>
          {bm !== "internal" && (
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-customer`}>
                Customer
              </label>
              <select className="fy-select" id={`${id}-customer`} value={cust} onChange={(e) => setCustomer(e.target.value)}>
                <option value="">No customer yet</option>
                {(customers.data ?? []).map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.legal_name}
                  </option>
                ))}
              </select>
            </div>
          )}
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-status`}>
              Status
            </label>
            <select className="fy-select" id={`${id}-status`} value={st} onChange={(e) => setStatus(e.target.value)}>
              {Object.entries(statusNames).map(([k, v]) => (
                <option key={k} value={k}>
                  {v}
                </option>
              ))}
            </select>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-currency`}>
              Currency
            </label>
            <input
              className="fy-input fy-mono"
              id={`${id}-currency`}
              value={cur}
              maxLength={3}
              disabled={!isNew && !setUp}
              onChange={(e) => setCurrency(e.target.value.toUpperCase())}
              {...field("currency")}
            />
            {error("currency")}
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-starts`}>
              Starts
            </label>
            <input className="fy-input" id={`${id}-starts`} type="date" value={starts} onChange={(e) => setStartsOn(e.target.value)} />
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-ends`}>
              Ends
            </label>
            <input className="fy-input" id={`${id}-ends`} type="date" value={ends} onChange={(e) => setEndsOn(e.target.value)} />
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-bh`}>
              Hours budget
            </label>
            <input className="fy-input fy-num" id={`${id}-bh`} inputMode="decimal" value={bh} placeholder="400" onChange={(e) => setBudgetHours(e.target.value)} {...field("budgetHours")} />
            {error("budgetHours")}
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-ba`}>
              Money budget ({cur})
            </label>
            <input className="fy-input fy-num" id={`${id}-ba`} inputMode="decimal" value={ba} placeholder="60000.00" onChange={(e) => setBudgetAmount(e.target.value)} {...field("budgetAmount")} />
            {error("budgetAmount")}
          </div>
        </div>
        <fieldset className="fy-field">
          <legend className="fy-label">{h.label}</legend>
          <p className="fy-small fy-muted">{h.tooltip}</p>
          {people.isError && <Refusal problem={problemOf(people.error)} title="The people could not be listed" />}
          <ul className="fy-list">
            {(people.data ?? []).map((p) => {
              const v = picked[p.user_id];
              return (
                <li key={p.user_id} className="fy-list__item">
                  <label className="fy-row gap-2 fy-list__main">
                    <input type="checkbox" checked={Boolean(v)} onChange={(e) => setChosen({ ...picked, [p.user_id]: e.target.checked })} />
                    {p.display_name}
                  </label>
                  <label className="fy-row gap-2 fy-small">
                    <input type="checkbox" checked={v === "manager"} disabled={!v} onChange={(e) => setChosen({ ...picked, [p.user_id]: e.target.checked ? "manager" : true })} />
                    Manager
                  </label>
                  {v === "manager" && !p.can_approve && <span className="fy-small fy-warning">Holds no role that approves time</span>}
                </li>
              );
            })}
          </ul>
        </fieldset>
        {current?.configured && current.budget_minutes != null && <p className="fy-small fy-muted">Budget now: {hours(current.budget_minutes)}.</p>}
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={close} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy}>
            {isNew ? "Add the project" : setUp ? "Set up the project" : "Save the project"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
