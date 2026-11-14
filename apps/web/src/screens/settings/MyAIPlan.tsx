import { CircleOff } from "lucide-react";
import { useId } from "react";
import { helpText } from "../../i18n";
import { Icon } from "../../ui";
import { SettingsPage } from "./SettingsPage";

// MyAIPlan is Profile › Use your own AI plan (docs/design/screens MyAiPlan; AI-73): a person running their own assistant
// chats on their own provider plan. It is P2's, so this release says it is not offered, what the assistant uses instead,
// and what is never supported.
export function MyAIPlan() {
  const id = useId();
  const h = helpText("settings.ai-plan");
  return (
    <SettingsPage title={h.label} lead={h.tooltip} screen="my-ai-plan">
      <section className="fy-card" aria-labelledby={`${id}-plan`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-plan`}>
            Your plan
          </h2>
          <span className="fy-chip fy-chip--neutral">
            <Icon of={CircleOff} />
            Not offered yet
          </span>
        </div>
        <div className="fy-card__body fy-stack fy-stack--3">
          <p>
            Connecting your own provider plan is not offered yet. Until it is, the assistant uses only the organisation's own connections, chosen by the Owner and
            the administrators, within the data each one may receive.
          </p>
          <p className="fy-secondary">When it is offered, it will be turned on by an Owner, and used only for your own chats in Fyniq: never for scheduled agents, connected apps or other people.</p>
        </div>
      </section>
      <section className="fy-card" aria-labelledby={`${id}-never`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-never`}>
            Never supported
          </h2>
        </div>
        <ul className="fy-card__body fy-stack fy-stack--2 list-disc pl-6">
          <li>A consumer chat plan meant for the provider's own apps.</li>
          <li>Reusing a chat session or a browser cookie.</li>
        </ul>
      </section>
    </SettingsPage>
  );
}
