import { Dialog } from "@base-ui/react/dialog";
import { Link, useNavigate, useRouterState } from "@tanstack/react-router";
import { Info, LayoutGrid, LogOut, Search, Sparkles, X } from "lucide-react";
import { lazy, Suspense, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import type { Session } from "../api/client";
import type { TurnContext } from "../components/ai/assistant";
import { AssistantContext } from "../components/ai/context";
import { Button, buttonVariants } from "../components/ui/button";
import { helpText } from "../i18n";
import { setMode } from "../mode";
import { nav, type Mode, type NavFeature } from "../registry";
import { available, enterToNext, keyOf, matcher, onShortcutSheet, useShortcutSettings } from "../shortcuts";
import { Icon } from "../ui";
import { AppearanceChoices, AppearanceMenu } from "./AppearanceMenu";
import { About, HelpMenu } from "./HelpMenu";
import { TourHost, TourOffer } from "../tour/TourHost";
import { bottomBar, navFor } from "./navigation";

const CommandPalette = lazy(() => import("./CommandPalette"));
const AssistantPanel = lazy(() => import("../components/ai/AssistantPanel"));
const ShortcutSheet = lazy(() => import("./Shortcuts").then((m) => ({ default: m.ShortcutSheet })));

const mainGroups = [
  ["personal", "For you"],
  ["main", "Workspace"],
] as const;
const footGroups = [["footer", ""]] as const;
const allGroups = [...mainGroups, ["footer", "Settings"]] as const;

function NavItem({ item, current, className, onNavigate }: { item: NavFeature; current: boolean; className: string; onNavigate?: () => void }) {
  const label = helpText(item.help).label;
  return (
    <Link to={item.route} className={className} aria-current={current ? "page" : undefined} onClick={onNavigate} data-tour={item.id}>
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
// is always visible. It changes only what is shown (FR-UX-24); the page, its filters and drafts stay. In the compact
// class it is the designed pill (docs/design/screens AppShell), one button that names the mode and switches it, since a
// 320 px header has no room for both options (FR-UX-37).
export function ModeSwitch({ mode }: { mode: Mode }) {
  const h = helpText("shell.mode");
  const choices: [Mode, string][] = [
    ["basic", "Basic"],
    ["expert", "Expert"],
  ];
  const name = (m: Mode) => (m === "basic" ? "Basic" : "Expert");
  const other: Mode = mode === "basic" ? "expert" : "basic";
  return (
    <>
      <button type="button" className="fy-modepill fy-only-compact pointer-coarse:min-h-(--touch-target)" onClick={() => setMode(other)} title={h.tooltip} data-tour="mode"
        aria-label={`${h.label}: ${name(mode)}. Switch to ${name(other)}`}>
        {name(mode)}
      </button>
      <div className="fy-segmented fy-from-medium" role="radiogroup" aria-label={h.label} title={h.tooltip} data-tour="mode">
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
    </>
  );
}

// AppFrame is the frame of every signed-in page (MASTER.md §5, §5.1; docs/design/screens AppShell):
// the sidebar (a rail in the medium class, a bottom bar in compact) cut to the person's roles and
// the organisation's feature switches (FR-UX-40), the header with search and commands (Ctrl+K or
// ⌘K), the Basic | Expert switch, the operations tray (FR-INT-11), the notification bell, appearance and sign out, and the main region with the
// banners on top (FR-NTF-01, FR-NTF-03; the signed-in layout passes both, which read the API). The organisation
// switcher joins it with p1-app-shell 2.1–4.1. The assistant's button and Ctrl+J or ⌘J open the AI panel (FR-UX-17,
// p1-ai-assistant design D12), docked beside the page in expanded, a sheet in medium and full screen in compact.
export function AppFrame({ session, title, mode, operations, bell, banners, logo, children }: {
  session: Pick<Session, "roles" | "features"> & { user_id?: string; super_admin?: boolean };
  title: string;
  mode: Mode;
  operations?: ReactNode;
  bell?: ReactNode;
  banners?: ReactNode;
  logo?: ReactNode; // the organisation's logo (FR-ORG-09), which the signed-in layout reads
  children: ReactNode;
}) {
  const path = useRouterState({ select: (s) => s.location.pathname });
  const [open, setOpen] = useState(false);
  const [wanted, setWanted] = useState(false); // load the palette's code only once it is asked for
  const [more, setMore] = useState(false);
  const [about, setAbout] = useState(false);
  const [assistant, setAssistant] = useState(false);
  const [context, setContext] = useState<TurnContext | undefined>(undefined);
  const [sheet, setSheetOpen] = useState(false);
  const [sheetWanted, setSheetWanted] = useState(false); // load the sheet's code only once it is asked for
  const setSheet = (o: boolean) => {
    if (o) setSheetWanted(true);
    setSheetOpen(o);
  };
  const navigate = useNavigate();
  const keys = useShortcutSettings(session.user_id ?? "");
  const opener = useRef<HTMLButtonElement>(null);
  const visible = navFor(nav, session, mode);
  const bottom = bottomBar(visible);
  const search = helpText("shell.command-palette");
  const signOut = helpText("auth.sign-out");
  const aboutText = helpText("shell.about");
  const openPalette = () => {
    setWanted(true);
    setOpen(true);
  };
  const ai = helpText("shell.assistant");
  const closeAssistant = () => {
    setAssistant(false);
    opener.current?.focus();
  };
  const enabled = session.features?.ai_assistant ?? false;
  const panel = useMemo(
    () => ({
      openWith: (c?: TurnContext) => {
        setContext(c);
        setAssistant(true);
      },
      enabled,
    }),
    [enabled],
  );

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && !e.altKey && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setWanted(true);
        setOpen((o) => !o);
      }
      if ((e.metaKey || e.ctrlKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "j") {
        e.preventDefault();
        setAssistant((o) => !o);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // Keyboard-first entry (FR-UX-06): the single-character shortcuts the person may use in their mode, never while
  // they type in a field or a dialog is open; and Enter to the next field in an entry form.
  useEffect(() => {
    const list = available(session, mode, keys);
    const step = matcher();
    const onKey = (e: KeyboardEvent) => {
      enterToNext(e);
      const key = keyOf(e);
      if (!key) return;
      const hit = step(key, e.timeStamp, list);
      if (!hit) return;
      e.preventDefault();
      if (hit === "wait") return;
      if (hit.to) void navigate({ to: hit.to });
      else showSheet();
    };
    const showSheet = () => {
      setSheetWanted(true);
      setSheetOpen(true);
    };
    window.addEventListener("keydown", onKey);
    const off = onShortcutSheet(showSheet);
    return () => {
      window.removeEventListener("keydown", onKey);
      off();
    };
  }, [session, mode, keys, navigate]);

  return (
    <AssistantContext.Provider value={panel}>
    <div className="fy-app" data-ai={assistant ? "open" : undefined}>
      <a href="#main" className="fy-sr focus:not-sr-only focus:absolute focus:top-2 focus:left-2 focus:z-(--z-toast) focus:rounded-md focus:bg-surface-1 focus:px-3 focus:py-2 focus:shadow-2">
        Skip to content
      </a>
      <nav className="fy-side" aria-label="Main">
        <Link to="/" className="fy-brand" aria-label="Fyniq home">
          <span className="fy-brand__mark" aria-hidden="true" data-letter="f" />
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
          <span className="fy-brand__mark" aria-hidden="true" data-letter="f" />
        </Link>
        {logo}
        <span className="fy-top__title fy-truncate">{title}</span>
        <button className="fy-search fy-from-expanded" type="button" onClick={openPalette} title={search.tooltip} aria-keyshortcuts="Control+K Meta+K" data-tour="search">
          <Icon of={Search} />
          <span>{search.label}</span>
          <span className="fy-kbd">Ctrl K</span>
        </button>
        <div className="fy-top__actions">
          <ModeSwitch mode={mode} />
          <Button ref={opener} variant="ghost" size="sm" onClick={() => (assistant ? closeAssistant() : setAssistant(true))} aria-expanded={assistant}
            aria-label={ai.label} title={ai.tooltip} aria-keyshortcuts="Control+J Meta+J" data-tour="assistant">
            <Icon of={Sparkles} size={20} />
            <span className="fy-from-expanded">{ai.label}</span>
          </Button>
          <Button variant="ghost" size="icon" className="fy-until-expanded" onClick={openPalette} aria-label={search.label} title={search.tooltip} data-tour="search">
            <Icon of={Search} size={20} />
          </Button>
          {operations}
          {bell}
          <AppearanceMenu className="fy-from-medium" />
          <HelpMenu className="fy-from-medium" />
          <form method="post" action="/api/auth/signout" className="fy-from-medium">
            <button className={buttonVariants({ variant: "ghost", size: "icon" })} type="submit" aria-label={signOut.label} title={signOut.tooltip}>
              <Icon of={LogOut} size={20} />
            </button>
          </form>
        </div>
      </header>
      <main className="fy-main" id="main" tabIndex={-1}>
        {banners}
        <div className="px-4 pt-4 empty:hidden">
          <TourOffer session={session} />
        </div>
        {children}
      </main>
      <TourHost session={session} />
      <nav className="fy-bottom" aria-label="Main, bottom bar">
        {bottom.map((n) => (
          <NavItem key={n.id} item={n} current={current(n, path)} className="fy-bottom__item" />
        ))}
        <button type="button" className="fy-bottom__item border-0 bg-transparent font-[inherit]" onClick={() => setMore(true)} aria-haspopup="dialog">
          <Icon of={LayoutGrid} />
          <span>More</span>
        </button>
      </nav>
      {/* The compact More sheet: the whole navigation, then the account's appearance, About and sign out, which the
          compact header has no room for (docs/design/screens AppShell; FR-UX-37). It is always there, so a person whose
          roles show few pages still reaches them. */}
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
            <hr className="fy-divider my-3" />
            <section className="fy-stack fy-stack--3" aria-labelledby="more-appearance">
              <h3 className="fy-eyebrow" id="more-appearance">
                Appearance
              </h3>
              <AppearanceChoices />
            </section>
            <div className="fy-row fy-row--between fy-row--wrap mt-3">
              <button type="button" className="fy-btn fy-btn--ghost fy-btn--sm" title={aboutText.tooltip} aria-haspopup="dialog"
                onClick={() => {
                  setMore(false);
                  setAbout(true);
                }}>
                <Icon of={Info} />
                {aboutText.label}
              </button>
              <form method="post" action="/api/auth/signout">
                <button className="fy-btn fy-btn--ghost fy-btn--sm" type="submit" title={signOut.tooltip}>
                  <Icon of={LogOut} />
                  {signOut.label}
                </button>
              </form>
            </div>
          </Dialog.Popup>
        </Dialog.Portal>
      </Dialog.Root>
      <About open={about} onOpenChange={setAbout} />
      {sheetWanted && (
        <Suspense fallback={null}>
          <ShortcutSheet open={sheet} onOpenChange={setSheet} session={session} mode={mode} />
        </Suspense>
      )}
      {assistant && (
        <Suspense fallback={null}>
          <AssistantPanel enabled={enabled} mode={mode} context={context} onClearContext={() => setContext(undefined)}
            onClose={closeAssistant} />
        </Suspense>
      )}
      {wanted && (
        <Suspense fallback={null}>
          <CommandPalette open={open} onOpenChange={setOpen} />
        </Suspense>
      )}
    </div>
    </AssistantContext.Provider>
  );
}
