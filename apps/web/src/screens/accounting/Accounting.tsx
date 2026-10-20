import { Link } from "@tanstack/react-router";
import { BookOpen, CalendarRange, ChevronRight, NotebookPen, Scale, ScrollText, type LucideIcon } from "lucide-react";
import { helpText } from "../../i18n";
import { Icon } from "../../ui";

const pages: { to: string; help: string; icon: LucideIcon }[] = [
  { to: "/accounting/journals", help: "accounting.journals", icon: NotebookPen },
  { to: "/accounting/close", help: "accounting.periods", icon: CalendarRange },
  { to: "/accounting/opening-balances", help: "accounting.opening-balances", icon: Scale },
  { to: "/reports/general-ledger", help: "reports.general-ledger", icon: ScrollText },
  { to: "/accounting/chart-of-accounts", help: "accounting.chart", icon: BookOpen },
];

// Accounting is the Accounting workspace's first page (docs/design/screens: the breadcrumb's "Accounting"): its
// pages, each with what it is for. Schedules join with their task.
export function Accounting() {
  return (
    <div className="fy-page fy-page--narrow fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">Accounting</h1>
          <p className="fy-secondary">Journals, the periods they post in, and the accounts they post to.</p>
        </div>
      </div>
      <nav className="fy-card" aria-label="Accounting pages">
        <ul className="fy-list">
          {pages.map((p) => {
            const h = helpText(p.help);
            return (
              <li key={p.to}>
                <Link className="fy-list__item" to={p.to}>
                  <span className="fy-list__icon">
                    <Icon of={p.icon} size={20} />
                  </span>
                  <span className="fy-list__main">
                    <span className="fy-list__title">{h.label}</span>
                    <span className="fy-list__meta whitespace-normal">{h.tooltip}</span>
                  </span>
                  <Icon of={ChevronRight} />
                </Link>
              </li>
            );
          })}
        </ul>
      </nav>
    </div>
  );
}
