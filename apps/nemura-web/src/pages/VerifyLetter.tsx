import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { helpText, t } from "../i18n";

type Answer = components["schemas"]["LetterVerificationView"];
type Disclosure = components["schemas"]["LetterDisclosureView"];

// sha256Hex is the fingerprint of a file's exact bytes, worked out here so the file itself never leaves the recipient's device (NM-LTR-05).
async function sha256Hex(file: File): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", await file.arrayBuffer());
  return Array.from(new Uint8Array(digest), (b) => b.toString(16).padStart(2, "0")).join("");
}

// VerifyLetter is nm-verify-letter (NM-LTR-05): anyone with a letter in hand enters its reference and the code printed on it, or chooses the file to compare its fingerprint, and learns whether the company
// issued it and it stands. It shows the letter's kind, the day it was issued and the company, and nothing of the person or the words; a wrong code and an unknown reference are answered alike. When it is opened
// from a link the person shared (the token is in the address after the #, so it is never sent in a request line or logged), it also shows the letter's facts about them until the link ends. It needs no sign-in and is not indexed.
export function VerifyLetter() {
  const [reference, setReference] = useState("");
  const [code, setCode] = useState("");
  const [answer, setAnswer] = useState<Answer | null>(null);
  const [error, setError] = useState("");
  const [shared, setShared] = useState<Disclosure | null>(null);
  const token = typeof window === "undefined" ? "" : window.location.hash.replace(/^#/, "");
  useEffect(() => {
    if (!token) return;
    void api.POST("/v1/letter-disclosure", { body: { token } }).then(({ data, error: failed }) => {
      if (failed) setError((failed as { detail?: string }).detail ?? t("verify.failed"));
      else setShared(data ?? { found: false });
    });
  }, [token]);
  const settle = ({ data, error: failed }: { data?: Answer; error?: unknown }) => {
    if (failed) setError((failed as { detail?: string }).detail ?? t("verify.failed"));
    else setAnswer(data ?? null);
  };
  const look = async (ev: React.FormEvent) => {
    ev.preventDefault();
    setError("");
    setAnswer(null);
    settle(await api.POST("/v1/letter-verification", { body: { reference, code } }));
  };
  const compare = async (ev: React.ChangeEvent<HTMLInputElement>) => {
    const file = ev.target.files?.[0];
    if (!file) return;
    setError("");
    setAnswer(null);
    let sha256: string;
    try {
      sha256 = await sha256Hex(file);
    } catch {
      setError(t("verify.fileFailed"));
      return;
    }
    settle(await api.POST("/v1/letter-verification/file", { body: { sha256 } }));
  };
  const facts = (a: Answer) => t("verify.facts", { type: t(`verify.type.${a.type ?? "other"}`), on: a.issued_on ?? "", company: a.company ?? "" });
  return (
    <main className="nm-signin__main" id="main">
      <meta name="robots" content="noindex" />
      <div className="nm-signin__card">
        <div className="fy-stack fy-stack--2">
          <h1 className="fy-h1" title={helpText("verify.page").tooltip}>{t("verify.title")}</h1>
          <p className="fy-secondary">{t("verify.lead")}</p>
        </div>
        {shared && !shared.found && <p role="alert">{t("verify.disclosureNone")}</p>}
        {shared?.found && (
          <section className="fy-stack fy-stack--2" aria-labelledby="disclosure-h">
            <h2 className="fy-h2" id="disclosure-h">{t("verify.disclosureTitle")}</h2>
            <p>{facts({ found: true, type: shared.type, issued_on: shared.issued_on, company: shared.company })}</p>
            {shared.status === "withdrawn" ? (
              <p role="alert"><span className="fy-chip fy-chip--critical">{t("verify.withdrawn")}</span> {t("verify.disclosureWithdrawn")}</p>
            ) : (
              <>
                <p className="fy-secondary">{t("verify.disclosureLead", { when: shared.expires_at ?? "" })}</p>
                <ul className="fy-stack fy-stack--1">
                  {(shared.facts ?? []).map((f) => <li key={f.label}>{t("verify.disclosureFact", { label: f.label, value: f.value })}</li>)}
                </ul>
              </>
            )}
          </section>
        )}
        <form className="fy-stack fy-stack--3" onSubmit={look}>
          <label className="fy-field">{t("verify.reference")}<input className="fy-input" value={reference} onChange={(e) => setReference(e.target.value)} required autoComplete="off" /></label>
          <label className="fy-field">{t("verify.code")}<input className="fy-input" value={code} onChange={(e) => setCode(e.target.value)} required autoComplete="off" /></label>
          <button className="fy-btn fy-btn--primary" type="submit">{t("verify.button")}</button>
        </form>
        <div className="fy-stack fy-stack--2">
          <p className="fy-secondary">{t("verify.fileLead")}</p>
          <label className="fy-field" title={helpText("verify.file").tooltip}>{t("verify.file")}<input className="fy-input" type="file" accept="application/pdf,.pdf" onChange={compare} /></label>
        </div>
        {error && <p role="alert" className="fy-secondary">{error}</p>}
        {answer && !answer.found && <p role="status">{t("verify.notFound")}</p>}
        {answer?.found && answer.status === "valid" && <p role="status"><span className="fy-chip fy-chip--good">{t("verify.valid")}</span> {facts(answer)}</p>}
        {answer?.found && answer.status === "withdrawn" && <p role="alert"><span className="fy-chip fy-chip--critical">{t("verify.withdrawn")}</span> {facts(answer)}</p>}
      </div>
    </main>
  );
}
