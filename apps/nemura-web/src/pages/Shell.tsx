import { LogOut } from "lucide-react";
import type { ReactNode } from "react";
import { helpText, t } from "../i18n";

// Shell is the frame of a signed-in page: the brand, the way to the people and home, sign-out as a form post (no script holds the session), and the main area.
export function Shell({ children, directory, features }: { children: ReactNode; directory?: boolean; features?: boolean }) {
  return (
    <div className="nm-shell">
      <header className="nm-shell__bar">
        <span className="fy-row" style={{ gap: "var(--space-4)" }}>
          <span className="fy-brand">
            <span className="fy-brand__mark" aria-hidden="true">n</span>
            <span>{t("app.name").toLowerCase()}</span>
          </span>
          <nav aria-label={t("app.name")} className="fy-row" style={{ gap: "var(--space-3)" }}>
            <a href="/">{t("nav.home")}</a>
            {directory && <a href="/people/directory">{t("nav.directory")}</a>}
            {features && <a href="/settings/features">{t("nav.features")}</a>}
            {directory && <a href="/settings/packs">{t("nav.packs")}</a>}
            {directory && <a href="/settings/policy">{t("nav.policy")}</a>}
          </nav>
        </span>
        <form method="post" action="/api/auth/signout">
          <button className="fy-btn fy-btn--secondary" type="submit" title={helpText("nav.signOut").tooltip}>
            <LogOut size={16} aria-hidden="true" />
            {t("nav.signOut")}
          </button>
        </form>
      </header>
      {children}
    </div>
  );
}

// team is whether the roles read every person's record: the people team's (Appendix B). The API decides; this only shows the link.
export const team = (roles: readonly string[]): boolean => roles.some((r) => ["hr_officer", "hr_admin", "payroll_admin", "owner"].includes(r));

// admin is whether the roles switch features: the HR administrator and the Owner (Appendix B). The API decides; this only shows the link.
export const admin = (roles: readonly string[]): boolean => roles.some((r) => ["hr_admin", "owner"].includes(r));

// hr is whether the roles add people: the HR officer and the HR administrator (Appendix B). The API decides; this only shows the button.
export const hr = (roles: readonly string[]): boolean => roles.some((r) => ["hr_officer", "hr_admin"].includes(r));
