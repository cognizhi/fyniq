import { Copy, KeyRound, Printer, TriangleAlert } from "lucide-react";
import { useId, useState } from "react";
import { Icon } from "../ui";

// RecoveryCodes shows a Super admin's ten printed recovery codes, once (open question 36, FR-ADM-09).
// They live only in this page's memory: never in storage, the address bar or a log (SEC-15). The
// person confirms they kept them before going on.
export function RecoveryCodes({ codes, onDone, doneLabel }: { codes: string[]; onDone: () => void; doneLabel: string }) {
  const [saved, setSaved] = useState(false);
  const [copied, setCopied] = useState(false);
  const id = useId();
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(codes.join("\n"));
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };
  return (
    <section className="fy-card fy-card--pad fy-stack" aria-labelledby={`${id}-title`}>
      <div className="fy-stack fy-stack--2">
        <h2 className="fy-h3" id={`${id}-title`}>
          <Icon of={KeyRound} size={20} /> Your recovery codes
        </h2>
        <p className="fy-secondary">
          If you lose your sign-in, one of these codes and the other Super admin's confirmation give you your access back. Each
          works once.
        </p>
      </div>
      <div className="fy-banner fy-banner--warning">
        <Icon of={TriangleAlert} size={20} />
        <div className="fy-banner__text">
          <span className="fy-strong">Shown once</span>
          <span className="fy-small fy-secondary">
            Print them or write them down and keep them offline. Fyniq keeps only a fingerprint of each code and cannot show them
            again.
          </span>
        </div>
      </div>
      <ol className="recovery-codes fy-mono" aria-label="Recovery codes">
        {codes.map((c) => (
          <li key={c}>{c}</li>
        ))}
      </ol>
      <div className="fy-row fy-row--wrap">
        <button className="fy-btn fy-btn--secondary" type="button" onClick={copy}>
          <Icon of={Copy} />
          {copied ? "Copied" : "Copy all"}
        </button>
        <button className="fy-btn fy-btn--secondary" type="button" onClick={() => window.print()}>
          <Icon of={Printer} />
          Print
        </button>
      </div>
      <label className="fy-row fy-row--top" style={{ gap: "var(--space-2)" }}>
        <input type="checkbox" checked={saved} onChange={(e) => setSaved(e.target.checked)} />
        <span>I have kept these codes somewhere safe, offline.</span>
      </label>
      <div className="fy-actionbar fy-actionbar--editor">
        <button className="fy-btn fy-btn--primary" type="button" disabled={!saved} onClick={onDone}>
          {doneLabel}
        </button>
      </div>
    </section>
  );
}
