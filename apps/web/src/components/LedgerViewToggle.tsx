import { helpText } from "../i18n";
import type { Mode } from "../registry";
import { setLedgerView, useLedgerView, type LedgerView } from "../view";

const choices: [LedgerView, string][] = [
  ["plain", "Plain view"],
  ["accountant", "Accountant view"],
];

// LedgerViewToggle switches the person between Plain view (Money in / Money out / Category) and Accountant view (Debit
// / Credit / Account) wherever a page shows money in and out (FR-UX-08). Expert mode only: Basic always uses Plain view.
export function LedgerViewToggle({ mode }: { mode: Mode }) {
  const view = useLedgerView(mode);
  const h = helpText("ledger.view");
  if (mode !== "expert") return null;
  return (
    <div className="fy-segmented" role="radiogroup" aria-label={h.label} title={h.tooltip}>
      {choices.map(([v, label]) => (
        <button
          key={v}
          type="button"
          role="radio"
          aria-checked={view === v}
          tabIndex={view === v ? 0 : -1}
          data-view={v}
          onClick={() => setLedgerView(v)}
          onKeyDown={(e) => {
            if (["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(e.key)) {
              e.preventDefault();
              const next = v === "plain" ? "accountant" : "plain";
              setLedgerView(next);
              (e.currentTarget.parentElement?.querySelector(`[data-view="${next}"]`) as HTMLElement | null)?.focus();
            }
          }}
        >
          {label}
        </button>
      ))}
    </div>
  );
}
