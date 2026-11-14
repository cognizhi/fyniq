import { useId, type ReactNode } from "react";

// A form drawn from a connector manifest's configuration schema (FR-INT-25): one field per property, by its type, with
// its title, description and default. The Integrations page and the AI connections use it alike.

export type ManifestProperty = { type?: string; title?: string; description?: string; enum?: string[]; default?: unknown; pattern?: string };
export type ManifestSchema = { properties?: Record<string, ManifestProperty>; required?: string[] };
export type ManifestValues = Record<string, string | boolean>;

// propertiesOf are a schema's properties, in its order.
export function propertiesOf(schema: unknown): [string, ManifestProperty][] {
  return Object.entries((schema as ManifestSchema | undefined)?.properties ?? {});
}

// initialValues are a configuration's values as the fields edit them: a switch as a boolean, the rest as text.
export function initialValues(schema: unknown, current: Record<string, unknown>): ManifestValues {
  return Object.fromEntries(
    propertiesOf(schema).map(([k, p]) => {
      const v = current[k];
      if (p.type === "boolean") return [k, v === true];
      if (v === undefined || v === null) return [k, ""];
      return [k, typeof v === "object" ? JSON.stringify(v, null, 2) : String(v)];
    }),
  ) as ManifestValues;
}

// collect reads the fields back into a configuration: an empty field is left out, so the manifest's default applies;
// errors name each field whose text is not what its type needs.
export function collect(schema: unknown, values: ManifestValues): { out: Record<string, unknown>; errs: Record<string, string> } {
  const out: Record<string, unknown> = {};
  const errs: Record<string, string> = {};
  for (const [k, p] of propertiesOf(schema)) {
    const v = values[k] ?? "";
    if (typeof v === "boolean") out[k] = v;
    else if (v.trim() === "") continue;
    else if (p.type === "object" || p.type === "array") {
      try {
        out[k] = JSON.parse(v);
      } catch {
        errs[k] = `${p.title ?? k} is not valid JSON. Check the brackets, quotes and commas, then save again.`;
      }
    } else if (p.type === "integer") out[k] = Number.parseInt(v, 10);
    else out[k] = v.trim();
  }
  return { out, errs };
}

// ManifestFields draws the fields of a schema, controlled by values; bad names the fields to mark wrong.
export function ManifestFields({ schema, values, onChange, bad, skip = [] }: {
  schema: unknown;
  values: ManifestValues;
  onChange: (values: ManifestValues) => void;
  bad: Record<string, string>;
  skip?: string[];
}) {
  const id = useId();
  const required = (schema as ManifestSchema | undefined)?.required ?? [];
  return (
    <>
      {propertiesOf(schema)
        .filter(([k]) => !skip.includes(k))
        .map(([k, p]) => {
          const v = values[k] ?? "";
          const fid = `${id}-${k}`;
          const label: ReactNode = (
            <>
              {p.title ?? k}
              {required.includes(k) && (
                <span className="fy-req" aria-hidden="true">
                  {" "}
                  *
                </span>
              )}
            </>
          );
          const help = (
            <span className="fy-help" id={`${fid}-help`}>
              {p.description}
              {p.default !== undefined && typeof p.default !== "object" ? ` Leave it empty for ${String(p.default)}.` : ""}
            </span>
          );
          const err = bad[k] && (
            <span className="fy-error" id={`${fid}-err`}>
              {bad[k]}
            </span>
          );
          const describedBy = bad[k] ? `${fid}-help ${fid}-err` : `${fid}-help`;
          if (typeof v === "boolean")
            return (
              <div key={k} className="fy-row fy-row--top gap-3">
                <button className="fy-switch" type="button" role="switch" id={fid} aria-checked={v} aria-labelledby={`${fid}-l`} aria-describedby={`${fid}-help`} onClick={() => onChange({ ...values, [k]: !v })} />
                <span className="fy-stack gap-0.5">
                  <span className="fy-strong" id={`${fid}-l`}>
                    {label}
                  </span>
                  {help}
                </span>
              </div>
            );
          return (
            <div key={k} className="fy-field">
              <label className="fy-label" htmlFor={fid}>
                {label}
              </label>
              {p.enum ? (
                <select className="fy-select" id={fid} value={v} aria-describedby={describedBy} onChange={(e) => onChange({ ...values, [k]: e.target.value })}>
                  <option value="">Choose…</option>
                  {p.enum.map((o) => (
                    <option key={o} value={o}>
                      {o}
                    </option>
                  ))}
                </select>
              ) : p.type === "object" || p.type === "array" ? (
                <textarea className="fy-textarea fy-mono" id={fid} value={v} aria-invalid={Boolean(bad[k])} aria-describedby={describedBy} onChange={(e) => onChange({ ...values, [k]: e.target.value })} />
              ) : (
                <input
                  className={p.type === "string" ? "fy-input fy-mono" : "fy-input"}
                  id={fid}
                  type={p.type === "integer" || p.type === "number" ? "number" : "text"}
                  value={v}
                  placeholder={typeof p.default === "string" ? p.default : undefined}
                  required={required.includes(k)}
                  aria-describedby={describedBy}
                  onChange={(e) => onChange({ ...values, [k]: e.target.value })}
                />
              )}
              {help}
              {err}
            </div>
          );
        })}
    </>
  );
}
