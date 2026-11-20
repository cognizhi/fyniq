import { useQuery } from "@tanstack/react-query";
import { FlaskConical } from "lucide-react";
import { useState } from "react";
import { api } from "../api/client";
import { Refusal } from "../components/Refusal";
import { Button } from "../components/ui/button";
import { Icon, problemOf, type ApiProblem } from "../ui";

// A practice organisation says so at the top of every page (FR-UX-36): it is a separate organisation to try a task in, with nothing real in
// it, and one action takes the person back to the organisation that keeps their books.
export function PracticeBanner({ practice, onSwitched = () => window.location.assign("/") }: { practice?: boolean; onSwitched?: () => void }) {
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const orgs = useQuery({
    queryKey: ["my-organisations"],
    enabled: !!practice,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/session/organisations", { params: { query: { page_size: 50 } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
  if (!practice) return null;
  const real = (orgs.data ?? []).find((o) => !o.practice);
  const back = async () => {
    if (!real) return;
    setBusy(true);
    const out = await api.POST("/v1/session/organisation", { body: { organisation_id: real.id }, headers: { "Idempotency-Key": crypto.randomUUID() } });
    setBusy(false);
    if (out.error) setProblem(problemOf(out.error));
    else onSwitched();
  };
  return (
    <div className="fy-stack gap-0">
      <div className="fy-banner fy-banner--warning" role="note">
        <Icon of={FlaskConical} size={20} />
        <div className="fy-banner__text">
          <strong>Practice organisation</strong>
          <span className="fy-small">Nothing here is real, and nothing you do here changes your books. Open it to try a task.</span>
        </div>
        {real && (
          <div className="fy-banner__action">
            <Button variant="secondary" size="sm" type="button" onClick={() => void back()} disabled={busy}>
              Back to {real.name}
            </Button>
          </div>
        )}
      </div>
      {problem && <Refusal problem={problem} title="Could not switch organisation" />}
    </div>
  );
}
