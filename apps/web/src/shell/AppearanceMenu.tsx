import { Menu } from "@base-ui/react/menu";
import { Activity, Check, Contrast, Monitor, Moon, Rows3, Sun, SunMoon } from "lucide-react";
import type { ReactNode } from "react";
import { setAppearance, useAppearance, type Appearance } from "../appearance";
import { buttonVariants } from "../components/ui/button";
import { helpText } from "../i18n";
import { cn } from "../lib/utils";
import { entry } from "../registry";
import { Icon } from "../ui";

// The header's appearance menu (MASTER.md §2 "Themes"; experience/visual-design Themes, Density,
// Motion; FR-UX-38): theme, high contrast, density and motion. Each group's label and help come
// from the registry entry's help key. The same choices are in the command palette, and in the compact class in the
// More sheet (AppearanceChoices), since the compact header has room only for search, the mode and notifications.

const item =
  "flex min-h-8 cursor-default items-center gap-2 rounded-sm px-2 py-1.5 text-base text-foreground outline-none select-none data-highlighted:bg-surface-3 pointer-coarse:min-h-(--touch-target)";

type Choice = { id: Parameters<typeof entry>[0]; field: keyof Appearance; options: { value: string; label: string; icon: ReactNode }[] };

function Group({ id, field, options }: Choice) {
  const a = useAppearance();
  const e = entry(id);
  const h = helpText(e.help);
  return (
    <Menu.RadioGroup value={a[field]} onValueChange={(v) => setAppearance({ [field]: v } as Partial<Appearance>)}>
      <Menu.GroupLabel className="px-2 pt-2 pb-1 text-xs font-medium text-muted-foreground">
        {h.label}
        {e.mode === "expert" && <span className="ms-2 rounded-sm border border-border px-1 text-xs">Expert</span>}
      </Menu.GroupLabel>
      {options.map((o) => (
        <Menu.RadioItem key={o.value} value={o.value} className={item} closeOnClick={false}>
          {o.icon}
          <span className="flex-1">{o.label}</span>
          <Menu.RadioItemIndicator keepMounted={false}>
            <Icon of={Check} />
          </Menu.RadioItemIndicator>
        </Menu.RadioItem>
      ))}
    </Menu.RadioGroup>
  );
}

// The four choices, each a registry entry with its options; the header's menu and the compact More sheet show the same.
const groups: Choice[] = [
  {
    id: "appearance.theme",
    field: "theme",
    options: [
      { value: "light", label: "Light", icon: <Icon of={Sun} /> },
      { value: "dark", label: "Dark", icon: <Icon of={Moon} /> },
      { value: "system", label: "System", icon: <Icon of={Monitor} /> },
    ],
  },
  {
    id: "appearance.contrast",
    field: "contrast",
    options: [
      { value: "standard", label: "Standard", icon: <Icon of={Contrast} /> },
      { value: "more", label: "High contrast", icon: <Icon of={Contrast} /> },
      { value: "system", label: "System", icon: <Icon of={Monitor} /> },
    ],
  },
  {
    id: "appearance.density",
    field: "density",
    options: [
      { value: "comfortable", label: "Comfortable", icon: <Icon of={Rows3} /> },
      { value: "compact", label: "Compact", icon: <Icon of={Rows3} /> },
    ],
  },
  {
    id: "appearance.motion",
    field: "motion",
    options: [
      { value: "on", label: "Motion on", icon: <Icon of={Activity} /> },
      { value: "reduce", label: "Reduce motion", icon: <Icon of={Activity} /> },
    ],
  },
];

export function AppearanceMenu({ className }: { className?: string }) {
  return (
    <Menu.Root>
      <Menu.Trigger className={cn(buttonVariants({ variant: "ghost", size: "icon" }), className)} aria-label="Appearance" title="Appearance: theme, high contrast, density and motion">
        <Icon of={SunMoon} size={20} />
      </Menu.Trigger>
      <Menu.Portal>
        <Menu.Positioner sideOffset={4} align="end" className="z-(--z-popover)">
          <Menu.Popup data-slot="menu-popup" className="min-w-56 rounded-lg border border-border bg-popover p-1 text-popover-foreground shadow-2 outline-none">
            {groups.map((g) => (
              <Group key={g.id} {...g} />
            ))}
          </Menu.Popup>
        </Menu.Positioner>
      </Menu.Portal>
    </Menu.Root>
  );
}

// AppearanceChoices are the same four choices as segmented controls (a radio group each, arrow keys between its
// options), for the compact More sheet, where the header has no room for the menu (FR-UX-37; docs/design/screens
// AppShell, the More sheet).
export function AppearanceChoices() {
  const a = useAppearance();
  return (
    <div className="fy-stack fy-stack--3">
      {groups.map((g) => {
        const e = entry(g.id);
        const h = helpText(e.help);
        const pick = (v: string) => setAppearance({ [g.field]: v } as Partial<Appearance>);
        return (
          <div key={g.id} className="fy-row fy-row--between fy-row--wrap gap-2">
            <span className="fy-small" id={`more-${g.id}`}>
              {h.label}
              {e.mode === "expert" && <span className="ms-2 rounded-sm border border-border px-1 text-xs">Expert</span>}
            </span>
            <div className="fy-segmented" role="radiogroup" aria-labelledby={`more-${g.id}`} title={h.tooltip}>
              {g.options.map((o, i) => {
                const on = a[g.field] === o.value;
                return (
                  <button key={o.value} type="button" role="radio" aria-checked={on} tabIndex={on ? 0 : -1} data-value={o.value} onClick={() => pick(o.value)}
                    onKeyDown={(ev) => {
                      const step = ev.key === "ArrowRight" || ev.key === "ArrowDown" ? 1 : ev.key === "ArrowLeft" || ev.key === "ArrowUp" ? -1 : 0;
                      if (!step) return;
                      ev.preventDefault();
                      const next = g.options[(i + step + g.options.length) % g.options.length]!;
                      pick(next.value);
                      (ev.currentTarget.parentElement?.querySelector(`[data-value="${next.value}"]`) as HTMLElement | null)?.focus();
                    }}>
                    {o.label}
                  </button>
                );
              })}
            </div>
          </div>
        );
      })}
    </div>
  );
}
