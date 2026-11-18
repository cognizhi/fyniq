import { useEffect, useRef, useSyncExternalStore } from "react";

// Bulk actions on selected rows from the command palette (FR-UX-06): a page whose rows a person selected offers what it
// can do to them, and the palette lists those actions first while they stay selected. Each action is the page's own
// button, so the palette adds no other way to change anything.

export interface BulkAction {
  label: string; // names the rows: "Accept the selected suggestions (3)"
  run: () => void;
}

const listeners = new Set<() => void>();
let current: readonly BulkAction[] = [];

function notify() {
  for (const l of listeners) l();
}

// useBulkActions offers a page's actions on its selected rows while it shows them; pass none when nothing is selected.
export function useBulkActions(actions: readonly BulkAction[]): void {
  const latest = useRef(actions);
  useEffect(() => {
    latest.current = actions;
  });
  const labels = actions.map((a) => a.label).join("\n");
  useEffect(() => {
    if (!labels) return;
    const mine = labels.split("\n").map((label, i) => ({ label, run: () => latest.current[i]?.run() }));
    current = mine;
    notify();
    return () => {
      if (current === mine) {
        current = [];
        notify();
      }
    };
  }, [labels]);
}

// selectedRowActions is what the page offers now, outside React (tests).
export function selectedRowActions(): readonly BulkAction[] {
  return current;
}

export function useSelectedRowActions(): readonly BulkAction[] {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => current,
  );
}
