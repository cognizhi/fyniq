import { useQuery } from "@tanstack/react-query";
import { useId } from "react";
import { helpText } from "../../i18n";
import { listProjects } from "./data";

// useProjectIds are the IDs of the organisation's projects: a project is a value of the Project dimension, so a cost line names its project
// by that ID. Empty while projects are off or unreadable (the field then does not show).
export function useProjectIds(): Set<string> {
  const q = useQuery({ queryKey: ["projects"], queryFn: listProjects, retry: false, staleTime: 60_000 });
  return new Set((q.data ?? []).map((p) => p.id));
}

// BillableFields is "Bill to the customer" on a cost line that carries a project (FR-PRJ-08): billable, and a markup on the cost. It shows
// only when one of the line's dimension values is a project, and says why when it is not.
export function BillableFields(props: {
  ids: string[];
  billable?: boolean;
  markup?: string;
  onChange: (next: { billable: boolean; markup_percent: string }) => void;
  error?: string;
  disabled?: boolean;
  className?: string;
}) {
  const id = useId();
  const projects = useProjectIds();
  if (projects.size === 0 || !props.ids.some((v) => projects.has(v))) return null;
  const h = helpText("project.billable");
  const billable = props.billable ?? false;
  return (
    <div className={props.className ?? "fy-field"}>
      <span className="fy-label" title={h.tooltip}>
        {h.label}
      </span>
      <div className="fy-row gap-3">
        <label className="fy-row gap-2 fy-small" htmlFor={`${id}-b`}>
          <input
            id={`${id}-b`}
            type="checkbox"
            checked={billable}
            disabled={props.disabled}
            onChange={(e) => props.onChange({ billable: e.target.checked, markup_percent: e.target.checked ? (props.markup ?? "") : "" })}
          />
          Bill this cost to the customer
        </label>
        {billable && (
          <label className="fy-row gap-2 fy-small" htmlFor={`${id}-m`}>
            Markup (%)
            <input
              id={`${id}-m`}
              className="fy-input fy-num"
              style={{ width: "6rem" }}
              inputMode="decimal"
              placeholder="0"
              value={props.markup ?? ""}
              disabled={props.disabled}
              aria-invalid={Boolean(props.error) || undefined}
              aria-describedby={props.error ? `${id}-err` : undefined}
              onChange={(e) => props.onChange({ billable, markup_percent: e.target.value })}
            />
          </label>
        )}
      </div>
      {props.error ? (
        <span className="fy-error" id={`${id}-err`}>
          {props.error}
        </span>
      ) : (
        <span className="fy-help">{h.tooltip}</span>
      )}
    </div>
  );
}

// markupError is what is wrong with a typed markup, or null: a percentage from 0 to 999.99 with at most two decimals.
export function markupError(billable: boolean | undefined, markup: string | undefined): string | null {
  const m = (markup ?? "").trim();
  if (!billable) return null;
  if (m && !/^\d{1,3}(\.\d{1,2})?$/.test(m)) return "Enter the markup as a percentage from 0 to 999.99, such as 10 or 12.5, or leave it blank.";
  return null;
}

// ProjectChoice is the project a purchase order is for (FR-PRJ-08): the bill made from the order carries it. It shows only while projects are on
// and there is one that is not closed.
export function ProjectChoice(props: { id: string; value: string; onChange: (v: string) => void; disabled?: boolean }) {
  const q = useQuery({ queryKey: ["projects"], queryFn: listProjects, retry: false, staleTime: 60_000 });
  const open = (q.data ?? []).filter((p) => p.status !== "closed");
  if (open.length === 0) return null;
  return (
    <div className="fy-field">
      <label className="fy-label" htmlFor={props.id}>
        Project (optional)
      </label>
      <select className="fy-select" id={props.id} value={props.value} disabled={props.disabled} onChange={(e) => props.onChange(e.target.value)}>
        <option value="">No project</option>
        {open.map((p) => (
          <option key={p.id} value={p.id}>
            {p.code} {p.name}
          </option>
        ))}
      </select>
      <span className="fy-help">A bill made from this order carries the project, so the cost is the project's.</span>
    </div>
  );
}
