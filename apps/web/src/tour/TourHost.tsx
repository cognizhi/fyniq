import { useQueryClient } from "@tanstack/react-query";
import { useNavigate, useRouterState } from "@tanstack/react-router";
import { Compass } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import type { Session } from "../api/client";
import { landingOf, personalOnly } from "../shell/navigation";
import { Icon } from "../ui";
import { onTourStart, saveTour, setTourRunning, startTour, useHelpState, useTourRunning } from "./state";
import { stepsFor } from "./steps";
import { Tour, targetOf } from "./Tour";

type Person = Pick<Session, "roles"> & { user_id?: string };

// TourHost runs the first-run tour in the shell (FR-UX-34): started from the offer, Help or the command palette, on Home
// for those who have it, where its first steps are. It saves where the person is as each step shows, and finished or
// skipped when it closes: the tour's only write (G4-104).
export function TourHost({ session }: { session: Person }) {
  const queries = useQueryClient();
  const navigate = useNavigate();
  const path = useRouterState({ select: (s) => s.location.pathname });
  const [running, setRunning] = useState<number | null>(null);
  const roles = (session.roles ?? []).join(",");
  const steps = useMemo(() => stepsFor(roles ? roles.split(",") : []), [roles]);
  useEffect(() => setTourRunning(running !== null), [running]);
  const hasHome = !personalOnly(session.roles) && !landingOf(session.roles);
  useEffect(
    () =>
      onTourStart((at) => {
        if (!hasHome || path === "/") {
          setRunning(at);
          return;
        }
        void navigate({ to: "/" }).then(() => {
          // Home's steps start once it has drawn; after 3 s whatever is shown is toured.
          const home = steps.find((s) => s.target === "home");
          const until = Date.now() + 3000;
          const wait = () => (!home || targetOf(home) || Date.now() > until ? setRunning(at) : requestAnimationFrame(wait));
          wait();
        });
      }),
    [hasHome, path, navigate, steps],
  );
  if (running === null) return null;
  return (
    <Tour
      steps={steps}
      start={running}
      onStep={(at) => void saveTour(queries, "started", at)}
      onClose={(how, at) => {
        setRunning(null);
        void saveTour(queries, how, at);
      }}
    />
  );
}

// TourOffer offers the tour where the person lands, until they answer it, and offers to resume one they left part-way
// (FR-UX-34): starting it, or Not now, which saves it skipped. Help and the command palette replay it whatever its state.
export function TourOffer({ session }: { session: Person }) {
  const queries = useQueryClient();
  const state = useHelpState(Boolean(session.user_id));
  const steps = stepsFor(session.roles);
  const touring = useTourRunning();
  const s = state.data;
  if (!s?.offer || touring) return null;
  const resume = s.tour === "started";
  const at = resume ? Math.min(s.step, steps.length - 1) : 0;
  return (
    <section className="fy-plain items-center" aria-labelledby="tour-offer">
      <Icon of={Compass} />
      <span className="fy-grow fy-stack gap-0">
        <span className="fy-strong" id="tour-offer">
          {resume ? "Pick up the tour where you left it" : "New here? Take the tour"}
        </span>
        <span className="fy-small fy-secondary">
          {resume ? `Step ${at + 1} of ${steps.length}, about 2 minutes in all.` : `${steps.length} steps, about 2 minutes, on the pages you will use. It changes nothing.`}
        </span>
      </span>
      <button type="button" className="fy-btn fy-btn--primary fy-btn--sm" onClick={() => startTour(at)}>
        {resume ? "Resume the tour" : "Start the tour"}
      </button>
      <button type="button" className="fy-btn fy-btn--ghost fy-btn--sm" onClick={() => void saveTour(queries, "skipped", s.step)}>
        Not now
      </button>
    </section>
  );
}
