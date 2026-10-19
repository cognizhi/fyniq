import { Dialog } from "@base-ui/react/dialog";
import { useNavigate } from "@tanstack/react-router";
import { Command } from "cmdk";
import { Contrast, House, Monitor, Moon, Rows3, Sun } from "lucide-react";
import type { ReactNode } from "react";
import { setAppearance, type Appearance } from "../appearance";
import { helpText } from "../i18n";
import { routes, type Mode } from "../registry";
import { Icon } from "../ui";

// The command palette (cmdk in a Base UI dialog; MASTER.md §7; FR-UX-01 in part): Go to the pages
// the person can open, and switch the theme, contrast, density and motion (experience/visual-design
// Themes: "from the header and the command palette"). Loaded on demand, so cmdk is not in the
// initial bundle (NFR-PERF-UI-03). Search, create, reports and Ask AI arrive with p1-app-shell 2.2.

const item =
  "flex min-h-9 cursor-default items-center gap-2 rounded-sm px-2 text-base text-foreground outline-none select-none data-[selected=true]:bg-surface-3 pointer-coarse:min-h-(--touch-target)";
const group = "px-1 py-1 [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:font-medium [&_[cmdk-group-heading]]:text-muted-foreground";

function ModeTag({ mode }: { mode: Mode }) {
  return mode === "expert" ? <span className="ms-auto rounded-sm border border-border px-1 text-xs text-foreground-secondary">Expert</span> : null;
}

export default function CommandPalette({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const navigate = useNavigate();
  const run = (f: () => void) => () => {
    f();
    onOpenChange(false);
  };
  const look = (change: Partial<Appearance>) => run(() => setAppearance(change));
  const action = (label: string, icon: ReactNode, onSelect: () => void, mode: Mode = "basic") => (
    <Command.Item key={label} value={label} onSelect={onSelect} className={item}>
      {icon}
      {label}
      <ModeTag mode={mode} />
    </Command.Item>
  );
  const palette = helpText("shell.command-palette");

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-(--z-modal) bg-scrim" />
        <Dialog.Popup
          data-slot="dialog-popup"
          className="fixed inset-x-4 top-[12vh] z-(--z-modal) mx-auto max-w-xl overflow-hidden rounded-lg border border-border bg-popover text-popover-foreground shadow-3 outline-none"
        >
          <Dialog.Title className="sr-only">{palette.label}</Dialog.Title>
          <Dialog.Description className="sr-only">{palette.tooltip}</Dialog.Description>
          <Command label={palette.label} loop>
            <Command.Input
              placeholder="Type a page or a command…"
              className="h-12 w-full border-0 border-b border-border bg-transparent px-4 text-md text-foreground outline-none placeholder:text-muted-foreground"
            />
            <Command.List className="max-h-80 overflow-y-auto p-1">
              <Command.Empty className="px-3 py-6 text-center text-base text-muted-foreground">Nothing matches. Try another word.</Command.Empty>
              <Command.Group heading="Go to" className={group}>
                {routes
                  .filter((r) => r.signedIn)
                  .map((r) => action(helpText(r.help).label, <Icon of={House} />, run(() => void navigate({ to: r.path })), r.mode))}
              </Command.Group>
              <Command.Group heading={helpText("appearance.theme").label} className={group}>
                {action("Light theme", <Icon of={Sun} />, look({ theme: "light" }))}
                {action("Dark theme", <Icon of={Moon} />, look({ theme: "dark" }))}
                {action("System theme", <Icon of={Monitor} />, look({ theme: "system" }))}
                {action("High contrast on", <Icon of={Contrast} />, look({ contrast: "more" }))}
                {action("High contrast off", <Icon of={Contrast} />, look({ contrast: "standard" }))}
                {action("Compact density", <Icon of={Rows3} />, look({ density: "compact" }), "expert")}
                {action("Comfortable density", <Icon of={Rows3} />, look({ density: "comfortable" }), "expert")}
                {action("Reduce motion", <Icon of={Monitor} />, look({ motion: "reduce" }))}
                {action("Motion on", <Icon of={Monitor} />, look({ motion: "on" }))}
              </Command.Group>
            </Command.List>
          </Command>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
