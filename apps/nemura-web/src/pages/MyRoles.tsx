import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, team } from "./Shell";

type Role = components["schemas"]["HiringRole"];

// MyRoles is nm-my-hiring-roles (NM-REC-01): the roles that name the person as hiring manager, each with where it stands and how many candidates are at each stage. It shows no salary and no candidate's name: the
// database function behind it returns nothing else.
export function MyRoles({ roles }: { roles: readonly string[] }) {
  const [list, setList] = useState<Role[]>([]);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    api.GET("/v1/me/hiring-roles").then(({ data, error }) => (setFailed(!data && !!error), setList(data?.roles ?? [])), () => setFailed(true));
  }, []);
  return (
    <Shell roles={roles} directory={team(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-my-hiring-roles" />
        <h1 className="fy-h1" title={helpText("roles.page").tooltip}>{t("roles.title")}</h1>
        <p className="fy-secondary">{t("roles.lead")}</p>
        {failed && <p role="alert">{t("roles.failed")}</p>}
        {!failed && list.length === 0 && <p className="fy-secondary">{t("roles.none")}</p>}
        <ul className="fy-stack fy-stack--2">
          {list.map((r) => (
            <li key={r.id} className="fy-card fy-card--pad fy-stack fy-stack--1">
              <span className="fy-strong">{t("roles.head", { title: r.title, department: r.department, state: t(`roles.state.${r.state}`) })}</span>
              <span className="fy-secondary">{t("roles.target", { on: r.target_start })}</span>
              {r.post_state && <span className="fy-secondary">{t("roles.post", { state: t(`roles.post.${r.post_state}`) })}</span>}
              {r.pass_needed && <span className="fy-secondary">{t("roles.pass")}</span>}
              <span>{t("roles.counts", { applied: String(r.candidates.applied), screening: String(r.candidates.screening), interview: String(r.candidates.interview), offer: String(r.candidates.offer), hired: String(r.candidates.hired), out: String(r.candidates.not_progressing) })}</span>
            </li>
          ))}
        </ul>
      </main>
    </Shell>
  );
}
