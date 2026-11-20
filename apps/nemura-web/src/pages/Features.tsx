import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t, textKeys } from "../i18n";
import { Shell, admin, team } from "./Shell";

type Feature = components["schemas"]["Feature"];
type State = { status: "loading" } | { status: "refused" } | { status: "ready"; features: Feature[] };

// Features is nm-features: each optional feature on, off or held by another system (NM-FEAT-01, NM-FEAT-03). The API enforces the dependencies and says why in plain
// words; this page shows its answer and never decides.
export function Features({ roles }: { roles: readonly string[] }) {
  const [state, set] = useState<State>({ status: "loading" });
  const reload = () =>
    api.GET("/v1/features").then(
      ({ data }) => set(data ? { status: "ready", features: data.features ?? [] } : { status: "refused" }),
      () => set({ status: "refused" }),
    );
  useEffect(() => {
    void reload();
  }, []);
  return (
    <Shell roles={roles} directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-features" />
        <h1 className="fy-h1" title={helpText("features.page").tooltip}>{t("features.title")}</h1>
        <p className="fy-secondary">{t("features.lead")}</p>
        {state.status === "loading" && <p role="status" className="fy-secondary">{t("features.loading")}</p>}
        {state.status === "refused" && <p role="alert" className="fy-secondary">{t("features.none")}</p>}
        {state.status === "ready" && (
          <table className="fy-table">
            <thead>
              <tr>
                <th scope="col">{t("features.feature")}</th>
                <th scope="col">{t("features.state")}</th>
                <th scope="col">{t("features.system")}</th>
                <th scope="col">{t("features.needs")}</th>
                <th scope="col"><span className="fy-sr-only">{t("features.apply")}</span></th>
              </tr>
            </thead>
            <tbody>
              {state.features.map((f) => (
                <Row key={f.id} f={f} canSet={admin(roles)} saved={reload} />
              ))}
            </tbody>
          </table>
        )}
      </main>
    </Shell>
  );
}

const label = (id: string, fallback: string): string => (textKeys.includes(`feature.${id}`) ? t(`feature.${id}`) : fallback);

function Row({ f, canSet, saved }: { f: Feature; canSet: boolean; saved: () => unknown }) {
  const [st, setSt] = useState(f.state);
  const [system, setSystem] = useState(f.external_system ?? "");
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    setBusy(true);
    setMsg(null);
    try {
      const { error } = await api.PUT("/v1/features/{feature}", { params: { path: { feature: f.id } }, body: { state: st, external_system: st === "external" ? system : undefined } });
      if (error) setMsg({ ok: false, text: (error as { detail?: string }).detail ?? t("features.failed") });
      else {
        setMsg({ ok: true, text: t("features.saved") });
        await saved();
      }
    } catch {
      setMsg({ ok: false, text: t("features.failed") });
    } finally {
      setBusy(false);
    }
  };
  const id = `feature-${f.id}`;
  return (
    <tr>
      <th scope="row">
        {label(f.id, f.name)}
        {!f.built && <span className="fy-small fy-muted"> {t("features.later", { phase: f.phase })}</span>}
      </th>
      <td>
        <select className="fy-input" id={id} aria-label={`${label(f.id, f.name)}: ${t("features.state")}`} value={st} disabled={!canSet} onChange={(e) => setSt(e.target.value as Feature["state"])}>
          {(["on", "off", "external"] as const).map((s) => (
            <option key={s} value={s}>{t(`state.${s}`)}</option>
          ))}
        </select>
      </td>
      <td>
        {st === "external" && (
          <input className="fy-input" aria-label={`${label(f.id, f.name)}: ${t("features.system")}`} value={system} maxLength={100} disabled={!canSet} onChange={(e) => setSystem(e.target.value)} />
        )}
      </td>
      <td>{(f.needs ?? []).map((n) => label(n, n)).join(", ")}</td>
      <td>
        {canSet && (
          <button className="fy-btn fy-btn--secondary" type="button" disabled={busy || (st === f.state && system === (f.external_system ?? ""))} onClick={() => void submit()}>
            {busy ? t("features.applying") : t("features.apply")}
          </button>
        )}
        {msg && <p role={msg.ok ? "status" : "alert"} className="fy-small">{msg.text}</p>}
      </td>
    </tr>
  );
}
