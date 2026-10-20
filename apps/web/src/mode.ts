import { useSyncExternalStore } from "react";
import type { Mode } from "./registry";

// The experience mode (PRD-001 §7.8; FR-UX-23, FR-UX-24, FR-UX-27): Basic shows the essentials,
// Expert adds the rest. It is presentation only: it never changes what a person may do, what the
// API checks or the request an action sends.
//
// Provisional (20 Oct 2026): the choice is kept in this browser's storage, like the appearance,
// until the preferences operation stores it on the server (p1-app-shell task 4.1). Until a person
// chooses, their roles decide (FR-UX-27).

// Roles that start in Expert, unless the person is also an Owner: the Owner wins (FR-UX-27).
const expertRoles = new Set(["accountant", "controller", "tax_agent", "auditor", "payroll_administrator"]);

export function defaultMode(roles: readonly string[] | null | undefined): Mode {
  const r = roles ?? [];
  if (r.includes("owner")) return "basic";
  return r.some((x) => expertRoles.has(x)) ? "expert" : "basic";
}

const storageKey = "fyniq.mode";
const listeners = new Set<() => void>();
let chosen: Mode | null = null;
let loaded = false;

function read(): Mode | null {
  try {
    const v = window.localStorage.getItem(storageKey);
    return v === "basic" || v === "expert" ? v : null;
  } catch {
    return null;
  }
}

function current(): Mode | null {
  if (!loaded) {
    chosen = read();
    loaded = true;
  }
  return chosen;
}

export function setMode(m: Mode): void {
  chosen = m;
  loaded = true;
  try {
    window.localStorage.setItem(storageKey, m);
  } catch {
    // storage may be off; the choice still applies to this page
  }
  for (const l of listeners) l();
}

// forgetMode drops the stored choice (tests, and a person who wants the default back).
export function forgetMode(): void {
  chosen = null;
  loaded = false;
  try {
    window.localStorage.removeItem(storageKey);
  } catch {
    // nothing stored
  }
  for (const l of listeners) l();
}

function subscribe(l: () => void): () => void {
  listeners.add(l);
  return () => listeners.delete(l);
}

// useMode is the person's mode: their choice, or their roles' default.
export function useMode(roles: readonly string[] | null | undefined): Mode {
  const c = useSyncExternalStore(subscribe, current);
  return c ?? defaultMode(roles);
}
