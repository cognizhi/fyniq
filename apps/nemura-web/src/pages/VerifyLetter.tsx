import { useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { helpText, t } from "../i18n";

type Answer = components["schemas"]["LetterVerificationView"];

// VerifyLetter is nm-verify-letter (NM-LTR-05): anyone with a letter in hand enters its reference and the code printed on it and learns whether the company issued it and it stands. It shows the letter's kind, the
// day it was issued and the company, and nothing of the person or the words; a wrong code and an unknown reference are answered alike. It needs no sign-in and is not indexed.
export function VerifyLetter() {
  const [reference, setReference] = useState("");
  const [code, setCode] = useState("");
  const [answer, setAnswer] = useState<Answer | null>(null);
  const [error, setError] = useState("");
  const look = async (ev: React.FormEvent) => {
    ev.preventDefault();
    setError("");
    setAnswer(null);
    const { data, error: failed } = await api.POST("/v1/letter-verification", { body: { reference, code } });
    if (failed) setError((failed as { detail?: string }).detail ?? t("verify.failed"));
    else setAnswer(data ?? null);
  };
  return (
    <main className="nm-signin__main" id="main">
      <meta name="robots" content="noindex" />
      <div className="nm-signin__card">
        <div className="fy-stack fy-stack--2">
          <h1 className="fy-h1" title={helpText("verify.page").tooltip}>{t("verify.title")}</h1>
          <p className="fy-secondary">{t("verify.lead")}</p>
        </div>
        <form className="fy-stack fy-stack--3" onSubmit={look}>
          <label className="fy-field">{t("verify.reference")}<input className="fy-input" value={reference} onChange={(e) => setReference(e.target.value)} required autoComplete="off" /></label>
          <label className="fy-field">{t("verify.code")}<input className="fy-input" value={code} onChange={(e) => setCode(e.target.value)} required autoComplete="off" /></label>
          <button className="fy-btn fy-btn--primary" type="submit">{t("verify.button")}</button>
        </form>
        {error && <p role="alert" className="fy-secondary">{error}</p>}
        {answer && !answer.found && <p role="status">{t("verify.notFound")}</p>}
        {answer?.found && answer.status === "valid" && <p role="status"><span className="fy-chip fy-chip--good">{t("verify.valid")}</span> {t("verify.facts", { type: t(`verify.type.${answer.type ?? "other"}`), on: answer.issued_on ?? "", company: answer.company ?? "" })}</p>}
        {answer?.found && answer.status === "withdrawn" && <p role="alert"><span className="fy-chip fy-chip--critical">{t("verify.withdrawn")}</span> {t("verify.facts", { type: t(`verify.type.${answer.type ?? "other"}`), on: answer.issued_on ?? "", company: answer.company ?? "" })}</p>}
      </div>
    </main>
  );
}
