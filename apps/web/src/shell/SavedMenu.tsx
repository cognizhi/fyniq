import { Star } from "lucide-react";
import { lazy, Suspense, useState } from "react";
import { buttonVariants } from "../components/ui/button";
import { helpText } from "../i18n";
import { cn } from "../lib/utils";
import { Icon } from "../ui";

// The dialog and its code load when it is first opened, so the shell's first load stays small (NFR-PERF-UI-03).
const Dialog = lazy(() => import("./SavedDialog"));

export function SavedDialog(props: { open: boolean; onOpenChange: (open: boolean) => void }) {
  return (
    <Suspense fallback={null}>
      <Dialog {...props} />
    </Suspense>
  );
}

// The header's Favourites and views (FR-UX-12; docs/design/screens AppShell): pin the page, save it with the filters, columns
// and sort its address carries, open and take away what is saved. It is the person's own, kept on the server. A page whose
// filters are not in its address can be pinned but a view of it keeps no filters; the lists whose filters live in the address
// (the ledger, the statements, categorise) save theirs in full.

export function SavedMenu({ className }: { className?: string }) {
  const [open, setOpen] = useState(false);
  const h = helpText("shell.saved");
  return (
    <>
      <button type="button" className={cn(buttonVariants({ variant: "ghost", size: "icon" }), className)} onClick={() => setOpen(true)} aria-label={h.label} title={h.tooltip} aria-haspopup="dialog">
        <Icon of={Star} size={20} />
      </button>
      {open && <SavedDialog open={open} onOpenChange={setOpen} />}
    </>
  );
}

