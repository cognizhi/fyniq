import { useEffect, useMemo, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, admin, hr, team } from "./Shell";

type Entry = components["schemas"]["DirectoryEntry"];
type State = { status: "loading" } | { status: "failed" } | { status: "ready"; people: Entry[] };

// Directory is nm-people: everyone who works here today, with their job and manager, nothing restricted (NM-PPL-06). A name opens the record for those who may read it.
export function Directory({ roles }: { roles: readonly string[] }) {
  const [state, set] = useState<State>({ status: "loading" });
  const [q, setQ] = useState("");
  useEffect(() => {
    let live = true;
    api.GET("/v1/people").then(
      ({ data }) => live && set(data ? { status: "ready", people: data.people ?? [] } : { status: "failed" }),
      () => live && set({ status: "failed" }),
    );
    return () => {
      live = false;
    };
  }, []);
  const shown = useMemo(() => {
    if (state.status !== "ready") return [];
    const words = q.toLowerCase().split(/\s+/).filter(Boolean);
    return state.people.filter((p) => words.every((w) => [p.name, p.preferred_name, p.job_title, p.department, p.manager_name].some((v) => v?.toLowerCase().includes(w))));
  }, [state, q]);
  const open = team(roles);
  return (
    <Shell roles={roles} directory={open} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-people" />
        <h1 className="fy-h1" title={helpText("directory.page").tooltip}>{t("directory.title")}</h1>
        <p className="fy-secondary">{t("directory.lead")}</p>
        {hr(roles) && <p><a className="fy-btn fy-btn--primary" href="/people/new">{t("new.add")}</a></p>}
        {state.status === "loading" && <p role="status" className="fy-secondary">{t("directory.loading")}</p>}
        {state.status === "failed" && <p role="alert" className="fy-secondary">{t("directory.failed")}</p>}
        {state.status === "ready" && state.people.length === 0 && <p role="status" className="fy-secondary">{t("directory.empty")}</p>}
        {state.status === "ready" && state.people.length > 0 && (
          <>
            <div className="fy-field">
              <label className="fy-label" htmlFor="directory-q">{t("directory.filter")}</label>
              <input className="fy-input" id="directory-q" type="search" value={q} onChange={(e) => setQ(e.target.value)} />
            </div>
            {shown.length === 0 ? (
              <p role="status" className="fy-secondary">{t("directory.none")}</p>
            ) : (
              <table className="fy-table">
                <thead>
                  <tr>
                    <th scope="col">{t("directory.name")}</th>
                    <th scope="col">{t("directory.job")}</th>
                    <th scope="col">{t("directory.department")}</th>
                    <th scope="col">{t("directory.manager")}</th>
                    <th scope="col">{t("directory.email")}</th>
                  </tr>
                </thead>
                <tbody>
                  {shown.map((p) => (
                    <tr key={p.id}>
                      <th scope="row">{open ? <a href={`/people/${p.id}`}>{p.name}</a> : p.name}</th>
                      <td>{p.job_title}</td>
                      <td>{p.department}</td>
                      <td>{p.manager_name}</td>
                      <td>{p.work_email}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </>
        )}
      </main>
    </Shell>
  );
}
