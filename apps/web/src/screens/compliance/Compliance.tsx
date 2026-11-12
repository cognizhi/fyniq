import { Link } from "@tanstack/react-router";
import { CalendarClock, ChevronRight, ClipboardCheck, FileSearch, Megaphone, History, Landmark, ShieldCheck, TriangleAlert, type LucideIcon } from "lucide-react";
import { helpText } from "../../i18n";
import { routes } from "../../registry";
import { useSignedIn } from "../../session";
import { Icon } from "../../ui";

const pages: { to: string; help: string; icon: LucideIcon }[] = [
  { to: "/compliance/calendar", help: "compliance.calendar", icon: CalendarClock },
  { to: "/compliance/controls", help: "compliance.controls", icon: ShieldCheck },
  { to: "/compliance/attestations", help: "compliance.attestations", icon: ClipboardCheck },
  { to: "/compliance/risks", help: "compliance.risks", icon: TriangleAlert },
  { to: "/compliance/privacy/notices", help: "compliance.privacy-notices", icon: Megaphone },
  { to: "/compliance/gst", help: "compliance.gst", icon: Landmark },
  { to: "/compliance/evidence", help: "compliance.evidence", icon: FileSearch },
  { to: "/compliance/audit-trail", help: "compliance.audit-trail", icon: History },
];

// Compliance is the Compliance workspace's first page (docs/design/screens: the breadcrumb's "Compliance"; p1-compliance-
// center D9): its pages, each with what it is for. In Basic mode it lists the pages Basic shows; ⌘K still finds the rest.
export function Compliance() {
  const { mode } = useSignedIn();
  const shown = pages.filter((p) => mode === "expert" || routes.find((r) => r.path === p.to)?.mode === "basic");
  return (
    <div className="fy-page fy-page--narrow fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">Compliance</h1>
          <p className="fy-secondary">The statutory deadlines, the controls that keep the books and the platform in order, the GST position, the evidence kept and the audit trail.</p>
        </div>
      </div>
      <nav className="fy-card" aria-label="Compliance pages">
        <ul className="fy-list">
          {shown.map((p) => {
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
