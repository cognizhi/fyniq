import { Menu } from "@base-ui/react/menu";
import { Ellipsis, PanelRightClose, PanelRightOpen } from "lucide-react";
import { useId, useState, type KeyboardEvent, type ReactNode } from "react";
import { cn } from "../lib/utils";
import { Icon } from "../ui";
import { Button } from "./ui/button";
import { buttonVariants } from "./ui/button";

// Record page anatomy (PRD-001 §7.3; experience/navigation "Record page anatomy"): every record page (invoice, bill,
// transaction, journal, pay run, asset, contact) has a header, body tabs and a collapsible right panel. The header shows
// the number, the status chip, the contact, the key amounts and the primary actions, and a "⋯" menu holds the others.
// The body's tabs are Details, Lines, Ledger impact, Attachments, Activity and Related, in that order; a record that
// has no lines, for one, leaves that tab out. The right panel shows the checks for this record, the AI's suggestions and
// the context, and below 1024 px it sits below the body (docs/design-system MASTER.md, device classes).

export type RecordTab = "details" | "lines" | "ledger" | "attachments" | "activity" | "related";

/** The six tabs, in order, with the names a person sees. */
export const recordTabs: readonly [RecordTab, string][] = [
  ["details", "Details"],
  ["lines", "Lines"],
  ["ledger", "Ledger impact"],
  ["attachments", "Attachments"],
  ["activity", "Activity"],
  ["related", "Related"],
];

const panelKey = "fyniq.record-panel";

// The right panel's open state is a per-person convenience, kept in the browser; the page works without it.
function savedPanel(): boolean {
  try {
    return localStorage.getItem(panelKey) !== "closed";
  } catch {
    return true;
  }
}

function keepPanel(open: boolean) {
  try {
    localStorage.setItem(panelKey, open ? "open" : "closed");
  } catch {
    /* a private window: it opens again next time */
  }
}

type FrameProps = {
  /** The header: a RecordHeader, or the page's own. */
  header: ReactNode;
  /** What each tab holds; a tab with no content is left out. */
  tabs: Partial<Record<RecordTab, ReactNode>>;
  /** The right panel: the record's checks, the AI's suggestions and the context. Left out, the body takes the width. */
  panel?: ReactNode;
  panelLabel?: string;
  /** The tab shown first; Details by default. */
  initial?: RecordTab;
  className?: string;
};

export function RecordFrame({ header, tabs, panel, panelLabel = "Checks, suggestions and context", initial = "details", className }: FrameProps) {
  const id = useId();
  const shown = recordTabs.filter(([key]) => tabs[key] !== undefined && tabs[key] !== null && tabs[key] !== false);
  const [picked, setPicked] = useState<RecordTab>(initial);
  const [open, setOpen] = useState(savedPanel);
  const tab = shown.some(([key]) => key === picked) ? picked : (shown[0]?.[0] ?? "details");

  const pick = (key: RecordTab, focus = false) => {
    setPicked(key);
    if (focus) document.getElementById(`${id}-tab-${key}`)?.focus();
  };
  const onKey = (e: KeyboardEvent, i: number) => {
    const last = shown.length - 1;
    const to = e.key === "ArrowRight" ? (i === last ? 0 : i + 1) : e.key === "ArrowLeft" ? (i === 0 ? last : i - 1) : e.key === "Home" ? 0 : e.key === "End" ? last : -1;
    if (to < 0) return;
    e.preventDefault();
    pick(shown[to]![0], true);
  };
  const togglePanel = () => {
    keepPanel(!open);
    setOpen(!open);
  };
  const withPanel = Boolean(panel) && open;

  return (
    <div className={cn("fy-page fy-enter", className)}>
      {header}
      <div className="fy-row fy-row--between fy-row--wrap gap-2">
        <div className={cn("fy-tabs fy-grow", shown.length < 2 && "hidden")} role="tablist" aria-label="Record">
          {shown.map(([key, label], i) => (
            <button key={key} type="button" role="tab" id={`${id}-tab-${key}`} aria-selected={tab === key} aria-controls={`${id}-panel-${key}`} tabIndex={tab === key ? 0 : -1}
              onClick={() => pick(key)} onKeyDown={(e) => onKey(e, i)}>
              {label}
            </button>
          ))}
        </div>
        {panel && (
          <Button variant="ghost" size="sm" type="button" aria-expanded={open} aria-controls={`${id}-aside`} onClick={togglePanel} title={open ? "Hide the panel at the side" : "Show the panel at the side"}>
            <Icon of={open ? PanelRightClose : PanelRightOpen} />
            {open ? "Hide panel" : "Show panel"}
          </Button>
        )}
      </div>
      <div className={withPanel ? "fy-split" : undefined}>
        <div className="fy-stack fy-stack--6 min-w-0" role="tabpanel" id={`${id}-panel-${tab}`} aria-labelledby={`${id}-tab-${tab}`} tabIndex={0}>
          {tabs[tab]}
        </div>
        {panel && (
          <aside className="fy-stack fy-stack--4" id={`${id}-aside`} aria-label={panelLabel} hidden={!open}>
            {panel}
          </aside>
        )}
      </div>
    </div>
  );
}

