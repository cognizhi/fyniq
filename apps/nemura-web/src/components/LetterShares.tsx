import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { helpText, t } from "../i18n";

type Share = components["schemas"]["LetterShare"];
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("letters.failed");
const lifetimes = ["1", "7", "14", "30"] as const;

// LetterShares is what a person does with their own issued letter besides downloading it (NM-LTR-05): withdraw its verification, or share what it says about them through a link that expires and that
// they can revoke. The API decides who may; a link is shown once, when it is made, and only a fingerprint of it is kept.
export function LetterShares({ issued, reference }: { issued: string; reference: string }) {
  const [links, setLinks] = useState<Share[]>([]);
  const [days, setDays] = useState<(typeof lifetimes)[number]>("7");
  const [note, setNote] = useState<{ ok: boolean; text: string } | null>(null);
  const load = async () => {
    try {
      const { data } = await api.GET("/v1/me/letters/{id}/disclosure-links", { params: { path: { id: issued } } });
      setLinks(data?.links ?? []);
    } catch {
      setLinks([]);
    }
  };
  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [issued]);
  const share = async () => {
    const { data, error } = await api.POST("/v1/me/letters/{id}/disclosure-links", { params: { path: { id: issued } }, body: { days: Number(days) } });
    setNote(data ? { ok: true, text: t("letters.shared", { url: `${window.location.origin}/verify#${data.token}`, when: data.expires_at }) } : { ok: false, text: problem(error) });
    if (data) await load();
  };
  const revoke = async (id: string) => {
    const { error } = await api.POST("/v1/me/letter-disclosure-links/{id}/revocation", { params: { path: { id } } });
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: t("letters.revokedNote") });
    if (!error) await load();
  };
  const unverify = async () => {
    const { error } = await api.POST("/v1/me/letters/{id}/verification/withdrawal", { params: { path: { id: issued } } });
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: t("letters.unverified") });
  };
  return (
    <div className="fy-stack fy-stack--2">
      <div className="fy-row" style={{ gap: "var(--space-2)", flexWrap: "wrap" }}>
        <label className="fy-field">
          <span className="fy-small">{t("letters.shareDays", { reference })}</span>
          <select className="fy-input" value={days} onChange={(e) => setDays(e.target.value as (typeof lifetimes)[number])}>
            {lifetimes.map((d) => <option key={d} value={d}>{t("letters.shareDaysOption", { days: d })}</option>)}
          </select>
        </label>
        <button className="fy-btn fy-btn--secondary" type="button" title={helpText("letters.share").tooltip} onClick={share}>{t("letters.share")}</button>
        <button className="fy-btn fy-btn--secondary" type="button" title={helpText("letters.unverify").tooltip} onClick={unverify}>{t("letters.unverify")}</button>
      </div>
      {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
      {links.length > 0 && (
        <section className="fy-stack fy-stack--1" aria-label={t("letters.shares", { reference })}>
          <ul className="fy-stack fy-stack--1">
            {links.map((l) => (
              <li key={l.id} className="fy-row" style={{ gap: "var(--space-2)" }}>
                <span className="fy-secondary">{l.revoked_at ? t("letters.shareRevoked", { when: l.revoked_at }) : t("letters.shareLine", { made: l.created_at, until: l.expires_at })}</span>
                {!l.revoked_at && <button className="fy-btn fy-btn--secondary" type="button" onClick={() => revoke(l.id)}>{t("letters.revoke")}</button>}
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}
