import { Link } from "@tanstack/react-router";

// The planning pages, as tabs under Planning (p1-planning design D12): Cash ahead in both modes, the rest in Expert.
const pages = [
  { to: "/planning/cash", label: "Cash ahead", expert: false },
  { to: "/planning/forecast", label: "Forecast", expert: true },
  { to: "/planning/budgets", label: "Budgets", expert: true },
  { to: "/planning/feasibility", label: "Feasibility", expert: true },
] as const;

// PlanningTabs links the planning pages, marking the one shown.
export function PlanningTabs({ current }: { current: (typeof pages)[number]["to"] }) {
  return (
    <nav className="fy-tabs" aria-label="Planning">
      {pages.map((p) => (
        <Link key={p.to} to={p.to} aria-current={p.to === current ? "page" : undefined} className={p.expert ? "fy-expert-only" : undefined}>
          {p.label}
        </Link>
      ))}
    </nav>
  );
}
