import { useQuery } from "@tanstack/react-query";
import { CircleCheck, TriangleAlert } from "lucide-react";
import { useId } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Icon, problemOf } from "../../ui";

type Readiness = components["schemas"]["Readiness"];
type Framework = Readiness["frameworks"][number];

async function readiness() {
  const { data, error } = await api.GET("/v1/compliance/readiness");
  if (error) throw error;
  return data;
}

const statusWords: Record<Framework["gaps"][number]["status"], string> = { fail: "Fails", overdue: "Overdue", not_assessed: "Not assessed" };

// ReadinessTab is Compliance › Controls › Readiness (CMP-08, rev 0.617): for each framework, how many mapped controls pass, fail, are overdue, are
// not assessed or do not apply, the controls that are not passing in plain words, and the ISO/IEC 27001:2022 Annex A numbers no control maps to.
// It is readiness, never a certificate.
export function ReadinessTab() {
  const id = useId();
  const r = useQuery({ queryKey: ["readiness"], queryFn: readiness });
  if (r.isError) return <Refusal problem={problemOf(r.error)} title="The readiness could not be shown" />;
  if (!r.data) return <p className="fy-small">Loading the readiness…</p>;
  const data: Readiness = r.data;
  return (
    <>
      <p className="fy-small fy-secondary">{data.notice}</p>
      {data.frameworks
        .filter((f) => f.controls > 0)
        .map((f) => (
          <section key={f.key} className="fy-card" aria-labelledby={`${id}-${f.key}`}>
            <div className="fy-card__head">
              <h2 className="fy-h3" id={`${id}-${f.key}`}>
                {f.title}
              </h2>
              <span className={f.fail + f.overdue + f.not_assessed === 0 ? "fy-chip fy-chip--good" : "fy-chip fy-chip--warning"}>
                <Icon of={f.fail + f.overdue + f.not_assessed === 0 ? CircleCheck : TriangleAlert} />
                {f.pass} of {f.controls} pass
              </span>
            </div>
            <p className="fy-card__body fy-small fy-secondary">
              {f.stage}. {f.fail} fail, {f.overdue} overdue, {f.not_assessed} not assessed, {f.not_applicable} do not apply, {f.planned} with a check still to arrive.
            </p>
            {f.gaps.length > 0 && (
              <ul className="fy-list" aria-label={`Gaps in ${f.title}`}>
                {f.gaps.map((g) => (
                  <li key={g.id} className="fy-list__item fy-list__item--wrap">
                    <span className="fy-list__main">
                      <span className="fy-list__title whitespace-normal">
                        {g.id}: {g.control}
                      </span>
                      <span className="fy-list__meta whitespace-normal">{g.detail}</span>
                    </span>
                    <span className="fy-chip fy-chip--warning">
                      <Icon of={TriangleAlert} />
                      {statusWords[g.status]}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </section>
        ))}
      <section className="fy-card" aria-labelledby={`${id}-annex`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-annex`}>
            ISO/IEC 27001:2022 Annex A
          </h2>
          <span className="fy-chip">
            {data.annex_a.covered.length} of {data.annex_a.total} numbers have a control
          </span>
        </div>
        <p className="fy-card__body fy-small fy-secondary">
          No control maps to these numbers yet. They are gaps for the statement of applicability, whose wording waits for the certification body.
        </p>
        <p className="fy-card__body fy-small" aria-label="Annex A numbers with no control">
          {data.annex_a.uncovered.join(", ")}
        </p>
      </section>
    </>
  );
}
