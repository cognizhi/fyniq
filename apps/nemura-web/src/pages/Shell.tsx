import { LogOut } from "lucide-react";
import type { ReactNode } from "react";
import { helpText, t } from "../i18n";

// Shell is the frame of a signed-in page: the brand, the way to the people and home, sign-out as a form post (no script holds the session), and the main area.
export function Shell({ children, directory, features, roles }: { children: ReactNode; directory?: boolean; features?: boolean; roles?: readonly string[] }) {
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
            <a href="/me/leave">{t("nav.leave")}</a>
            <a href="/me/leave/new">{t("nav.apply")}</a>
            <a href="/me/requests">{t("nav.requests")}</a>
            <a href="/me/details">{t("nav.details")}</a>
            <a href="/me/ask/hr">{t("nav.ask")}</a>
            <a href="/me/letters/new">{t("letters.ask")}</a>
            {directory && <a href="/people/requests">{t("nav.queue")}</a>}
            {directory && <a href="/people/onboarding">{t("nav.onboarding")}</a>}
            {payroll(roles ?? []) && <a href="/people/payroll">{t("nav.payroll")}</a>}
            {directory && <a href="/people/letters">{t("nav.letters")}</a>}
            {directory && <a href="/settings/letters">{t("nav.letterSettings")}</a>}
            {approver(roles ?? []) && <a href="/people/leave">{t("nav.teamLeave")}</a>}
            {directory && <a href="/people/leave/government-paid">{t("nav.gov")}</a>}
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

// approver is whether the roles decide leave requests: a line manager for their reports, the HR officer and administrator, the Owner. The API decides; this only shows the link.
export const approver = (roles: readonly string[]): boolean => roles.some((r) => ["line_manager", "hr_officer", "hr_admin", "owner"].includes(r));

// team is whether the roles read every person's record: the people team's (Appendix B). The API decides; this only shows the link.
export const team = (roles: readonly string[]): boolean => roles.some((r) => ["hr_officer", "hr_admin", "payroll_admin", "owner"].includes(r));

// admin is whether the roles switch features: the HR administrator and the Owner (Appendix B). The API decides; this only shows the link.
export const admin = (roles: readonly string[]): boolean => roles.some((r) => ["hr_admin", "owner"].includes(r));

// hr is whether the roles add people: the HR officer and the HR administrator (Appendix B). The API decides; this only shows the button.
export const hr = (roles: readonly string[]): boolean => roles.some((r) => ["hr_officer", "hr_admin"].includes(r));

// payroll reports whether the roles see pay: the Payroll administrator and the Owner alone.
export const payroll = (roles: readonly string[]): boolean => roles.some((r) => r === "payroll_admin" || r === "owner");
