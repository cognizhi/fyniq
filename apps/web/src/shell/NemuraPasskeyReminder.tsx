import { KeyRound } from "lucide-react";
import { helpText } from "../i18n";
import { Icon } from "../ui";

// Nemura says this person approves pay runs there, and a pay run's approval is signed with a passkey enrolled here (PRD-001 FR-PPL-09, PPL-19): until one is, Nemura's approval would be refused. A reminder, never a
// grant and never hidden in Basic mode (safety information, §7.8); it goes when a passkey is enrolled.
export function NemuraPasskeyReminder({ needed }: { needed?: boolean }) {
  if (!needed) return null;
  return (
    <div className="fy-banner fy-banner--warning" role="note" title={helpText("people.passkey-reminder").tooltip}>
      <Icon of={KeyRound} size={20} />
      <div className="fy-banner__text">
        <strong>Add a passkey to approve pay runs in Nemura</strong>
        <span className="fy-small">Nemura signs a pay run's approval with a passkey enrolled here. Add one, then approve there. This gives you no role in Fyniq.</span>
      </div>
      <div className="fy-banner__action">
        <a className="fy-btn fy-btn--secondary fy-btn--sm" href="/sign-in/enrol">
          Add a passkey
        </a>
      </div>
    </div>
  );
}
