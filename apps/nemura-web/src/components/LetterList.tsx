import { useEffect, useState } from "react";
import { api } from "../api/client";
import { confirmWithPasskey } from "../api/stepup";
import type { components } from "../api/schema";
import { t } from "../i18n";

type Request = components["schemas"]["LetterRequest"];
type Note = { ok: boolean; text: string } | null;
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("letters.failed");

// open shows a PDF the API returned as base64 in a new tab, without a file on a disk.
function open(base64: string): void {
  const bytes = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0));
  window.open(URL.createObjectURL(new Blob([bytes], { type: "application/pdf" })), "_blank", "noopener");
}

// LetterList lists letter requests and what each caller may do with them: the person withdraws and downloads their own; the people team checks, reads and refuses; the signatory
// reads the exact letter, sees its hash and signs with a passkey (NM-LTR-03, NM-LTR-07). The API decides who may do what; a button the caller may not use is refused in words.
export function LetterList({ mine, roles }: { mine: boolean; roles: readonly string[] }) {
  const [list, setList] = useState<Request[]>([]);
  const [note, setNote] = useState<Note>(null);
  const [reasons, setReasons] = useState<Record<string, string>>({});
  const staff = !mine && roles.some((r) => ["hr_officer", "hr_admin", "owner"].includes(r));
  const signer = !mine && roles.includes("owner");
  const load = async () => {
    try {
      const { data } = await api.GET("/v1/letters", { params: { query: { mine } } });
      setList(data?.requests ?? []);
    } catch {
      setList([]);
    }
  };
  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mine]);
  const done = async (error: unknown, ok: string) => {
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: ok });
    if (!error) await load();
  };
  const check = async (r: Request) => {
    const { data, error } = await api.POST("/v1/letters/{id}/check", { params: { path: { id: r.id } } });
    await done(error, t("letters.checked", { reference: data?.reference ?? "" }));
  };
  const read = async (r: Request) => {
    const { data, error } = await api.GET("/v1/letters/{id}/draft", { params: { path: { id: r.id } } });
    if (data) open(data.content_base64);
    else setNote({ ok: false, text: problem(error) });
  };
  const sign = async (r: Request) => {
    const failed = await confirmWithPasskey().catch(() => "");
    if (failed !== undefined) {
      setNote({ ok: false, text: failed || t("letters.passkeyFailed") });
      return;
    }
    const { error } = await api.POST("/v1/letters/{id}/signature", { params: { path: { id: r.id } }, body: { sha256: r.sha256 ?? "" } });
    await done(error, t("letters.signed"));
  };
  const refuse = async (r: Request) => {
    const { error } = await api.POST("/v1/letters/{id}/refusal", { params: { path: { id: r.id } }, body: { reason: reasons[r.id] ?? "" } });
    await done(error, t("letters.refused"));
  };
  const file = async (r: Request) => {
    const { data, error } = await api.GET("/v1/letters/{id}/file", { params: { path: { id: r.id } } });
    if (data) open(data.content_base64);
    else setNote({ ok: false, text: problem(error) });
  };
  const link = async (r: Request) => {
    const { data, error } = await api.POST("/v1/letters/{id}/link", { params: { path: { id: r.id } } });
    setNote(data ? { ok: true, text: t("letters.linkMade", { url: `${window.location.origin}/api/v1/letter-links/download?token=${data.token}`, when: data.expires_at }) } : { ok: false, text: problem(error) });
  };
  const withdraw = async (r: Request) => {
    const { error } = await api.POST("/v1/letters/{id}/withdrawal", { params: { path: { id: r.id } } });
    await done(error, t("letters.withdrawn"));
  };
  return (
    <section className="fy-stack fy-stack--2" aria-labelledby={mine ? "myletters-h" : "letters-h"}>
      <h2 className="fy-h2" id={mine ? "myletters-h" : "letters-h"}>{t(mine ? "letters.mine" : "letters.title")}</h2>
      {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
      {list.length === 0 ? <p className="fy-secondary">{t(mine ? "letters.noneMine" : "letters.none")}</p> : (
        <ul className="fy-stack fy-stack--2">
          {list.map((r) => (
            <li key={r.id} className="fy-stack fy-stack--1">
              <span>{t("letters.line", { name: r.person_name, type: r.type, purpose: r.purpose, due: r.due_on })} <strong>{t(`letters.state.${r.state}`)}</strong></span>
              {r.reference && <span className="fy-secondary">{t("letters.reference", { reference: r.reference })}</span>}
              {r.state === "checked" && r.sha256 && signer && <span className="fy-muted">{t("letters.hash", { sha: r.sha256 })}</span>}
              {r.decision_reason && <span className="fy-secondary">{r.decision_reason}</span>}
              <div className="fy-row" style={{ gap: "var(--space-2)", flexWrap: "wrap" }}>
                {staff && r.state === "requested" && <button className="fy-btn fy-btn--primary" type="button" onClick={() => check(r)}>{t("letters.check")}</button>}
                {staff && r.state === "checked" && <button className="fy-btn fy-btn--secondary" type="button" onClick={() => read(r)}>{t("letters.read")}</button>}
                {signer && r.state === "checked" && <button className="fy-btn fy-btn--primary" type="button" onClick={() => sign(r)}>{t("letters.sign")}</button>}
                {staff && (r.state === "requested" || r.state === "checked") && (
                  <>
                    <input className="fy-input" aria-label={t("letters.refuseReason", { name: r.person_name, type: r.type })} value={reasons[r.id] ?? ""} maxLength={500} onChange={(e) => setReasons({ ...reasons, [r.id]: e.target.value })} />
                    <button className="fy-btn fy-btn--secondary" type="button" onClick={() => refuse(r)}>{t("letters.refuse")}</button>
                  </>
                )}
                {r.state === "issued" && <button className="fy-btn fy-btn--primary" type="button" onClick={() => file(r)}>{t("letters.download")}</button>}
                {r.state === "issued" && <button className="fy-btn fy-btn--secondary" type="button" onClick={() => link(r)}>{t("letters.link")}</button>}
                {mine && (r.state === "requested" || r.state === "checked") && <button className="fy-btn fy-btn--secondary" type="button" onClick={() => withdraw(r)}>{t("letters.withdraw")}</button>}
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
