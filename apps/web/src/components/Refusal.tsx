import { CircleX } from "lucide-react";
import { useEffect, useId, useRef } from "react";
import { Icon, type ApiProblem } from "../ui";

// Refusal shows what the API refused and how to fix it (ENG-23, FR-INT-07): the problem's detail,
// its fix and code, and each field's message. It takes focus when it appears, so a screen reader
// announces it and the keyboard starts from it (MASTER.md "Forms": the error summary).
export function Refusal({ problem, title = "There is a problem" }: { problem: ApiProblem; title?: string }) {
  const ref = useRef<HTMLDivElement>(null);
  const id = useId();
  useEffect(() => ref.current?.focus(), [problem]);
  const fields = (problem.errors ?? []).filter((e) => e.message);
  return (
    <div className="fy-banner fy-banner--critical" role="alert" tabIndex={-1} ref={ref} aria-labelledby={id}>
      <Icon of={CircleX} size={20} />
      <div className="fy-banner__text">
        <span className="fy-strong" id={id}>
          {title}
        </span>
        <span className="fy-small">
          {problem.detail ?? "Fyniq could not do that."}
          {problem.fix && ` ${problem.fix}`}
          {problem.code && <span className="fy-muted"> ({problem.code})</span>}
        </span>
        {fields.length > 0 && (
          <ul className="fy-small">
            {fields.map((e, i) => (
              <li key={`${e.field}-${i}`}>
                {e.message}
                {e.fix && ` ${e.fix}`}
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
