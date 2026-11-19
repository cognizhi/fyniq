import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { t } from "../i18n";

type Row = components["schemas"]["RequestRow"];
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("steps.failed");

// RequestSteps is every request of the person in one list with where it is: received, with HR, ready or closed, the day it is due and the reply (NM-ESS-04). A change of details
// or a question that nobody has answered can be withdrawn here; letters and flexible work are withdrawn where they are made.
export function RequestSteps() {
  const [rows, setRows] = useState<Row[] | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const load = () =>
    api.GET("/v1/me/requests").then(
      ({ data }) => setRows(data?.requests ?? []),
      () => setRows([]),
    );
  useEffect(() => {
    void load();
  }, []);
  const withdraw = async (id: string) => {
    const { error } = await api.POST("/v1/me/requests/{id}/withdrawal", { params: { path: { id } } });
    setNote(error ? problem(error) : t("steps.withdrawn"));
    if (!error) await load();
  };
  return (
    <section className="fy-stack fy-stack--2" aria-labelledby="steps-h">
      <h2 className="fy-h2" id="steps-h">{t("steps.title")}</h2>
      {rows !== null && rows.length === 0 && <p className="fy-secondary">{t("steps.none")}</p>}
      <ul className="fy-stack fy-stack--2">
        {(rows ?? []).map((r) => (
          <li key={r.id} className="fy-stack fy-stack--1">
            <span className="fy-row" style={{ gap: "var(--space-3)" }}>
              <span className="fy-strong">{t(`steps.kind.${r.kind}`, { title: r.kind === "details_change" ? t(`steps.change.${r.title}`) : r.title })}</span>
              <span className="fy-chip">{t(`steps.step.${r.step}`)}</span>
              <span className="fy-secondary">{t("steps.due", { due: r.due_on })}</span>
            </span>
            {r.reply && <span className="fy-secondary">{t("steps.reply", { reply: r.reply })}</span>}
            {(r.kind === "details_change" || r.kind === "question") && r.step === "received" && (
              <button className="fy-btn fy-btn--secondary" type="button" onClick={() => withdraw(r.id)}>{t("steps.withdraw")}</button>
            )}
          </li>
        ))}
      </ul>
      {note && <p role="status" className="fy-secondary">{note}</p>}
    </section>
  );
}