type Amount = { label: string; value: string };
type MoreItem = { label: string; onSelect: () => void; disabled?: boolean };

type HeaderProps = {
  title: string;
  /** The record's number, once it has one. */
  number?: string;
  /** Its status chip (a StateChips or a chip with an icon, never colour alone). */
  status?: ReactNode;
  /** The contact it is with, as a link or a name. */
  contact?: ReactNode;
  /** The key amounts: what it totals, what is open. */
  amounts?: Amount[];
  /** The primary actions: Approve, Post, Send, Record payment. */
  primary?: ReactNode;
  /** The other actions, behind a "⋯" menu. */
  more?: MoreItem[];
  breadcrumb?: ReactNode;
};

/** RecordHeader is the header every record page has (experience/navigation "Header"). */
export function RecordHeader({ title, number, status, contact, amounts = [], primary, more = [], breadcrumb }: HeaderProps) {
  return (
    <>
      {breadcrumb}
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <div className="fy-row fy-row--wrap">
            <h1 className="fy-h1">{title}</h1>
            {status}
          </div>
          {(number || contact) && (
            <p className="fy-secondary">
              {number}
              {number && contact ? " · " : ""}
              {contact}
            </p>
          )}
          {amounts.length > 0 && (
            <dl className="fy-row fy-row--wrap gap-4" aria-label="Key amounts">
              {amounts.map((a) => (
                <div key={a.label}>
                  <dt className="fy-small fy-secondary">{a.label}</dt>
                  <dd className="fy-num fy-strong">{a.value}</dd>
                </div>
              ))}
            </dl>
          )}
        </div>
        {(primary || more.length > 0) && (
          <div className="fy-pagehead__actions">
            {primary}
            {more.length > 0 && (
              <Menu.Root>
                <Menu.Trigger className={cn(buttonVariants({ variant: "ghost", size: "icon" }))} aria-label="More actions" title="More actions">
                  <Icon of={Ellipsis} size={20} />
                </Menu.Trigger>
                <Menu.Portal>
                  <Menu.Positioner sideOffset={4} align="end" className="z-(--z-popover)">
                    <Menu.Popup className="min-w-48 rounded-lg border border-border bg-popover p-1 text-popover-foreground shadow-2 outline-none">
                      {more.map((m) => (
                        <Menu.Item key={m.label} disabled={m.disabled} onClick={m.onSelect}
                          className="flex min-h-8 cursor-default items-center rounded-sm px-2 py-1.5 text-base text-foreground outline-none select-none data-highlighted:bg-surface-3 data-disabled:opacity-60 pointer-coarse:min-h-(--touch-target)">
                          {m.label}
                        </Menu.Item>
                      ))}
                    </Menu.Popup>
                  </Menu.Positioner>
                </Menu.Portal>
              </Menu.Root>
            )}
          </div>
        )}
      </div>
    </>
  );
}
