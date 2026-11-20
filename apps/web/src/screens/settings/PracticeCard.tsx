import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleCheck, FlaskConical } from "lucide-react";
import { useState } from "react";
import { api, newKey } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";

// Settings › Organisation › Practice (FR-UX-36; the owner's decision of 21 Nov 2026): an Owner opens one practice organisation, with empty books and
// nothing copied, to try a task without touching the real books, and moves between their organisations here.
export function PracticeCard({ mayOpen, onSwitched = () => window.location.assign("/") }: { mayOpen: boolean; onSwitched?: () => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const [made, setMade] = useState<string | null>(null);
  const orgs = useQuery({
    queryKey: ["my-organisations"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/session/organisations", { params: { query: { page_size: 50 } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
  const items = orgs.data ?? [];
  const holds = items.some((o) => o.practice);
  const open = async () => {
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () => api.POST("/v1/organisation/practice", { body: {}, headers: { "Idempotency-Key": newKey() } }));
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    setMade(out.data.name);
    await queries.invalidateQueries({ queryKey: ["my-organisations"] });
  };
  const go = async (id: string) => {
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/session/organisation", { body: { organisation_id: id }, headers: { "Idempotency-Key": newKey() } });
    setBusy(false);
    if (out.error) setProblem(problemOf(out.error));
    else onSwitched();
  };
  if (!mayOpen && items.length < 2) return null;
  return (
    <section className="fy-card" aria-labelledby="practice-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="practice-title">
          Practice organisation
        </h2>
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        {problem && <Refusal problem={problem} title="The practice organisation could not be opened" />}
        <p className="fy-small">
          A practice organisation is a separate, clearly labelled organisation with empty books, where you can try a task without touching the real ones. Only an Owner opens one, and one at a time.
        </p>
        {made && (
          <p className="fy-small" role="status">
            {made} is open: switch to it below.
          </p>
        )}
        {items.length > 1 && (
          <ul className="fy-list" aria-label="Your organisations">
            {items.map((o) => (
              <li key={o.id} className="fy-list__item">
                <span className="fy-list__main">
                  <span className="fy-list__title whitespace-normal">
                    {o.name}
                    {o.practice && <span className="fy-chip fy-chip--warning ms-2">
                        <Icon of={FlaskConical} /> Practice
                      </span>}
                  </span>
                </span>
                {o.current ? (
                  <span className="fy-chip fy-chip--good">
                    <Icon of={CircleCheck} /> You are here
                  </span>
                ) : (
                  <Button size="sm" variant="secondary" type="button" onClick={() => void go(o.id)} disabled={busy} aria-label={`Switch to ${o.name}`}>
                    Switch
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}
        {mayOpen && (
          <div>
            <Button variant="secondary" type="button" onClick={() => void open()} disabled={busy || holds} title={holds ? "You hold a practice organisation: one at a time" : undefined}>
              <Icon of={FlaskConical} />
              Open a practice organisation
            </Button>
            {holds && <span className="fy-small fy-secondary ms-2">You hold one already.</span>}
          </div>
        )}
      </div>
    </section>
  );
}
