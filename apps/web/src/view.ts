import { useSyncExternalStore } from "react";
import { persist } from "./preferenceSink";
import type { Mode } from "./registry";

// Plain view and Accountant view (FR-UX-08, §7.8): the same figures, as "Money in / Money out / Category" or as
// "Debit / Credit / Account". Each person chooses in Expert mode; Basic mode always uses the Plain view.
//
// Kept like the mode: on the server as the person's own preference (FR-UX-23; preferences.ts) and in this browser's storage.

export type LedgerView = "plain" | "accountant";

const storageKey = "fyniq.view";
const listeners = new Set<() => void>();
let chosen: LedgerView | null = null;
let loaded = false;

function current(): LedgerView {
  if (!loaded) {
    try {
      const v = window.localStorage.getItem(storageKey);
      chosen = v === "plain" || v === "accountant" ? v : null;
    } catch {
      chosen = null;
    }
    loaded = true;
  }
  return chosen ?? "plain";
}

// theChosenView is the person's own choice, if they made one.
export function theChosenView(): LedgerView | null {
  current();
  return chosen;
}

export function setLedgerView(v: LedgerView): void {
  applyLedgerView(v);
  persist({ ledger_view: v });
}

// applyLedgerView applies a view without sending it: the person's choice as the server has it.
export function applyLedgerView(v: LedgerView | null): void {
  chosen = v;
  loaded = true;
  try {
    if (v) window.localStorage.setItem(storageKey, v);
    else window.localStorage.removeItem(storageKey);
  } catch {
    // storage may be off; the choice still applies to this page
  }
  for (const l of listeners) l();
}

function subscribe(l: () => void): () => void {
  listeners.add(l);
  return () => listeners.delete(l);
}

// useLedgerView is the view a page shows: the person's choice in Expert mode, Plain in Basic mode.
export function useLedgerView(mode: Mode): LedgerView {
  const v = useSyncExternalStore(subscribe, current);
  return mode === "expert" ? v : "plain";
}

// viewWords are the column names of a view: money that comes in and goes out of a money account, and what it is for.
export function viewWords(v: LedgerView): { in: string; out: string; what: string } {
  return v === "accountant" ? { in: "Debit", out: "Credit", what: "Account" } : { in: "Money in", out: "Money out", what: "Category" };
}
