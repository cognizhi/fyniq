import { ArrowRight, ChevronDown, ChevronRight, Layers } from "lucide-react";
import { useId, useState } from "react";
import { setMode } from "../mode";
import type { Mode } from "../registry";
import { Icon } from "../ui";
import { Button } from "./ui/button";

export interface AdvancedDetail {
  label: string; // where it is and what it is: "Line 1 · Department"
  value: string;
}

// AdvancedDetails is "Has advanced details (N)" (FR-UX-25, §7.8; docs/design/screens InvoiceEditor, InvoiceRecord): in
// Basic mode, the values a record has in fields only Expert mode shows, read-only, with "Open in Expert mode" to change
// them, so nothing a record holds is hidden. Nothing in Expert mode, where the fields show, or when the record has none.
export function AdvancedDetails({ mode, details }: { mode: Mode; details: readonly AdvancedDetail[] }) {
  const id = useId();
  const [open, setOpen] = useState(false);
  if (mode === "expert" || details.length === 0) return null;
  return (
    <div className="fy-plain" role="group" aria-labelledby={`${id}-title`}>
      <Icon of={Layers} />
      <div className="fy-stack fy-stack--2 fy-grow">
        <Button variant="ghost" size="sm" type="button" className="self-start ps-0" id={`${id}-title`} aria-expanded={open} aria-controls={`${id}-list`} onClick={() => setOpen(!open)}>
          <Icon of={open ? ChevronDown : ChevronRight} />
          Has advanced details ({details.length})
        </Button>
        {open && (
          <>
            <dl className="fy-dl" id={`${id}-list`}>
              {details.map((d) => (
                <div key={d.label}>
                  <dt className="fy-small fy-secondary">{d.label}</dt>
                  <dd>{d.value}</dd>
                </div>
              ))}
            </dl>
            <span className="fy-help">You can see them here; change them in Expert mode.</span>
            <Button variant="ghost" size="sm" type="button" className="self-start ps-0" onClick={() => setMode("expert")}>
              Open in Expert mode
              <Icon of={ArrowRight} />
            </Button>
          </>
        )}
      </div>
    </div>
  );
}
