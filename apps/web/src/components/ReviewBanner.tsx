import { TriangleAlert } from "lucide-react";
import register from "../../../../docs/prd/reviews/review-status.json";
import { helpText } from "../i18n";
import { Icon } from "../ui";

// ReviewBanner is the "Needs additional verification" caveat (FR-UX-41, QA-UI-09): while an area of
// rules awaits its second review, every screen the review register lists for it says so at the top,
// with who reviews it and what to check. The register (docs/prd/reviews/review-status.json) is read
// at build time; the first release after an area's review merges drops its banner.
export function reviewsFor(screen: string) {
  return register.areas.filter((a) => a.status === "pending" && a.screens.includes(screen) && !register.never.includes(screen));
}

export function ReviewBanner({ screen }: { screen: string }) {
  const areas = reviewsFor(screen);
  if (areas.length === 0) return null;
  const h = helpText(register.help_key);
  return (
    <div className="fy-banner fy-banner--warning" role="note" aria-label={register.heading}>
      <Icon of={TriangleAlert} size={20} />
      <div className="fy-banner__text">
        <span className="fy-strong" title={h.tooltip}>
          {register.heading}
        </span>
        {areas.map((a) => (
          <span key={a.id} className="fy-small">
            {a.short}. {a.message}
          </span>
        ))}
      </div>
    </div>
  );
}
