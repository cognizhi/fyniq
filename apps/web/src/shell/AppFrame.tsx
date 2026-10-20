import { Dialog } from "@base-ui/react/dialog";
import { Link, useRouterState } from "@tanstack/react-router";
import { LayoutGrid, LogOut, Search, X } from "lucide-react";
import { lazy, Suspense, useEffect, useState, type ReactNode } from "react";
import type { Session } from "../api/client";
import { Button, buttonVariants } from "../components/ui/button";
import { helpText } from "../i18n";
import { setMode } from "../mode";
import { nav, type Mode, type NavFeature } from "../registry";
import { Icon } from "../ui";
import { AppearanceMenu } from "./AppearanceMenu";
import { bottomBar, navFor } from "./navigation";

const CommandPalette = lazy(() => import("./CommandPalette"));

const mainGroups = [
  ["personal", "For you"],
  ["main", "Workspace"],
] as const;
const footGroups = [["footer", ""]] as const;
const allGroups = [...mainGroups, ["footer", "Settings"]] as const;

function NavItem({ item, current, className, onNavigate }: { item: NavFeature; current: boolean; className: string; onNavigate?: () => void }) {
  const label = helpText(item.help).label;
  return (
    <Link to={item.route} className={className} aria-current={current ? "page" : undefined} onClick={onNavigate}>
      <Icon of={item.icon} />
      <span className="fy-nav__label">{label}</span>
    </Link>
  );
}

// current says whether an item is the page shown: its route, or a page under it (Settings › …).
function current(item: NavFeature, path: string): boolean {
  return item.route === "/" ? path === "/" : path === item.route || path.startsWith(`${item.route}/`);
}

function NavList({ items, path, onNavigate, groups = mainGroups }: { items: readonly NavFeature[]; path: string; onNavigate?: () => void; groups?: readonly (readonly [NavFeature["group"], string])[] }) {
  return (
    <div className="fy-nav">
      {groups.map(([g, title]) => {
        const mine = items.filter((n) => n.group === g);
        return mine.length === 0 ? null : (
          <div key={g} className="contents">
            {title && <div className="fy-nav__group fy-eyebrow">{title}</div>}
            {mine.map((n) => (
              <NavItem key={n.id} item={n} current={current(n, path)} className="fy-nav__item" onNavigate={onNavigate} />
            ))}
          </div>
        );
      })}
    </div>
  );
}

