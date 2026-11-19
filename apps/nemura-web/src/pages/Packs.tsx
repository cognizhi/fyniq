import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, admin, team } from "./Shell";

type Version = components["schemas"]["PackVersion"];
type State = { status: "loading" } | { status: "refused" } | { status: "ready"; packs: Version[] };

// Packs is nm-country-pack: each pack version with its official sources, who verified it, and the researched values the company may decide to use (NM-PACK-02, NM-PACK-07).
export function Packs({ roles }: { roles: readonly string[] }) {
  const [state, set] = useState<State>({ status: "loading" });
  const reload = () =>
    api.GET("/v1/packs").then(
      ({ data }) => set(data ? { status: "ready", packs: data.packs ?? [] } : { status: "refused" }),
      () => set({ status: "refused" }),
    );
  useEffect(() => {
    void reload();
  }, []);
  return (
    <Shell directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-country-pack" />
        <h1 className="fy-h1" title={helpText("packs.page").tooltip}>{t("packs.title")}</h1>
        <p className="fy-secondary">{t("packs.lead")}</p>
        {state.status === "loading" && <p role="status" className="fy-secondary">{t("packs.loading")}</p>}
        {state.status === "refused" && <p role="alert" className="fy-secondary">{t("packs.none")}</p>}
        {state.status === "ready" && state.packs.map((v) => <PackVersion key={v.id} v={v} canSet={admin(roles)} saved={reload} />)}
      </main>
    </Shell>
  );
}

function PackVersion({ v, canSet, saved }: { v: Version; canSet: boolean; saved: () => unknown }) {
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const [reasons, setReasons] = useState<Record<string, string>>({});
  const report = (error: unknown) => setMsg(error ? { ok: false, text: (error as { detail?: string }).detail ?? t("packs.failed") } : { ok: true, text: t("packs.saved") });
  const accept = async () => {
    const { error } = await api.POST("/v1/packs/{version}/acceptances", { params: { path: { version: v.id } } });
    report(error);
    if (!error) await saved();
  };
  const decide = async (table: string, key: string) => {
    const { error } = await api.POST("/v1/packs/{version}/decisions", { params: { path: { version: v.id } }, body: { table, key, reason: reasons[`${table}/${key}`] ?? "" } });
    report(error);
    if (!error) await saved();
  };
  return (
    <section className="fy-stack fy-stack--3" aria-labelledby={`pack-${v.id}`}>
      <h2 className="fy-h2" id={`pack-${v.id}`}>{v.id}</h2>
      <p className="fy-secondary">{v.title}</p>
      <dl className="fy-stack fy-stack--1">
        <div className="fy-row" style={{ gap: "var(--space-3)" }}><dt className="fy-strong">{t("packs.from")}</dt><dd>{v.effective_from}</dd></div>
        <div className="fy-row" style={{ gap: "var(--space-3)" }}><dt className="fy-strong">{t("packs.review")}</dt><dd>{v.review_date}</dd></div>
        <div className="fy-row" style={{ gap: "var(--space-3)" }}><dt className="fy-strong">{t("packs.verifier")}</dt><dd>{v.verifier}</dd></div>
        <div className="fy-row" style={{ gap: "var(--space-3)" }}><dt className="fy-strong">{t("packs.researched")}</dt><dd>{v.researched_values}</dd></div>
      </dl>
      {v.accepted && <p className="fy-secondary">{t("packs.accepted")}</p>}
      {!v.accepted && canSet && (
        <button className="fy-btn fy-btn--secondary" type="button" onClick={() => void accept()}>
          {t("packs.accept")}
        </button>
      )}
      <p className="fy-small fy-muted">{t("packs.notice")}</p>
      {msg && <p role={msg.ok ? "status" : "alert"} className="fy-small">{msg.text}</p>}
      <h3 className="fy-h3">{t("packs.sources")}</h3>
      <ul className="fy-stack fy-stack--1">
        {(v.sources ?? []).map((s) => (
          <li key={s.id}>
            <a href={s.url} rel="noreferrer noopener" target="_blank">{s.title}</a> <span className="fy-small fy-muted">{s.section}; {t("packs.read")} {s.retrieved}</span>
            <blockquote className="fy-small">{s.quote}</blockquote>
          </li>
        ))}
      </ul>
      <h3 className="fy-h3">{t("packs.values")}</h3>
      <table className="fy-table">
        <thead>
          <tr><th scope="col">{t("packs.table")}</th><th scope="col">{t("packs.key")}</th><th scope="col">{t("packs.status")}</th><th scope="col">{t("packs.reason")}</th></tr>
        </thead>
        <tbody>
          {(v.values ?? []).map((x) => {
            const k = `${x.table}/${x.key}`;
            return (
              <tr key={k}>
                <th scope="row">{x.table}</th>
                <td>{x.key}</td>
                <td>{x.decided ? t("packs.decided") : t("packs.undecided")}</td>
                <td>
                  {!x.decided && canSet && (
                    <span className="fy-row" style={{ gap: "var(--space-2)" }}>
                      <input className="fy-input" aria-label={`${k}: ${t("packs.reason")}`} value={reasons[k] ?? ""} onChange={(e) => setReasons({ ...reasons, [k]: e.target.value })} />
                      <button className="fy-btn fy-btn--secondary" type="button" onClick={() => void decide(x.table, x.key)}>{t("packs.decide")}</button>
                    </span>
                  )}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </section>
  );
}
