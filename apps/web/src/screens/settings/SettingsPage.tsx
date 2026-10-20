import { Link, useRouterState } from "@tanstack/react-router";
import { ChevronLeft, Sparkles } from "lucide-react";
import type { ReactNode } from "react";
import { Button } from "../../components/ui/button";
import { helpText } from "../../i18n";
import { setMode } from "../../mode";
import { settingsNav, type NavFeature } from "../../registry";
import { useSignedIn } from "../../session";
import { navFor } from "../../shell/navigation";
import { Icon } from "../../ui";

const groups = [
  ["you", "Your settings"],
  ["organisation", "Organisation"],
] as const;

// useSettingsNav is the Settings pages the person may open in their mode, and how many more Expert
// mode would show them (FR-UX-26: Basic says that more is there, and how to reach it).
export function useSettingsNav(): { visible: NavFeature[]; hidden: number } {
  const { session, mode } = useSignedIn();
  const visible = navFor(settingsNav, session, mode);
  return { visible, hidden: navFor(settingsNav, session, "expert").length - visible.length };
}

// SettingsList is the list of Settings pages, by section: the Settings page itself in the compact
// class and the side navigation of every Settings page from medium (docs/design/screens UsersRoles).
export function SettingsList({ compact }: { compact?: boolean }) {
  const path = useRouterState({ select: (s) => s.location.pathname });
  const { visible, hidden } = useSettingsNav();
  return (
    <div className="fy-stack fy-stack--3">
      {groups.map(([g, title]) => {
        const items = visible.filter((n) => n.group === g);
        return items.length === 0 ? null : (
          <div key={g} className="fy-stack fy-stack--2">
            <span className="fy-eyebrow px-2.5">{title}</span>
            <ul className="fy-list">
              {items.map((n) => {
                const h = helpText(n.help);
                return (
                  <li key={n.id}>
                    <Link className={compact ? "fy-list__item" : "fy-list__item min-h-9 gap-2.5 rounded-md border-b-0 px-2.5 py-1.5"} to={n.route} aria-current={path === n.route ? "page" : undefined}>
                      <Icon of={n.icon} />
                      <span className="fy-list__main">
                        <span className="fy-list__title">{h.label}</span>
                        {compact && <span className="fy-help">{h.tooltip}</span>}
                      </span>
                    </Link>
                  </li>
                );
              })}
            </ul>
          </div>
        );
      })}
      {hidden > 0 && (
        <div className="fy-stack fy-stack--2 px-2.5">
          <p className="fy-small fy-secondary">
            {hidden === 1 ? "One more page is" : `${hidden} more pages are`} in Expert mode: the organisation's settings your roles let you see.
          </p>
          <Button size="sm" onClick={() => setMode("expert")}>
            <Icon of={Sparkles} />
            Switch to Expert
          </Button>
        </div>
      )}
    </div>
  );
}

// SettingsPage is the frame of each Settings page: the side navigation from medium, the way back to
// all settings in compact, and the page head.
export function SettingsPage({ title, lead, actions, children }: { title: string; lead?: ReactNode; actions?: ReactNode; children: ReactNode }) {
  return (
    <div className="fy-page fy-enter">
      <Link className="fy-btn fy-btn--ghost fy-btn--sm fy-only-compact -ms-2 self-start" to="/settings">
        <Icon of={ChevronLeft} />
        All settings
      </Link>
      <div className="fy-row fy-row--top fy-row--wrap gap-6">
        <nav className="fy-from-medium sticky top-18 flex-[0_0_208px]" aria-label="Settings">
          <SettingsList />
        </nav>
        <div className="fy-stack fy-stack--6 min-w-0 flex-[1_1_300px]">
          <div className="fy-pagehead">
            <div className="fy-pagehead__text">
              <span className="fy-eyebrow">Settings</span>
              <h1 className="fy-h1">{title}</h1>
              {lead && <p className="fy-secondary">{lead}</p>}
            </div>
            {actions && <div className="fy-pagehead__actions">{actions}</div>}
          </div>
          {children}
        </div>
      </div>
    </div>
  );
}
