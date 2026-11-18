import { Menu } from "@base-ui/react/menu";
import { useNavigate, useRouterState } from "@tanstack/react-router";
import { BookOpen, CircleHelp, Compass, Info } from "lucide-react";
import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { Refusal } from "../components/Refusal";
import { buttonVariants } from "../components/ui/button";
import { Dialog } from "../components/ui/dialog";
import { dateTime } from "../format/date";
import { guideFor } from "../help/links";
import { helpText } from "../i18n";
import { cn } from "../lib/utils";
import { startTour } from "../tour/state";
import { Icon, problemOf } from "../ui";

// The header's Help menu (screen inventory: the app shell's header; REL-05, p1-security-hardening design D11). About
// Fyniq shows the platform version and the git commit the services were built from, as getPlatformVersion answers them,
// so a person can quote them when they report a problem; every exported report names the same in its footer.

const item =
  "flex min-h-8 cursor-default items-center gap-2 rounded-sm px-2 py-1.5 text-base text-foreground outline-none select-none data-highlighted:bg-surface-3 pointer-coarse:min-h-(--touch-target)";

export function HelpMenu({ className }: { className?: string }) {
  const [about, setAbout] = useState(false);
  const help = helpText("shell.help");
  const a = helpText("shell.about");
  const g = helpText("help.guide");
  const t = helpText("shell.tour");
  const navigate = useNavigate();
  const path = useRouterState({ select: (st) => st.location.pathname });
  // The guide opens on the page of the screen the person is on (FR-UX-33), else at its index.
  const open = () => {
    const to = guideFor(path);
    void navigate(to ? { to: to.to, hash: to.hash } : { to: "/help" });
  };
  return (
    <>
      <Menu.Root>
        <Menu.Trigger className={cn(buttonVariants({ variant: "ghost", size: "icon" }), className)} aria-label={help.label} title={help.tooltip} data-tour="help">
          <Icon of={CircleHelp} size={20} />
        </Menu.Trigger>
        <Menu.Portal>
          <Menu.Positioner sideOffset={4} align="end" className="z-(--z-popover)">
            <Menu.Popup data-slot="menu-popup" className="min-w-48 rounded-lg border border-border bg-popover p-1 text-popover-foreground shadow-2 outline-none">
              <Menu.Item className={item} onClick={open}>
                <Icon of={BookOpen} />
                <span>{g.label}</span>
              </Menu.Item>
              <Menu.Item className={item} onClick={() => startTour(0)} title={t.tooltip}>
                <Icon of={Compass} />
                <span>{t.label}</span>
              </Menu.Item>
              <Menu.Item className={item} onClick={() => setAbout(true)}>
                <Icon of={Info} />
                <span>{a.label}</span>
              </Menu.Item>
            </Menu.Popup>
          </Menu.Positioner>
        </Menu.Portal>
      </Menu.Root>
      <About open={about} onOpenChange={setAbout} />
    </>
  );
}

type Version = components["schemas"]["PlatformVersion"];

// About reads the version each time it opens; the shell has no query client of its own. The compact More sheet opens it
// too.
export function About({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const a = helpText("shell.about");
  const [v, setV] = useState<Version | null>(null);
  const [failed, setFailed] = useState<unknown>(null);
  useEffect(() => {
    if (!open) return;
    let live = true;
    setV(null);
    setFailed(null);
    api.GET("/v1/platform/version").then(
      ({ data, error }) => {
        if (!live) return;
        if (error || !data) setFailed(error ?? new Error("no version"));
        else setV(data);
      },
      (e: unknown) => live && setFailed(e),
    );
    return () => {
      live = false;
    };
  }, [open]);
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title={a.label} description="Intelligence behind every number">
      <div className="fy-stack fy-stack--3">
        {!v && failed === null && <p role="status">Reading the version…</p>}
        {failed !== null && <Refusal problem={problemOf(failed)} title="The version could not be read" />}
        {v && (
          <dl className="fy-dl">
            <div>
              <dt>Version</dt>
              <dd className="fy-num">{v.version}</dd>
            </div>
            <div>
              <dt>Commit</dt>
              <dd className="fy-mono">{v.commit}</dd>
            </div>
            <div>
              <dt>Built</dt>
              <dd>{v.build_time === "unknown" ? "Not recorded" : dateTime(v.build_time)}</dd>
            </div>
            {v.release && (
              <div>
                <dt>Release</dt>
                <dd>{v.release}</dd>
              </div>
            )}
            <div>
              <dt>Build</dt>
              <dd>{v.flavour === "local" ? "Local mode" : v.flavour === "release" ? "Release build" : v.flavour}</dd>
            </div>
          </dl>
        )}
        <p className="fy-small fy-secondary">{a.tooltip}</p>
      </div>
    </Dialog>
  );
}
