import { TriangleAlert } from "lucide-react";
import { helpText, t } from "../i18n";
import { review, reviewsFor, type Register } from "../review";

export function ReviewBanner({ screen, register = review }: { screen: string; register?: Register }) {
  const areas = reviewsFor(screen, register);
  if (areas.length === 0) return null;
  return (
    <div className="fy-banner fy-banner--warning" role="note" aria-label={t("review.heading")}>
      <TriangleAlert size={20} aria-hidden="true" />
      <div className="fy-banner__text">
        <span className="fy-strong" title={helpText(register.help_key).tooltip}>
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
