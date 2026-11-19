import type { Session } from "../api/client";
import { MyHomePanels } from "../components/MyHomePanels";
import { PeopleTiles } from "../components/PeopleTiles";
import { ReviewBanner } from "../components/ReviewBanner";
import { Shell, admin, team } from "./Shell";
import { helpText, t, textKeys } from "../i18n";

const roleLabel = (r: string): string => (textKeys.includes(`role.${r}`) ? t(`role.${r}`) : r);

// Home is the landing, by role (PRD-002 §3): My home for everyone but the people team, People home for them. Nothing of the company's finances
// is on it: the session names the person's roles and where they land, no more (NM-ROLE-02).
export function Home({ session, screen }: { session: Session; screen: "me" | "people" }) {
  const key = screen === "people" ? "home.people" : "home.me";
  const roles = session.roles ?? [];
  return (
    <Shell roles={roles} directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen={screen === "people" ? "nm-people-home" : "nm-my-home"} />
        <h1 className="fy-h1" title={helpText(key).tooltip}>{t(key)}</h1>
        <p className="fy-secondary">{t("home.hello")}</p>
        {screen === "people" && <PeopleTiles />}
        {screen === "me" && <MyHomePanels />}
        <section className="fy-stack fy-stack--2" aria-labelledby="roles-h">
          <h2 className="fy-h2" id="roles-h">{t("home.roles")}</h2>
          {roles.length === 0 ? (
            <p className="fy-secondary">{t("home.noRoles")}</p>
          ) : (
            <ul className="fy-row" style={{ gap: "var(--space-2)", flexWrap: "wrap" }}>
              {roles.map((r) => (
                <li key={r} className="fy-chip">{roleLabel(r)}</li>
              ))}
            </ul>
          )}
        </section>
        <p className="fy-muted">{t("home.soon")}</p>
      </main>
    </Shell>
  );
}
