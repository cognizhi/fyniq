import { CircleX } from "lucide-react";
import { useEffect, useId, useRef } from "react";
import { Icon, type ApiProblem } from "../ui";

// sentence ends a text with a full stop unless it already ends a sentence: the API's fixes are imperatives without one.
export function sentence(text: string): string {
  const t = text.trim();
  return t === "" || /[.!?:]$/.test(t) ? t : `${t}.`;
}

// fieldName is the field a message is about, as a person reads it: "body.lines[0].account_id" is "Lines 1 account id".
export function fieldName(field: string): string {
  const name = field
    .replace(/^(body|query|path)\./, "")
    .replace(/\[(\d+)\]/g, (_, n: string) => ` ${Number(n) + 1}`)
    .replace(/[._]+/g, " ")
    .trim();
  return name.charAt(0).toUpperCase() + name.slice(1);
}

// Refusal shows what the API refused and how to fix it (ENG-23, FR-INT-07): the problem's detail,
// its fix and code, and each field's name, message and fix, each a sentence. It takes focus when it
// appears, so a screen reader announces it and the keyboard starts from it (MASTER.md "Forms": the
// error summary).
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
          {sentence(problem.detail ?? "Fyniq could not do that")}
          {problem.fix && ` ${sentence(problem.fix)}`}
          {problem.code && <span className="fy-muted"> ({problem.code})</span>}
        </span>
        {fields.length > 0 && (
          <ul className="fy-small">
            {fields.map((e, i) => (
              <li key={`${e.field}-${i}`}>
                {e.field && <span className="fy-strong">{fieldName(e.field)}: </span>}
                {sentence(e.message ?? "")}
                {e.fix && ` ${sentence(e.fix)}`}
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
