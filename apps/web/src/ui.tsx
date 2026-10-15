import type { LucideIcon } from "lucide-react";

// Icon draws a Lucide icon at the kit's sizes (MASTER.md: Lucide only, 16/20/24 px, 1.5 px stroke).
// Icons sit beside text, so they are hidden from assistive technology.
export function Icon({ of: Of, size = 16 }: { of: LucideIcon; size?: 16 | 20 | 24 | 32 }) {
  return <Of className={size === 16 ? "fy-i" : `fy-i fy-i--${size}`} aria-hidden="true" focusable="false" />;
}

// Brand is the Fyniq word mark.
export function Brand() {
  return (
    <span className="fy-brand">
      <span className="fy-brand__mark" aria-hidden="true">
        f
      </span>
      <span className="fy-brand__word">Fyniq</span>
    </span>
  );
}

// problemOf reads an API refusal (RFC 9457 problem details) the generated client returns.
export interface ApiProblem {
  code?: string;
  detail?: string;
  fix?: string;
  errors?: { field?: string; message?: string; fix?: string }[];
}

export function problemOf(error: unknown): ApiProblem {
  return error && typeof error === "object" ? (error as ApiProblem) : {};
}