// ModeSwitch is the header's Basic | Expert control (FR-UX-23, experience/modes): the current mode
// is always visible. It changes only what is shown (FR-UX-24); the page, its filters and drafts stay.
export function ModeSwitch({ mode }: { mode: Mode }) {
  const h = helpText("shell.mode");
  const choices: [Mode, string][] = [
    ["basic", "Basic"],
    ["expert", "Expert"],
  ];
  return (
    <div className="fy-segmented" role="radiogroup" aria-label={h.label} title={h.tooltip}>
      {choices.map(([m, label]) => (
        <button key={m} type="button" role="radio" aria-checked={mode === m} tabIndex={mode === m ? 0 : -1} onClick={() => setMode(m)}
          onKeyDown={(e) => {
            if (["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(e.key)) {
              e.preventDefault();
              const next = m === "basic" ? "expert" : "basic";
              setMode(next);
              (e.currentTarget.parentElement?.querySelector(`[data-mode="${next}"]`) as HTMLElement | null)?.focus();
            }
          }}
          data-mode={m}>
          {label}
        </button>
      ))}
    </div>
  );
}

// AppFrame is the frame of every signed-in page (MASTER.md §5, §5.1; docs/design/screens AppShell):
// the sidebar (a rail in the medium class, a bottom bar in compact) cut to the person's roles and
// the organisation's feature switches (FR-UX-40), the header with search and commands (Ctrl+K or
// ⌘K), the Basic | Expert switch, appearance and sign out, and the main region. The organisation
// switcher, notifications and the AI button join it with p1-app-shell 2.1–4.1 and wave 6.
export function AppFrame({ session, title, mode, children }: { session: Pick<Session, "roles" | "features">; title: string; mode: Mode; children: ReactNode }) {
  const path = useRouterState({ select: (s) => s.location.pathname });
  const [open, setOpen] = useState(false);
  const [wanted, setWanted] = useState(false); // load the palette's code only once it is asked for
  const [more, setMore] = useState(false);
  const visible = navFor(nav, session, mode);
  const bottom = bottomBar(visible);
  const search = helpText("shell.command-palette");
  const signOut = helpText("auth.sign-out");
  const openPalette = () => {
    setWanted(true);
    setOpen(true);
  };

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && !e.altKey && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setWanted(true);
        setOpen((o) => !o);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return (
    <div className="fy-app">
      <a href="#main" className="fy-sr focus:not-sr-only focus:absolute focus:top-2 focus:left-2 focus:z-(--z-toast) focus:rounded-md focus:bg-surface-1 focus:px-3 focus:py-2 focus:shadow-2">
        Skip to content
      </a>
      <nav className="fy-side" aria-label="Main">
        <Link to="/" className="fy-brand" aria-label="Fyniq home">
          <span className="fy-brand__mark" aria-hidden="true">
            f
          </span>
          <span className="fy-brand__word" aria-hidden="true">
            Fyniq
          </span>
        </Link>
        <NavList items={visible} path={path} />
        <div className="fy-side__foot">
          <NavList items={visible} path={path} groups={footGroups} />
        </div>
      </nav>
      <header className="fy-top">
        <Link to="/" className="fy-brand" aria-label="Fyniq home">
          <span className="fy-brand__mark" aria-hidden="true">
            f
          </span>
        </Link>
        <span className="fy-top__title fy-truncate">{title}</span>
        <button className="fy-search fy-from-expanded" type="button" onClick={openPalette} title={search.tooltip} aria-keyshortcuts="Control+K Meta+K">
          <Icon of={Search} />
          <span>{search.label}</span>
          <span className="fy-kbd">Ctrl K</span>
        </button>
        <div className="fy-top__actions">
          <ModeSwitch mode={mode} />
          <Button variant="ghost" size="icon" className="fy-until-expanded" onClick={openPalette} aria-label={search.label} title={search.tooltip}>
            <Icon of={Search} size={20} />
          </Button>
          <AppearanceMenu />
          <form method="post" action="/api/auth/signout">
            <button className={buttonVariants({ variant: "ghost", size: "icon" })} type="submit" aria-label={signOut.label} title={signOut.tooltip}>
              <Icon of={LogOut} size={20} />
            </button>
          </form>
        </div>
      </header>
      <main className="fy-main" id="main" tabIndex={-1}>
        {children}
      </main>
      <nav className="fy-bottom" aria-label="Main, bottom bar">
        {bottom.items.map((n) => (
          <NavItem key={n.id} item={n} current={current(n, path)} className="fy-bottom__item" />
        ))}
        {bottom.more && (
          <button type="button" className="fy-bottom__item border-0 bg-transparent font-[inherit]" onClick={() => setMore(true)}>
            <Icon of={LayoutGrid} />
            <span>More</span>
          </button>
        )}
      </nav>
      {bottom.more && (
        <Dialog.Root open={more} onOpenChange={setMore}>
          <Dialog.Portal>
            <Dialog.Backdrop className="fixed inset-0 z-(--z-modal) bg-scrim" />
            <Dialog.Popup data-slot="dialog-popup" className="fixed inset-x-0 bottom-0 z-(--z-modal) max-h-[80vh] overflow-y-auto rounded-t-lg border-t border-border bg-surface-1 p-3 shadow-3">
              <div className="flex items-center justify-between pb-2">
                <Dialog.Title className="text-lg font-semibold">More</Dialog.Title>
                <Dialog.Close className={buttonVariants({ variant: "ghost", size: "icon" })} aria-label="Close">
                  <Icon of={X} size={20} />
                </Dialog.Close>
              </div>
              <NavList items={visible} path={path} onNavigate={() => setMore(false)} groups={allGroups} />
            </Dialog.Popup>
          </Dialog.Portal>
        </Dialog.Root>
      )}
      {wanted && (
        <Suspense fallback={null}>
          <CommandPalette open={open} onOpenChange={setOpen} />
        </Suspense>
      )}
    </div>
  );
}
