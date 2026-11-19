import { useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, admin, team } from "./Shell";

type Answer = components["schemas"]["AIAnswer"];
type Turn = { question: string; answer: Answer | null; error?: string };
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("askai.failed");

// AskAssistant is nm-ask (NM-AI-02, 03, 06): a person asks about their own leave, their pay day, a rule of the pack or a line of their payslip. Every answer says where it comes from and carries the AI badge;
// a payslip line is explained as a rule and the page shows the person's own figures beside it, so no pay figure is sent to a model. It changes nothing.
export function AskAssistant({ roles }: { roles: readonly string[] }) {
  const [question, setQuestion] = useState("");
  const [turns, setTurns] = useState<Turn[]>([]);
  const send = async (ev: React.FormEvent) => {
    ev.preventDefault();
    const q = question.trim();
    if (!q) return;
    setQuestion("");
    const { data, error } = await api.POST("/v1/assistant/ask", { body: { question: q } });
    setTurns([...turns, { question: q, answer: data ?? null, error: data ? undefined : problem(error) }]);
  };
  return (
    <Shell roles={roles} directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-ask" />
        <h1 className="fy-h1" title={helpText("askai.page").tooltip}>{t("askai.title")}</h1>
        <p className="fy-secondary">{t("askai.can")}</p>
        <p className="fy-secondary">{t("askai.cannot")}</p>
        <ul className="fy-stack fy-stack--2" aria-live="polite">
          {turns.map((x, i) => (
            <li key={i} className="fy-stack fy-stack--1">
              <p className="fy-strong">{t("askai.you", { question: x.question })}</p>
              {x.error && <p role="alert" className="fy-secondary">{x.error}</p>}
              {x.answer && <span className="fy-chip fy-chip--ai">{t("askai.ai")}</span>}
              {(x.answer?.segments ?? []).map((s, j) => (
                <p key={j}>{s.text} <span className="fy-secondary">{t("askai.sources", { sources: (s.sources ?? []).join("; ") })}</span></p>
              ))}
            </li>
          ))}
        </ul>
        <form className="fy-stack fy-stack--2" onSubmit={send} aria-label={t("askai.title")}>
          <label className="fy-field">
            <span>{t("askai.question")}</span>
            <input className="fy-input" maxLength={1000} value={question} onChange={(ev) => setQuestion(ev.target.value)} />
          </label>
          <button className="fy-btn fy-btn--primary" type="submit">{t("askai.send")}</button>
        </form>
        <a href="/me/ask/hr">{t("askai.hr")}</a>
      </main>
    </Shell>
  );
}
