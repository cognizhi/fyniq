import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Layers, Pencil, Plus, Tags } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { listDimensions, type Account, type Dimension, type DimensionValue } from "./data";

type Kind = Dimension["kind"];

// An organisation has Department and Project once each and up to 8 custom dimensions (FR-DIM-01).
const maxCustom = 8;
const kindName: Record<Kind, string> = { department: "Department", project: "Project", custom: "Custom" };
const codeFormat = /^[0-9A-Za-z][0-9A-Za-z._/-]{0,29}$/;

// kindsLeft are the kinds the organisation can still add.
export function kindsLeft(dimensions: readonly Dimension[]): Kind[] {
  const has = (k: Kind) => dimensions.some((d) => d.kind === k);
  const out: Kind[] = [];
  if (!has("department")) out.push("department");
  if (!has("project")) out.push("project");
  if (dimensions.filter((d) => d.kind === "custom").length < maxCustom) out.push("custom");
  return out;
}

// DimensionForm adds a dimension or renames one and puts it out of use or back.
function DimensionForm({ editing, dimensions, onClose, onSaved }: { editing: Dimension | "new" | null; dimensions: Dimension[]; onClose: () => void; onSaved: (what: string) => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const id = useId();
  const isNew = editing === "new";
  const current = editing && editing !== "new" ? editing : null;
  const left = kindsLeft(dimensions);
  const [kind, setKind] = useState<Kind | null>(null);
  const [name, setName] = useState<string | null>(null);
  const [active, setActive] = useState<boolean | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const k = kind ?? current?.kind ?? left[0] ?? "custom";
  const n = name ?? current?.name ?? (k === "custom" ? "" : kindName[k]);
  const on = active ?? current?.active ?? true;

  const close = () => {
    setKind(null);
    setName(null);
    setActive(null);
    setError("");
    setProblem(null);
    onClose();
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!n.trim()) return setError("Give the dimension a name, such as Region.");
    setError("");
    setBusy(true);
    setProblem(null);
    const out = current
      ? await guarded(confirm, () =>
          api.PUT("/v1/dimensions/{dimension_id}", { params: { path: { dimension_id: current.id }, header: { "If-Match": `"${current.version}"` } }, body: { name: n.trim(), active: on } }),
        )
      : await (() => {
          const key = newKey();
          return guarded(confirm, () => api.POST("/v1/dimensions", { params: { header: { "Idempotency-Key": key } }, body: { kind: k, name: n.trim() } }));
        })();
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["dimensions"] });
    onSaved(current ? `${out.data.name} saved.` : `${out.data.name} added.`);
    close();
  };

  return (
    <Dialog
      open={editing !== null}
      onOpenChange={(o) => !o && close()}
      dismissible={!busy}
      eyebrow="Dimensions"
      title={isNew ? "Add a dimension" : (current?.name ?? "Dimension")}
      description={isNew ? "Department and Project once each, and up to 8 of your own. A dimension is never deleted: put it out of use instead." : "Its kind never changes. Out of use, it keeps its values and the lines that carry them."}
    >
      {problem && <Refusal problem={problem} title={isNew ? "The dimension was not added" : "The dimension was not saved"} />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <div className="fy-form fy-form--2">
          {isNew && (
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-kind`}>
                Kind
              </label>
              <select
                className="fy-select"
                id={`${id}-kind`}
                value={k}
                onChange={(e) => {
                  setKind(e.target.value as Kind);
                  setName(null);
                }}
              >
                {left.map((x) => (
                  <option key={x} value={x}>
                    {kindName[x]}
                  </option>
                ))}
              </select>
            </div>
          )}
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-name`}>
              Name
            </label>
            <input
              className="fy-input"
              id={`${id}-name`}
              value={n}
              maxLength={100}
              onChange={(e) => setName(e.target.value)}
              aria-invalid={error ? true : undefined}
              aria-describedby={error ? `${id}-name-error` : undefined}
            />
            {error && (
              <span className="fy-error" id={`${id}-name-error`}>
                {error}
              </span>
            )}
          </div>
        </div>
        {current && (
          <label className="fy-row gap-2">
            <input type="checkbox" checked={on} onChange={(e) => setActive(e.target.checked)} />
            In use: takes new values
          </label>
        )}
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={close} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy}>
            {isNew ? "Add the dimension" : "Save the dimension"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// ValuesDialog lists a dimension's values and adds, renames and puts them out of use.
function ValuesDialog({ dimension, values, may, onClose, onSaved }: { dimension: Dimension | null; values: DimensionValue[]; may: boolean; onClose: () => void; onSaved: (what: string) => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const id = useId();
  const [editing, setEditing] = useState<DimensionValue | null>(null);
  const [code, setCode] = useState("");
  const [name, setName] = useState("");
  const [active, setActive] = useState(true);
  const [errors, setErrors] = useState<{ code?: string; name?: string }>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const mine = values.filter((v) => v.dimension_id === dimension?.id);
  const open = may && (editing !== null || dimension?.active === true);

  const reset = () => {
    setEditing(null);
    setCode("");
    setName("");
    setActive(true);
    setErrors({});
    setProblem(null);
  };
  const close = () => {
    reset();
    onClose();
  };
  const change = (v: DimensionValue) => {
    reset();
    setEditing(v);
    setName(v.name);
    setActive(v.active);
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!dimension) return;
    const errs: typeof errors = {};
    if (!editing && !codeFormat.test(code.trim())) errs.code = "Give a code of a letter or digit, then up to 29 letters, digits, dots, slashes, underscores or hyphens, such as ENG.";
    if (!name.trim()) errs.name = "Give the value a name, such as Engineering.";
    setErrors(errs);
    if (Object.keys(errs).length) return;
    setBusy(true);
    setProblem(null);
    const out = editing
      ? await guarded(confirm, () =>
          api.PUT("/v1/dimension-values/{value_id}", { params: { path: { value_id: editing.id }, header: { "If-Match": `"${editing.version}"` } }, body: { name: name.trim(), active } }),
        )
      : await (() => {
          const key = newKey();
          return guarded(confirm, () =>
            api.POST("/v1/dimension-values", { params: { header: { "Idempotency-Key": key } }, body: { dimension_id: dimension.id, code: code.trim(), name: name.trim() } }),
          );
        })();
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["dimensions"] });
    onSaved(`${out.data.code} ${out.data.name} ${editing ? "saved" : "added"}.`);
    reset();
  };

  const tid = `${id}-values`;
  return (
    <Dialog
      open={dimension !== null}
      onOpenChange={(o) => !o && close()}
      dismissible={!busy}
      wide
      eyebrow="Dimensions"
      title={`Values of ${dimension?.name ?? ""}`}
      description="A value's code never changes. Out of use, a value stays on the lines that carry it."
    >
      {problem && <Refusal problem={problem} title={editing ? "The value was not saved" : "The value was not added"} />}
      {mine.length === 0 ? (
        <p className="fy-secondary">No values yet.</p>
      ) : (
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--compact" aria-labelledby={tid}>
            <caption className="fy-sr" id={tid}>
              Values of {dimension?.name}
            </caption>
            <thead>
              <tr>
                <th scope="col">Code</th>
                <th scope="col">Name</th>
                <th scope="col">
                  <span className="fy-sr">Status and actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {mine.map((v) => (
                <tr key={v.id}>
                  <td className="fy-mono">{v.code}</td>
                  <td>{v.name}</td>
                  <td>
                    <span className="fy-row fy-row--wrap justify-end gap-2">
                      {!v.active && <span className="fy-chip fy-chip--neutral">Out of use</span>}
                      {may && (
                        <Button variant="ghost" size="sm" onClick={() => change(v)} aria-label={`Change ${v.code} ${v.name}`}>
                          <Icon of={Pencil} />
                        </Button>
                      )}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {may && dimension && !dimension.active && !editing && <p className="fy-small fy-secondary">{dimension.name} is out of use: put it back in use to add values.</p>}
      {open && (
        <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate aria-label={editing ? `Change ${editing.code}` : "Add a value"}>
          <h3 className="fy-h4">{editing ? `Change ${editing.code}` : "Add a value"}</h3>
          <div className="fy-form fy-form--2">
            {!editing && (
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-code`}>
                  Code
                </label>
                <input
                  className="fy-input"
                  id={`${id}-code`}
                  value={code}
                  maxLength={30}
                  onChange={(e) => setCode(e.target.value)}
                  aria-invalid={errors.code ? true : undefined}
                  aria-describedby={`${id}-code-help${errors.code ? ` ${id}-code-error` : ""}`}
                />
                <span className="fy-help" id={`${id}-code-help`}>
                  Short and unique in {dimension?.name}, such as ENG; it never changes.
                </span>
                {errors.code && (
                  <span className="fy-error" id={`${id}-code-error`}>
                    {errors.code}
                  </span>
                )}
              </div>
            )}
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-name`}>
                Name
              </label>
              <input
                className="fy-input"
                id={`${id}-name`}
                value={name}
                maxLength={200}
                onChange={(e) => setName(e.target.value)}
                aria-invalid={errors.name ? true : undefined}
                aria-describedby={errors.name ? `${id}-name-error` : undefined}
              />
              {errors.name && (
                <span className="fy-error" id={`${id}-name-error`}>
                  {errors.name}
                </span>
              )}
            </div>
          </div>
          {editing && (
            <label className="fy-row gap-2">
              <input type="checkbox" checked={active} onChange={(e) => setActive(e.target.checked)} />
              In use: lines can take it
            </label>
          )}
          <div className="fy-row justify-end gap-2">
            {editing && (
              <Button variant="ghost" type="button" onClick={reset} disabled={busy}>
                Cancel the change
              </Button>
            )}
            <Button variant="primary" type="submit" disabled={busy}>
              {editing ? "Save the value" : "Add the value"}
            </Button>
          </div>
        </form>
      )}
    </Dialog>
  );
}

// Dimensions is the chart's Dimensions section (docs/design/screens ChartOfAccounts; FR-DIM-01): Department, Project
// and the organisation's own dimensions, each with its values. Those who keep the chart add, rename and put them out of
// use; a dimension and a value are never deleted.
// requiredOn says which accounts require a dimension (FR-DIM-01), by code: "4010 Sales, 6110 Rent".
export function requiredOn(d: Dimension, accounts: readonly Account[]): string {
  const on = accounts.filter((a) => a.required_dimension_ids?.includes(d.id)).sort((x, y) => x.code.localeCompare(y.code, "en", { numeric: true }));
  if (on.length === 0) return "Optional everywhere";
  return on.map((a) => `${a.code} ${a.name}`).join(", ");
}

export function Dimensions({ may, accounts, onSaved }: { may: boolean; accounts: readonly Account[]; onSaved: (what: string) => void }) {
  const q = useQuery({ queryKey: ["dimensions"], queryFn: listDimensions });
  const [editing, setEditing] = useState<Dimension | "new" | null>(null);
  const [showing, setShowing] = useState<string | null>(null);
  const add = helpText("chart.dimension-add");
  const valuesHelp = helpText("chart.dimension-values");
  const dims = q.data?.dimensions ?? [];
  const values = q.data?.values ?? [];
  const tid = useId();
  const showingDim = dims.find((d) => d.id === showing) ?? null;

  return (
    <section className="fy-card" aria-labelledby={tid}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={tid}>
          Dimensions
        </h2>
        {q.data && <span className="fy-small fy-muted">{dims.length} of 10 used · Department and Project are built in</span>}
        {may && q.data && kindsLeft(dims).length > 0 && (
          <Button size="sm" onClick={() => setEditing("new")} title={add.tooltip}>
            <Icon of={Plus} />
            {add.label}
          </Button>
        )}
      </div>
      {q.isError && <Refusal problem={problemOf(q.error)} title="The dimensions could not be shown" />}
      {q.isPending && <p className="fy-secondary fy-card--pad">Loading the dimensions…</p>}
      {q.data && dims.length === 0 && (
        <div className="fy-empty fy-card--pad">
          <Icon of={Layers} size={24} />
          <p className="fy-secondary">No dimensions yet. Add Department or Project to see costs and revenue by team or by project, and up to 8 of your own, such as Region.</p>
        </div>
      )}
      {dims.length > 0 && (
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--compact" aria-labelledby={tid}>
            <thead>
              <tr>
                <th scope="col">Dimension</th>
                <th scope="col">Values</th>
                <th scope="col">Required on</th>
                <th scope="col">
                  <span className="fy-sr">Status and actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {dims.map((d) => {
                const mine = values.filter((v) => v.dimension_id === d.id && v.active);
                return (
                  <tr key={d.id}>
                    <td>
                      <span className="fy-row fy-row--wrap gap-2">
                        {d.name}
                        {d.kind === "custom" && <span className="fy-tag">Custom</span>}
                      </span>
                    </td>
                    <td className="fy-secondary">{mine.length ? mine.map((v) => v.name).join(" · ") : "None yet"}</td>
                    <td className="fy-secondary">{d.active ? requiredOn(d, accounts) : "Nowhere while out of use"}</td>
                    <td>
                      <span className="fy-row fy-row--wrap justify-end gap-2">
                        {!d.active && <span className="fy-chip fy-chip--neutral">Out of use</span>}
                        <Button variant="ghost" size="sm" onClick={() => setShowing(d.id)} title={valuesHelp.tooltip} aria-label={`Values of ${d.name}`}>
                          <Icon of={Tags} />
                          {valuesHelp.label}
                        </Button>
                        {may && (
                          <Button variant="ghost" size="sm" onClick={() => setEditing(d)} aria-label={`Change ${d.name}`}>
                            <Icon of={Pencil} />
                          </Button>
                        )}
                      </span>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      <DimensionForm editing={editing} dimensions={dims} onClose={() => setEditing(null)} onSaved={onSaved} />
      <ValuesDialog dimension={showingDim} values={values} may={may} onClose={() => setShowing(null)} onSaved={onSaved} />
    </section>
  );
}
