import { useSyncExternalStore } from "react";
import type { Mode } from "./registry";

// Plain view and Accountant view (FR-UX-08, §7.8): the same figures, as "Money in / Money out / Category" or as
// "Debit / Credit / Account". Each person chooses in Expert mode; Basic mode always uses the Plain view.
//
// Provisional, like the mode: the choice is kept in this browser's storage until the preferences operation stores it on
// the server (p1-app-shell task 4.1).

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

export function setLedgerView(v: LedgerView): void {
  chosen = v;
  loaded = true;
  try {
    window.localStorage.setItem(storageKey, v);
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
