import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, admin, team } from "./Shell";

type Type = components["schemas"]["LetterType"];
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("letters.failed");

// RequestLetter is nm-request-letter: the person chooses a letter the company issues and says what it is for (NM-LTR-01, NM-LTR-03). The API fills it from their record and
// names a fact the record does not hold; nothing here writes a letter.
export function RequestLetter({ roles }: { roles: readonly string[] }) {
  const [types, setTypes] = useState<Type[]>([]);
  const [type, setType] = useState("");
  const [purpose, setPurpose] = useState("");
  const [note, setNote] = useState<{ ok: boolean; text: string } | null>(null);
  useEffect(() => {
    api.GET("/v1/letters/types").then(
      ({ data }) => {
        const on = (data?.types ?? []).filter((x) => x.enabled && x.published_version > 0);
        setTypes(on);
        setType(on[0]?.key ?? "");
      },
      () => setTypes([]),
    );
  }, []);
  const send = async (e: React.FormEvent) => {
    e.preventDefault();
    const { data, error } = await api.POST("/v1/me/letters", { body: { type, purpose } });
    setNote(data ? { ok: true, text: t("request.sent", { due: data.due_on }) } : { ok: false, text: problem(error) });
  };
  return (
    <Shell roles={roles} directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-request-letter" />
        <h1 className="fy-h1" title={helpText("request.page").tooltip}>{t("request.title")}</h1>
        <p className="fy-secondary">{t("request.lead")}</p>
        {types.length === 0 ? (
          <p className="fy-secondary">{t("request.none")}</p>
        ) : (
          <form className="fy-stack fy-stack--2" onSubmit={send} aria-label={t("request.title")}>
            <label className="fy-field">
              <span>{t("request.type")}</span>
              <select className="fy-input" value={type} onChange={(e) => setType(e.target.value)}>
                {types.map((x) => (
                  <option key={x.key} value={x.key}>{x.title}</option>
                ))}
              </select>
            </label>
            <p className="fy-secondary">{types.find((x) => x.key === type)?.statement}</p>
            <label className="fy-field">
              <span>{t("request.purpose")}</span>
              <input className="fy-input" required maxLength={300} value={purpose} onChange={(e) => setPurpose(e.target.value)} />
            </label>
            <button className="fy-btn fy-btn--primary" type="submit">{t("request.send")}</button>
          </form>
        )}
        {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
        <a href="/me/requests">{t("letters.mine")}</a>
      </main>
    </Shell>
  );
}
